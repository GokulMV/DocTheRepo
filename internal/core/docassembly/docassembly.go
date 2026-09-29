// Package docassembly owns the layout of generated documentation files (the Tree, plan § 8.4): one
// Markdown file per source file, one section per symbol, surgical updates, and hand-written
// <!-- dth:human --> blocks that regeneration never touches.
package docassembly

import (
	"fmt"
	"path"
	"regexp"
	"sort"
	"strings"
)

const (
	humanOpen  = "<!-- dth:human -->"
	humanClose = "<!-- dth:end -->"
)

var (
	markerRE     = regexp.MustCompile(`(?m)^<!--\s*dth:chunk\s+([0-9a-f]{16})\s*-->\s*$`)
	headerRE     = regexp.MustCompile(`(?m)^<!--\s*dth:generated\b.*-->\s*$`)
	humanBlockRE = regexp.MustCompile(`(?s)<!--\s*dth:human\s*-->.*?<!--\s*dth:end\s*-->`)
	headingRE    = regexp.MustCompile(`(?m)^(#{1,2})(\s+)`)
)

// DocPath maps a source file to its generated doc file: <docsPath><sourcePath>.md.
func DocPath(docsPath, sourcePath string) string {
	return strings.TrimSuffix(docsPath, "/") + "/" + strings.TrimPrefix(sourcePath, "/") + ".md"
}

// IndexPath is the per-directory index file under the docs path.
func IndexPath(docsPath, sourceDir string) string {
	d := strings.Trim(sourceDir, "/")
	if d == "" || d == "." {
		return strings.TrimSuffix(docsPath, "/") + "/README.md"
	}
	return strings.TrimSuffix(docsPath, "/") + "/" + d + "/README.md"
}

// Section is one symbol's generated documentation.
type Section struct {
	ChunkID string
	Symbol  string
	// Body is the generated Markdown for the symbol, without its heading. H1/H2 headings inside it are
	// demoted to H3 so they cannot break the file's section structure.
	Body string
}

// Update describes one regeneration of a doc file.
type Update struct {
	SourcePath string
	// Order lists chunk IDs in source order; sections are written in this order. IDs absent from Order
	// keep their existing relative order after the ordered ones.
	Order   []string
	Upserts []Section
	Removes []string
	// Summary replaces the file summary shown under the title (empty keeps the existing one).
	Summary string
}

type section struct {
	id, symbol string
	generated  string
	human      []string
}

type doc struct {
	header   []string // human blocks that live before the first section
	summary  string
	sections []*section
}

// Assemble applies u to the existing file content (nil for a new file) and returns the new content and
// whether anything changed.
func Assemble(existing []byte, u Update) ([]byte, bool) {
	d := parse(string(existing))
	byID := map[string]*section{}
	for _, s := range d.sections {
		byID[s.id] = s
	}
	for _, id := range u.Removes {
		if s := byID[id]; s != nil {
			// A removed symbol's hand-written notes are kept, moved to the file header, rather than lost.
			d.header = append(d.header, s.human...)
			delete(byID, id)
		}
	}
	for _, up := range u.Upserts {
		body := strings.TrimSpace(headingRE.ReplaceAllString(stripHuman(up.Body), "###$2"))
		if s := byID[up.ChunkID]; s != nil {
			s.symbol, s.generated = up.Symbol, body
		} else {
			byID[up.ChunkID] = &section{id: up.ChunkID, symbol: up.Symbol, generated: body}
		}
	}
	if u.Summary != "" {
		d.summary = strings.TrimSpace(u.Summary)
	}

	var ordered []*section
	placed := map[string]bool{}
	for _, id := range u.Order {
		if s := byID[id]; s != nil && !placed[id] {
			ordered = append(ordered, s)
			placed[id] = true
		}
	}
	for _, s := range d.sections { // existing sections not in Order keep their relative order
		if byID[s.id] != nil && !placed[s.id] {
			ordered = append(ordered, byID[s.id])
			placed[s.id] = true
		}
	}
	var rest []*section // new sections not in Order: deterministic by symbol
	for id, s := range byID {
		if !placed[id] {
			rest = append(rest, s)
		}
	}
	sort.Slice(rest, func(i, j int) bool { return rest[i].symbol < rest[j].symbol })
	d.sections = append(ordered, rest...)

	out := render(u.SourcePath, d)
	return []byte(out), out != string(existing)
}

func render(sourcePath string, d *doc) string {
	var b strings.Builder
	fmt.Fprintf(&b, "<!-- dth:generated source=%q — edit only inside dth:human blocks -->\n", sourcePath)
	fmt.Fprintf(&b, "# `%s`\n", sourcePath)
	if d.summary != "" {
		b.WriteString("\n" + d.summary + "\n")
	}
	for _, h := range d.header {
		b.WriteString("\n" + h + "\n")
	}
	for _, s := range d.sections {
		fmt.Fprintf(&b, "\n<!-- dth:chunk %s -->\n## `%s`\n", s.id, s.symbol)
		if s.generated != "" {
			b.WriteString("\n" + s.generated + "\n")
		}
		for _, h := range s.human {
			b.WriteString("\n" + h + "\n")
		}
	}
	return b.String()
}

// parse reads a file written by render (or hand-edited within the rules).
func parse(content string) *doc {
	d := &doc{}
	if strings.TrimSpace(content) == "" {
		return d
	}
	locs := markerRE.FindAllStringSubmatchIndex(content, -1)
	headEnd := len(content)
	if len(locs) > 0 {
		headEnd = locs[0][0]
	}
	head := content[:headEnd]
	d.header = humanBlockRE.FindAllString(head, -1)
	head = headerRE.ReplaceAllString(humanBlockRE.ReplaceAllString(head, ""), "")
	lines := strings.Split(strings.TrimSpace(head), "\n")
	if len(lines) > 0 && strings.HasPrefix(lines[0], "# ") {
		lines = lines[1:]
	}
	d.summary = strings.TrimSpace(strings.Join(lines, "\n"))

	for i, loc := range locs {
		end := len(content)
		if i+1 < len(locs) {
			end = locs[i+1][0]
		}
		s := &section{id: content[loc[2]:loc[3]]}
		body := content[loc[1]:end]
		s.human = humanBlockRE.FindAllString(body, -1)
		body = strings.TrimSpace(humanBlockRE.ReplaceAllString(body, ""))
		if strings.HasPrefix(body, "## ") {
			first, rest, _ := strings.Cut(body, "\n")
			s.symbol = strings.Trim(strings.TrimPrefix(first, "## "), "` ")
			body = strings.TrimSpace(rest)
		}
		s.generated = body
		d.sections = append(d.sections, s)
	}
	return d
}

func stripHuman(s string) string { return humanBlockRE.ReplaceAllString(s, "") }

// IndexEntry is one line of a directory index.
type IndexEntry struct {
	Name    string // file or subdirectory name, relative to the directory
	Summary string
	IsDir   bool
}

// RenderIndex renders a directory index README listing child doc files and subdirectories with one-line
// summaries. Human blocks in the existing index are preserved.
func RenderIndex(existing []byte, sourceDir string, entries []IndexEntry) ([]byte, bool) {
	humans := humanBlockRE.FindAllString(string(existing), -1)
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].IsDir != entries[j].IsDir {
			return entries[i].IsDir
		}
		return entries[i].Name < entries[j].Name
	})
	title := strings.Trim(sourceDir, "/")
	if title == "" || title == "." {
		title = "(repository root)"
	}
	var b strings.Builder
	b.WriteString("<!-- dth:generated index — edit only inside dth:human blocks -->\n")
	fmt.Fprintf(&b, "# `%s`\n\n", title)
	for _, e := range entries {
		link := e.Name + ".md"
		if e.IsDir {
			link = path.Join(e.Name, "README.md")
		}
		line := fmt.Sprintf("- [`%s`](%s)", e.Name, link)
		if s := firstLine(e.Summary); s != "" {
			line += " — " + s
		}
		b.WriteString(line + "\n")
	}
	for _, h := range humans {
		b.WriteString("\n" + h + "\n")
	}
	out := b.String()
	return []byte(out), out != string(existing)
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	if len(s) > 160 {
		s = s[:157] + "..."
	}
	return s
}

// Summary extracts a doc file's summary paragraph (used to build directory indexes).
func Summary(content []byte) string { return parse(string(content)).summary }

var indexLineRE = regexp.MustCompile("^- \\[`([^`]+)`\\]\\(([^)]*)\\)(?: — (.*))?$")

// ParseIndex reads the entries of an index rendered by RenderIndex, so an update can change only the
// entries whose files changed without re-reading every child doc.
func ParseIndex(existing []byte) []IndexEntry {
	var out []IndexEntry
	for _, line := range strings.Split(string(existing), "\n") {
		m := indexLineRE.FindStringSubmatch(strings.TrimRight(line, "\r"))
		if m == nil {
			continue
		}
		out = append(out, IndexEntry{Name: m[1], Summary: m[3], IsDir: strings.HasSuffix(m[2], "/README.md")})
	}
	return out
}
