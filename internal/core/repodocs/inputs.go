package repodocs

import (
	"context"
	"fmt"
	"path"
	"sort"
	"strings"

	"github.com/GokulMV/DocTheRepo/internal/ports"
)

// Reader reads a file at the commit being documented (tests, migrations and CI files are not indexed).
type Reader func(ctx context.Context, p string) ([]byte, error)

// Card is a short note about one file, written once per file structure and used as input (never shown).
type Card struct {
	Path    string       `json:"path"`
	Shape   string       `json:"shape"`
	Purpose string       `json:"purpose"`
	Symbols []CardSymbol `json:"symbols,omitempty"`
	Notes   string       `json:"notes,omitempty"`
}

// CardSymbol is one declaration on a card.
type CardSymbol struct {
	Name string `json:"name"`
	What string `json:"what"`
}

// env is everything one run knows, shared by the input builders.
type env struct {
	facts    *Facts
	files    map[string]*File
	mods     []Module
	modOf    map[string]string
	cards    map[string]Card
	guides   map[string]*Doc // module key → guide, once written
	read     Reader
	symbols  []Symbol // every symbol, most called first
	modEdges map[[2]string]int
}

func newEnv(f *Facts, mods []Module, cards map[string]Card, read Reader) *env {
	e := &env{facts: f, files: f.FileByPath(), mods: mods, modOf: ModuleOf(mods), cards: cards, guides: map[string]*Doc{}, read: read, modEdges: map[[2]string]int{}}
	for _, fl := range f.Files {
		e.symbols = append(e.symbols, fl.Symbols...)
	}
	sort.SliceStable(e.symbols, func(i, j int) bool { return e.symbols[i].In > e.symbols[j].In })
	for _, c := range f.Calls {
		a, b := e.modOf[c.From], e.modOf[c.To]
		if a != "" && b != "" && a != b {
			e.modEdges[[2]string{a, b}] += c.N
		}
	}
	return e
}

func (e *env) module(key string) *Module {
	for i := range e.mods {
		if e.mods[i].Key == key {
			return &e.mods[i]
		}
	}
	return nil
}

// numbered renders code with its line numbers so the model can cite path:line.
func numbered(body string, first int, maxLines int) string {
	lines := strings.Split(strings.TrimRight(body, "\n"), "\n")
	var b strings.Builder
	for i, l := range lines {
		if i == maxLines {
			fmt.Fprintf(&b, "      … %d more lines\n", len(lines)-maxLines)
			break
		}
		fmt.Fprintf(&b, "%5d| %s\n", first+i, l)
	}
	return b.String()
}

func clip(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "\n… (trimmed)"
}

// block is one titled part of a prompt, kept or dropped as a unit when trimming to the budget.
type block struct {
	title    string
	body     string
	priority int // lower is kept first
}

func (b block) tokens() int { return (len(b.title) + len(b.body)) / 4 }

// render keeps blocks in priority order until the budget (in tokens) is used, then lists what was left out.
func render(blocks []block, budget int) string {
	sort.SliceStable(blocks, func(i, j int) bool { return blocks[i].priority < blocks[j].priority })
	var out strings.Builder
	used := 0
	var dropped []string
	for _, b := range blocks {
		if strings.TrimSpace(b.body) == "" {
			continue
		}
		t := b.tokens()
		if used+t > budget {
			if room := (budget - used) * 4; room > 2000 {
				fmt.Fprintf(&out, "## %s\n%s\n\n", b.title, clip(b.body, room-200))
				used = budget
				continue
			}
			dropped = append(dropped, b.title)
			continue
		}
		fmt.Fprintf(&out, "## %s\n%s\n\n", b.title, b.body)
		used += t
	}
	if len(dropped) > 0 {
		fmt.Fprintf(&out, "(Left out for length: %s.)\n", strings.Join(dropped, ", "))
	}
	return out.String()
}

func (e *env) factsOf(kinds ...string) []Fact {
	var out []Fact
	for _, f := range e.facts.Facts {
		for _, k := range kinds {
			if f.Kind == k {
				out = append(out, f)
				break
			}
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func factLines(fs []Fact, limit int) string {
	var b strings.Builder
	seen := map[string]bool{}
	n := 0
	for _, f := range fs {
		k := f.Kind + f.Name + f.Path
		if seen[k] {
			continue
		}
		seen[k] = true
		if n == limit {
			fmt.Fprintf(&b, "… and more\n")
			break
		}
		n++
		where := ""
		if f.Path != "" {
			where = fmt.Sprintf(" [%s:%d]", f.Path, max(f.Line, 1))
		}
		from := ""
		if f.From != "" {
			from = " in " + f.From
		}
		fmt.Fprintf(&b, "- %s: %s%s%s\n", f.Kind, f.Name, from, where)
	}
	return b.String()
}

// moduleGraph lists dependencies between modules and draws them as a mermaid flowchart.
func (e *env) moduleGraph() (text, diagram string) {
	type edge struct {
		a, b string
		n    int
	}
	var edges []edge
	for k, n := range e.modEdges {
		edges = append(edges, edge{k[0], k[1], n})
	}
	sort.Slice(edges, func(i, j int) bool {
		if edges[i].n != edges[j].n {
			return edges[i].n > edges[j].n
		}
		return edges[i].a+edges[i].b < edges[j].a+edges[j].b
	})
	title := map[string]string{}
	for _, m := range e.mods {
		title[m.Key] = m.Title
	}
	var t strings.Builder
	for i, ed := range edges {
		if i == 80 {
			break
		}
		fmt.Fprintf(&t, "- %s → %s (%d calls)\n", title[ed.a], title[ed.b], ed.n)
	}
	var d strings.Builder
	d.WriteString("flowchart LR\n")
	used := map[string]bool{}
	for i, ed := range edges {
		if i == 40 {
			break
		}
		used[ed.a], used[ed.b] = true, true
		fmt.Fprintf(&d, "  %s --> %s\n", mermaidID(ed.a), mermaidID(ed.b))
	}
	for _, m := range e.mods {
		if used[m.Key] || len(edges) == 0 {
			fmt.Fprintf(&d, "  %s[\"%s\"]\n", mermaidID(m.Key), strings.ReplaceAll(m.Title, `"`, "'"))
		}
	}
	return t.String(), d.String()
}

func mermaidID(k string) string { return "m_" + strings.ReplaceAll(k, "-", "_") }

func (e *env) cardText(p string) string {
	c, ok := e.cards[p]
	if !ok {
		return ""
	}
	var b strings.Builder
	b.WriteString(c.Purpose)
	for _, s := range c.Symbols {
		fmt.Fprintf(&b, "; %s: %s", s.Name, s.What)
	}
	if c.Notes != "" {
		b.WriteString(". " + c.Notes)
	}
	return b.String()
}

func (e *env) bodies(syms []Symbol, n, maxLines int) string {
	var b strings.Builder
	for i, s := range syms {
		if i == n {
			break
		}
		if s.Body == "" {
			continue
		}
		fmt.Fprintf(&b, "### %s (%s:%d, called from %d places)\n```\n%s```\n", s.Name, s.Path, s.Line, s.In, numbered(s.Body, max(s.Line, 1), maxLines))
	}
	return b.String()
}

func (e *env) special(limit int) string {
	var b strings.Builder
	for _, p := range sortedKeys(e.facts.Special) {
		fmt.Fprintf(&b, "### %s\n```\n%s```\n", p, numbered(clip(e.facts.Special[p], limit), 1, 300))
	}
	return b.String()
}

func (e *env) readMatching(ctx context.Context, match func(string) bool, maxFiles, perFile int) string {
	if e.read == nil {
		return ""
	}
	var b strings.Builder
	n := 0
	for _, p := range e.facts.AllPaths {
		if !match(p) {
			continue
		}
		if n == maxFiles {
			fmt.Fprintf(&b, "… more files not shown\n")
			break
		}
		raw, err := e.read(ctx, p)
		if err != nil {
			continue
		}
		n++
		fmt.Fprintf(&b, "### %s\n```\n%s```\n", p, numbered(clip(string(raw), perFile), 1, 400))
	}
	return b.String()
}

func (e *env) moduleList() string {
	var b strings.Builder
	for _, m := range e.mods {
		purpose := ""
		if g := e.guides[m.Key]; g != nil {
			purpose = g.AtAGlance
		} else {
			var ps []string
			for _, f := range m.Files {
				if c, ok := e.cards[f]; ok && c.Purpose != "" {
					ps = append(ps, c.Purpose)
				}
				if len(ps) == 3 {
					break
				}
			}
			purpose = strings.Join(ps, " ")
		}
		fmt.Fprintf(&b, "- %s (%d files, %d lines, %d test files): %s\n", m.Title, len(m.Files), m.Lines, len(m.Tests), clip(purpose, 400))
	}
	return b.String()
}

func (e *env) guidesText(sectionKeys []string, perGuide int) string {
	var b strings.Builder
	for _, m := range e.mods {
		g := e.guides[m.Key]
		if g == nil {
			continue
		}
		fmt.Fprintf(&b, "### %s\n%s\n", m.Title, g.AtAGlance)
		for _, k := range sectionKeys {
			if sec := g.Section(k); sec != nil {
				fmt.Fprintf(&b, "**%s.** %s\n", sec.Title, clip(sec.Markdown, perGuide))
			}
		}
		b.WriteString("\n")
	}
	return b.String()
}

func (e *env) entryPoints() string {
	var b strings.Builder
	for _, s := range e.symbols {
		base := strings.ToLower(path.Base(s.Path))
		name := strings.ToLower(s.Name)
		if name == "main" || name == "main.main" || strings.HasSuffix(name, ".main") || (strings.HasPrefix(base, "main.") && s.Line > 0) ||
			strings.HasPrefix(s.Path, "cmd/") && name == "main" {
			fmt.Fprintf(&b, "- %s [%s:%d]\n", s.Name, s.Path, s.Line)
		}
	}
	for _, fl := range e.facts.Files {
		base := strings.ToLower(strings.TrimSuffix(path.Base(fl.Path), path.Ext(fl.Path)))
		if base == "index" || base == "app" || base == "server" || base == "main" || base == "cli" || base == "manage" || base == "wsgi" || base == "asgi" {
			fmt.Fprintf(&b, "- file %s\n", fl.Path)
		}
	}
	return clip(b.String(), 4000)
}

func (e *env) testsText(ctx context.Context) string {
	byDir := map[string]int{}
	for _, p := range e.facts.AllPaths {
		if IsTest(p) {
			byDir[path.Dir(p)]++
		}
	}
	var b strings.Builder
	b.WriteString("Test files by directory:\n")
	for i, d := range sortedKeys(byDir) {
		if i == 120 {
			b.WriteString("…\n")
			break
		}
		fmt.Fprintf(&b, "- %s: %d\n", d, byDir[d])
	}
	// A sample: helpers and fixtures first, then one test per top directory.
	picked := map[string]bool{}
	seenTop := map[string]bool{}
	var sample []string
	for _, p := range e.facts.AllPaths {
		l := strings.ToLower(p)
		if IsTest(p) && (strings.Contains(l, "helper") || strings.Contains(l, "fixture") || strings.Contains(l, "conftest") || strings.Contains(l, "setup")) && len(sample) < 4 {
			sample, picked[p] = append(sample, p), true
		}
	}
	for _, p := range e.facts.AllPaths {
		top := strings.SplitN(p, "/", 2)[0]
		if IsTest(p) && !picked[p] && !seenTop[top] && len(sample) < 8 {
			sample, picked[p], seenTop[top] = append(sample, p), true, true
		}
	}
	b.WriteString("\n")
	b.WriteString(e.readMatching(ctx, func(p string) bool { return picked[p] }, 8, 5000))
	return b.String()
}

func (e *env) errorSymbols() string {
	var errs []Symbol
	for _, s := range e.symbols {
		n := strings.ToLower(s.Name)
		if strings.Contains(n, "err") || strings.Contains(n, "exception") || strings.Contains(n, "fault") {
			errs = append(errs, s)
		}
	}
	var b strings.Builder
	for i, s := range errs {
		if i == 60 {
			break
		}
		fmt.Fprintf(&b, "- %s %s [%s:%d] %s\n", s.Kind, s.Name, s.Path, s.Line, s.Signature)
	}
	b.WriteString("\n")
	b.WriteString(e.bodies(errs, 12, 30))
	return b.String()
}

// inputs assembles the prompt material for one document.
func (e *env) inputs(ctx context.Context, spec Spec, mod *Module, budget int) string {
	var bl []block
	add := func(title, body string, prio int) { bl = append(bl, block{title, body, prio}) }
	for _, need := range spec.Needs {
		switch need {
		case "modules":
			add("Modules", e.moduleList(), 1)
		case "module_guides":
			add("Module guides (summaries)", e.guidesText([]string{"purpose", "responsibilities", "how", "deps"}, 900), 2)
		case "module_graph":
			t, _ := e.moduleGraph()
			if mod != nil {
				var in, out []string
				for k, n := range e.modEdges {
					if k[0] == mod.Key {
						out = append(out, fmt.Sprintf("- uses %s (%d calls)", e.titleOf(k[1]), n))
					}
					if k[1] == mod.Key {
						in = append(in, fmt.Sprintf("- used by %s (%d calls)", e.titleOf(k[0]), n))
					}
				}
				sort.Strings(in)
				sort.Strings(out)
				t = strings.Join(append(out, in...), "\n")
			}
			add("Dependencies between modules", t, 3)
		case "diagram":
			_, d := e.moduleGraph()
			add("Component diagram (already drawn; explain it, do not redraw)", "```mermaid\n"+d+"```", 4)
		case "facts_summary":
			add("What the code declares", factLines(e.factsOf("endpoint"), 40)+factLines(e.factsOf("datastore", "topic_pub", "topic_sub", "service", "cloud_resource"), 60)+
				factLines(e.factsOf("env_var"), 40)+factLines(e.factsOf("dependency"), 30), 3)
		case "facts_detail":
			add("Endpoints, topics and stores", factLines(e.factsOf("endpoint", "topic_pub", "topic_sub", "datastore"), 300), 2)
		case "special":
			add("Repository files (README, build, deploy, CI)", e.special(6000), 5)
		case "entry_points":
			add("Entry points", e.entryPoints(), 2)
		case "central_bodies":
			add("Most-called code", e.bodies(e.symbols, 14, 60), 6)
		case "endpoints":
			eps := e.factsOf("endpoint")
			add("Endpoints", factLines(eps, 400), 1)
			add("Handlers", e.handlerBodies(eps, 25), 4)
		case "topics":
			ts := e.factsOf("topic_pub", "topic_sub")
			add("Topics and queues", factLines(ts, 300), 1)
			add("Producer and consumer code", e.handlerBodies(ts, 20), 4)
		case "datastores":
			add("Datastores", factLines(e.factsOf("datastore"), 100), 1)
		case "migrations":
			add("Schema and migrations", e.readMatching(ctx, IsMigration, 30, 6000), 2)
		case "env":
			add("Environment variables", factLines(e.factsOf("env_var"), 400), 1)
		case "tests":
			add("Tests", e.testsText(ctx), 1)
			add("Modules and their tests", e.moduleTests(), 2)
		case "ci":
			add("CI and deployment files", e.readMatching(ctx, func(p string) bool { return IsCI(p) || IsDeploy(p) }, 20, 6000), 1)
		case "error_symbols":
			add("Error types and where they are raised", e.errorSymbols(), 2)
		case "deps":
			add("Dependencies and external systems", factLines(e.factsOf("dependency", "service", "cloud_resource", "datastore"), 200), 1)
		case "owners":
			add("Owners", factLines(e.factsOf("owner"), 100), 1)
		case "module":
			e.moduleInputs(mod, add)
		case "commits":
			add("Recent commits (newest first)", commitLines(e.facts.Commits, 120, false), 1)
		case "decisions":
			var ds []ports.Commit
			for _, c := range e.facts.Commits {
				if IsDecisionCommit(c) {
					ds = append(ds, c)
				}
			}
			add("Commits that record decisions", commitLines(ds, 60, true), 1)
			add("Decision record files", e.readMatching(ctx, IsADR, 20, 5000), 2)
		}
	}
	return render(bl, budget)
}

func (e *env) titleOf(key string) string {
	if m := e.module(key); m != nil {
		return m.Title
	}
	return key
}

func (e *env) moduleTests() string {
	var b strings.Builder
	for _, m := range e.mods {
		fmt.Fprintf(&b, "- %s: %d test files\n", m.Title, len(m.Tests))
	}
	return b.String()
}

func (e *env) handlerBodies(fs []Fact, n int) string {
	var picked []Symbol
	seen := map[string]bool{}
	for _, f := range fs {
		if f.From == "" || seen[f.From] {
			continue
		}
		fl := e.files[f.Path]
		if fl == nil {
			continue
		}
		for _, s := range fl.Symbols {
			if s.Name == f.From || strings.HasSuffix(f.From, s.Name) {
				picked = append(picked, s)
				seen[f.From] = true
				break
			}
		}
		if len(picked) == n {
			break
		}
	}
	return e.bodies(picked, n, 50)
}

func (e *env) moduleInputs(m *Module, add func(string, string, int)) {
	if m == nil {
		return
	}
	in := map[string]bool{}
	for _, f := range m.Files {
		in[f] = true
	}
	var files strings.Builder
	var syms, exported []Symbol
	for _, p := range m.Files {
		fl := e.files[p]
		if fl == nil {
			continue
		}
		fmt.Fprintf(&files, "- %s (%d lines): %s\n", p, fl.Lines, e.cardText(p))
		for _, s := range fl.Symbols {
			syms = append(syms, s)
			if s.Exported() {
				exported = append(exported, s)
			}
		}
	}
	add("Module", fmt.Sprintf("%s: %d files, %d lines. Directory: %s", m.Title, len(m.Files), m.Lines, m.Dir), 0)
	add("Files (with notes)", files.String(), 1)
	sort.SliceStable(exported, func(i, j int) bool { return exported[i].In > exported[j].In })
	var sig strings.Builder
	for i, s := range exported {
		if i == 120 {
			sig.WriteString("…\n")
			break
		}
		fmt.Fprintf(&sig, "- %s [%s:%d] %s\n", s.Name, s.Path, s.Line, s.Signature)
	}
	add("Declarations other code can use", sig.String(), 2)
	var facts []Fact
	for _, f := range e.facts.Facts {
		if in[f.Path] {
			facts = append(facts, f)
		}
	}
	add("What this module declares (endpoints, env vars, stores, topics)", factLines(facts, 150), 3)
	sort.SliceStable(syms, func(i, j int) bool {
		if syms[i].In != syms[j].In {
			return syms[i].In > syms[j].In
		}
		return syms[i].Lines > syms[j].Lines
	})
	add("Central code", e.bodies(syms, 8, 80), 4)
	if len(m.Tests) > 0 {
		add("Tests", strings.Join(m.Tests, "\n"), 5)
	}
}

func commitLines(cs []ports.Commit, n int, full bool) string {
	var b strings.Builder
	for i, c := range cs {
		if i == n {
			fmt.Fprintf(&b, "… %d older commits\n", len(cs)-n)
			break
		}
		msg := strings.TrimSpace(c.Message)
		if !full {
			msg = strings.SplitN(msg, "\n", 2)[0]
		}
		sha := c.SHA
		if len(sha) > 7 {
			sha = sha[:7]
		}
		fmt.Fprintf(&b, "- %s %s (%s): %s\n", c.At.Format("2006-01-02"), sha, c.Author, clip(msg, 800))
	}
	return b.String()
}
