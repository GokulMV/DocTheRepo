package repodocs

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// DocSection is a written section.
type DocSection struct {
	Key      string   `json:"key"`
	Title    string   `json:"title"`
	Markdown string   `json:"markdown"`
	Score    float64  `json:"score"`
	Why      []string `json:"why,omitempty"`
}

// Doc is one written document.
type Doc struct {
	ID         string       `json:"id"`
	RepoID     string       `json:"repo_id"`
	Type       string       `json:"type"`
	Key        string       `json:"key"` // module key for module guides, else the type
	Title      string       `json:"title"`
	Group      string       `json:"group"`
	Order      int          `json:"order"`
	AtAGlance  string       `json:"at_a_glance"`
	Sections   []DocSection `json:"sections"`
	Gaps       []string     `json:"gaps,omitempty"`
	Confidence float64      `json:"confidence"`
	Why        []string     `json:"why,omitempty"`
	Calibrated bool         `json:"calibrated,omitempty"` // support judged by a calibrated model (Jev)
	InputsHash string       `json:"inputs_hash"`
	// FileHashes are the content hashes of the files it was written from (to measure later change).
	FileHashes map[string]string `json:"file_hashes,omitempty"`
	// Changed is the share of its source lines that changed since it was written (0 when current).
	Changed   float64 `json:"changed,omitempty"`
	SourceSHA string  `json:"source_sha"`
	Model     string  `json:"model,omitempty"`
	TokensIn  int64   `json:"tokens_in"`
	TokensOut int64   `json:"tokens_out"`
	CostUSD   float64 `json:"cost_usd"`
	// DraftProblems are what the checks found in the first draft, when it had to be repaired.
	DraftProblems []string  `json:"draft_problems,omitempty"`
	Status        string    `json:"status"` // ok | failed
	Error         string    `json:"error,omitempty"`
	UpdatedAt     time.Time `json:"updated_at"`
}

// Section finds a section by key.
func (d *Doc) Section(key string) *DocSection {
	for i := range d.Sections {
		if d.Sections[i].Key == key {
			return &d.Sections[i]
		}
	}
	return nil
}

// Label is the confidence as High / Medium / Low.
func Label(c float64) string {
	switch {
	case c >= 0.8:
		return "high"
	case c >= 0.6:
		return "medium"
	}
	return "low"
}

// citeRE matches [path:line] and [path:line-line].
var citeRE = regexp.MustCompile(`\[([A-Za-z0-9_./@+\-]+\.[A-Za-z0-9]+|[A-Za-z0-9_./@+\-]*(?:Dockerfile|Makefile|Jenkinsfile|Procfile)):(\d+)(?:-(\d+))?\]`)

// Citation is a [path:line] reference in a section.
type Citation struct {
	Path      string
	Line, End int
}

// Citations lists the references in markdown.
func Citations(md string) []Citation {
	var out []Citation
	for _, m := range citeRE.FindAllStringSubmatch(md, -1) {
		l, _ := strconv.Atoi(m[2])
		e := l
		if m[3] != "" {
			e, _ = strconv.Atoi(m[3])
		}
		out = append(out, Citation{Path: m[1], Line: l, End: e})
	}
	return out
}

var (
	fenceRE    = regexp.MustCompile("(?s)```.*?```")
	tableRowRE = regexp.MustCompile(`(?m)^\s*\|.*\|\s*$`)
	tickRE     = regexp.MustCompile("`([^`\n]{2,80})`")
	camelRE    = regexp.MustCompile(`[a-z][A-Z]|[A-Z]{2,}[a-z]`)
	mermaidRE  = regexp.MustCompile("(?s)```mermaid\\s*\\n(.*?)```")
)

// Words counts prose words: code blocks and tables are not counted.
func Words(md string) int {
	md = fenceRE.ReplaceAllString(md, " ")
	md = tableRowRE.ReplaceAllString(md, " ")
	md = citeRE.ReplaceAllString(md, " ")
	n := 0
	for _, w := range strings.Fields(md) {
		if strings.IndexFunc(w, func(r rune) bool {
			return r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r > 127
		}) >= 0 {
			n++
		}
	}
	return n
}

var mermaidKinds = []string{"flowchart", "graph", "sequenceDiagram", "erDiagram", "classDiagram", "stateDiagram", "stateDiagram-v2", "gantt", "journey", "mindmap", "timeline"}

// common words that appear in backticks without being names from the code.
var commonTicks = map[string]bool{"null": true, "nil": true, "true": true, "false": true, "none": true, "undefined": true, "json": true, "yaml": true,
	"http": true, "https": true, "get": true, "post": true, "put": true, "patch": true, "delete": true, "200": true, "404": true, "500": true}

// identifiers returns backticked names that look like code (not commands or prose), and those not known.
func identifiers(md string, known map[string]bool, material string) (total int, unknown []string) {
	md = fenceRE.ReplaceAllString(md, " ")
	seen := map[string]bool{}
	for _, m := range tickRE.FindAllStringSubmatch(md, -1) {
		t := strings.TrimSpace(m[1])
		if strings.ContainsAny(t, " =<>\"'{}$") || seen[t] {
			continue // a command, an expression or a value, not a name
		}
		seen[t] = true
		name := strings.TrimSuffix(strings.TrimSuffix(t, "()"), "(…)")
		if i := strings.IndexByte(name, '('); i > 0 {
			name = name[:i]
		}
		looksCode := strings.ContainsAny(name, "._/#:") || camelRE.MatchString(name) || (strings.ToUpper(name) == name && strings.Contains(name, "_"))
		if !looksCode || commonTicks[strings.ToLower(name)] {
			continue
		}
		total++
		if ok := matchKnown(name, known) || inMaterial(name, material); !ok {
			unknown = append(unknown, t)
		}
	}
	return total, unknown
}

// inMaterial reports a name written in the material itself; a trailing wildcard (DTH_*) matches its prefix.
func inMaterial(name, material string) bool {
	if material == "" {
		return false
	}
	n := strings.TrimRight(name, "*")
	if len(n) < 3 {
		return false
	}
	return strings.Contains(material, n)
}

func matchKnown(name string, known map[string]bool) bool {
	l := strings.ToLower(strings.Trim(name, "./"))
	if known[l] {
		return true
	}
	// foo.Bar → Bar, pkg/x.go:12 → pkg/x.go, Type.Method → either part.
	if i := strings.LastIndex(l, ":"); i > 0 {
		if known[l[:i]] {
			return true
		}
	}
	for _, sep := range []string{".", "#", "::", "/"} {
		if i := strings.LastIndex(l, sep); i >= 0 && i < len(l)-1 {
			if known[l[i+len(sep):]] || known[l[:i]] {
				return true
			}
		}
	}
	return false
}

// written is the model's reply.
type written struct {
	AtAGlance string `json:"at_a_glance"`
	Sections  []struct {
		Key      string `json:"key"`
		Markdown string `json:"markdown"`
	} `json:"sections"`
	Gaps []string `json:"gaps"`
}

// checkResult is what the checks found in a reply.
type checkResult struct {
	hard []string // must be fixed (a repair is asked for)
	// per section: citations, valid citations, identifiers, unknown identifiers
	cites, valid, idents map[string]int
	unknown              map[string][]string
	bad                  map[string][]string // citations that do not resolve
}

// check validates a reply against its spec, the facts, and the material the model was given (a name that
// appears in the material is grounded even when it is not a declaration: config keys, environment
// variables, table names, error codes).
func check(spec Spec, w *written, f *Facts, known map[string]bool, material string) checkResult {
	r := checkResult{cites: map[string]int{}, valid: map[string]int{}, idents: map[string]int{}, unknown: map[string][]string{}, bad: map[string][]string{}}
	if strings.TrimSpace(w.AtAGlance) == "" {
		r.hard = append(r.hard, "at_a_glance is empty: write 3-5 plain sentences")
	} else if n := len(strings.Fields(w.AtAGlance)); n > 140 {
		r.hard = append(r.hard, fmt.Sprintf("at_a_glance has %d words; keep it under 120", n))
	}
	got := map[string]string{}
	for _, s := range w.Sections {
		got[s.Key] = s.Markdown
	}
	files := f.FileByPath()
	all := map[string]bool{}
	for _, p := range f.AllPaths {
		all[p] = true
	}
	for _, sec := range spec.Sections {
		md, ok := got[sec.Key]
		if !ok || strings.TrimSpace(md) == "" {
			if sec.Required {
				r.hard = append(r.hard, fmt.Sprintf("section %q (%s) is missing or empty", sec.Key, sec.Title))
			}
			continue
		}
		if sec.Words > 0 {
			if n := Words(md); n > sec.Words*16/10 {
				r.hard = append(r.hard, fmt.Sprintf("section %q has %d words; the limit is about %d: cut it down", sec.Key, n, sec.Words))
			}
		}
		for _, c := range Citations(md) {
			r.cites[sec.Key]++
			fl, indexed := files[c.Path]
			switch {
			case indexed && c.Line >= 1 && c.Line <= max(fl.Lines, 1)+5:
				r.valid[sec.Key]++
			case c.Line == 0 && (indexed || all[c.Path]):
				r.valid[sec.Key]++ // [path:0]: the file as a whole (shown without line numbers)
			case !indexed && all[c.Path]:
				r.valid[sec.Key]++ // a file that is not indexed (tests, config): the path is enough
			default:
				r.bad[sec.Key] = append(r.bad[sec.Key], fmt.Sprintf("%s:%d", c.Path, c.Line))
			}
		}
		if sec.Cite && sec.Words >= 150 && r.cites[sec.Key] == 0 {
			r.hard = append(r.hard, fmt.Sprintf("section %q makes claims about the code without any [path:line] citation", sec.Key))
		}
		if n := len(r.bad[sec.Key]); n > 2 {
			r.hard = append(r.hard, fmt.Sprintf("section %q cites places that do not exist: %s (cite only paths and lines shown in the material)", sec.Key, strings.Join(first(r.bad[sec.Key], 5), ", ")))
		}
		total, unknown := identifiers(md, known, material)
		r.idents[sec.Key], r.unknown[sec.Key] = total, unknown
		if len(unknown) > 3 {
			r.hard = append(r.hard, fmt.Sprintf("section %q names things that are not in the code: %s (use only names from the material)", sec.Key, strings.Join(first(unknown, 6), ", ")))
		}
		for _, mm := range mermaidRE.FindAllStringSubmatch(md, -1) {
			head := strings.Fields(strings.TrimSpace(mm[1]))
			if len(head) == 0 || !containsStr(mermaidKinds, head[0]) {
				r.hard = append(r.hard, fmt.Sprintf("section %q has a mermaid block that does not start with a diagram type (flowchart, sequenceDiagram, erDiagram…)", sec.Key))
			}
		}
	}
	return r
}

func first(xs []string, n int) []string {
	if len(xs) > n {
		return xs[:n]
	}
	return xs
}

func containsStr(xs []string, v string) bool {
	for _, x := range xs {
		if x == v {
			return true
		}
	}
	return false
}

// DocPath is where a document's sections live in the search index.
func DocPath(d Doc) string { return "@docs/" + d.Type + "/" + d.Key }

// Link is the document's address in the Hub.
func Link(d Doc) string { return "/docs/r/" + d.RepoID + "/" + d.Type + "/" + d.Key }

// refRE matches anything written like a citation, [something:12] or [something:12-20], with the space
// before it.
var refRE = regexp.MustCompile(`\s?\[([^\[\]\n:]+):(\d+)(?:-\d+)?\]`)

// DropDeadRefs removes citation-like references that point at no file in the repository (a module title,
// a directory, an invented path) so readers do not see them; real citations and Markdown links are kept.
func DropDeadRefs(md string, f *Facts) string {
	known := map[string]bool{}
	for _, p := range f.AllPaths {
		known[p] = true
	}
	for _, fl := range f.Files {
		known[fl.Path] = true
	}
	var b strings.Builder
	last := 0
	for _, m := range refRE.FindAllStringSubmatchIndex(md, -1) {
		if m[1] < len(md) && md[m[1]] == '(' {
			continue // [text:1](url) is a link
		}
		if known[strings.TrimSpace(md[m[2]:m[3]])] {
			continue
		}
		b.WriteString(md[last:m[0]])
		last = m[1]
	}
	b.WriteString(md[last:])
	return b.String()
}
