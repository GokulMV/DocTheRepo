package api

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/GokulMV/DocTheRepo/internal/core/aggregate"
	"github.com/GokulMV/DocTheRepo/internal/observability"
	"github.com/GokulMV/DocTheRepo/internal/ports"
)

type fakeSignals struct {
	err  error
	n    int
	last ports.WebhookRequest
}

func (f *fakeSignals) Webhook(_ context.Context, source, id string, req ports.WebhookRequest) (int, error) {
	f.last = req
	return f.n, f.err
}

func TestSignalHookStatuses(t *testing.T) {
	f := &fakeSignals{n: 3}
	h := NewRouter(Deps{Log: slog.New(slog.DiscardHandler), Metrics: observability.NewMetrics(), Signals: f, WebhookPerMinute: 60})
	do := func(body string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/hooks/alertmanager/c1?token=abc", strings.NewReader(body)))
		return rec
	}
	rec := do(`{}`)
	assert.Equal(t, http.StatusAccepted, rec.Code)
	assert.JSONEq(t, `{"accepted":3}`, rec.Body.String())
	assert.Equal(t, "abc", f.last.Query["token"][0], "the query string reaches the adapter (token auth)")

	f.err = aggregate.ErrOverloaded
	rec = do(`{}`)
	assert.Equal(t, http.StatusServiceUnavailable, rec.Code)
	assert.Equal(t, "5", rec.Header().Get("Retry-After"))
	f.err = ports.ErrInvalidSignature
	assert.Equal(t, http.StatusUnauthorized, do(`{}`).Code)
	f.err = &ports.ValidationError{Code: "MALFORMED_PAYLOAD", Message: "x"}
	assert.Equal(t, http.StatusBadRequest, do(`{}`).Code)
	f.err = ports.ErrNotFound
	assert.Equal(t, http.StatusNotFound, do(`{}`).Code)
	f.err = nil
	assert.Equal(t, http.StatusRequestEntityTooLarge, do(strings.Repeat("x", MaxWebhookBytes+1)).Code)

	// The per-connector limit (60/min, burst 6 here) answers 429 once exhausted.
	limited := 0
	for i := 0; i < 20; i++ {
		if do(`{}`).Code == http.StatusTooManyRequests {
			limited++
		}
	}
	assert.Greater(t, limited, 0)
}
