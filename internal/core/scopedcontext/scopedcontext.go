// Package scopedcontext builds the smallest correct context for an LLM call about changed code: the
// changed symbol bodies, one hop of callers/callees/referenced types as signatures (same file, then across
// files via imports), and optional knowledge/provider snippets, fitted to a token budget (plan § 8.6).
package scopedcontext

import (
	"fmt"
	"path"
	"regexp"
	"sort"
	"strings"

	"github.com/GokulMV/DocTheRepo/internal/core/chunker"
	"github.com/GokulMV/DocTheRepo/internal/ports"
)

// Target is one changed/added chunk that needs documentation.
type Target struct {
	Chunk ports.Chunk
}

// Snippet is extra context from a knowledge source (Confluence, known issues) or a ContextProvider.
type Snippet struct {
	Source string `json:"source"`
	Title  string `json:"title"`
	Text   string `json:"text"`
}

// Input is everything Build needs. Analyses are keyed by repo-relative path.
type Input struct {
	Targets []Target
	// Files holds analyses of the files that contain targets.
	Files map[string]*chunker.FileAnalysis
	// CrossFiles holds analyses of files reached through the targets' imports (see ImportCandidates).
	CrossFiles   map[string]*chunker.FileAnalysis
	Knowledge    []Snippet
	Providers    []Snippet
	BudgetTokens int
}

// Signature is one related declaration included as context.
type Signature struct {
	Path      string `json:"path"`
	Symbol    string `json:"symbol"`
	Relation  string `json:"relation"` // caller | callee | type
	Of        string `json:"of"`       // the target symbol it relates to
	Signature string `json:"signature"`
}

// Context is the assembled, budgeted context.
type Context struct {
	Targets   []ports.Chunk     `json:"targets"`
	Imports   map[string]string `json:"imports,omitempty"`
	SameFile  []Signature       `json:"same_file,omitempty"`
	CrossFile []Signature       `json:"cross_file,omitempty"`
	Knowledge []Snippet         `json:"knowledge,omitempty"`
	Providers []Snippet         `json:"providers,omitempty"`
	Tokens    int               `json:"tokens"`
	// Dropped records what was cut to fit the budget, for the context.assembled event and metrics.
	Dropped []string `json:"dropped,omitempty"`
}

// Build assembles and budgets the context.
func Build(in Input) Context {
	c := Context{Imports: map[string]string{}}
	seen := map[string]bool{}
	add := func(dst *[]Signature, s Signature) {
		k := s.Path + "|" + s.Symbol
		if seen[k] {
			return
		}
		seen[k] = true
		*dst = append(*dst, s)
	}
	targetKeys := map[string]bool{}
	for _, t := range in.Targets {
		targetKeys[t.Chunk.Path+"|"+t.Chunk.Symbol] = true
		seen[t.Chunk.Path+"|"+t.Chunk.Symbol] = true
	}

	for _, t := range in.Targets {
		c.Targets = append(c.Targets, t.Chunk)
		fa := in.Files[t.Chunk.Path]
		if fa == nil {
			continue
		}
		if fa.ImportBlk != "" {
			c.Imports[t.Chunk.Path] = fa.ImportBlk
		}
		def := findDef(fa, t.Chunk.Symbol)
		if def == nil {
			continue
		}
		byName := indexByName(fa)
		// Callees in the same file.
		for _, call := range def.Calls {
			for _, d := range byName[call.Name] {
				if d.Symbol != def.Symbol && !targetKeys[t.Chunk.Path+"|"+d.Symbol] {
					add(&c.SameFile, Signature{Path: t.Chunk.Path, Symbol: d.Symbol, Relation: "callee", Of: def.Symbol, Signature: d.Signature})
				}
			}
		}
		// Callers in the same file.
		for i := range fa.Definitions {
			d := &fa.Definitions[i]
			if d.Symbol == def.Symbol {
				continue
			}
			for _, call := range d.Calls {
				if call.Name == def.Name || call.Name == lastName(def.Symbol) {
					add(&c.SameFile, Signature{Path: t.Chunk.Path, Symbol: d.Symbol, Relation: "caller", Of: def.Symbol, Signature: d.Signature})
					break
				}
			}
		}
		// Types referenced in the signature.
		words := wordSet(def.Signature)
		for i := range fa.Definitions {
			d := &fa.Definitions[i]
			if isTypeKind(d.Kind) && d.Symbol != def.Symbol && words[lastName(d.Symbol)] {
				add(&c.SameFile, Signature{Path: t.Chunk.Path, Symbol: d.Symbol, Relation: "type", Of: def.Symbol, Signature: typeSignature(d)})
			}
		}
		// One hop across files, through this file's imports.
		qualifiers := importQualifiers(fa)
		for _, call := range def.Calls {
			q, name := splitQualifier(call.Callee)
			if q == "" {
				if qualifiers.names[call.Name] { // `from x import f` / `import { f } from './x'`
					name = call.Name
				} else {
					continue
				}
			} else if !qualifiers.aliases[q] && !qualifiers.names[q] {
				continue
			}
			for p, cfa := range in.CrossFiles {
				for _, d := range crossMatches(cfa, q, name) {
					add(&c.CrossFile, Signature{Path: p, Symbol: d.Symbol, Relation: "callee", Of: def.Symbol, Signature: d.Signature})
				}
			}
		}
		for p, cfa := range in.CrossFiles {
			for i := range cfa.Definitions {
				d := &cfa.Definitions[i]
				if isTypeKind(d.Kind) && words[lastName(d.Symbol)] && qualifiers.names[lastName(d.Symbol)] {
					add(&c.CrossFile, Signature{Path: p, Symbol: d.Symbol, Relation: "type", Of: def.Symbol, Signature: typeSignature(d)})
				}
			}
		}
	}
	sortSigs(c.SameFile)
	sortSigs(c.CrossFile)
	c.Knowledge = append(c.Knowledge, in.Knowledge...)
	c.Providers = append(c.Providers, in.Providers...)
	c.fit(in.BudgetTokens)
	return c
}

// fit drops context in plan order until the estimate fits: cross-file signatures, then same-file
// signatures and imports, then knowledge/provider snippets. Target bodies are never dropped.
func (c *Context) fit(budget int) {
	c.Tokens = chunker.EstimateTokens(c.Render())
	if budget <= 0 {
		return
	}
	for c.Tokens > budget {
		switch {
		case len(c.CrossFile) > 0:
			last := c.CrossFile[len(c.CrossFile)-1]
			c.CrossFile = c.CrossFile[:len(c.CrossFile)-1]
			c.Dropped = append(c.Dropped, "cross_file:"+last.Path+"#"+last.Symbol)
		case len(c.SameFile) > 0:
			last := c.SameFile[len(c.SameFile)-1]
			c.SameFile = c.SameFile[:len(c.SameFile)-1]
			c.Dropped = append(c.Dropped, "same_file:"+last.Path+"#"+last.Symbol)
		case len(c.Imports) > 0:
			for _, k := range sortedKeys(c.Imports) {
				delete(c.Imports, k)
				c.Dropped = append(c.Dropped, "imports:"+k)
				break
			}
		case len(c.Providers) > 0:
			last := c.Providers[len(c.Providers)-1]
			c.Providers = c.Providers[:len(c.Providers)-1]
			c.Dropped = append(c.Dropped, "provider:"+last.Source)
		case len(c.Knowledge) > 0:
			last := c.Knowledge[len(c.Knowledge)-1]
			c.Knowledge = c.Knowledge[:len(c.Knowledge)-1]
			c.Dropped = append(c.Dropped, "knowledge:"+last.Title)
		default:
			return // only target bodies remain; they are never cut
		}
		c.Tokens = chunker.EstimateTokens(c.Render())
	}
}

// Render formats the context as prompt text. Retrieved/related content is fenced as data.
func (c Context) Render() string {
	var b strings.Builder
	b.WriteString("## Changed code\n")
	for _, t := range c.Targets {
		fmt.Fprintf(&b, "\n### %s — %s (chunk %s)\n```%s\n%s\n```\n", t.Path, t.Symbol, t.ID, t.Language, t.Content)
	}
	if len(c.Imports) > 0 {
		b.WriteString("\n## Imports\n")
		for _, p := range sortedKeys(c.Imports) {
			fmt.Fprintf(&b, "\n%s:\n```\n%s\n```\n", p, c.Imports[p])
		}
	}
	writeSigs := func(title string, sigs []Signature) {
		if len(sigs) == 0 {
			return
		}
		b.WriteString("\n## " + title + "\n")
		for _, s := range sigs {
			fmt.Fprintf(&b, "- %s of `%s` — %s `%s`: `%s`\n", s.Relation, s.Of, s.Path, s.Symbol, oneLine(s.Signature))
		}
	}
	writeSigs("Related declarations (same file)", c.SameFile)
	writeSigs("Related declarations (other files)", c.CrossFile)
	writeSnips := func(title string, sn []Snippet) {
		if len(sn) == 0 {
			return
		}
		b.WriteString("\n## " + title + "\n")
		for _, s := range sn {
			fmt.Fprintf(&b, "\n### %s — %s\n<data>\n%s\n</data>\n", s.Source, s.Title, s.Text)
		}
	}
	writeSnips("Team knowledge", c.Knowledge)
	writeSnips("Additional context", c.Providers)
	return b.String()
}

type qualifierSet struct{ aliases, names map[string]bool }

// importQualifiers returns identifiers through which a file refers to imported code: package aliases
// (Go `pay.Charge`, Python `os.path`), and imported names (Python/TS/Java/Rust).
func importQualifiers(fa *chunker.FileAnalysis) qualifierSet {
	q := qualifierSet{aliases: map[string]bool{}, names: map[string]bool{}}
	for _, im := range fa.Imports {
		if im.Alias != "" {
			q.aliases[im.Alias] = true
		} else if im.Path != "" {
			q.aliases[path.Base(strings.ReplaceAll(strings.ReplaceAll(im.Path, "::", "/"), ".", "/"))] = true
		}
		for _, n := range im.Names {
			q.names[n] = true
		}
	}
	return q
}

func crossMatches(fa *chunker.FileAnalysis, qualifier, name string) []*chunker.Definition {
	var out []*chunker.Definition
	for i := range fa.Definitions {
		d := &fa.Definitions[i]
		if d.Name == name || d.Symbol == name || (qualifier != "" && d.Symbol == qualifier+"."+name) || lastName(d.Symbol) == name {
			out = append(out, d)
		}
	}
	return out
}

func splitQualifier(callee string) (string, string) {
	for _, sep := range []string{"::", "."} {
		if i := strings.LastIndex(callee, sep); i > 0 {
			q := callee[:i]
			if j := strings.LastIndexAny(q, ".:"); j >= 0 { // a.b.c → qualifier "b"
				q = q[j+1:]
			}
			return q, callee[i+len(sep):]
		}
	}
	return "", callee
}

func findDef(fa *chunker.FileAnalysis, symbol string) *chunker.Definition {
	for i := range fa.Definitions {
		if fa.Definitions[i].Symbol == symbol {
			return &fa.Definitions[i]
		}
	}
	if fa.Module != nil && symbol == chunker.ModuleSymbol {
		return fa.Module
	}
	return nil
}

func indexByName(fa *chunker.FileAnalysis) map[string][]*chunker.Definition {
	out := map[string][]*chunker.Definition{}
	for i := range fa.Definitions {
		d := &fa.Definitions[i]
		out[lastName(d.Symbol)] = append(out[lastName(d.Symbol)], d)
	}
	return out
}

func lastName(symbol string) string {
	if i := strings.IndexByte(symbol, '('); i > 0 {
		symbol = symbol[:i]
	}
	if i := strings.LastIndexByte(symbol, '.'); i >= 0 {
		symbol = symbol[i+1:]
	}
	return symbol
}

func isTypeKind(k string) bool {
	switch k {
	case "type", "interface", "enum", "record", "trait", "class":
		return true
	}
	return false
}

// typeSignature includes small type bodies (struct fields matter for understanding a signature).
func typeSignature(d *chunker.Definition) string {
	if len(d.Content) <= 600 && d.Kind != "class" {
		return strings.TrimSpace(d.Content)
	}
	return d.Signature
}

var wordRE = regexp.MustCompile(`[A-Za-z_][A-Za-z0-9_]*`)

func wordSet(s string) map[string]bool {
	out := map[string]bool{}
	for _, w := range wordRE.FindAllString(s, -1) {
		out[w] = true
	}
	return out
}

func oneLine(s string) string { return strings.Join(strings.Fields(s), " ") }

func sortSigs(s []Signature) {
	rank := map[string]int{"callee": 0, "type": 1, "caller": 2}
	sort.SliceStable(s, func(i, j int) bool {
		if rank[s[i].Relation] != rank[s[j].Relation] {
			return rank[s[i].Relation] < rank[s[j].Relation]
		}
		return s[i].Path+s[i].Symbol < s[j].Path+s[j].Symbol
	})
}

func sortedKeys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
