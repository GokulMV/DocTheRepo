package api

import (
	"context"
	"errors"
	"io"
	"net/http"
	"sync"

	"github.com/go-chi/chi/v5"
	"golang.org/x/time/rate"

	"github.com/GokulMV/DocTheRepo/internal/ingest"
	"github.com/GokulMV/DocTheRepo/internal/observability"
	"github.com/GokulMV/DocTheRepo/internal/ports"
)

// MaxWebhookBytes bounds a webhook body (plan § 7.1).
const MaxWebhookBytes = 5 << 20

// GitIngest handles verified git-host webhooks.
type GitIngest interface {
	GitWebhook(ctx context.Context, kind, connectorID string, hdr http.Header, body []byte) (ingest.Result, error)
}

// connectorLimiter enforces a per-connector request rate (default 6,000/min, bursts allowed because
// alert tools and Firehose batch).
type connectorLimiter struct {
	mu     sync.Mutex
	perMin int
	m      map[string]*rate.Limiter
}

func (l *connectorLimiter) allow(id string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.m == nil {
		l.m = map[string]*rate.Limiter{}
	}
	lim, ok := l.m[id]
	if !ok {
		lim = rate.NewLimiter(rate.Limit(float64(l.perMin)/60), max(1, l.perMin/10))
		l.m[id] = lim
	}
	return lim.Allow()
}

func gitHook(g GitIngest, lim *connectorLimiter) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		kind, id := chi.URLParam(r, "kind"), chi.URLParam(r, "connector_id")
		if !lim.allow(id) {
			w.Header().Set("Retry-After", "1")
			WriteError(w, r, http.StatusTooManyRequests, "RATE_LIMITED", "too many webhook deliveries for this connector", nil)
			return
		}
		body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, MaxWebhookBytes))
		if err != nil {
			var mbe *http.MaxBytesError
			if errors.As(err, &mbe) {
				WriteError(w, r, http.StatusRequestEntityTooLarge, "PAYLOAD_TOO_LARGE", "webhook body exceeds 5 MB", nil)
				return
			}
			WriteError(w, r, http.StatusBadRequest, "MALFORMED_PAYLOAD", "could not read body", nil)
			return
		}
		res, err := g.GitWebhook(r.Context(), kind, id, r.Header, body)
		switch {
		case errors.Is(err, ports.ErrNotFound):
			WriteError(w, r, http.StatusNotFound, "UNKNOWN_CONNECTOR", "no enabled "+kind+" connector with this ID", nil)
			return
		case errors.Is(err, ports.ErrInvalidSignature):
			observability.Logger(r.Context()).Warn("webhook signature rejected", "connector_id", id, "kind", kind)
			WriteError(w, r, http.StatusUnauthorized, "INVALID_SIGNATURE", "webhook signature verification failed", nil)
			return
		case err != nil:
			var v *ports.ValidationError
			if errors.As(err, &v) {
				WriteError(w, r, http.StatusBadRequest, "MALFORMED_PAYLOAD", v.Message, nil)
				return
			}
			WriteErr(w, r, err)
			return
		}
		if res.Accepted {
			observability.Logger(r.Context()).Info("webhook accepted", "connector_id", id, "job_id", res.JobID)
		}
		WriteJSON(w, res.Status, res)
	}
}
