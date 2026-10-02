package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/GokulMV/DocTheRepo/internal/settings"
	"github.com/GokulMV/DocTheRepo/internal/store/storetest"
)

// fakeHub serves the API calls the CLI makes and records requests.
type fakeHub struct {
	*httptest.Server
	reqs []string
	body map[string]map[string]any
}

func newFakeHub(t *testing.T) *fakeHub {
	h := &fakeHub{body: map[string]map[string]any{}}
	h.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := r.Method + " " + strings.TrimPrefix(r.URL.Path, "/api/v1")
		h.reqs = append(h.reqs, key+"?"+r.URL.RawQuery)
		var in map[string]any
		_ = json.NewDecoder(r.Body).Decode(&in)
		h.body[key] = in
		if r.Header.Get("Authorization") != "Bearer dth_pat_ok" {
			w.WriteHeader(http.StatusUnauthorized)
			fmt.Fprint(w, `{"error":{"code":"UNAUTHENTICATED","message":"sign in","correlation_id":"c1"}}`)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		switch key {
		case "GET /me":
			fmt.Fprint(w, `{"email":"ann@acme.com","role":"admin"}`)
		case "GET /repos":
			fmt.Fprint(w, `{"items":[{"id":"r1","full_name":"acme/shop","default_branch":"main","docs_path":"docs/generated/","last_processed_sha":"0123456789ab","enabled":true,"push":{"mode":"pr_auto_merge"}}]}`)
		case "POST /repos":
			fmt.Fprint(w, `{"id":"r2"}`)
		case "GET /jobs":
			fmt.Fprint(w, `{"items":[{"job_id":"j1","type":"code_push","status":"failed","attempts":5,"max_attempts":5,"error":"boom","updated_at":"2026-09-29T10:00:00Z"}]}`)
		case "POST /jobs/j1/retry":
			fmt.Fprint(w, `{"job_id":"j2"}`)
		case "POST /repos/r1/import":
			fmt.Fprint(w, `{"job_id":"j3"}`)
		case "POST /repos/r1/dry-run":
			fmt.Fprint(w, `{"status":"done","message":"dry run: 2 chunks","result":{"documented":2,"estimated_tokens":900,"triage":{"decision":"PROCEED","reason":"signature changed"}}}`)
		case "GET /analytics/usage":
			fmt.Fprint(w, `{"series":[{"key":"qa","points":[{"calls":3,"tokens":1200,"cost_usd":0.5}]}],"totals":{"calls":3,"tokens":1200,"cost_usd":0.5}}`)
		case "POST /tokens":
			fmt.Fprint(w, `{"token":"dth_pat_new"}`)
		case "GET /tokens":
			fmt.Fprint(w, `{"items":[{"id":"t1","name":"cli","created_at":"2026-09-29T10:00:00Z"}]}`)
		case "DELETE /tokens/t1":
			w.WriteHeader(http.StatusNoContent)
		case "POST /reindex":
			fmt.Fprint(w, `{"job_id":"j4","chunks_to_reembed":10,"estimated_tokens":5000}`)
		case "POST /ask":
			if strings.Contains(r.Header.Get("Accept"), "text/event-stream") {
				w.Header().Set("Content-Type", "text/event-stream")
				fmt.Fprint(w, "event: delta\ndata: {\"text\":\"Refunds \"}\n\n")
				fmt.Fprint(w, "event: delta\ndata: {\"text\":\"use the gateway [1].\"}\n\n")
				fmt.Fprint(w, "event: citation\ndata: {\"n\":1}\n\n")
				fmt.Fprint(w, "event: done\ndata: {\"answer\":\"Refunds use the gateway [1].\",\"citations\":[{\"n\":1,\"type\":\"code\",\"repo\":\"acme/shop\",\"path\":\"refund.go\"}]}\n\n")
				return
			}
			fmt.Fprint(w, `{"answer":"x","citations":[]}`)
		default:
			w.WriteHeader(http.StatusNotFound)
			fmt.Fprint(w, `{"error":{"code":"NOT_FOUND","message":"no","correlation_id":"c"}}`)
		}
	}))
	t.Cleanup(h.Close)
	return h
}

func runCLI(t *testing.T, args ...string) (string, error) {
	t.Helper()
	var out, errOut bytes.Buffer
	root := newRoot(&out, &errOut)
	root.SetArgs(args)
	err := root.ExecuteContext(context.Background())
	return out.String(), err
}

func TestCLIAgainstHub(t *testing.T) {
	t.Setenv("DTH_CLI_CONFIG", filepath.Join(t.TempDir(), "cli.json"))
	h := newFakeHub(t)
	base := []string{"--server", h.URL, "--token", "dth_pat_ok"}
	cli := func(args ...string) string {
		t.Helper()
		out, err := runCLI(t, append(args, base...)...)
		require.NoError(t, err, out)
		return out
	}

	assert.Contains(t, cli("login"), "Signed in to "+h.URL+" as ann@acme.com (admin)")
	cfg := loadConfig()
	assert.Equal(t, cliConfig{Server: h.URL, Token: "dth_pat_ok"}, cfg, "login persists the server and token")
	st, _ := os.Stat(configPath())
	assert.Equal(t, os.FileMode(0o600), st.Mode().Perm())

	out, err := runCLI(t, "status") // uses the saved config
	require.NoError(t, err)
	assert.Contains(t, out, "Repos:  1 tracked")
	assert.Contains(t, out, "1 failed")

	out = cli("repos")
	assert.Contains(t, out, "acme/shop")
	assert.Contains(t, out, "01234567")
	assert.Contains(t, cli("repos", "add", "acme/web", "--connector", "c1"), "Tracking acme/web (r2)")
	assert.Contains(t, cli("import", "acme/shop", "--path", "docs"), "job j3")
	assert.Equal(t, []any{"docs"}, h.body["POST /repos/r1/import"]["source_paths"])
	out = cli("dry-run", "acme/shop")
	assert.Contains(t, out, "Triage: PROCEED — signature changed")
	assert.Contains(t, out, "estimated input tokens: 900")
	_, err = runCLI(t, append([]string{"dry-run", "acme/none"}, base...)...)
	assert.ErrorContains(t, err, `no tracked repository "acme/none"`)

	out = cli("ask", "how", "do", "refunds", "work?", "--repo", "acme/shop")
	assert.Contains(t, out, "Refunds use the gateway [1].")
	assert.Contains(t, out, "[1] acme/shop/refund.go (code)")
	assert.Equal(t, "how do refunds work?", h.body["POST /ask"]["question"])
	assert.Equal(t, []any{"r1"}, h.body["POST /ask"]["scope"].(map[string]any)["repo_ids"])

	assert.Contains(t, cli("jobs", "--status", "failed"), "boom")
	assert.Contains(t, cli("retry", "j1", "--override-ceiling"), "Queued job j2")
	assert.Equal(t, true, h.body["POST /jobs/j1/retry"]["override_ceiling"])
	out = cli("usage", "--by", "feature")
	assert.Contains(t, out, "qa")
	assert.Contains(t, out, "$0.50")
	assert.Contains(t, cli("token", "create", "ci"), "dth_pat_new")
	assert.Contains(t, cli("token", "list"), "cli")
	assert.Contains(t, cli("token", "revoke", "t1"), "Revoked.")
	_, err = runCLI(t, append([]string{"reindex"}, base...)...)
	assert.ErrorContains(t, err, "--yes")
	assert.Contains(t, cli("reindex", "--yes"), "10 chunks")
	var js []map[string]any
	require.NoError(t, json.Unmarshal([]byte(cli("repos", "--json")), &js))
	assert.Equal(t, "acme/shop", js[0]["full_name"])

	_, err = runCLI(t, "me", "--server", h.URL)
	assert.Error(t, err)
	_, err = runCLI(t, "status", "--server", h.URL, "--token", "dth_pat_bad")
	assert.ErrorContains(t, err, "UNAUTHENTICATED: sign in (ref c1)")
}

func TestCLINotSignedIn(t *testing.T) {
	t.Setenv("DTH_CLI_CONFIG", filepath.Join(t.TempDir(), "none.json"))
	t.Setenv("DTH_TOKEN", "")
	_, err := runCLI(t, "repos", "--server", "http://127.0.0.1:1")
	assert.ErrorContains(t, err, "not signed in")
}

func TestAdapterTestWithStubEngine(t *testing.T) {
	bin := filepath.Join(t.TempDir(), "stubengine")
	out, err := exec.Command("go", "build", "-o", bin, "../../test/mocks/stubengine").CombinedOutput()
	require.NoError(t, err, string(out))
	got, err := runCLI(t, "adapter-test", bin, "{task_file}", "{result_file}")
	require.NoError(t, err, got)
	assert.Contains(t, got, "Conforms to DocGen v2.")

	t.Setenv("STUB_MODE", "badschema")
	got, err = runCLI(t, "adapter-test", bin, "{task_file}", "{result_file}")
	assert.Error(t, err)
	assert.Contains(t, got, "FAIL")
}

func TestMigrate(t *testing.T) {
	url := storetest.URL(t)
	out, err := runCLI(t, "migrate", "--database-url", url)
	require.NoError(t, err)
	assert.Contains(t, out, "Schema at version")
	out, err = runCLI(t, "migrate", "version", "--database-url", url)
	require.NoError(t, err)
	assert.Contains(t, out, "Schema at version")
	_, err = runCLI(t, "migrate", "force", "x", "--database-url", url)
	assert.Error(t, err)
	t.Setenv("DTH_DATABASE_URL", "")
	_, err = runCLI(t, "migrate")
	assert.ErrorContains(t, err, "DTH_DATABASE_URL")
}

func TestShellQuote(t *testing.T) {
	assert.Equal(t, "plain", shellQuote("plain"))
	assert.Equal(t, `"has space"`, shellQuote("has space"))
	assert.Equal(t, `"say \"hi\""`, shellQuote(`say "hi"`))
}

// dth init turns answers into a settings file the Hub applies at start, and a checklist; secrets are only
// referenced, never asked for.
func TestInitWizard(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "hub.yaml")
	var buf bytes.Buffer
	root := newRoot(&buf, &buf)
	root.SetIn(strings.NewReader("2\nhttps://docs.acme.com/\n3\n2\nhttps://login.microsoftonline.com/<tenant-id>/v2.0\nhttps://login.microsoftonline.com/t1/v2.0\napp-1\nacme.com\nann@acme.com\n\n7\n4\n"))
	root.SetArgs([]string{"init", "-o", out})
	require.NoError(t, root.Execute(), buf.String())
	text := buf.String()
	assert.Contains(t, text, "Callback URL: https://docs.acme.com/api/v1/auth/callback")
	assert.Contains(t, text, "It must be an https URL without <placeholders>.")
	assert.Contains(t, text, "DTH_SECRET_OIDC_CLIENT_SECRET")
	assert.Contains(t, text, "docker compose up -d")

	b, err := os.ReadFile(out)
	require.NoError(t, err)
	doc, err := settings.Load(context.Background(), []settings.Source{{Name: "hub.yaml", Data: b}}, nil)
	require.NoError(t, err, "the file is valid for the Hub")
	require.NotNil(t, doc.Auth.SSO)
	assert.Equal(t, "microsoft", doc.Auth.SSO.Provider)
	assert.Equal(t, "${env:DTH_SECRET_OIDC_CLIENT_SECRET}", doc.Auth.SSO.ClientSecret.Value)
	assert.True(t, *doc.Auth.Password, "both methods")
	assert.Equal(t, []settings.User{{Email: "ann@acme.com", Role: "owner"}}, doc.Users)
	assert.Empty(t, doc.Providers, "model chosen later")

	root = newRoot(&buf, &buf)
	root.SetIn(strings.NewReader(""))
	root.SetArgs([]string{"init", "-o", out})
	assert.ErrorContains(t, root.Execute(), "exists")
}
