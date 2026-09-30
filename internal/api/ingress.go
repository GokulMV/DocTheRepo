package api

import (
	"context"
	"errors"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"golang.org/x/time/rate"

	"github.com/GokulMV/DocTheRepo/internal/adapters/signal/firehose"
	"github.com/GokulMV/DocTheRepo/internal/core/aggregate"
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

// SignalIngress handles verified signal pushes (Sentry, PagerDuty, Alertmanager, …).
type SignalIngress interface {
	Webhook(ctx context.Context, source, connectorID string, req ports.WebhookRequest) (int, error)
}

func signalHook(s SignalIngress, lim *connectorLimiter) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		source, id := chi.URLParam(r, "source"), chi.URLParam(r, "connector_id")
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
		n, err := s.Webhook(r.Context(), source, id, ports.WebhookRequest{Header: r.Header, Query: r.URL.Query(), Body: body})
		switch {
		case err == nil:
			WriteJSON(w, http.StatusAccepted, map[string]int{"accepted": n})
		case errors.Is(err, ports.ErrNotFound):
			WriteError(w, r, http.StatusNotFound, "UNKNOWN_CONNECTOR", "no enabled "+source+" connector with this ID", nil)
		case errors.Is(err, ports.ErrInvalidSignature):
			observability.Logger(r.Context()).Warn("signal webhook authentication rejected", "connector_id", id, "source", source)
			WriteError(w, r, http.StatusUnauthorized, "INVALID_SIGNATURE", "webhook authentication failed", nil)
		case errors.Is(err, aggregate.ErrOverloaded):
			w.Header().Set("Retry-After", "5")
			WriteError(w, r, http.StatusServiceUnavailable, "OVERLOADED", "the Hub is catching up; retry shortly", nil)
		default:
			var v *ports.ValidationError
			if errors.As(err, &v) {
				WriteError(w, r, http.StatusBadRequest, "MALFORMED_PAYLOAD", v.Message, nil)
				return
			}
			WriteErr(w, r, err)
		}
	}
}

// FirehoseIngress handles Amazon Data Firehose HTTP endpoint deliveries; it returns once events are
// persisted, with the delivery's request ID.
type FirehoseIngress interface {
	Firehose(ctx context.Context, connectorID string, req ports.WebhookRequest) (requestID string, n int, err error)
}

// MaxFirehoseBytes bounds a Firehose delivery (the stream's buffer size is configurable up to 64 MiB; the
// Hub's setup guide recommends 5 MiB).
const MaxFirehoseBytes = 64 << 20

// firehoseHook answers in Firehose's own contract: 200 {requestId, timestamp} when delivered, any other
// status with errorMessage makes Firehose retry and, after its retry window, back up to S3.
func firehoseHook(f FirehoseIngress, lim *connectorLimiter) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "connector_id")
		requestID := r.Header.Get("X-Amz-Firehose-Request-Id")
		reply := func(status int, msg string) {
			if status == http.StatusTooManyRequests || status == http.StatusServiceUnavailable {
				w.Header().Set("Retry-After", "5")
			}
			WriteJSON(w, status, firehose.Response{RequestID: requestID, Timestamp: time.Now().UnixMilli(), ErrorMessage: msg})
		}
		if !lim.allow(id) {
			reply(http.StatusTooManyRequests, "too many deliveries for this connector")
			return
		}
		body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, MaxFirehoseBytes))
		if err != nil {
			var mbe *http.MaxBytesError
			if errors.As(err, &mbe) {
				reply(http.StatusRequestEntityTooLarge, "delivery exceeds 64 MiB; lower the stream's buffer size")
				return
			}
			reply(http.StatusBadRequest, "could not read body")
			return
		}
		rid, n, err := f.Firehose(r.Context(), id, ports.WebhookRequest{Header: r.Header, Query: r.URL.Query(), Body: body})
		if rid != "" {
			requestID = rid
		}
		var v *ports.ValidationError
		switch {
		case err == nil:
			observability.Logger(r.Context()).Debug("firehose delivery persisted", "connector_id", id, "events", n)
			reply(http.StatusOK, "")
		case errors.Is(err, ports.ErrNotFound):
			reply(http.StatusNotFound, "no enabled Firehose or CloudWatch connector with this ID")
		case errors.Is(err, ports.ErrInvalidSignature):
			observability.Logger(r.Context()).Warn("firehose access key rejected", "connector_id", id)
			reply(http.StatusUnauthorized, "access key rejected")
		case errors.Is(err, aggregate.ErrOverloaded), errors.Is(err, context.DeadlineExceeded):
			reply(http.StatusServiceUnavailable, "the Hub is catching up; retry shortly")
		case errors.As(err, &v):
			reply(http.StatusBadRequest, v.Message)
		default:
			observability.Logger(r.Context()).Error("firehose delivery failed", "connector_id", id, "err", err)
			reply(http.StatusInternalServerError, "internal error")
		}
	}
}
