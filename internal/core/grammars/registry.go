package grammars

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	sitter "github.com/tree-sitter/go-tree-sitter"
	tsgo "github.com/tree-sitter/tree-sitter-go/bindings/go"
	tsjava "github.com/tree-sitter/tree-sitter-java/bindings/go"
	tsjs "github.com/tree-sitter/tree-sitter-javascript/bindings/go"
	tspy "github.com/tree-sitter/tree-sitter-python/bindings/go"
	tsrust "github.com/tree-sitter/tree-sitter-rust/bindings/go"
	tsts "github.com/tree-sitter/tree-sitter-typescript/bindings/go"
)

// Language is a grammar plus its spec. It is safe for concurrent use.
type Language struct {
	Spec
	ts      *sitter.Language
	parsers sync.Pool
	sets    specSets
	// Builtin is false for grammars loaded from the grammar directory.
	Builtin bool
}

// specSets are the spec lists as lookup maps, built once.
type specSets struct {
	comments, strings, sigParents, imports, annotations, scopes, fnDecls, fnValues map[string]bool
}

func toSet(xs []string) map[string]bool {
	m := make(map[string]bool, len(xs))
	for _, x := range xs {
		m[x] = true
	}
	return m
}

func newLanguage(spec Spec, ts *sitter.Language, builtin bool) *Language {
	if len(spec.NameFields) == 0 {
		spec.NameFields = []string{"name"}
	}
	if spec.CallArgsField == "" {
		spec.CallArgsField = "arguments"
	}
	l := &Language{Spec: spec, ts: ts, Builtin: builtin, sets: specSets{
		comments: toSet(spec.Comments), strings: toSet(spec.Strings), sigParents: toSet(spec.SignificantStringParents),
		imports: toSet(spec.Imports), annotations: toSet(spec.Annotations), scopes: toSet(spec.Scopes),
		fnDecls: toSet(spec.FunctionDeclarators), fnValues: toSet(spec.FunctionValues),
	}}
	l.parsers.New = func() any {
		p := sitter.NewParser()
		if err := p.SetLanguage(ts); err != nil {
			panic(fmt.Sprintf("grammar %s rejected by parser: %v", spec.Name, err)) // validated at registration
		}
		return p
	}
	return l
}

// Parse parses src. The caller must Close the returned tree.
func (l *Language) Parse(src []byte) (*sitter.Tree, error) {
	p := l.parsers.Get().(*sitter.Parser)
	defer l.parsers.Put(p)
	tree := p.Parse(src, nil)
	if tree == nil {
		return nil, fmt.Errorf("%s parser returned no tree", l.Name)
	}
	return tree, nil
}

// IsComment reports whether kind is a comment node.
func (l *Language) IsComment(kind string) bool { return l.sets.comments[kind] }

// IsString reports whether kind is a string-literal node.
func (l *Language) IsString(kind string) bool { return l.sets.strings[kind] }

// IsSignificantStringParent reports whether a string under kind is structurally meaningful.
func (l *Language) IsSignificantStringParent(kind string) bool { return l.sets.sigParents[kind] }

// IsImport reports whether kind is an import statement.
func (l *Language) IsImport(kind string) bool { return l.sets.imports[kind] }

// IsAnnotation reports whether kind is a decorator/annotation.
func (l *Language) IsAnnotation(kind string) bool { return l.sets.annotations[kind] }

// IsScope reports whether definitions nested in kind get a qualified name.
func (l *Language) IsScope(kind string) bool { return l.sets.scopes[kind] }

// IsFunctionDeclarator reports whether kind may declare a function via its value.
func (l *Language) IsFunctionDeclarator(kind string) bool { return l.sets.fnDecls[kind] }

// IsFunctionValue reports whether kind is a function expression.
func (l *Language) IsFunctionValue(kind string) bool { return l.sets.fnValues[kind] }

// Registry resolves file extensions to languages.
type Registry struct {
	byExt  map[string]*Language
	byName map[string]*Language
}

// NewBuiltin returns a registry with the compiled-in grammars (Go, Java, Python, TypeScript/TSX,
// JavaScript, Rust).
func NewBuiltin() *Registry {
	r := &Registry{byExt: map[string]*Language{}, byName: map[string]*Language{}}
	ptrs := map[string]*sitter.Language{
		"go":         sitter.NewLanguage(tsgo.Language()),
		"java":       sitter.NewLanguage(tsjava.Language()),
		"python":     sitter.NewLanguage(tspy.Language()),
		"typescript": sitter.NewLanguage(tsts.LanguageTypescript()),
		"tsx":        sitter.NewLanguage(tsts.LanguageTSX()),
		"javascript": sitter.NewLanguage(tsjs.Language()),
		"rust":       sitter.NewLanguage(tsrust.Language()),
	}
	for name, ts := range ptrs {
		r.add(newLanguage(builtinSpecs[name], ts, true))
	}
	return r
}

func (r *Registry) add(l *Language) {
	r.byName[l.Name] = l
	for _, ext := range l.Extensions {
		r.byExt[strings.ToLower(ext)] = l
	}
}

// ForPath returns the language for a file path, or nil when no grammar is available.
func (r *Registry) ForPath(path string) *Language {
	return r.byExt[strings.ToLower(filepath.Ext(path))]
}

// ByName returns a language by grammar name, or nil.
func (r *Registry) ByName(name string) *Language { return r.byName[name] }

// Names lists registered grammar names, sorted.
func (r *Registry) Names() []string {
	out := make([]string, 0, len(r.byName))
	for n := range r.byName {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

// LoadDir registers every grammar in dir: for each <name>.json spec there must be a <name>.so shared
// library exporting tree_sitter_<name> (or spec.symbol). A runtime grammar for an extension already served
// by a built-in one overrides it, logged at INFO, so teams can pin a newer grammar without a release.
// A missing directory is not an error. Individual bad grammars are logged and skipped so one broken
// file never prevents startup; the returned error lists them.
func (r *Registry) LoadDir(dir string, log *slog.Logger) error {
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read grammar directory %s: %w", dir, err)
	}
	var problems []string
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".json" {
			continue
		}
		base := strings.TrimSuffix(e.Name(), ".json")
		if err := r.loadOne(dir, base, log); err != nil {
			log.Error("skipping runtime grammar", "grammar", base, "err", err)
			problems = append(problems, fmt.Sprintf("%s: %v", base, err))
		}
	}
	if len(problems) > 0 {
		return fmt.Errorf("some runtime grammars failed to load: %s", strings.Join(problems, "; "))
	}
	return nil
}

func (r *Registry) loadOne(dir, base string, log *slog.Logger) error {
	raw, err := os.ReadFile(filepath.Join(dir, base+".json"))
	if err != nil {
		return err
	}
	var spec Spec
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&spec); err != nil {
		return fmt.Errorf("parse spec: %w", err)
	}
	if spec.Name == "" {
		spec.Name = base
	}
	if len(spec.Extensions) == 0 {
		return errors.New("spec declares no extensions")
	}
	if len(spec.Definitions) == 0 {
		return errors.New("spec declares no definition node kinds")
	}
	sym := spec.Symbol
	if sym == "" {
		sym = "tree_sitter_" + strings.ReplaceAll(spec.Name, "-", "_")
	}
	ptr, err := loadLanguageSymbol(filepath.Join(dir, base+".so"), sym)
	if err != nil {
		return err
	}
	ts := sitter.NewLanguage(ptr)
	p := sitter.NewParser()
	defer p.Close()
	if err := p.SetLanguage(ts); err != nil {
		return fmt.Errorf("incompatible grammar ABI: %w", err)
	}
	l := newLanguage(spec, ts, false)
	for _, ext := range spec.Extensions {
		if prev := r.byExt[strings.ToLower(ext)]; prev != nil {
			log.Info("runtime grammar overrides existing grammar", "extension", ext, "previous", prev.Name, "grammar", spec.Name)
		}
	}
	r.add(l)
	return nil
}
