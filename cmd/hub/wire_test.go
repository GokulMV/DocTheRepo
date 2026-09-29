package main

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/GokulMV/DocTheRepo/internal/adapters/secrets/localfile"
	"github.com/GokulMV/DocTheRepo/internal/api"
	"github.com/GokulMV/DocTheRepo/internal/config"
	"github.com/GokulMV/DocTheRepo/internal/observability"
	"github.com/GokulMV/DocTheRepo/internal/ports"
	"github.com/GokulMV/DocTheRepo/internal/queue"
	"github.com/GokulMV/DocTheRepo/internal/secrets"
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
