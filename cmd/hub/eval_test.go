package main

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/GokulMV/DocTheRepo/internal/adapters/secrets/localfile"
	"github.com/GokulMV/DocTheRepo/internal/api"
	"github.com/GokulMV/DocTheRepo/internal/config"
	"github.com/GokulMV/DocTheRepo/internal/core/rag"
	"github.com/GokulMV/DocTheRepo/internal/observability"
	"github.com/GokulMV/DocTheRepo/internal/ports"
	"github.com/GokulMV/DocTheRepo/internal/queue"
	"github.com/GokulMV/DocTheRepo/internal/secrets"
	"github.com/GokulMV/DocTheRepo/internal/store/storetest"
	"github.com/GokulMV/DocTheRepo/test/eval"
	"github.com/GokulMV/DocTheRepo/test/fixtures"
	"github.com/GokulMV/DocTheRepo/test/mocks/githubmock"
	"github.com/GokulMV/DocTheRepo/test/mocks/stubllm"
)

// TestRetrievalEval asks the golden questions (test/eval/golden.json) over the fixture repositories through
// the real pipeline and API and scores the cited files. With the deterministic stub model it gates the
// build: a drop of more than 10 points below test/eval/baseline.json fails. Point it at a real model with
// DTH_EVAL_BASE_URL (OpenAI-compatible), DTH_EVAL_API_KEY, DTH_EVAL_MODEL, and DTH_EVAL_EMBED_MODEL to get
// a report without the gate. DTH_EVAL_REPORT=path writes the JSON report; DTH_EVAL_UPDATE_BASELINE=1
// rewrites the baseline from this run.
func TestRetrievalEval(t *testing.T) {
	ctx := context.Background()
	st := storetest.New(t)
	kek, err := localfile.Open(filepath.Join(t.TempDir(), "k"))
	require.NoError(t, err)
	cfg := config.Default()
	cfg.Auth.Mode = "local"
	log := slog.New(slog.DiscardHandler)
	m := observability.NewMetrics()
	q := queue.New(st, queue.Options{})
	a, err := wire(ctx, cfg, st, secrets.NewBox(kek), q, log, m)
	require.NoError(t, err)
	_, err = a.auth.BootstrapOwner(ctx, "owner@acme.com", "correct horse battery staple")
	require.NoError(t, err)
	srv := httptest.NewServer(api.NewRouter(api.Deps{Log: log, Metrics: m, Git: a.ingest, Auth: a.auth, V1: a.v1Routes()}))
	defer srv.Close()
	jar, _ := cookiejar.New(nil)
	c := &apiClient{t: t, base: srv.URL + "/api/v1", c: &http.Client{Jar: jar, Timeout: 5 * time.Minute}}
	_, out := c.call("POST", "/auth/local/login", map[string]string{"email": "owner@acme.com", "password": "correct horse battery staple"})
	c.csrf = out["csrf_token"].(string)

	gh := githubmock.New()
	defer gh.Close()
	repos, err := fixtures.Load(fixtures.Dir())
	require.NoError(t, err)
	names := fixtures.Seed(gh, repos)

	mode, kind, baseURL, key, model, embedModel := "stub", "openai_compat", os.Getenv("DTH_EVAL_BASE_URL"), os.Getenv("DTH_EVAL_API_KEY"), os.Getenv("DTH_EVAL_MODEL"), os.Getenv("DTH_EVAL_EMBED_MODEL")
	if baseURL == "" {
		stub := stubllm.New()
		defer stub.Close()
		baseURL, key, model, embedModel = stub.URL+"/v1", "sk-eval", "stub", "stub-embed"
	} else {
		mode = "provider"
		if k := os.Getenv("DTH_EVAL_KIND"); k != "" {
			kind = k
		}
	}

	code, out := c.call("POST", "/connectors", map[string]any{"type": "github", "name": "GitHub", "credentials": "ghp_eval", "webhook_secret": "s",
		"mode": "poll", "config": map[string]string{"base_url": gh.APIURL()}})
	require.Equal(t, http.StatusCreated, code, out)
	connID := out["id"].(string)
	code, out = c.call("POST", "/providers", map[string]any{"kind": kind, "name": "eval", "base_url": baseURL, "api_key": key})
	require.Equal(t, http.StatusCreated, code, out)
	provID := out["id"].(string)
	for f, mdl := range map[string]string{"qa": model, "embedding": embedModel} {
		code, out = c.call("PUT", "/routes/"+f, map[string]any{"provider_id": provID, "model": mdl})
		require.Equal(t, http.StatusNoContent, code, out)
	}
	// Index code (no docgen route: retrieval is what is measured) and import each repo's Markdown.
	var repoIDs []string
	for _, n := range names {
		code, out = c.call("POST", "/repos", map[string]any{"connector_id": connID, "full_name": n})
		require.Equal(t, http.StatusCreated, code, out)
		repoIDs = append(repoIDs, out["id"].(string))
	}
	code, out = c.call("POST", "/connectors/"+connID+"/sync", nil)
	require.Equal(t, http.StatusAccepted, code, out)
	for range names {
		runJob(t, a, q, ports.JobCodePush)
	}
	for _, id := range repoIDs {
		code, out = c.call("POST", "/repos/"+id+"/import", map[string]any{})
		require.Equal(t, http.StatusAccepted, code, out)
		runJob(t, a, q, ports.JobImportDocs)
	}

	golden, err := eval.Golden()
	require.NoError(t, err)
	var results []eval.Result
	// The engine is called directly: the API's per-user rate limit (30/min) would otherwise dominate the
	// run time; POST /ask adds only ACL scoping (owner: everything) and the answer cache on top.
	for _, g := range golden {
		ans, err := a.rag.Ask(ctx, rag.Query{Question: g.Question, Scope: rag.Scope{All: true}})
		require.NoError(t, err, g.ID)
		var cited []string
		for _, ci := range ans.Citations {
			cited = append(cited, ci.Repo+":"+ci.Path)
		}
		results = append(results, eval.Score(g, cited))
	}
	rep := eval.Summarize(mode, model, results)
	t.Logf("retrieval eval (%s): %d questions, citation precision %.3f, recall %.3f, hit rate %.3f", mode, rep.Questions, rep.Precision, rep.Recall, rep.HitRate)
	for _, r := range results {
		if r.Recall < 1 || r.Precision < 1 {
			t.Logf("  %-7s P=%.2f R=%.2f cited=%v expected=%v", r.ID, r.Precision, r.Recall, r.Cited, r.Expected)
		}
	}
	if p := os.Getenv("DTH_EVAL_REPORT"); p != "" {
		b, _ := json.MarshalIndent(rep, "", "  ")
		require.NoError(t, os.WriteFile(p, b, 0o644))
	}
	if mode != "stub" {
		return // real models are reported, not gated
	}
	if os.Getenv("DTH_EVAL_UPDATE_BASELINE") == "1" {
		b, _ := json.MarshalIndent(eval.Baseline{Precision: rep.Precision, Recall: rep.Recall}, "", "  ")
		require.NoError(t, os.WriteFile(filepath.Join(eval.Dir(), "baseline.json"), append(b, '\n'), 0o644))
		return
	}
	base, err := eval.LoadBaseline()
	require.NoError(t, err, "run once with DTH_EVAL_UPDATE_BASELINE=1 to create test/eval/baseline.json")
	require.NoError(t, eval.Check(rep, base))
}
