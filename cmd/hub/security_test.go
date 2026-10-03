package main

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/GokulMV/DocTheRepo/internal/core/security"
	"github.com/GokulMV/DocTheRepo/test/mocks/githubmock"
)

const vulnerableLogin = `package auth

import "database/sql"

// Login looks a user up by name.
func Login(db *sql.DB, name string) error {
	rows, err := db.Query("SELECT id FROM users WHERE name = '" + name + "'")
	if err != nil {
		return err
	}
	return rows.Close()
}
`

// The Security section: plan (no model call), scan (attack + verify per kryptonite module), a re-scan of
// unchanged code costs nothing, and only findings someone selected are fixed, in a pull request.
func TestSecurityScanAndFix(t *testing.T) {
	ctx := context.Background()
	env := newSignalEnv(t, "security")
	a, q, c := env.a, env.q, env.c
	gh := githubmock.New()
	defer gh.Close()
	gh.CreateRepo("acme/shop", "main", map[string]string{"internal/auth/login.go": vulnerableLogin, "go.mod": "module shop\n\ngo 1.25\n", "README.md": "# Shop\n"})
	code, out := c.call("POST", "/connectors", map[string]any{"type": "github", "name": "GitHub", "credentials": "ghp_x", "mode": "poll",
		"config": map[string]string{"base_url": gh.APIURL(), "bot_login": gh.BotLogin}})
	require.Equal(t, http.StatusCreated, code, out)
	code, out = c.call("POST", "/repos", map[string]any{"connector_id": out["id"], "full_name": "acme/shop"})
	require.Equal(t, http.StatusCreated, code, out)
	repoID := out["id"].(string)

	code, out = c.call("GET", "/security/modules", nil)
	require.Equal(t, http.StatusOK, code, out)
	assert.Len(t, out["items"], 11)
	assert.Equal(t, "kryptonite", out["source"].(map[string]any)["name"])

	// Phase 2 gate: the plan says what will be read and roughly what it costs, without a model call.
	code, out = c.call("POST", "/security/repos/"+repoID+"/plan", map[string]any{"modules": []string{"security-pentest"}})
	require.Equal(t, http.StatusOK, code, out)
	assert.Equal(t, []any{"internal/auth/login.go"}, out["files"].(map[string]any)["security-pentest"])
	assert.Positive(t, out["estimated_tokens"])
	assert.Zero(t, env.stub.Calls("security"))
	code, out = c.call("POST", "/security/repos/"+repoID+"/plan", map[string]any{"modules": []string{"load-chaos-resilience"}})
	assert.Equal(t, http.StatusBadRequest, code, "a module that needs a running app is refused")

	code, out = c.call("POST", "/security/repos/"+repoID+"/scans", map[string]any{"modules": []string{"security-pentest"}})
	require.Equal(t, http.StatusAccepted, code, out)
	scanID := out["scan_id"].(string)
	runJob(t, a, q, security.JobScan)
	code, out = c.call("GET", "/security/scans/"+scanID, nil)
	require.Equal(t, http.StatusOK, code, out)
	assert.Equal(t, "done", out["status"])
	assert.Equal(t, "NO-GO", out["verdict"], "an open P0 blocks GO")
	fs := out["findings"].([]any)
	require.Len(t, fs, 1)
	f := fs[0].(map[string]any)
	assert.Equal(t, "confirmed", f["status"])
	assert.Equal(t, "P0", f["priority"], "critical × easy")
	assert.Equal(t, "internal/auth/login.go", f["file"])
	assert.EqualValues(t, 7, f["line_start"])
	assert.Equal(t, "", f["fix_status"], "nothing is fixed until someone asks")
	assert.Empty(t, gh.PRs("acme/shop"))
	calls := env.stub.Calls("security")
	assert.Equal(t, 2, calls, "one attacker and one verifier call")

	// Unchanged code: the module's verified result is reused.
	code, out = c.call("POST", "/security/repos/"+repoID+"/scans", map[string]any{"modules": []string{"security-pentest"}})
	require.Equal(t, http.StatusAccepted, code, out)
	runJob(t, a, q, security.JobScan)
	assert.Equal(t, calls, env.stub.Calls("security"), "a re-scan of unchanged code makes no model call")
	code, out = c.call("GET", "/security", nil)
	require.Equal(t, http.StatusOK, code, out)
	require.Len(t, out["items"], 1)

	// Phase 6: the person selects the finding and clicks Fix; phase 7 is a pull request, never a merge.
	code, out = c.call("POST", "/security/repos/"+repoID+"/fix", map[string]any{"finding_ids": []string{f["id"].(string)}})
	require.Equal(t, http.StatusAccepted, code, out)
	assert.EqualValues(t, 1, out["queued"])
	runJob(t, a, q, security.JobFix)
	prs := gh.PRs("acme/shop")
	require.Len(t, prs, 1)
	pr := prs[0]
	assert.True(t, strings.HasPrefix(pr.Head, "dth/security-fix-"))
	assert.Equal(t, "main", pr.Base)
	assert.False(t, pr.Merged)
	assert.Contains(t, pr.Body, "P0 · security-pentest: SQL built from user input")
	assert.Contains(t, pr.Body, "Nothing was merged automatically")
	fixed, ok := gh.File("acme/shop", pr.Head, "internal/auth/login.go")
	require.True(t, ok)
	assert.Contains(t, fixed, "name = $1")
	assert.NotContains(t, fixed, `+ name +`)
	_, ok = gh.File("acme/shop", pr.Head, "internal/auth/login_test.go")
	assert.True(t, ok, "the two tests come with the fix")
	main, _ := gh.File("acme/shop", "main", "internal/auth/login.go")
	assert.Equal(t, vulnerableLogin, main, "the tracked branch is untouched")

	code, out = c.call("GET", "/security/scans/"+scanID, nil)
	require.Equal(t, http.StatusOK, code, out)
	f = out["findings"].([]any)[0].(map[string]any)
	assert.Equal(t, "pr_opened", f["fix_status"])
	assert.NotEmpty(t, f["fix_pr_url"])
	assert.Contains(t, f["fix"].(map[string]any)["risk"], "only the query changes")

	code, out = c.call("POST", "/security/repos/"+repoID+"/fix", map[string]any{"finding_ids": []string{f["id"].(string)}})
	assert.Equal(t, http.StatusConflict, code, "a finding already in a pull request is not fixed twice")
	_ = ctx
}
