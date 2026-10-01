package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/GokulMV/DocTheRepo/internal/api"
	"github.com/GokulMV/DocTheRepo/internal/ports"
)

// TestInboxAPI drives § 7.5 through HTTP: list and filter the Inbox, read an issue with its decode and
// events, acknowledge it, mark another as known (the next event is suppressed), manage and dry-run rules,
// review suggestions, and check that viewers can read but not change anything.
func TestInboxAPI(t *testing.T) {
	ctx := context.Background()
	env := newSignalEnv(t, "decode", "suggest")
	a, st, q, c := env.a, env.st, env.q, env.c

	ingest := func(evs ...ports.SignalEvent) {
		require.NoError(t, a.signals.Ingest(ctx, evs))
		require.NoError(t, a.agg.Flush(ctx))
	}
	ingest(
		ports.SignalEvent{Source: "sentry", Kind: ports.KindError, ExternalID: "e1", Service: "orders", Environment: "prod", ExceptionType: "NullPointerException",
			Title: "NullPointerException in OrderService.refund", Severity: ports.SeverityError},
		ports.SignalEvent{Source: "alertmanager", Kind: ports.KindAlert, ExternalID: "a1", Service: "lb", Environment: "prod", RuleID: "LBHealth",
			Title: "health check failed on lb-1", Severity: ports.SeverityWarning},
	)
	runJob(t, a, q, ports.JobDecodeIssue)
	runJob(t, a, q, ports.JobDecodeIssue)

	list := func(query string) []map[string]any {
		code, out := c.call("GET", "/issues"+query, nil)
		require.Equal(t, http.StatusOK, code, out)
		var items []map[string]any
		for _, it := range out["items"].([]any) {
			items = append(items, it.(map[string]any))
		}
		return items
	}
	all := list("")
	require.Len(t, all, 2)
	assert.Len(t, all[0]["sparkline"], 24)
	assert.Len(t, list("?service=orders"), 1)
	assert.Len(t, list("?severity=error"), 1, "warning-level alerts drop out")
	assert.Len(t, list("?q=refund"), 1)
	assert.Len(t, list("?source=alertmanager"), 1)
	assert.Len(t, list("?since=1h"), 2)
	page := list("?limit=1")
	require.Len(t, page, 1)
	code, out := c.call("GET", "/issues?limit=1", nil)
	require.Equal(t, http.StatusOK, code)
	cur := out["next_cursor"].(string)
	second := list("?limit=1&cursor=" + cur)
	require.Len(t, second, 1)
	assert.NotEqual(t, page[0]["id"], second[0]["id"], "keyset pagination moves on")
	code, _ = c.call("GET", "/issues?status=bogus", nil)
	assert.Equal(t, http.StatusBadRequest, code)

	var npeID, lbID string
	for _, it := range all {
		if it["service"] == "orders" {
			npeID = it["id"].(string)
		} else {
			lbID = it["id"].(string)
		}
	}
	code, det := c.call("GET", "/issues/"+npeID, nil)
	require.Equal(t, http.StatusOK, code, det)
	assert.Equal(t, "decoded", det["status"])
	require.NotNil(t, det["decode"])
	assert.Equal(t, "stub decode of NullPointerException in OrderService.refund", det["decode"].(map[string]any)["summary"])
	assert.Len(t, det["events"], 1)
	code, _ = c.call("GET", "/issues/"+ports.NewID(), nil)
	assert.Equal(t, http.StatusNotFound, code)

	code, out = c.call("PATCH", "/issues/"+npeID, map[string]any{"status": "acknowledged"})
	require.Equal(t, http.StatusOK, code, out)
	assert.Equal(t, "acknowledged", out["status"])
	code, _ = c.call("PATCH", "/issues/"+npeID, map[string]any{"status": "suppressed"})
	assert.Equal(t, http.StatusBadRequest, code, "suppression comes only from a rule")
	code, out = c.call("POST", "/issues/"+npeID+"/decode", map[string]any{"force": true})
	require.Equal(t, http.StatusAccepted, code, out)
	assert.NotEmpty(t, out["job_id"])

	// Mark the health-check alert as known: it leaves the Inbox and its next event is suppressed.
	code, ki := c.call("POST", "/issues/"+lbID+"/mark-known", map[string]any{"title": "LB health checks during deploys", "reason": "expected_noise"})
	require.Equal(t, http.StatusCreated, code, ki)
	assert.Equal(t, true, ki["enabled"])
	assert.Equal(t, []any{"lb"}, ki["match"].(map[string]any)["services"])
	assert.Len(t, list(""), 1, "suppressed issues leave the default Inbox")
	assert.Len(t, list("?status=suppressed"), 1)
	ingest(ports.SignalEvent{Source: "alertmanager", Kind: ports.KindAlert, ExternalID: "a2", Service: "lb", Environment: "prod", RuleID: "LBHealth",
		Title: "health check failed on lb-1", Severity: ports.SeverityWarning})
	var suppressed int64
	require.NoError(t, st.Pool.QueryRow(ctx, `SELECT suppressed_count FROM issues WHERE id = $1`, lbID).Scan(&suppressed))
	assert.Equal(t, int64(1), suppressed, "the rule is live immediately (the handler reloads the matcher)")

	code, out = c.call("POST", "/known-issues/test", map[string]any{"match": map[string]any{"services": []string{"orders"}}})
	require.Equal(t, http.StatusOK, code, out)
	assert.Equal(t, float64(1), out["would_match_last_7d"])
	code, _ = c.call("POST", "/known-issues/test", map[string]any{"match": map[string]any{}})
	assert.Equal(t, http.StatusBadRequest, code, "an empty rule would match everything")

	kiID := ki["id"].(string)
	code, out = c.call("PATCH", "/known-issues/"+kiID, map[string]any{"enabled": false})
	require.Equal(t, http.StatusOK, code, out)
	assert.Equal(t, false, out["enabled"])
	code, out = c.call("GET", "/known-issues", nil)
	require.Equal(t, http.StatusOK, code)
	assert.Len(t, out["items"], 1)
	code, out = c.call("POST", "/known-issues", map[string]any{"title": "Refund NPE (ticket ORD-12)", "reason": "known_bug",
		"match": map[string]any{"fingerprints": []string{all[0]["fingerprint"].(string)}}, "expires_at": time.Now().Add(7 * 24 * time.Hour)})
	require.Equal(t, http.StatusCreated, code, out)
	code, _ = c.call("DELETE", "/known-issues/"+out["id"].(string), nil)
	assert.Equal(t, http.StatusNoContent, code)

	code, out = c.call("POST", "/known-issues/from-text", map[string]any{"text": `"NullPointerException in OrderService.refund" is ticket ORD-12, fix in progress`})
	require.Equal(t, http.StatusOK, code, out)
	assert.Equal(t, "high", out["confidence"])
	code, out = c.call("GET", "/known-issues/suggestions", nil)
	require.Equal(t, http.StatusOK, code, out)
	assert.Empty(t, out["items"])
	code, _ = c.call("POST", "/known-issues/suggestions/"+ports.NewID()+"/accept", nil)
	assert.Equal(t, http.StatusNotFound, code)

	// A viewer reads the Inbox but cannot change it.
	viewerID := ports.NewID()
	_, err := st.Pool.Exec(ctx, `INSERT INTO users (id, email, role) VALUES ($1, 'viewer@acme.com', 'viewer')`, viewerID)
	require.NoError(t, err)
	raw, _, err := a.auth.CreateToken(ctx, viewerID, "test", time.Hour)
	require.NoError(t, err)
	viewer := func(method, path string, body any) int {
		var b []byte
		if body != nil {
			b, _ = json.Marshal(body)
		}
		req, _ := http.NewRequest(method, strings.TrimSuffix(c.base, "/api/v1")+"/api/v1"+path, bytes.NewReader(b))
		req.Header.Set("Authorization", "Bearer "+raw)
		resp, err := http.DefaultClient.Do(req)
		require.NoError(t, err)
		resp.Body.Close()
		return resp.StatusCode
	}
	assert.Equal(t, http.StatusOK, viewer("GET", "/issues", nil))
	assert.Equal(t, http.StatusOK, viewer("GET", "/issues/"+npeID, nil))
	assert.Equal(t, http.StatusOK, viewer("GET", "/known-issues", nil))
	assert.Equal(t, http.StatusForbidden, viewer("PATCH", "/issues/"+npeID, map[string]any{"status": "resolved"}))
	assert.Equal(t, http.StatusForbidden, viewer("POST", "/issues/"+npeID+"/mark-known", map[string]any{}))
	assert.Equal(t, http.StatusForbidden, viewer("POST", "/known-issues/test", map[string]any{"match": map[string]any{"services": []string{"x"}}}))
	_ = api.CSRFHeader

	var actions int
	require.NoError(t, st.Pool.QueryRow(ctx, `SELECT count(*) FROM audit_log WHERE action LIKE 'issue.%' OR action LIKE 'known_issue.%'`).Scan(&actions))
	assert.GreaterOrEqual(t, actions, 6, "every change is audited")
}
