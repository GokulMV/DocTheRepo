// Package repodocs writes a repository's documentation as a set of readable, cited documents (an
// Overview, the whole-application Architecture, one guide per module, and the other types in Catalog)
// instead of one doc per source file.
//
// It works in stages. Facts come from the code index and the knowledge graph without any model call;
// the repository is split into modules; each file gets a short card (cached by the file's structure, so
// edits inside function bodies do not redo it); then each document is written from its own inputs,
// checked (sections, length, citations, identifiers), and scored for confidence. A document is written
// again only when its inputs change materially.
package repodocs

import (
	"crypto/sha256"
	"encoding/hex"
	"path"
	"sort"
	"strings"

	"github.com/GokulMV/DocTheRepo/internal/ports"
)

// Symbol is a declaration in the code.
type Symbol struct {
	Name      string `json:"name"`
	Kind      string `json:"kind,omitempty"`
	Path      string `json:"path"`
	Line      int    `json:"line,omitempty"`
	Lines     int    `json:"lines,omitempty"`
	Signature string `json:"signature,omitempty"`
	Body      string `json:"-"`
	// In counts calls from other files (how central the symbol is).
	In int `json:"in,omitempty"`
}

// Exported reports a declaration other code is meant to use (by the usual naming conventions).
func (s Symbol) Exported() bool {
	n := s.Name
	if i := strings.LastIndexAny(n, ".#"); i >= 0 {
		n = n[i+1:]
	}
	if n == "" || strings.HasPrefix(n, "_") {
		return false
	}
	switch strings.ToLower(path.Ext(s.Path)) {
	case ".go":
		return n[0] >= 'A' && n[0] <= 'Z'
	}
	return true
}

// File is a source file the Hub indexed.
type File struct {
	Path     string   `json:"path"`
	Language string   `json:"language,omitempty"`
	Lines    int      `json:"lines"`
	Symbols  []Symbol `json:"symbols,omitempty"`
	// Hash identifies the content; Shape identifies the structure (declarations and signatures).
	Hash  string `json:"hash"`
	Shape string `json:"shape"`
}

// Fact is something the graph knows, with where in the code it comes from.
type Fact struct {
	Kind string `json:"kind"` // endpoint | env_var | datastore | topic_pub | topic_sub | dependency | owner | service | cloud_resource
	Name string `json:"name"`
	Path string `json:"path,omitempty"`
	Line int    `json:"line,omitempty"`
	// From is the symbol or file it belongs to.
	From string `json:"from,omitempty"`
}

// Call is a call from one file to another (aggregated from symbol calls).
type Call struct {
	From, To string
	N        int
}

// Import is a file importing a package of the same repository, by the package's directory (Go). Calls
// through values (a method on a struct field) are not resolved to their file, but the import is.
type Import struct {
	From, Dir string
}

// Facts are what the Hub knows about a repository at a commit, gathered without a model.
type Facts struct {
	RepoID   string
	Repo     string
	Head     string
	Service  string
	Files    []File
	Facts    []Fact
	Calls    []Call
	Imports  []Import
	AllPaths []string          // every path in the tree, including files that are not indexed (tests, docs, config)
	Special  map[string]string // contents of README, build, deploy and CI files, clipped
	Diagrams []string          // diagrams authored in the repository
	// Commits are the recent commits on the documented branch (newest first), for Recent changes and
	// Decision records.
	Commits []ports.Commit
}

// IsTest reports a test file by the usual conventions.
func IsTest(p string) bool {
	l := strings.ToLower(p)
	base := path.Base(l)
	switch {
	case strings.HasSuffix(base, "_test.go"), strings.HasPrefix(base, "test_") && strings.HasSuffix(base, ".py"),
		strings.HasSuffix(base, "_test.py"), strings.Contains(base, ".test."), strings.Contains(base, ".spec."),
		strings.HasSuffix(base, "test.java"), strings.HasSuffix(base, "tests.cs"), strings.HasSuffix(base, "_spec.rb"):
		return true
	}
	for _, seg := range strings.Split(path.Dir(l), "/") {
		switch seg {
		case "test", "tests", "__tests__", "spec", "e2e", "testdata":
			return true
		}
	}
	return false
}

// SpecialFile reports files read whole as context: READMEs, build, deploy and CI configuration.
func SpecialFile(p string) bool {
	l := strings.ToLower(p)
	base := path.Base(l)
	dir := path.Dir(l)
	switch {
	case dir == "." && (strings.HasPrefix(base, "readme") || base == "contributing.md" || base == "architecture.md"):
		return true
	case base == "dockerfile" || strings.HasPrefix(base, "docker-compose") || base == "compose.yaml" || base == "compose.yml",
		base == "makefile" || base == "justfile" || base == "taskfile.yml" || base == "procfile",
		base == "go.mod" || base == "package.json" || base == "pyproject.toml" || base == "requirements.txt" || base == "pom.xml" ||
			base == "build.gradle" || base == "build.gradle.kts" || base == "cargo.toml" || base == "gemfile" || base == "composer.json",
		base == "codeowners",
		strings.HasPrefix(l, ".github/workflows/"), base == ".gitlab-ci.yml", base == "jenkinsfile", strings.HasPrefix(l, ".circleci/"),
		base == "app.yaml" || base == "serverless.yml" || base == "fly.toml" || base == "railway.json" || base == "vercel.json" || base == "netlify.toml",
		strings.HasPrefix(base, "chart.yaml") || strings.HasPrefix(base, "values.yaml"),
		base == ".env.example" || base == "env.example":
		return true
	}
	return false
}

// Shape fingerprints a file's structure: its declarations and their signatures, in order.
func Shape(syms []Symbol) string {
	h := sha256.New()
	for _, s := range syms {
		h.Write([]byte(s.Kind + "\x00" + s.Name + "\x00" + s.Signature + "\n"))
	}
	return hex.EncodeToString(h.Sum(nil))[:16]
}

// Hash fingerprints any list of strings (order matters).
func Hash(parts ...string) string {
	h := sha256.New()
	for _, p := range parts {
		h.Write([]byte(p))
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))[:16]
}

// FileByPath indexes files.
func (f *Facts) FileByPath() map[string]*File {
	m := make(map[string]*File, len(f.Files))
	for i := range f.Files {
		m[f.Files[i].Path] = &f.Files[i]
	}
	return m
}

// Known is the set of names the docs may mention in backticks: symbols (full and short), files,
// directories, and every fact name.
func (f *Facts) Known() map[string]bool {
	k := map[string]bool{}
	add := func(s string) {
		s = strings.TrimSpace(s)
		if s == "" {
			return
		}
		k[strings.ToLower(s)] = true
	}
	for _, fl := range f.Files {
		add(fl.Path)
		add(path.Base(fl.Path))
		for d := path.Dir(fl.Path); d != "." && d != "/"; d = path.Dir(d) {
			add(d)
			add(path.Base(d))
		}
		for _, s := range fl.Symbols {
			add(s.Name)
			for _, sep := range []string{".", "#", "::"} {
				if i := strings.LastIndex(s.Name, sep); i >= 0 {
					add(s.Name[i+len(sep):])
					add(s.Name[:i])
				}
			}
		}
	}
	for _, p := range f.AllPaths {
		add(p)
		add(path.Base(p))
	}
	for _, x := range f.Facts {
		add(x.Name)
		if i := strings.IndexByte(x.Name, ' '); i > 0 { // "POST /orders" → "/orders"
			add(x.Name[i+1:])
		}
	}
	return k
}

// sortedKeys returns a map's keys in order.
func sortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// SystemLink is one way two repositories talk to each other.
type SystemLink struct {
	FromRepo string `json:"from_repo"`
	FromName string `json:"from_name"`
	ToRepo   string `json:"to_repo"`
	ToName   string `json:"to_name"`
	Kind     string `json:"kind"` // event | api | call | library | image | pipeline | other
	Via      string `json:"via"`  // the topic, endpoint, symbol or package
	Path     string `json:"path,omitempty"`
	Line     int    `json:"line,omitempty"`
	N        int    `json:"n"`
}
