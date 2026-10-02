package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/GokulMV/DocTheRepo/internal/ingest"
	"github.com/GokulMV/DocTheRepo/internal/ports"
)

// fakeAtlassian serves the Jira Cloud (v3) and Confluence REST endpoints the adapters use.
func fakeAtlassian(t *testing.T, jiraDone *atomic.Bool) *httptest.Server {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") == "" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		issue := func() map[string]any {
			status, cat, updated := "In Progress", "indeterminate", "2026-10-01T10:00:00.000+0000"
			if jiraDone.Load() {
				status, cat, updated = "Done", "done", "2026-10-02T09:00:00.000+0000"
			}
			return map[string]any{"key": "ENG-1", "fields": map[string]any{"summary": "Checkout connection pool exhausted",
				"labels": []string{"known-issue"}, "updated": updated, "issuetype": map[string]any{"name": "Bug"},
				"project":     map[string]any{"key": "ENG"},
				"status":      map[string]any{"name": status, "statusCategory": map[string]any{"key": cat}},
				"description": map[string]any{"type": "doc", "content": []any{map[string]any{"type": "paragraph", "content": []any{map[string]any{"type": "text", "text": "The checkout service runs out of database connections under load."}}}}}}}
		}
		switch {
		case r.URL.Path == "/rest/api/3/myself":
			fmt.Fprint(w, `{"timeZone":"UTC"}`)
		case r.URL.Path == "/rest/api/3/search/jql":
			_ = json.NewEncoder(w).Encode(map[string]any{"issues": []any{issue()}, "isLast": true})
		case r.URL.Path == "/rest/api/3/issue/ENG-1":
			_ = json.NewEncoder(w).Encode(issue())
		case r.URL.Path == "/wiki/rest/api/content/search":
			if strings.Contains(r.URL.Query().Get("cql"), "label =") {
				fmt.Fprint(w, `{"results":[],"_links":{}}`)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"results": []any{map[string]any{"id": "123", "type": "page", "status": "current",
				"title": "Checkout runbook", "space": map[string]any{"key": "ENG"}, "version": map[string]any{"when": "2026-10-01T10:00:00Z"},
				"body":     map[string]any{"storage": map[string]any{"value": "<p>When <strong>checkout</strong> exhausts its pool, scale the pool.</p>"}},
				"metadata": map[string]any{"labels": map[string]any{"results": []any{map[string]any{"name": "runbook"}}}},
				"_links":   map[string]any{"webui": "/spaces/ENG/pages/123/Checkout+runbook"}}}, "_links": map[string]any{}})
		default:
			w.WriteHeader(http.StatusNotFound)
			fmt.Fprint(w, `{}`)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

// TestKnowledgeEndToEnd: Confluence and Jira connectors sync into the Library and Palace, a labelled
// Jira issue becomes a draft known-issue rule with a proposed match, the issue moving to Done flips the
// enabled rule to label_only, and from-link explains a pasted Jira URL.
func TestKnowledgeEndToEnd(t *testing.T) {
	ctx := context.Background()
	env := newSignalEnv(t, "suggest")
	a, st, q, c := env.a, env.st, env.q, env.c
	var done atomic.Bool
	site := fakeAtlassian(t, &done)
	require.NoError(t, a.docs.SeedShelves(ctx)) // the scheduler's seed_library_shelves task

	// A recent checkout issue the Jira ticket describes.
	require.NoError(t, a.signals.Ingest(ctx, []ports.SignalEvent{{Source: "sentry", Kind: ports.KindError, ExternalID: "e1", Service: "checkout",
		Environment: "prod", ExceptionType: "PoolExhausted", Title: "PoolExhausted: connection pool exhausted", Message: "connection pool exhausted"}}))
	require.NoError(t, a.agg.Flush(ctx))

	code, out := c.call("POST", "/connectors", map[string]any{"type": "jira", "name": "jira", "credentials": "tok",
		"config": map[string]string{"base_url": site.URL, "projects": "ENG", "email": "bot@acme.com"}})
	require.Equal(t, http.StatusCreated, code, out)
	jiraID := out["id"].(string)
	code, out = c.call("POST", "/connectors", map[string]any{"type": "confluence", "name": "wiki", "credentials": "tok",
		"config": map[string]string{"base_url": site.URL + "/wiki", "spaces": "ENG", "timezone": "UTC"}})
	require.Equal(t, http.StatusCreated, code, out)

	n, err := a.knowledge.Enqueue(ctx)
	require.NoError(t, err)
	require.Equal(t, 2, n)
	runJob(t, a, q, ports.JobKnowledgeSync)
	runJob(t, a, q, ports.JobKnowledgeSync)
	n, err = a.knowledge.Enqueue(ctx)
	require.NoError(t, err)
	assert.Zero(t, n, "the 15-minute interval has not elapsed")

	// Library: the Jira shelf and the Runbooks shelf (the page is labelled runbook).
	code, out = c.call("GET", "/library/shelves/jira", nil)
	require.Equal(t, http.StatusOK, code, out)
	items := out["items"].([]any)
	require.Len(t, items, 1)
	assert.Equal(t, "ENG-1: Checkout connection pool exhausted", items[0].(map[string]any)["title"])
	code, out = c.call("GET", "/library/shelves/runbooks", nil)
	require.Equal(t, http.StatusOK, code, out)
	assert.Equal(t, site.URL+"/wiki/spaces/ENG/pages/123/Checkout+runbook", out["items"].([]any)[0].(map[string]any)["path"])

	// Chunks are searchable; the labelled issue is now a disabled, label-managed draft with a proposal.
	var chunks int
	require.NoError(t, st.Pool.QueryRow(ctx, `SELECT count(*) FROM chunks WHERE repo_id IS NULL AND source IN ('confluence','jira') AND deleted_at IS NULL`).Scan(&chunks))
	assert.Positive(t, chunks)
	code, out = c.call("GET", "/known-issues", nil)
	require.Equal(t, http.StatusOK, code, out)
	var rule map[string]any
	for _, it := range out["items"].([]any) {
		if m := it.(map[string]any); m["jira_key"] == "ENG-1" {
			rule = m
		}
	}
	require.NotNil(t, rule, "the labelled issue was imported: %v", out)
	assert.Equal(t, false, rule["enabled"])
	assert.Equal(t, true, rule["label_managed"])
	assert.Equal(t, "jira", rule["source"])
	assert.Equal(t, site.URL+"/browse/ENG-1", rule["ticket_url"])
	assert.NotEmpty(t, rule["match"].(map[string]any)["fingerprints"], "the suggest route proposed the checkout issue")

	// A human enables it; the issue then moves to Done upstream.
	ruleID := rule["id"].(string)
	code, out = c.call("PATCH", "/known-issues/"+ruleID, map[string]any{"enabled": true})
	require.Equal(t, http.StatusOK, code, out)
	done.Store(true)
	code, out = c.call("POST", "/connectors/"+jiraID+"/sync", nil) // "Sync now"
	require.Equal(t, http.StatusAccepted, code, out)
	assert.Len(t, out["job_ids"], 1)
	runJob(t, a, q, ports.JobKnowledgeSync)
	code, out = c.call("GET", "/known-issues/"+ruleID, nil)
	require.Equal(t, http.StatusOK, code, out)
	assert.Equal(t, "label_only", out["action"], "a fixed bug's errors are no longer hidden")
	assert.Equal(t, ingest.NoteFixedUpstream, out["upstream_note"])
	assert.Equal(t, "Done", out["upstream_status"])

	// from-link: same shape as from-text, plus the fetched document.
	code, out = c.call("POST", "/known-issues/from-link", map[string]any{"url": site.URL + "/browse/ENG-1"})
	require.Equal(t, http.StatusOK, code, out)
	assert.NotEmpty(t, out["explanation"])
	assert.Contains(t, out, "proposed_match")
	link := out["link"].(map[string]any)
	assert.Equal(t, "ENG-1", link["jira_key"])
	assert.Equal(t, true, link["done"])
	assert.Contains(t, link["source_text"], "runs out of database connections")
	code, out = c.call("POST", "/known-issues/from-link", map[string]any{"url": "https://unknown.example.com/browse/X-1"})
	assert.Equal(t, http.StatusBadRequest, code)
	assert.Equal(t, "NO_CONNECTOR", out["error"].(map[string]any)["code"])

	// Saving a from-link proposal keeps its origin.
	code, out = c.call("POST", "/known-issues", map[string]any{"title": "Pool exhaustion (ENG-1)", "reason": "known_bug", "enabled": false,
		"match": map[string]any{"services": []string{"checkout"}}, "source": "confluence", "confluence_page_id": "123"})
	require.Equal(t, http.StatusCreated, code, out)
	assert.Equal(t, "123", out["confluence_page_id"])
	code, _ = c.call("POST", "/known-issues", map[string]any{"title": "x", "reason": "known_bug", "match": map[string]any{"services": []string{"a"}}, "source": "suggested"})
	assert.Equal(t, http.StatusBadRequest, code, "suggested rules come only from suggestions")
}
