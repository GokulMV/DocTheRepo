package triage

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/GokulMV/DocTheRepo/internal/core/grammars"
	"github.com/GokulMV/DocTheRepo/internal/ports"
)

var reg = grammars.NewBuiltin()

func newTriage(t *testing.T) *Triage {
	t.Helper()
	tr, err := New(reg, Options{DocsPath: "docs/generated/"})
	require.NoError(t, err)
	return tr
}

func mod(path, old, new string) FileChange {
	return FileChange{Path: path, Status: ports.FileModified, Old: []byte(old), New: []byte(new)}
}

func TestTriage_PerLanguage(t *testing.T) {
	cases := []struct {
		name     string
		path     string
		old, new string
		want     Decision
		reason   string // substring
	}{
		// Go
		{"go comment only", "a.go", "package p\n// old\nfunc F() int { return 1 }\n", "package p\n// new words\nfunc F() int { return 1 }\n", Abort, "comments"},
		{"go whitespace only", "a.go", "package p\nfunc F() int { return 1 }\n", "package p\n\n\nfunc F()   int {\n\treturn 1\n}\n", Abort, ""},
		{"go log message text", "a.go", "package p\nfunc F() { log.Print(\"starting\") }\n", "package p\nfunc F() { log.Print(\"booting up\") }\n", Abort, "string"},
		{"go env var renamed", "a.go", "package p\nfunc F() string { return os.Getenv(\"A\") }\n", "package p\nfunc F() string { return os.Getenv(\"B\") }\n", Proceed, "body changed: F"},
		{"go import path changed", "a.go", "package p\nimport \"a/v1\"\n", "package p\nimport \"a/v2\"\n", Proceed, ""},
		{"go body logic", "a.go", "package p\nfunc F() int { return 1 }\n", "package p\nfunc F() int { return 2 }\n", Proceed, "body changed: F"},
		{"go signature", "a.go", "package p\nfunc F() int { return 1 }\n", "package p\nfunc F(x int) int { return 1 }\n", Proceed, "signature changed: F"},
		{"go added method", "a.go", "package p\ntype S struct{}\n", "package p\ntype S struct{}\nfunc (s S) M() {}\n", Proceed, "added: S.M"},
		{"go removed func", "a.go", "package p\nfunc F() {}\nfunc G() {}\n", "package p\nfunc F() {}\n", Proceed, "removed: G"},
		{"go struct field", "a.go", "package p\ntype S struct{ A int }\n", "package p\ntype S struct{ A int; B string }\n", Proceed, "type changed: S"},
		// Java
		{"java javadoc", "A.java", "class A { /** a */ int m() { return 1; } }", "class A { /** better docs */ int m() { return 1; } }", Abort, ""},
		{"java route annotation", "A.java", "class A { @GetMapping(\"/v1/x\") int m() { return 1; } }", "class A { @GetMapping(\"/v2/x\") int m() { return 1; } }", Proceed, ""},
		{"java exception message", "A.java", "class A { void m() { throw new E(\"bad\"); } }", "class A { void m() { throw new E(\"invalid input\"); } }", Abort, ""},
		// Python
		{"python docstring added", "a.py", "def f():\n    return 1\n", "def f():\n    \"\"\"Explain f.\"\"\"\n    return 1\n", Abort, ""},
		{"python indentation moves statement into block", "a.py", "def f(x):\n    if x:\n        a()\n    b()\n", "def f(x):\n    if x:\n        a()\n        b()\n", Proceed, "body changed: f"},
		{"python route decorator", "a.py", "@app.get('/a')\ndef f():\n    pass\n", "@app.get('/b')\ndef f():\n    pass\n", Proceed, ""},
		{"python environ key", "a.py", "import os\nX = os.environ['A']\n", "import os\nX = os.environ['B']\n", Proceed, ""},
		// TypeScript / JavaScript
		{"ts comment", "a.ts", "// a\nexport function f(): number { return 1 }\n", "/* b */\nexport function f(): number { return 1 }\n", Abort, ""},
		{"ts type change", "a.ts", "export interface I { a: number }\n", "export interface I { a: string }\n", Proceed, "type changed: I"},
		{"ts process.env key", "a.ts", "const k = process.env.A;\n", "const k = process.env.B;\n", Proceed, ""},
		{"js arrow body", "a.js", "const f = () => 1;\n", "const f = () => 2;\n", Proceed, "body changed: f"},
		// Rust
		{"rust doc comment", "a.rs", "/// a\npub fn f() -> i32 { 1 }\n", "/// b\npub fn f() -> i32 { 1 }\n", Abort, ""},
		{"rust attribute route", "a.rs", "#[get(\"/a\")]\nfn f() {}\n", "#[get(\"/b\")]\nfn f() {}\n", Proceed, ""},
		{"rust env macro", "a.rs", "fn f() -> &'static str { env!(\"A\") }\n", "fn f() -> &'static str { env!(\"B\") }\n", Proceed, ""},
	}
	tr := newTriage(t)
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			v := tr.File(mod(c.path, c.old, c.new))
			assert.Equal(t, c.want, v.Decision, v.Reason)
			assert.Contains(t, v.Reason, c.reason)
			assert.False(t, v.NeedsLLM)
			if c.want == Proceed {
				assert.NotNil(t, v.New)
			}
		})
	}
}

func TestTriage_PathRules(t *testing.T) {
	tr := newTriage(t)
	cases := []struct {
		fc   FileChange
		want Decision
		why  string
	}{
		{mod("docs/generated/a.go.md", "a", "b"), Abort, "generated-docs"},
		{mod("docs/generated", "a", "b"), Abort, "generated-docs"},
		{mod("docs/generatedX/a.go", "package p", "package p\nfunc F(){}"), Proceed, ""},
		{mod("go.sum", "a", "b"), Abort, "ignored"},
		{mod("web/node_modules/x/index.js", "a", "b"), Abort, "ignored"},
		{mod("README.md", "a", "b"), Abort, "documentation outside"},
		{mod("img/logo.png", "\x89PNG\x00", "\x89PNG\x00\x01"), Abort, "binary"},
	}
	for _, c := range cases {
		v := tr.File(c.fc)
		assert.Equal(t, c.want, v.Decision, c.fc.Path)
		assert.Contains(t, v.Reason, c.why, c.fc.Path)
	}
}

func TestTriage_AddedDeletedAndIndexOnly(t *testing.T) {
	tr := newTriage(t)
	v := tr.File(FileChange{Path: "a.go", Status: ports.FileAdded, New: []byte("package p\nfunc F() {}\nfunc G() {}\n")})
	assert.Equal(t, Proceed, v.Decision)
	assert.Contains(t, v.Reason, "2 definition")

	v = tr.File(FileChange{Path: "a.go", Status: ports.FileRemoved, Old: []byte("package p")})
	assert.Equal(t, Proceed, v.Decision)
	assert.Contains(t, v.Reason, "deleted")

	v = tr.File(FileChange{Path: "README.md", Status: ports.FileRemoved, Old: []byte("# x")})
	assert.Equal(t, Proceed, v.Decision, "a deleted doc still removes its imported chunks")

	v = tr.File(mod("pkg/a_test.go", "package p\nfunc TestA(t *testing.T) {}\n", "package p\nfunc TestA(t *testing.T) { t.Fail() }\n"))
	assert.Equal(t, Proceed, v.Decision)
	assert.True(t, v.IndexOnly, "tests are indexed but not documented")
}

func TestTriage_Renames(t *testing.T) {
	tr := newTriage(t)
	src := "package p\nfunc F() int { return 1 }\n"
	pure := tr.File(FileChange{Path: "new/a.go", PreviousPath: "old/a.go", Status: ports.FileRenamed, Similarity: 100, Old: []byte(src), New: []byte(src)})
	assert.Equal(t, Abort, pure.Decision)
	assert.True(t, pure.Rename)

	commentEdit := tr.File(FileChange{Path: "new/a.go", PreviousPath: "old/a.go", Status: ports.FileRenamed, Similarity: 95,
		Old: []byte(src), New: []byte("// hi\n" + src)})
	assert.True(t, commentEdit.Rename, "a rename with cosmetic edits is still a re-key")

	lowSim := tr.File(FileChange{Path: "new/a.go", PreviousPath: "old/a.go", Status: ports.FileRenamed, Similarity: 60, Old: []byte(src), New: []byte(src)})
	assert.Equal(t, Proceed, lowSim.Decision)
	assert.False(t, lowSim.Rename)

	edited := tr.File(FileChange{Path: "new/a.go", PreviousPath: "old/a.go", Status: ports.FileRenamed, Similarity: 92,
		Old: []byte(src), New: []byte("package p\nfunc F() int { return 2 }\n")})
	assert.Equal(t, Proceed, edited.Decision)
	assert.False(t, edited.Rename)

	langChange := tr.File(FileChange{Path: "a.py", PreviousPath: "a.go", Status: ports.FileRenamed, Similarity: 90, Old: []byte(src), New: []byte("x = 1\n")})
	assert.Equal(t, Proceed, langChange.Decision)
	assert.Contains(t, langChange.Reason, "language changed")

	plain := tr.File(FileChange{Path: "cfg/b.yaml", PreviousPath: "cfg/a.yaml", Status: ports.FileRenamed, Similarity: 100, Old: []byte("a: 1"), New: []byte("a: 1")})
	assert.True(t, plain.Rename, "renames of files without a grammar re-key too when unchanged")
}

func TestTriage_NoGrammarNeedsLLM_AndResolution(t *testing.T) {
	tr := newTriage(t)
	v := tr.File(mod("deploy/values.yaml", "replicas: 1\n", "replicas: 3\n"))
	assert.Equal(t, Proceed, v.Decision)
	assert.True(t, v.NeedsLLM)
	assert.Contains(t, v.Reason, ".yaml")

	unchanged := tr.File(mod("Makefile", "all:\n", "all:\n"))
	assert.Equal(t, Abort, unchanged.Decision)
	assert.False(t, unchanged.NeedsLLM)

	assert.Equal(t, Abort, ResolveLLM(v, true, true, "comment only").Decision)
	assert.Equal(t, Proceed, ResolveLLM(v, true, false, "unsure").Decision, "ambiguity resolves to PROCEED")
	r := ResolveLLM(v, false, true, "replica count changed")
	assert.Equal(t, Proceed, r.Decision)
	assert.False(t, r.NeedsLLM)
}

func TestTriage_ParseErrorsStillCompare(t *testing.T) {
	tr := newTriage(t)
	v := tr.File(mod("a.go", "package p\nfunc (", "package p\nfunc ( // x"))
	assert.Equal(t, Abort, v.Decision, "identical broken code with a new comment is still cosmetic")
}

func TestPush_Summary(t *testing.T) {
	tr := newTriage(t)
	src := "package p\nfunc F() {}\n"
	s := tr.Push([]FileChange{
		mod("a.go", "package p\n// x\n", "package p\n// y\n"),
		mod("docs/generated/a.md", "a", "b"),
	})
	assert.Equal(t, Abort, s.Decision)
	assert.Equal(t, "cosmetic changes only", s.Reason)
	assert.Len(t, s.Files, 2)

	s = tr.Push([]FileChange{{Path: "n/a.go", PreviousPath: "o/a.go", Similarity: 100, Old: []byte(src), New: []byte(src), Status: ports.FileRenamed}})
	assert.Equal(t, Abort, s.Decision)
	assert.Contains(t, s.Reason, "only renames")

	s = tr.Push([]FileChange{mod("a.go", src, "package p\nfunc F() { g() }\n"), mod("x.yaml", "a", "b")})
	assert.Equal(t, Proceed, s.Decision)
	assert.Contains(t, s.Reason, "structural changes in a.go")
	assert.Contains(t, s.Reason, "LLM triage needed for x.yaml")

	assert.Equal(t, "no changed files", tr.Push(nil).Reason)
}

func TestDeps_MajorBumpRule(t *testing.T) {
	cases := []struct {
		path, old, new string
		want           Decision
		why            string
	}{
		{"go.mod", "module m\nrequire (\n\tgithub.com/a/b v1.2.0\n)\n", "module m\nrequire (\n\tgithub.com/a/b v1.3.0\n)\n", Abort, "without a major"},
		{"go.mod", "module m\nrequire github.com/a/b v1.9.0\n", "module m\nrequire github.com/a/b/v2 v2.0.0\n", Proceed, "b v1.9.0 → v2.0.0"},
		{"go.mod", "module m\n", "module m\nrequire github.com/new/dep v3.0.0 // indirect\n", Abort, ""},
		{"package.json", `{"dependencies":{"react":"^17.0.2"}}`, `{"dependencies":{"react":"^18.2.0"}}`, Proceed, "react"},
		{"package.json", `{"dependencies":{"react":"^18.1.0"}}`, `{"dependencies":{"react":"^18.2.0"},"devDependencies":{"jest":"29"}}`, Abort, ""},
		{"package.json", `{"dependencies":`, `{}`, Proceed, "could not be parsed"},
		{"pom.xml", `<project><properties><spring.version>5.3.1</spring.version></properties><dependencies><dependency><groupId>org.springframework</groupId><artifactId>core</artifactId><version>${spring.version}</version></dependency></dependencies></project>`,
			`<project><properties><spring.version>6.0.0</spring.version></properties><dependencies><dependency><groupId>org.springframework</groupId><artifactId>core</artifactId><version>${spring.version}</version></dependency></dependencies></project>`, Proceed, "5.3.1 → 6.0.0"},
		{"Cargo.toml", "[dependencies]\nserde = \"1.0\"\ntokio = { version = \"1.2\", features = [\"full\"] }\n", "[dependencies]\nserde = \"1.0.1\"\ntokio = { version = \"2.0\" }\n", Proceed, "tokio"},
		{"Cargo.toml", "[package]\nversion = \"1.0.0\"\n", "[package]\nversion = \"2.0.0\"\n", Abort, ""},
		{"requirements.txt", "django==3.2.1\nrequests>=2.0\n", "django==4.0\nrequests>=2.31\n", Proceed, "django"},
		{"pyproject.toml", "[project]\ndependencies = [\n  \"fastapi>=0.95\",\n  \"pydantic>=1.10\",\n]\n", "[project]\ndependencies = [\n  \"fastapi>=0.110\",\n  \"pydantic>=2.5\",\n]\n", Proceed, "pydantic"},
		{"pyproject.toml", "[tool.poetry.dependencies]\npython = \"^3.10\"\n", "[tool.poetry.dependencies]\npython = \"^3.11\"\n", Abort, ""},
		{"build.gradle", "implementation 'com.google.guava:guava:31.1-jre'", "implementation 'com.google.guava:guava:32.0-jre'", Proceed, "guava"},
	}
	tr := newTriage(t)
	for _, c := range cases {
		v := tr.File(mod(c.path, c.old, c.new))
		assert.Equal(t, c.want, v.Decision, "%s: %s", c.path, v.Reason)
		assert.Contains(t, v.Reason, c.why, c.path)
	}
}

func TestMajor(t *testing.T) {
	for in, want := range map[string]string{"^2.3.1": "2", "v1.9.0": "1", "~=3.4": "3", ">=10": "10", "latest": "", "": ""} {
		assert.Equal(t, want, major(in), in)
	}
}

func TestGlobRegexp(t *testing.T) {
	cases := []struct {
		glob, path string
		match      bool
	}{
		{"**/go.sum", "go.sum", true},
		{"**/go.sum", "a/b/go.sum", true},
		{"**/vendor/**", "vendor/x/y.go", true},
		{"**/vendor/**", "src/vendor/x.go", true},
		{"**/*.min.js", "a/b.min.js", true},
		{"*.go", "a/b.go", false},
		{"*.go", "b.go", true},
		{"docs/?.md", "docs/a.md", true},
		{"docs/?.md", "docs/ab.md", false},
		{".github/**", ".github/workflows/ci.yml", true},
		{"a+b/(x).go", "a+b/(x).go", true},
	}
	for _, c := range cases {
		re, err := GlobRegexp(c.glob)
		require.NoError(t, err)
		assert.Equal(t, c.match, re.MatchString(c.path), "%s vs %s", c.glob, c.path)
	}
	_, err := New(reg, Options{Ignore: []string{"ok"}, IndexOnly: []string{}})
	require.NoError(t, err)
}

func TestTriage_ReasonCapsLongLists(t *testing.T) {
	var old, nw strings.Builder
	old.WriteString("package p\n")
	nw.WriteString("package p\n")
	for _, n := range []string{"A", "B", "C", "D", "E", "F", "G"} {
		nw.WriteString("func " + n + "() {}\n")
	}
	v := newTriage(t).File(mod("a.go", old.String(), nw.String()))
	assert.Contains(t, v.Reason, "and 2 more")
}
