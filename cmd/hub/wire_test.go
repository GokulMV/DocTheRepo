package main

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/GokulMV/DocTheRepo/internal/adapters/secrets/localfile"
	"github.com/GokulMV/DocTheRepo/internal/api"
	"github.com/GokulMV/DocTheRepo/internal/config"
	"github.com/GokulMV/DocTheRepo/internal/core/knownissues"
	"github.com/GokulMV/DocTheRepo/internal/observability"
	"github.com/GokulMV/DocTheRepo/internal/ports"
	"github.com/GokulMV/DocTheRepo/internal/queue"
	"github.com/GokulMV/DocTheRepo/internal/secrets"
	"github.com/GokulMV/DocTheRepo/internal/store"
	"github.com/GokulMV/DocTheRepo/internal/store/storetest"
)

// TestWire builds the whole application against a real database, runs every scheduled task once, and
// checks the webhook route is mounted.
func TestWire(t *testing.T) {
	st := storetest.New(t)
	kek, err := localfile.Open(filepath.Join(t.TempDir(), "k"))
	require.NoError(t, err)
	cfg := config.Default()
	log := slog.New(slog.DiscardHandler)
	m := observability.NewMetrics()
	q := queue.New(st, queue.Options{})
	a, err := wire(context.Background(), cfg, st, secrets.NewBox(kek), q, log, m)
	require.NoError(t, err)
	a.registerHandlers(queue.NewPool(q, st.Pool, queue.PoolOptions{}, log, m))
	for _, task := range a.tasks() {
		assert.NoError(t, task.Fn(context.Background()), task.Name)
	}
	assert.Equal(t, "pgvector", a.index.Kind())

	srv := httptest.NewServer(api.NewRouter(api.Deps{Log: log, Metrics: m, Git: a.ingest}))
	defer srv.Close()
	resp, err := http.Post(srv.URL+"/hooks/github/"+ports.NewID(), "application/json", strings.NewReader("{}"))
	require.NoError(t, err)
	resp.Body.Close()
	assert.Equal(t, http.StatusNotFound, resp.StatusCode, "unknown connector")
	resp, err = http.Post(srv.URL+"/hooks/gitlab/not-a-uuid", "application/json", strings.NewReader("{}"))
	require.NoError(t, err)
	resp.Body.Close()
	assert.Equal(t, http.StatusNotFound, resp.StatusCode, "malformed connector IDs are unknown, not errors")

	cfg.Vector.Backend, cfg.Vector.QdrantURL = "qdrant", "http://127.0.0.1:1"
	b, err := wire(context.Background(), cfg, st, secrets.NewBox(kek), q, log, m)
	require.NoError(t, err)
	assert.Equal(t, "qdrant", b.index.Kind())
}

// TestSignalPipelineWired: events enter through the app's ingest service, are aggregated and flushed, and
// known-issue rules (loaded from the database) suppress matching ones.
func TestSignalPipelineWired(t *testing.T) {
	ctx := context.Background()
	st := storetest.New(t)
	kek, err := localfile.Open(filepath.Join(t.TempDir(), "k"))
	require.NoError(t, err)
	m := observability.NewMetrics()
	a, err := wire(ctx, config.Default(), st, secrets.NewBox(kek), queue.New(st, queue.Options{}), slog.New(slog.DiscardHandler), m)
	require.NoError(t, err)
	_, err = a.knownIssues.Create(ctx, store.KnownIssue{Title: "noisy health checks", Reason: "expected_noise", Enabled: true,
		Match: knownissues.Match{Services: []string{"lb"}, MessageRegex: "health check"}})
	require.NoError(t, err)
	require.NoError(t, a.signals.Reload(ctx))

	var evs []ports.SignalEvent
	for i := 0; i < 100; i++ {
		evs = append(evs, ports.SignalEvent{ConnectorID: ports.NewID(), ExternalID: "e" + string(rune('a'+i%26)) + strings.Repeat("x", i/26), Source: "sentry",
			Kind: ports.KindError, Service: "checkout", ExceptionType: "TimeoutError", Message: "order " + ports.NewID() + " timed out"})
	}
	evs = append(evs, ports.SignalEvent{Source: "alertmanager", Kind: ports.KindAlert, Service: "lb", Title: "health check failed", RuleID: "LBHealth"})
	require.NoError(t, a.signals.Ingest(ctx, evs))
	require.NoError(t, a.agg.Flush(ctx))

	var issues, occurrences, suppressed int64
	require.NoError(t, st.Pool.QueryRow(ctx, `SELECT count(*), sum(occurrences), sum(suppressed_count) FROM issues`).Scan(&issues, &occurrences, &suppressed))
	assert.Equal(t, int64(2), issues, "100 timeouts with different order IDs are one issue")
	assert.Equal(t, int64(100), occurrences)
	assert.Equal(t, int64(1), suppressed)
	assert.Equal(t, 1.0, testutil.ToFloat64(m.IssuesNew.WithLabelValues("new")), "the suppressed issue is not reported for decoding")
	assert.Equal(t, 100.0, testutil.ToFloat64(m.SignalEvents.WithLabelValues("sentry", "accepted")))
	assert.Equal(t, 101.0, testutil.ToFloat64(m.EventsFlushed))
}
