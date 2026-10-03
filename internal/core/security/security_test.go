package security

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPriorityMatrix(t *testing.T) {
	// references/severity.md
	assert.Equal(t, "P0", Priority("critical", "moderate"))
	assert.Equal(t, "P1", Priority("critical", "hard"))
	assert.Equal(t, "P0", Priority("medium", "trivial"))
	assert.Equal(t, "P2", Priority("low", "hard"))
	assert.Equal(t, "P3", Priority("info", "hard"))
	assert.Equal(t, "P3", Priority("bogus", "bogus"), "unknown values rank least urgent")
	// Monotonic: never more urgent down a column or along a row.
	for si := 1; si < len(Severities); si++ {
		for _, e := range Exploitabilities {
			assert.GreaterOrEqual(t, Priority(Severities[si], e), Priority(Severities[si-1], e))
		}
	}
}

func TestVerdict(t *testing.T) {
	assert.Equal(t, "GO", Verdict(nil))
	assert.Equal(t, "GO", Verdict([]Finding{{Status: StatusRejected, Priority: "P0"}, {Status: StatusConfirmed, Priority: "P2"}}))
	assert.Equal(t, "NO-GO", Verdict([]Finding{{Status: StatusPlausible, Priority: "P1"}}))
}

func TestModules(t *testing.T) {
	ms := Modules()
	require.Len(t, ms, 11)
	assert.True(t, ms[0].Default, "defaults first")
	m, ok := ModuleByName("security-pentest")
	require.True(t, ok)
	assert.True(t, m.Static && m.Default)
	assert.Contains(t, m.Description, "authentication")
	assert.Contains(t, m.Playbook, "## Probes")
	load, _ := ModuleByName("load-chaos-resilience")
	assert.False(t, load.Static)
	assert.Contains(t, load.Reason, "running test instance")
	assert.Contains(t, Reference("severity"), "| `critical` |")
	assert.Contains(t, attackerSystem(m), "You work statically")
}

func TestReconSelection(t *testing.T) {
	tree := []string{"internal/api/auth_handlers.go", "internal/api/auth_handlers_test.go", "web/node_modules/x/index.js", "go.mod",
		".github/workflows/ci.yml", "deploy/docker-compose.yml", "README.md", "internal/store/orders.go", "docs/generated/a.md", "internal/util/strings.go"}
	paths := PathCandidates(tree, []string{"security-pentest", "supply-chain-deps", "config-and-secrets"})
	assert.Contains(t, paths, "internal/api/auth_handlers.go")
	assert.Contains(t, paths, "go.mod")
	assert.Contains(t, paths, ".github/workflows/ci.yml")
	assert.Contains(t, paths, "deploy/docker-compose.yml")
	assert.NotContains(t, paths, "internal/api/auth_handlers_test.go", "tests are skipped")
	assert.NotContains(t, paths, "web/node_modules/x/index.js")
	assert.NotContains(t, paths, "README.md")

	files := map[string]string{
		"internal/api/auth_handlers.go":    "func login() { query := \"SELECT * FROM users WHERE name='\" + name + \"'\"; db.Query(query) }",
		"internal/util/strings.go":         "func Trim(s string) string { return s }",
		"go.mod":                           "module x\nrequire github.com/a/b v1.0.0",
		strings.Repeat("big/", 1) + "x.go": strings.Repeat("exec(", 10),
	}
	sec := ModuleFiles("security-pentest", files)
	assert.Equal(t, []string{"big/x.go", "internal/api/auth_handlers.go"}, sec, "path match and keyword hits; plain helpers left out")
	assert.Equal(t, []string{"go.mod"}, ModuleFiles("supply-chain-deps", files))
	assert.Nil(t, ModuleFiles("no-such-module", files))
}

func TestFixNeverTouches(t *testing.T) {
	for p, why := range map[string]string{"../etc/passwd": "outside", "/abs": "outside", ".github/workflows/ci.yml": "CI", "package-lock.json": "lockfile",
		"go.sum": "lockfile", "a/b/../../../x": "outside"} {
		assert.Contains(t, forbidden(p), why, p)
	}
	assert.Empty(t, forbidden("internal/api/auth.go"))
	assert.Equal(t, []string{"api/auth_test.go", "api/other_test.go"}, testNeighbours("api/auth.go", []string{"api/auth.go", "api/auth_test.go", "api/other_test.go", "web/x.test.ts"}))
}
