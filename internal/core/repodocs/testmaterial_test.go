package repodocs

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTestNames(t *testing.T) {
	goSrc := "package queue\n\nimport \"testing\"\n\nfunc TestEnqueue(t *testing.T) {\n\tt.Run(\"dedupes by key\", func(t *testing.T) {})\n" +
		"\tfor _, tc := range cases {\n\t\tt.Run(tc.name, func(t *testing.T) {})\n\t}\n}\n\nfunc (s *QueueSuite) TestClaim() {}\n" +
		"func BenchmarkClaim(b *testing.B) {}\nfunc helper(t *testing.T) {}\nfunc FuzzParse(f *testing.F) {}\n"
	assert.Equal(t, []TestName{{5, "TestEnqueue"}, {6, `t.Run("dedupes by key")`}, {8, "t.Run(tc.name)"}, {12, "TestClaim"}, {13, "BenchmarkClaim"}, {15, "FuzzParse"}},
		TestNames("internal/queue/queue_test.go", goSrc))

	tsSrc := "import { Queue } from './queue';\n\ndescribe('Queue', () => {\n  beforeEach(() => {});\n  it(\"retries a failed job\", async () => {});\n" +
		"  test.skip(`drops after ${n} tries`, () => {});\n  describe.each([1, 2])('size %d', () => {});\n});\nsubmit('x');\n"
	assert.Equal(t, []TestName{{3, `describe("Queue")`}, {5, `it("retries a failed job")`}, {6, "test(\"drops after ${n} tries\")"}},
		TestNames("web/src/queue.test.ts", tsSrc))

	pySrc := "import pytest\n\nclass TestQueue:\n    def test_enqueue(self):\n        pass\n\n    def helper(self):\n        pass\n\nasync def test_claim():\n    pass\n"
	assert.Equal(t, []TestName{{3, "class TestQueue"}, {4, "def test_enqueue"}, {10, "def test_claim"}}, TestNames("tests/test_queue.py", pySrc))

	javaSrc := "class QueueTest {\n  @Test\n  void enqueuesOnce() {}\n  void helper() {}\n}\n"
	assert.Equal(t, []TestName{{3, "enqueuesOnce"}}, TestNames("src/test/java/QueueTest.java", javaSrc))
	rbSrc := "RSpec.describe Queue do\n  it \"claims a job\" do\n  end\nend\n"
	assert.Equal(t, []TestName{{2, `it("claims a job")`}}, TestNames("spec/queue_spec.rb", rbSrc))
}

func TestTestStem(t *testing.T) {
	for p, want := range map[string]string{"a/queue_test.go": "queue", "tests/test_queue.py": "queue", "src/queue.spec.ts": "queue",
		"src/QueueTest.java": "queue", "x/queue_tests.py": "queue", "spec/queue_spec.rb": "queue"} {
		assert.Equal(t, want, testStem(p), p)
	}
}

// A module guide's material holds the names of its tests with line numbers, and excerpts while the budget
// lasts; fixtures are not read, and the names count as grounded.
func TestModuleTestMaterial(t *testing.T) {
	f := &Facts{
		Files: []File{
			{Path: "internal/queue/queue.go", Lines: 400, Symbols: []Symbol{{Name: "Claim", Path: "internal/queue/queue.go", Line: 10, In: 9}}},
			{Path: "internal/queue/retry.go", Lines: 80},
		},
		AllPaths: []string{"internal/queue/queue.go", "internal/queue/retry.go", "internal/queue/queue_test.go", "internal/queue/retry_test.go",
			"internal/queue/testdata/job.json", "internal/queue/zz_big_test.go", "internal/queue/missing_test.go"},
	}
	var big strings.Builder
	big.WriteString("package queue\n\n")
	for i := range 2000 {
		fmt.Fprintf(&big, "func TestGenerated%04dWithAQuiteLongDescriptiveName(t *testing.T) {\n\tt.Run(\"case\", func(t *testing.T) {})\n}\n", i)
	}
	files := map[string]string{
		"internal/queue/queue_test.go":     "package queue\n\nimport \"testing\"\n\nfunc TestClaimTakesTheOldestJob(t *testing.T) {\n\tt.Run(\"skips locked\", func(t *testing.T) {})\n}\n",
		"internal/queue/retry_test.go":     "package queue\n\nfunc TestRetryBacksOff(t *testing.T) {}\n",
		"internal/queue/testdata/job.json": "{\"secret\": 1}",
		"internal/queue/zz_big_test.go":    big.String(),
	}
	var reads []string
	read := func(_ context.Context, p string) ([]byte, error) {
		reads = append(reads, p)
		if s, ok := files[p]; ok {
			return []byte(s), nil
		}
		return nil, errors.New("not found")
	}
	mods := Split(f, SplitOptions{MinLines: 10, MaxLines: 10000, Target: 25})
	require.Len(t, mods, 1)
	e := newEnv(f, mods, nil, read)

	tm := e.testMaterial(context.Background(), &mods[0])
	assert.LessOrEqual(t, len(tm), testMaterialBytes, "the whole block stays within the budget")
	assert.NotContains(t, reads, "internal/queue/testdata/job.json", "fixtures are not test code")
	assert.Contains(t, tm, "### internal/queue/queue_test.go (7 lines, 2 tests)\n- line 5: TestClaimTakesTheOldestJob\n- line 6: t.Run(\"skips locked\")\n")
	assert.Contains(t, tm, "- line 3: TestRetryBacksOff")
	assert.Contains(t, tm, "### internal/queue/zz_big_test.go (6002 lines, 4000 tests)")
	assert.Contains(t, tm, "more\n", "names past the names budget are counted, not listed")
	assert.Contains(t, tm, "### Excerpt: internal/queue/queue_test.go from line 5\n```\n    5| func TestClaimTakesTheOldestJob(t *testing.T) {\n")
	assert.Contains(t, tm, "Other test files (not shown): internal/queue/testdata/job.json")
	// The test of the most-called file comes first.
	assert.Less(t, strings.Index(tm, "queue_test.go ("), strings.Index(tm, "retry_test.go ("))

	in := e.inputs(context.Background(), Spec{Needs: []string{"module"}}, &mods[0], 100000)
	assert.Contains(t, in, "## Tests\nTest names with their line numbers")

	spec := Spec{Sections: []Section{{Key: "tests", Title: "Tests"}}}
	w := &written{AtAGlance: "Queues jobs."}
	w.Sections = append(w.Sections, struct {
		Key      string `json:"key"`
		Markdown string `json:"markdown"`
	}{"tests", "`TestClaimTakesTheOldestJob` checks ordering [internal/queue/queue_test.go:5]; `TestRetryBacksOff` covers retries."})
	r := check(spec, w, f, f.Known(), in)
	assert.Empty(t, r.unknown["tests"], "test names in the material are grounded")
	assert.Equal(t, 1, r.valid["tests"])
}
