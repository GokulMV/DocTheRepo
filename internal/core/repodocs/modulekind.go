package repodocs

import (
	"context"
	"fmt"
	"path"
	"regexp"
	"sort"
	"strings"
)

// briefDataWords is the "Data it owns" target for a module the code shows owning no persistent data:
// a sentence or two naming the state it holds is the whole section.
const briefDataWords = 60

// IsDocFile reports prose and the images that go with it (Markdown, reStructuredText, AsciiDoc, text).
func IsDocFile(p string) bool {
	switch strings.ToLower(path.Ext(p)) {
	case ".md", ".mdx", ".markdown", ".rst", ".txt", ".adoc", ".png", ".jpg", ".jpeg", ".gif", ".svg", ".webp":
		return true
	}
	return false
}

// IsGeneratedDoc reports documentation produced by a tool, not people: the Hub's own per-file docs
// (docs/generated/ by default) and any other generated/ folder of prose.
func IsGeneratedDoc(p string) bool {
	return IsDocFile(p) && strings.Contains("/"+strings.ToLower(p), "/generated/")
}

// IsDocsModule reports a module that is documentation rather than code: at least 90% of its files are
// prose or images. It gets the documentation-set guide (DocSetSpec) instead of the code guide.
func IsDocsModule(m *Module) bool {
	if m == nil || len(m.Files) == 0 {
		return false
	}
	n := 0
	for _, p := range m.Files {
		if IsDocFile(p) {
			n++
		}
	}
	return n*10 >= len(m.Files)*9
}

// moduleSpec is the guide a module gets: the documentation-set guide for a docs module, otherwise the
// code guide with a "Data it owns" target that follows what the module's code shows it storing.
func (e *env) moduleSpec(base Spec, m *Module) Spec {
	if IsDocsModule(m) {
		return DocSetSpec
	}
	if e.moduleData(m).empty() {
		secs := append([]Section(nil), base.Sections...)
		for i := range secs {
			if secs[i].Key == "data" {
				secs[i].Words = briefDataWords
				secs[i].Guide = "The code shows no tables, migrations, stores or persisted types in this module. Say so in one or two sentences, " +
					"then name the state it does hold (in-memory values, configuration structs, caches, or none), citing where it lives."
			}
		}
		base.Sections = secs
	}
	return base
}

// specForDoc is the spec a written document was written to: a module guide whose sections are the
// documentation-set guide's is matched back to DocSetSpec by its section keys.
func specForDoc(d Doc) (Spec, bool) {
	if d.Type == "module" {
		code := map[string]bool{}
		for _, s := range Catalog {
			if s.Type == "module" {
				for _, sec := range s.Sections {
					code[sec.Key] = true
				}
			}
		}
		for _, s := range d.Sections {
			if !code[s.Key] {
				return DocSetSpec, true
			}
		}
	}
	return SpecFor(d.Type)
}

// ownerModule is the module a path belongs to by directory, for files that are not indexed (migrations,
// images, generated docs): the deepest module whose directory holds it.
func (e *env) ownerModule(p string) string {
	dir := path.Dir(p)
	best, depth := "", -1
	for _, m := range e.mods {
		if !isAncestor(m.Dir, dir) || (m.Only && m.Dir != dir) {
			continue
		}
		d := 0
		if m.Dir != "." {
			d = strings.Count(m.Dir, "/") + 1
		}
		if d > depth {
			best, depth = m.Key, d
		}
	}
	return best
}

// sqlTableRE finds table names after SQL keywords. Keywords must be upper case (as SQL in code usually
// is), so prose such as "from the cache" does not count.
var sqlTableRE = regexp.MustCompile(`\b(FROM|JOIN|INTO|UPDATE|TABLE(?: IF NOT EXISTS)?)\s+([A-Za-z_][A-Za-z0-9_]*(?:\.[A-Za-z_][A-Za-z0-9_]*)?)\b(\()?`)

// notTables are words that follow those keywords without being a table.
var notTables = map[string]bool{"select": true, "set": true, "lateral": true, "only": true, "unnest": true, "values": true, "the": true,
	"a": true, "an": true, "where": true, "if": true, "exists": true, "not": true, "excluded": true, "jsonb_array_elements": true}

// persistRE spots a type stored by a database layer: struct tags, ORM table names, entity annotations.
var persistRE = regexp.MustCompile("`[^`]*\\b(db|gorm|bson|dynamodbav|pg|bun):\"|__tablename__|@Entity\\b|@Table\\(")

// tableRef is a table the module's code names in SQL, with the first place it does.
type tableRef struct {
	name string
	n    int
	path string
	line int
}

// moduleData is what the code shows a module storing: the deterministic signal behind the "Data it
// owns" target and the list the guide is written from.
type moduleData struct {
	tables     []tableRef
	migrations []string
	stores     []Fact
	persisted  []Symbol
}

func (d moduleData) empty() bool {
	return len(d.tables) == 0 && len(d.migrations) == 0 && len(d.stores) == 0 && len(d.persisted) == 0
}

func (e *env) moduleData(m *Module) moduleData {
	var d moduleData
	if m == nil {
		return d
	}
	in := map[string]bool{}
	for _, p := range m.Files {
		in[p] = true
	}
	seen := map[string]bool{}
	for _, f := range e.facts.Facts {
		if f.Kind == "datastore" && in[f.Path] && !seen[f.Name] {
			seen[f.Name] = true
			d.stores = append(d.stores, f)
		}
	}
	tables := map[string]*tableRef{}
	for _, p := range m.Files {
		fl := e.files[p]
		if fl == nil {
			continue
		}
		for _, s := range fl.Symbols {
			for _, mm := range sqlTableRE.FindAllStringSubmatchIndex(s.Body, -1) {
				kw, name := s.Body[mm[2]:mm[3]], s.Body[mm[4]:mm[5]]
				if (mm[6] >= 0 && (kw == "FROM" || kw == "JOIN")) || notTables[strings.ToLower(name)] {
					continue // a table function (FROM unnest(…)) or a keyword, not a table
				}
				t := tables[name]
				if t == nil {
					t = &tableRef{name: name, path: p}
					if s.Line > 0 {
						t.line = s.Line + strings.Count(s.Body[:mm[0]], "\n")
					}
					tables[name] = t
				}
				t.n++
			}
			if persistRE.MatchString(s.Body) && len(d.persisted) < 20 {
				d.persisted = append(d.persisted, s)
			}
		}
	}
	for _, t := range tables {
		d.tables = append(d.tables, *t)
	}
	sort.Slice(d.tables, func(i, j int) bool {
		if d.tables[i].n != d.tables[j].n {
			return d.tables[i].n > d.tables[j].n
		}
		return d.tables[i].name < d.tables[j].name
	})
	for _, p := range e.facts.AllPaths {
		if IsMigration(p) && !IsTest(p) && e.ownerModule(p) == m.Key {
			d.migrations = append(d.migrations, p)
		}
	}
	sort.Strings(d.migrations)
	return d
}

// text lists the module's data compactly (bounded however much SQL the module has).
func (d moduleData) text() string {
	if d.empty() {
		return "None found: no SQL tables, migrations, datastores or persisted types in this module's code."
	}
	var b strings.Builder
	if len(d.stores) > 0 {
		b.WriteString("Datastores it uses:\n")
		b.WriteString(factLines(d.stores, 20))
	}
	if len(d.tables) > 0 {
		b.WriteString("Tables its SQL names (most used first):\n")
		for i, t := range d.tables {
			if i == 30 {
				fmt.Fprintf(&b, "… and %d more\n", len(d.tables)-30)
				break
			}
			fmt.Fprintf(&b, "- %s (%s) [%s:%d]\n", t.name, plural(t.n, "statement"), t.path, t.line)
		}
	}
	if len(d.migrations) > 0 {
		fmt.Fprintf(&b, "Migration and schema files under it (%d):\n", len(d.migrations))
		for i, p := range d.migrations {
			if i == 20 {
				fmt.Fprintf(&b, "… and %d more\n", len(d.migrations)-20)
				break
			}
			fmt.Fprintf(&b, "- %s\n", p)
		}
	}
	if len(d.persisted) > 0 {
		b.WriteString("Types with storage tags or ORM mappings:\n")
		for _, s := range d.persisted {
			fmt.Fprintf(&b, "- %s [%s:%d]\n", s.Name, s.Path, s.Line)
		}
	}
	return b.String()
}

// docSetInputs is the material for a documentation-set guide: each hand-written document with its
// headings and opening, what is generated (one line, not its contents), and the modules of the code (to
// spot what no document covers).
func (e *env) docSetInputs(ctx context.Context, m *Module, add func(string, string, int)) {
	if m == nil {
		return
	}
	var docs, generated []string
	for _, p := range m.Files {
		if IsGeneratedDoc(p) {
			generated = append(generated, p)
		} else {
			docs = append(docs, p)
		}
	}
	images := 0
	inFiles := map[string]bool{}
	for _, p := range m.Files {
		inFiles[p] = true
	}
	for _, p := range e.facts.AllPaths {
		if inFiles[p] || !IsDocFile(p) || e.ownerModule(p) != m.Key {
			continue
		}
		switch strings.ToLower(path.Ext(p)) {
		case ".png", ".jpg", ".jpeg", ".gif", ".svg", ".webp":
			images++
		default:
			if IsGeneratedDoc(p) {
				generated = append(generated, p)
			}
		}
	}
	head := fmt.Sprintf("%s: %d hand-written documents. Directory: %s", m.Title, len(docs), m.Dir)
	if images > 0 {
		head += fmt.Sprintf("; %d images", images)
	}
	add("Documentation set", head, 0)
	if len(generated) > 0 {
		add("Generated documentation (not shown)", fmt.Sprintf("%d files under %s are generated, not written by people (the DocTheRepo Hub writes "+
			"its per-file docs from the code to docs/generated/ by default); they are refreshed by the tool, not edited by hand.", len(generated), generatedRoot(generated)), 1)
	}
	var b strings.Builder
	for i, p := range docs {
		if i == 80 {
			fmt.Fprintf(&b, "… and %d more documents\n", len(docs)-80)
			break
		}
		fl := e.files[p]
		lines := 0
		if fl != nil {
			lines = fl.Lines
		}
		fmt.Fprintf(&b, "### %s (%d lines)\n", p, lines)
		if c := e.cardText(p); c != "" {
			fmt.Fprintf(&b, "Note: %s\n", clip(c, 300))
		}
		b.WriteString(e.docOutline(ctx, p, fl))
	}
	add("Documents (headings with line numbers, and how each opens)", b.String(), 1)
	add("Modules of the code (to see what the documents cover)", e.moduleList(), 3)
}

// generatedRoot is the shortest directory ending in generated/ among the paths.
func generatedRoot(ps []string) string {
	root := ""
	for _, p := range ps {
		l := strings.ToLower(p)
		i := strings.Index("/"+l, "/generated/")
		if i < 0 {
			continue
		}
		r := p[:i+len("generated/")]
		if root == "" || len(r) < len(root) {
			root = r
		}
	}
	return root
}

// docOutline lists a document's headings with their line numbers and the start of its first paragraph,
// read from the file when a reader is set, else from the indexed heading sections.
func (e *env) docOutline(ctx context.Context, p string, fl *File) string {
	var text string
	if e.read != nil {
		if raw, err := e.read(ctx, p); err == nil {
			text = string(raw)
		}
	}
	var b strings.Builder
	if text == "" {
		if fl == nil {
			return ""
		}
		for i, s := range fl.Symbols {
			if i == 15 {
				break
			}
			h := strings.TrimSpace(strings.SplitN(s.Body, "\n", 2)[0])
			if s.Line > 0 {
				fmt.Fprintf(&b, "- %s [%s:%d]\n", h, p, s.Line)
			} else {
				fmt.Fprintf(&b, "- %s\n", h)
			}
		}
		return b.String()
	}
	fence := false
	headings := 0
	var opening []string
	for i, l := range strings.Split(text, "\n") {
		t := strings.TrimSpace(l)
		if strings.HasPrefix(t, "```") {
			fence = !fence
			continue
		}
		if fence {
			continue
		}
		if strings.HasPrefix(t, "#") && strings.Count(strings.SplitN(t, " ", 2)[0], "#") <= 3 {
			if headings < 15 {
				fmt.Fprintf(&b, "%5d| %s\n", i+1, t)
			}
			headings++
			continue
		}
		if t != "" && len(opening) < 4 && headings <= 1 {
			opening = append(opening, t)
		}
	}
	if headings > 15 {
		fmt.Fprintf(&b, "      … %d more headings\n", headings-15)
	}
	if len(opening) > 0 {
		fmt.Fprintf(&b, "Opens: %s\n", clip(strings.Join(opening, " "), 400))
	}
	return b.String()
}
