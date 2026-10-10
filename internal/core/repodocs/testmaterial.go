package repodocs

import (
	"bytes"
	"context"
	"fmt"
	"path"
	"regexp"
	"sort"
	"strings"
)

// Test material for a module guide: the names of its tests with line numbers (cheap, and what the Tests
// section is mostly about), then short excerpts while the budget lasts. Tests are not indexed, so they are
// read at the documented commit; the gateway scrubs credentials from them like from every model input.
const (
	maxTestFiles      = 10        // test files read per module, most relevant first
	maxTestExcerpts   = 8         // of those, how many get an excerpt
	maxTestFileBytes  = 512 << 10 // larger test files are generated or data: skipped
	testMaterialBytes = 12000     // the whole block, names and excerpts (~3000 tokens)
	testNamesBytes    = 6000      // the names part of it
	testExcerptBytes  = 2500      // one excerpt
	testExcerptLines  = 60
)

// TestName is one test, suite or case declared in a test file.
type TestName struct {
	Line int
	Name string // as the test framework calls it: TestQueue, t.Run("retries"), describe("Queue"), def test_x
}

var (
	goTestRE  = regexp.MustCompile(`^func\s+(?:\([^)]*\)\s*)?((?:Test|Benchmark|Fuzz|Example)\w*)\s*\(`)
	goRunRE   = regexp.MustCompile(`\b(\w+)\.Run\(\s*([^,()]{1,100}),\s*func\b`)
	jsTestRE  = regexp.MustCompile("^\\s*(describe|context|suite|it|test|specify)(?:\\.\\w+)?\\s*\\(?\\s*(?:'([^']*)'|\"([^\"]*)\"|`([^`]*)`)")
	pyTestRE  = regexp.MustCompile(`^\s*(?:async\s+)?def\s+(test\w*)\s*\(|^\s*class\s+(Test\w*)`)
	annotRE   = regexp.MustCompile(`^\s*(?:@(?:Test|ParameterizedTest|RepeatedTest)\b|\[(?:Test|Fact|Theory|TestMethod|TestCase)\b)`)
	methodRE  = regexp.MustCompile(`\b(?:void|fun|Task|async\s+Task)\s+` + "`?" + `([\w ]+?)` + "`?" + `\s*\(`)
	testExtRE = regexp.MustCompile(`\.(go|py|js|jsx|mjs|cjs|ts|tsx|mts|cts|rb|java|kt|cs)$`)
)

// isTestCode reports a test file holding code (not fixtures or golden data under a test directory).
func isTestCode(p string) bool { return IsTest(p) && testExtRE.MatchString(strings.ToLower(p)) }

// TestNames lists the tests declared in a test file, by its language's conventions: Go Test/Benchmark/
// Fuzz/Example functions and t.Run cases; JavaScript, TypeScript and Ruby describe/it/test blocks; Python
// test_ functions and Test classes; JUnit, Kotlin and .NET methods marked as tests.
func TestNames(p, src string) []TestName {
	ext := strings.TrimPrefix(path.Ext(strings.ToLower(p)), ".")
	var out []TestName
	marked := false
	for i, l := range strings.Split(src, "\n") {
		n := i + 1
		switch ext {
		case "go":
			if m := goTestRE.FindStringSubmatch(l); m != nil {
				out = append(out, TestName{n, m[1]})
			} else if m := goRunRE.FindStringSubmatch(l); m != nil {
				out = append(out, TestName{n, m[1] + ".Run(" + strings.TrimSpace(m[2]) + ")"})
			}
		case "py":
			if m := pyTestRE.FindStringSubmatch(l); m != nil {
				if m[1] != "" {
					out = append(out, TestName{n, "def " + m[1]})
				} else {
					out = append(out, TestName{n, "class " + m[2]})
				}
			}
		case "java", "kt", "cs":
			if annotRE.MatchString(l) {
				marked = true
			} else if m := methodRE.FindStringSubmatch(l); m != nil && marked {
				out = append(out, TestName{n, strings.TrimSpace(m[1])})
				marked = false
			}
		default: // JavaScript, TypeScript, Ruby
			if m := jsTestRE.FindStringSubmatch(l); m != nil {
				out = append(out, TestName{n, fmt.Sprintf("%s(%q)", m[1], m[2]+m[3]+m[4])})
			}
		}
	}
	return out
}

// testStem is the name of the file a test file tests: queue_test.go, test_queue.py, queue.spec.ts and
// QueueTest.java → queue.
func testStem(p string) string {
	b := strings.ToLower(path.Base(p))
	b = strings.TrimSuffix(b, path.Ext(b))
	for _, s := range []string{"_test", ".test", ".spec", "_spec", "tests", "test"} {
		if strings.HasSuffix(b, s) && len(b) > len(s) {
			b = strings.TrimSuffix(b, s)
			break
		}
	}
	return strings.Trim(strings.TrimPrefix(b, "test_"), "_.-")
}

// testFile is one test file read for a module guide.
type testFile struct {
	path  string
	src   string
	lines int
	score int
	names []TestName
}

// testMaterial reads a module's test files (those covering its central files first) and renders their
// test names with line numbers, then excerpts, within testMaterialBytes. Files not read are listed by path.
func (e *env) testMaterial(ctx context.Context, m *Module) string {
	// The weight of a module file: its size and how much it is called.
	weight := map[string]int{}
	for _, p := range m.Files {
		if fl := e.files[p]; fl != nil {
			w := max(fl.Lines, 1)
			for _, s := range fl.Symbols {
				w += 10 * s.In
			}
			weight[testStem(p)] += w
		}
	}
	var cands []testFile
	var other []string
	for _, p := range m.Tests {
		if isTestCode(p) {
			cands = append(cands, testFile{path: p, score: weight[testStem(p)]})
		} else {
			other = append(other, p)
		}
	}
	sort.SliceStable(cands, func(i, j int) bool {
		if cands[i].score != cands[j].score {
			return cands[i].score > cands[j].score
		}
		return cands[i].path < cands[j].path
	})
	var got []testFile
	for _, c := range cands {
		if len(got) == maxTestFiles || e.read == nil {
			other = append(other, c.path)
			continue
		}
		raw, err := e.read(ctx, c.path)
		if err != nil || len(raw) > maxTestFileBytes || bytes.IndexByte(raw[:min(len(raw), 8000)], 0) >= 0 {
			continue
		}
		c.src = string(raw)
		c.lines = strings.Count(strings.TrimRight(c.src, "\n"), "\n") + 1
		c.names = TestNames(c.path, c.src)
		got = append(got, c)
	}
	return renderTests(got, other)
}

// renderTests writes the test names of every file read (cut short only past testNamesBytes), then
// excerpts of the most relevant and largest while testMaterialBytes lasts, then the files not read.
func renderTests(got []testFile, other []string) string {
	var b strings.Builder
	b.WriteString("Test names with their line numbers (cite them as [path:line]):\n")
	for _, f := range got {
		fmt.Fprintf(&b, "### %s (%d lines, %d tests)\n", f.path, f.lines, len(f.names))
		for i, n := range f.names {
			if b.Len() >= testNamesBytes {
				fmt.Fprintf(&b, "- … %d more\n", len(f.names)-i)
				break
			}
			fmt.Fprintf(&b, "- line %d: %s\n", n.Line, clipLine(n.Name, 120))
		}
	}
	var tail strings.Builder
	if len(other) > 0 {
		sort.Strings(other)
		fmt.Fprintf(&tail, "Other test files (not shown): %s\n", clipLine(strings.Join(other, ", "), 600))
	}
	// Excerpts: start at the first test (imports say little), most relevant then largest first.
	ex := append([]testFile(nil), got...)
	sort.SliceStable(ex, func(i, j int) bool {
		if ex[i].score != ex[j].score {
			return ex[i].score > ex[j].score
		}
		return ex[i].lines > ex[j].lines
	})
	shown := 0
	for _, f := range ex {
		room := testMaterialBytes - b.Len() - tail.Len()
		if shown == maxTestExcerpts || room < 600 {
			break
		}
		start := 1
		if len(f.names) > 0 {
			start = f.names[0].Line
		}
		head := fmt.Sprintf("### Excerpt: %s from line %d\n```\n", f.path, start)
		body := excerpt(f.src, start, testExcerptLines, min(testExcerptBytes, room)-len(head)-4)
		if body == "" {
			continue
		}
		b.WriteString(head + body + "```\n")
		shown++
	}
	b.WriteString(tail.String())
	return b.String()
}

// excerpt renders up to maxLines lines from line start, numbered, in at most maxBytes.
func excerpt(src string, start, maxLines, maxBytes int) string {
	lines := strings.Split(strings.TrimRight(src, "\n"), "\n")
	var b strings.Builder
	for i := start - 1; i >= 0 && i < len(lines) && i < start-1+maxLines; i++ {
		l := fmt.Sprintf("%5d| %s\n", i+1, clipLine(lines[i], 200))
		if b.Len()+len(l) > maxBytes {
			break
		}
		b.WriteString(l)
	}
	return b.String()
}

func clipLine(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
