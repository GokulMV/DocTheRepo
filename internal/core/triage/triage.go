// Package triage decides, without an LLM in the common case, whether a pushed change is cosmetic
// (ABORT: nothing further is spent) or structural (PROCEED) — plan § 8.1.
package triage

import (
	"fmt"
	"path"
	"regexp"
	"sort"
	"strings"

	"github.com/GokulMV/DocTheRepo/internal/core/chunker"
	"github.com/GokulMV/DocTheRepo/internal/core/grammars"
	"github.com/GokulMV/DocTheRepo/internal/ports"
)

// Decision is the triage outcome.
type Decision string

const (
	Abort   Decision = "ABORT"
	Proceed Decision = "PROCEED"
)

// RenameSimilarity is the git-reported similarity at or above which an unchanged rename is re-keyed
// instead of re-embedded (plan § 8.2).
const RenameSimilarity = 90

// DefaultIgnore are paths never worth processing: dependency lockfiles, vendored and generated output.
var DefaultIgnore = []string{
	"**/go.sum", "**/package-lock.json", "**/yarn.lock", "**/pnpm-lock.yaml", "**/Cargo.lock",
	"**/poetry.lock", "**/Pipfile.lock", "**/composer.lock", "**/Gemfile.lock",
	"**/vendor/**", "**/node_modules/**", "**/dist/**", "**/build/**", "**/target/**", "**/.venv/**",
	"**/*.min.js", "**/*.min.css", "**/*.map", "**/*.pb.go", "**/*_pb2.py", "**/*.generated.*",
	"**/__snapshots__/**", "**/*.snap", ".github/**", "**/.gitignore", "**/.editorconfig",
	"**/LICENSE", "**/LICENSE.*", "**/CHANGELOG*",
}

// DefaultIndexOnly are paths that are indexed for search but never get generated documentation (tests).
var DefaultIndexOnly = []string{
	"**/*_test.go", "**/test/**", "**/tests/**", "**/__tests__/**", "**/*.test.ts", "**/*.test.tsx",
	"**/*.test.js", "**/*.spec.ts", "**/*.spec.js", "**/test_*.py", "**/*_test.py", "**/src/test/**",
}

// Options configures a Triage.
type Options struct {
	// DocsPath is the agent-owned generated-docs path; changes there never trigger work (bot-loop guard).
	DocsPath  string
	Ignore    []string
	IndexOnly []string
}

// FileChange is one changed file with its old and new content (nil when absent).
type FileChange struct {
	Path         string
	PreviousPath string
	Status       ports.FileStatus
	Similarity   int
	Old, New     []byte
}

// Verdict is the per-file result.
type Verdict struct {
	Path     string   `json:"path"`
	OldPath  string   `json:"old_path,omitempty"`
	Decision Decision `json:"decision"`
	Reason   string   `json:"reason"`
	// NeedsLLM is set when no grammar exists; the pipeline makes one spend-guarded LLM triage call and
	// resolves ambiguity to PROCEED.
	NeedsLLM bool `json:"needs_llm,omitempty"`
	// Rename marks a structurally unchanged rename: re-key chunks, do not re-embed.
	Rename bool `json:"rename,omitempty"`
	// IndexOnly files are chunked and indexed but get no generated documentation.
	IndexOnly bool   `json:"index_only,omitempty"`
	Language  string `json:"language,omitempty"`
	// Old and New are the parsed analyses, reused by chunking and scoped context.
	Old *chunker.FileAnalysis `json:"-"`
	New *chunker.FileAnalysis `json:"-"`
}

// Triage classifies changes.
type Triage struct {
	reg       *grammars.Registry
	docsPath  string
	ignore    []*regexp.Regexp
	indexOnly []*regexp.Regexp
}

// New builds a Triage. Nil Ignore/IndexOnly use the defaults; pass empty slices to disable them.
func New(reg *grammars.Registry, o Options) (*Triage, error) {
	if o.Ignore == nil {
		o.Ignore = DefaultIgnore
	}
	if o.IndexOnly == nil {
		o.IndexOnly = DefaultIndexOnly
	}
	t := &Triage{reg: reg, docsPath: strings.TrimSuffix(o.DocsPath, "/")}
	var err error
	if t.ignore, err = compileGlobs(o.Ignore); err != nil {
		return nil, err
	}
	if t.indexOnly, err = compileGlobs(o.IndexOnly); err != nil {
		return nil, err
	}
	return t, nil
}

// File classifies one change.
func (t *Triage) File(fc FileChange) Verdict {
	v := Verdict{Path: fc.Path, OldPath: fc.PreviousPath}
	switch {
	case t.inDocsPath(fc.Path) && (fc.PreviousPath == "" || t.inDocsPath(fc.PreviousPath)):
		return t.abort(v, "generated-docs path (written by the Hub; never re-triggers work)")
	case matchAny(t.ignore, fc.Path) && (fc.PreviousPath == "" || matchAny(t.ignore, fc.PreviousPath)):
		return t.abort(v, "ignored path")
	case chunker.IsMarkdown(fc.Path) && fc.Status != ports.FileRemoved:
		return t.abort(v, "documentation outside the generated-docs path is not processed (use import)")
	case (fc.New != nil && chunker.IsBinary(fc.New)) || (fc.Old != nil && chunker.IsBinary(fc.Old)):
		return t.abort(v, "binary file")
	}
	v.IndexOnly = matchAny(t.indexOnly, fc.Path)

	if fc.Status == ports.FileRemoved || fc.New == nil {
		v.Decision, v.Reason = Proceed, "file deleted: its chunks are removed"
		return v
	}
	if dependencyManifest(fc.Path) && fc.Old != nil {
		v.Decision, v.Reason = depsDecision(fc.Path, fc.Old, fc.New)
		return v
	}

	lang := t.reg.ForPath(fc.Path)
	if lang == nil {
		if fc.Old != nil && string(fc.Old) == string(fc.New) {
			return t.renameOrAbort(v, fc, "content unchanged")
		}
		v.Decision, v.NeedsLLM, v.Reason = Proceed, true, "no grammar for "+extOf(fc.Path)+"; needs LLM triage"
		return v
	}
	v.Language = lang.Name
	newA, err := chunker.Analyze(lang, fc.New)
	if err != nil {
		v.Decision, v.Reason = Proceed, "could not parse new content; treating as structural"
		return v
	}
	v.New = newA
	if fc.Old == nil {
		v.Decision, v.Reason = Proceed, fmt.Sprintf("new file with %d definition(s)", len(newA.Definitions))
		return v
	}
	oldLang := t.reg.ForPath(fc.PreviousPath)
	if fc.PreviousPath == "" {
		oldLang = lang
	}
	if oldLang == nil || oldLang.Name != lang.Name {
		v.Decision, v.Reason = Proceed, "language changed"
		return v
	}
	oldA, err := chunker.Analyze(lang, fc.Old)
	if err != nil {
		v.Decision, v.Reason = Proceed, "could not parse old content; treating as structural"
		return v
	}
	v.Old = oldA
	if equalTokens(oldA.Tokens, newA.Tokens) {
		return t.renameOrAbort(v, fc, "only comments, string literals, or whitespace changed")
	}
	v.Decision, v.Reason = Proceed, structuralReason(oldA, newA)
	return v
}

func (t *Triage) renameOrAbort(v Verdict, fc FileChange, why string) Verdict {
	if fc.PreviousPath != "" && fc.PreviousPath != fc.Path {
		if fc.Similarity >= RenameSimilarity {
			v.Decision, v.Rename, v.Reason = Abort, true, "renamed without structural change: chunks re-keyed, no re-embedding"
			return v
		}
		v.Decision, v.Reason = Proceed, "renamed (low similarity): chunks moved"
		return v
	}
	return t.abort(v, why)
}

func (t *Triage) abort(v Verdict, why string) Verdict {
	v.Decision, v.Reason = Abort, why
	return v
}

func (t *Triage) inDocsPath(p string) bool {
	return t.docsPath != "" && (p == t.docsPath || strings.HasPrefix(p, t.docsPath+"/"))
}

// Summary is the push-level outcome.
type Summary struct {
	Decision Decision  `json:"decision"`
	Reason   string    `json:"reason"`
	Files    []Verdict `json:"files"`
}

// Push classifies every file and summarises: PROCEED if any file proceeds or needs LLM triage.
func (t *Triage) Push(changes []FileChange) Summary {
	s := Summary{Decision: Abort}
	var proceed, renames, llm []string
	for _, fc := range changes {
		v := t.File(fc)
		s.Files = append(s.Files, v)
		switch {
		case v.NeedsLLM:
			llm = append(llm, v.Path)
		case v.Rename:
			renames = append(renames, v.Path)
		case v.Decision == Proceed:
			proceed = append(proceed, v.Path)
		}
	}
	switch {
	case len(proceed) > 0 || len(llm) > 0:
		s.Decision = Proceed
		parts := []string{}
		if len(proceed) > 0 {
			parts = append(parts, fmt.Sprintf("structural changes in %s", joinCapped(proceed, 5)))
		}
		if len(llm) > 0 {
			parts = append(parts, fmt.Sprintf("LLM triage needed for %s", joinCapped(llm, 5)))
		}
		s.Reason = strings.Join(parts, "; ")
	case len(renames) > 0:
		s.Reason = fmt.Sprintf("only renames (%s): re-keyed without re-embedding", joinCapped(renames, 5))
	case len(changes) == 0:
		s.Reason = "no changed files"
	default:
		s.Reason = "cosmetic changes only"
	}
	return s
}

// ResolveLLM applies an LLM triage answer to a NeedsLLM verdict. Anything but a clear "cosmetic" answer
// resolves to PROCEED: a false PROCEED costs one doc-gen call, a false ABORT silently misses a change.
func ResolveLLM(v Verdict, cosmetic bool, confident bool, why string) Verdict {
	v.NeedsLLM = false
	if cosmetic && confident {
		v.Decision, v.Reason = Abort, "LLM triage: "+why
		return v
	}
	v.Decision, v.Reason = Proceed, "LLM triage: "+why
	return v
}

func equalTokens(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// structuralReason names what changed: added/removed definitions, signature changes, body changes.
func structuralReason(o, n *chunker.FileAnalysis) string {
	oldSig, newSig := map[string]string{}, map[string]string{}
	oldBody, newBody := map[string]string{}, map[string]string{}
	typeLike := map[string]bool{}
	for _, d := range o.Definitions {
		oldSig[d.Symbol], oldBody[d.Symbol] = d.Signature, d.Content
	}
	for _, d := range n.Definitions {
		newSig[d.Symbol], newBody[d.Symbol] = d.Signature, d.Content
		switch d.Kind {
		case "type", "interface", "enum", "record", "trait":
			typeLike[d.Symbol] = true // their "body" is their shape
		}
	}
	var added, removed, sigs, bodies, types []string
	for s := range newSig {
		if _, ok := oldSig[s]; !ok {
			added = append(added, s)
		} else if typeLike[s] && (norm(oldSig[s]) != norm(newSig[s]) || oldBody[s] != newBody[s]) {
			types = append(types, s)
		} else if norm(oldSig[s]) != norm(newSig[s]) {
			sigs = append(sigs, s)
		} else if oldBody[s] != newBody[s] {
			bodies = append(bodies, s)
		}
	}
	for s := range oldSig {
		if _, ok := newSig[s]; !ok {
			removed = append(removed, s)
		}
	}
	var parts []string
	for _, p := range []struct {
		label string
		xs    []string
	}{{"added", added}, {"removed", removed}, {"type changed", types}, {"signature changed", sigs}, {"body changed", bodies}} {
		if len(p.xs) > 0 {
			sort.Strings(p.xs)
			parts = append(parts, p.label+": "+joinCapped(p.xs, 5))
		}
	}
	if len(parts) == 0 {
		return "top-level code changed"
	}
	return strings.Join(parts, "; ")
}

func norm(s string) string { return strings.Join(strings.Fields(s), " ") }

func joinCapped(xs []string, n int) string {
	if len(xs) <= n {
		return strings.Join(xs, ", ")
	}
	return fmt.Sprintf("%s and %d more", strings.Join(xs[:n], ", "), len(xs)-n)
}

func extOf(p string) string {
	if e := path.Ext(p); e != "" {
		return e
	}
	return path.Base(p)
}

// compileGlobs converts gitignore-style globs (*, ?, **) to anchored regexps.
func compileGlobs(globs []string) ([]*regexp.Regexp, error) {
	out := make([]*regexp.Regexp, 0, len(globs))
	for _, g := range globs {
		re, err := GlobRegexp(g)
		if err != nil {
			return nil, err
		}
		out = append(out, re)
	}
	return out, nil
}

// GlobRegexp compiles one glob. "**/" matches zero or more directories; "*" never crosses "/".
func GlobRegexp(g string) (*regexp.Regexp, error) {
	var b strings.Builder
	b.WriteString("^")
	for i := 0; i < len(g); i++ {
		c := g[i]
		switch {
		case c == '*' && i+1 < len(g) && g[i+1] == '*':
			if i+2 < len(g) && g[i+2] == '/' {
				b.WriteString("(?:.*/)?")
				i += 2
			} else {
				b.WriteString(".*")
				i++
			}
		case c == '*':
			b.WriteString("[^/]*")
		case c == '?':
			b.WriteString("[^/]")
		default:
			b.WriteString(regexp.QuoteMeta(string(c)))
		}
	}
	b.WriteString("$")
	re, err := regexp.Compile(b.String())
	if err != nil {
		return nil, fmt.Errorf("invalid glob %q: %w", g, err)
	}
	return re, nil
}

func matchAny(res []*regexp.Regexp, p string) bool {
	for _, re := range res {
		if re.MatchString(p) {
			return true
		}
	}
	return false
}
