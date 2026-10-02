package wiz

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/GokulMV/DocTheRepo/internal/ports"
)

func TestPoller(t *testing.T) {
	tokens, queries := 0, []map[string]any{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/oauth/token":
			require.NoError(t, r.ParseForm())
			if r.Form.Get("client_secret") != "s3cret" || r.Form.Get("audience") != "wiz-api" || r.Form.Get("grant_type") != "client_credentials" {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			tokens++
			_, _ = w.Write([]byte(`{"access_token":"tok-1","expires_in":3600}`))
		case "/graphql":
			if r.Header.Get("Authorization") != "Bearer tok-1" {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			var body struct {
				Query     string         `json:"query"`
				Variables map[string]any `json:"variables"`
			}
			require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
			queries = append(queries, body.Variables)
			if body.Variables["after"] == nil {
				_, _ = w.Write([]byte(`{"data":{"issues":{"nodes":[
					{"id":"i1","status":"OPEN","severity":"HIGH","updatedAt":"2026-10-01T10:00:00Z","sourceRule":{"id":"r1","name":"Public bucket"},
					 "entitySnapshot":{"providerId":"arn:aws:s3:::b1","name":"b1","subscriptionName":"prod"}},
					{"id":"i2","status":"RESOLVED","severity":"LOW","updatedAt":"2026-10-01T12:00:00Z","sourceRule":{"id":"r2","name":"x"}}],
					"pageInfo":{"hasNextPage":true,"endCursor":"c1"}}}}`))
				return
			}
			_, _ = w.Write([]byte(`{"data":{"issues":{"nodes":[
				{"id":"i3","status":"IN_PROGRESS","severity":"CRITICAL","updatedAt":"2026-10-01T11:00:00Z","sourceRule":{"id":"r3","name":"SSH open"},
				 "entitySnapshot":{"id":"vm-1","name":"bastion"}}],"pageInfo":{"hasNextPage":false,"endCursor":""}}}}`))
		}
	}))
	defer srv.Close()
	p := NewPoller()
	p.Now = func() time.Time { return time.Date(2026, 10, 2, 0, 0, 0, 0, time.UTC) }
	cc := ports.ConnectorConfig{ID: "w1", Type: "wiz", Credentials: `{"client_id":"id","client_secret":"s3cret"}`,
		Config: map[string]string{"api_url": srv.URL + "/graphql", "auth_url": srv.URL + "/oauth/token", "severities": "critical, high"}}
	var events []ports.SignalEvent
	var cursor string
	emit := func(stream, c string, evs []ports.SignalEvent) error {
		assert.Equal(t, "issues", stream)
		events, cursor = evs, c
		return nil
	}
	require.NoError(t, p.Poll(context.Background(), cc, map[string]string{}, emit))
	require.Len(t, events, 2, "resolved issues are skipped")
	assert.Equal(t, "Public bucket on b1", events[0].Title)
	assert.Equal(t, ports.SeverityError, events[0].Severity)
	assert.Equal(t, "arn:aws:s3:::b1", events[0].ResourceID)
	assert.Equal(t, ports.SeverityCritical, events[1].Severity)
	assert.Equal(t, "2026-10-01T11:00:00Z", cursor, "the newest kept issue")
	f := queries[0]["filterBy"].(map[string]any)
	assert.Equal(t, []any{"OPEN", "IN_PROGRESS"}, f["status"])
	assert.Equal(t, []any{"CRITICAL", "HIGH"}, f["severity"])
	assert.Equal(t, "2026-10-01T00:00:00Z", f["updatedAt"].(map[string]any)["after"], "first poll looks back 24 hours")
	assert.Equal(t, "c1", queries[1]["after"])

	require.NoError(t, p.Poll(context.Background(), cc, map[string]string{"issues": cursor}, emit))
	assert.Equal(t, 1, tokens, "the access token is cached")
	assert.Equal(t, "2026-10-01T11:00:00Z", queries[2]["filterBy"].(map[string]any)["updatedAt"].(map[string]any)["after"])

	bad := cc
	bad.Credentials = `{"client_id":"id","client_secret":"wrong"}`
	bad.ID = "w2"
	var ve *ports.ValidationError
	require.ErrorAs(t, p.Poll(context.Background(), bad, nil, emit), &ve)
	bad.Credentials = "not json"
	require.ErrorAs(t, p.Poll(context.Background(), bad, nil, emit), &ve)
	bad.Config = map[string]string{}
	require.ErrorAs(t, p.Poll(context.Background(), bad, nil, emit), &ve)
}
