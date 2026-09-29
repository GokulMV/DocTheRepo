package grammars

import (
	"bytes"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var quiet = slog.New(slog.NewTextHandler(io.Discard, nil))

func TestBuiltin_ResolvesEveryExtension(t *testing.T) {
	r := NewBuiltin()
	cases := map[string]string{
		"main.go": "go", "A.java": "java", "x.py": "python", "x.pyi": "python", "a.ts": "typescript",
		"a.mts": "typescript", "B.tsx": "tsx", "c.js": "javascript", "c.jsx": "javascript", "c.MJS": "javascript",
		"lib.rs": "rust",
	}
	for path, want := range cases {
		l := r.ForPath(path)
		require.NotNil(t, l, path)
		assert.Equal(t, want, l.Name, path)
		assert.True(t, l.Builtin)
	}
	assert.Nil(t, r.ForPath("values.yaml"))
	assert.Nil(t, r.ForPath("Makefile"))
	assert.Equal(t, []string{"go", "java", "javascript", "python", "rust", "tsx", "typescript"}, r.Names())
	assert.NotNil(t, r.ByName("rust"))
}

// Every node kind named in a built-in spec must exist in its grammar; a typo would silently disable a rule.
func TestBuiltin_SpecNodeKindsExistInGrammar(t *testing.T) {
	r := NewBuiltin()
	for _, name := range r.Names() {
		l := r.ByName(name)
		var kinds []string
		for k := range l.Definitions {
			kinds = append(kinds, k)
		}
		for k := range l.Wrappers {
			kinds = append(kinds, k)
		}
		for k := range l.Calls {
			kinds = append(kinds, k)
		}
		kinds = append(kinds, l.Comments...)
		kinds = append(kinds, l.Strings...)
		kinds = append(kinds, l.Imports...)
		kinds = append(kinds, l.Annotations...)
		kinds = append(kinds, l.SignificantStringParents...)
		kinds = append(kinds, l.FunctionDeclarators...)
		kinds = append(kinds, l.FunctionValues...)
		for _, k := range kinds {
			assert.NotZero(t, l.ts.IdForNodeKind(k, true), "%s: node kind %q not in grammar", name, k)
		}
		for _, f := range append(append([]string{}, l.NameFields...), l.BodyFields...) {
			assert.NotZero(t, l.ts.FieldIdForName(f), "%s: field %q not in grammar", name, f)
		}
	}
}

func TestParse_ConcurrentUseIsSafe(t *testing.T) {
	l := NewBuiltin().ByName("go")
	done := make(chan struct{})
	for i := 0; i < 8; i++ {
		go func() {
			defer func() { done <- struct{}{} }()
			for j := 0; j < 50; j++ {
				tree, err := l.Parse([]byte("package p\nfunc f() {}\n"))
				if err != nil {
					t.Error(err)
					return
				}
				tree.Close()
			}
		}()
	}
	for i := 0; i < 8; i++ {
		<-done
	}
}

func TestLoadDir_MissingDirIsFine(t *testing.T) {
	r := NewBuiltin()
	require.NoError(t, r.LoadDir(filepath.Join(t.TempDir(), "nope"), quiet))
}

func TestLoadDir_BadSpecsAreReportedAndSkipped(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "broken.json"), []byte("{"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "noext.json"), []byte(`{"definitions":{"x":"function"}}`), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "nodefs.json"), []byte(`{"extensions":[".x"]}`), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "unknown.json"), []byte(`{"extensions":[".x"],"definitions":{"x":"f"},"bogus":1}`), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "noso.json"), []byte(`{"extensions":[".x"],"definitions":{"x":"f"}}`), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "README.txt"), []byte("ignored"), 0o644))
	r := NewBuiltin()
	err := r.LoadDir(dir, quiet)
	require.Error(t, err)
	for _, want := range []string{"broken", "noext", "nodefs", "unknown", "noso"} {
		assert.Contains(t, err.Error(), want)
	}
	assert.Nil(t, r.ForPath("a.x"))
	assert.NotNil(t, r.ForPath("a.go"), "built-ins survive bad runtime grammars")
}

// TestLoadDir_RealSharedLibrary compiles the Go grammar's parser.c into a shared library, loads it at
// runtime under a new extension, and parses with it. Skipped when no C compiler is available.
func TestLoadDir_RealSharedLibrary(t *testing.T) {
	cc, err := exec.LookPath("cc")
	if err != nil {
		t.Skip("no C compiler; runtime grammar loading not exercised")
	}
	out, err := exec.Command("go", "list", "-m", "-f", "{{.Dir}}", "github.com/tree-sitter/tree-sitter-go").Output()
	require.NoError(t, err)
	src := filepath.Join(strings.TrimSpace(string(out)), "src")
	dir := t.TempDir()
	so := filepath.Join(dir, "golite.so")
	sources := []string{filepath.Join(src, "parser.c")}
	if _, err := os.Stat(filepath.Join(src, "scanner.c")); err == nil { // only some grammars have one
		sources = append(sources, filepath.Join(src, "scanner.c"))
	}
	cmd := exec.Command(cc, append([]string{"-shared", "-fPIC", "-O1", "-I", src, "-o", so}, sources...)...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Skipf("could not compile grammar: %v: %s", err, stderr.String())
	}
	spec := `{"name":"golite","symbol":"tree_sitter_go","extensions":[".golite",".go"],
	          "definitions":{"function_declaration":"function"},"comments":["comment"],"body_fields":["body"]}`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "golite.json"), []byte(spec), 0o644))

	r := NewBuiltin()
	require.NoError(t, r.LoadDir(dir, quiet))
	l := r.ForPath("x.golite")
	require.NotNil(t, l)
	assert.False(t, l.Builtin)
	assert.Equal(t, "golite", r.ForPath("main.go").Name, "a runtime grammar overrides the built-in for its extensions")
	tree, err := l.Parse([]byte("package p\nfunc f() {}\n"))
	require.NoError(t, err)
	defer tree.Close()
	assert.Equal(t, "source_file", tree.RootNode().Kind())
}
