package gcplogging

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/GokulMV/DocTheRepo/internal/ports"
)

func TestPollCloudLogging(t *testing.T) {
	var filters []string
	entries := []string{
		`{"insertId":"a1","logName":"projects/acme/logs/run","timestamp":"2026-09-30T11:58:00.100Z","severity":"ERROR","resource":{"type":"cloud_run_revision","labels":{"service_name":"billing"}},"textPayload":"ERROR db timeout"}`,
		`{"insertId":"a2","logName":"projects/acme/logs/run","timestamp":"2026-09-30T11:59:00.2Z","severity":"ERROR","resource":{"type":"cloud_run_revision","labels":{"service_name":"billing"}},"textPayload":"ERROR db timeout again"}`,
	}
	limited := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if limited {
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		var in map[string]any
		_ = json.NewDecoder(r.Body).Decode(&in)
		filters = append(filters, in["filter"].(string))
		assert.Equal(t, []any{"projects/acme"}, in["resourceNames"])
		if in["pageToken"] == nil {
			_, _ = w.Write([]byte(`{"entries":[` + entries[0] + `],"nextPageToken":"p2"}`))
			return
		}
		_, _ = w.Write([]byte(`{"entries":[` + entries[1] + `]}`))
	}))
	defer srv.Close()
	now := time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)
	p := &Poller{Endpoint: srv.URL, Now: func() time.Time { return now },
		Client: func(context.Context, ports.ConnectorConfig) (*http.Client, error) { return srv.Client(), nil }}
	cc := ports.ConnectorConfig{ID: "g1", Type: "gcp", Config: map[string]string{"project": "acme"}}
	cursors := map[string]string{}
	var got []ports.SignalEvent
	emit := func(s, c string, evs []ports.SignalEvent) error {
		cursors[s] = c
		got = append(got, evs...)
		return nil
	}
	require.NoError(t, p.Poll(context.Background(), cc, cursors, emit))
	require.Len(t, got, 2)
	assert.Equal(t, "a1", got[0].ExternalID)
	assert.Contains(t, filters[0], `(severity>=ERROR) AND timestamp>="2026-09-30T11:55:00Z"`)
	assert.Equal(t, "2026-09-30T11:59:00.2Z|a2", cursors["logs:acme"])

	got, filters = nil, nil
	require.NoError(t, p.Poll(context.Background(), cc, cursors, emit))
	assert.Contains(t, filters[0], `timestamp>="2026-09-30T11:59:00.2Z"`)
	require.Len(t, got, 1, "the first page's entry is before the cursor in real life; the fake returns it again")
	assert.Equal(t, "a1", got[0].ExternalID)

	limited = true
	err := p.Poll(context.Background(), cc, cursors, emit)
	tr, ok := ports.AsTransient(err)
	require.True(t, ok, "%v", err)
	assert.Equal(t, time.Minute, tr.RetryAfter)

	var ve *ports.ValidationError
	assert.ErrorAs(t, p.Poll(context.Background(), ports.ConnectorConfig{Config: map[string]string{}}, nil, emit), &ve)
	assert.True(t, strings.HasPrefix(DefaultEndpoint, "https://logging.googleapis.com/"))
}
