package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/GokulMV/DocTheRepo/internal/observability"
	"github.com/GokulMV/DocTheRepo/internal/ports"
)

var quiet = slog.New(slog.NewTextHandler(io.Discard, nil))

func do(t *testing.T, h http.Handler, method, path string, hdr map[string]string) (*httptest.ResponseRecorder, map[string]any) {
	t.Helper()
	req := httptest.NewRequest(method, path, nil)
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	var body map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	return rec, body
}

func TestHealthz_OK(t *testing.T) {
	h := NewRouter(Deps{Log: quiet, Metrics: observability.NewMetrics()})
	rec, body := do(t, h, http.MethodGet, "/healthz", nil)
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "ok", body["status"])
	assert.NotEmpty(t, rec.Header().Get("X-Correlation-ID"))
}

func TestReadyz_AllChecksPass(t *testing.T) {
	h := NewRouter(Deps{Log: quiet, Checks: map[string]ReadinessCheck{
		"db": func(context.Context) error { return nil },
	}})
	rec, body := do(t, h, http.MethodGet, "/readyz", nil)
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "ready", body["status"])
	assert.Equal(t, map[string]any{"db": "ok"}, body["checks"])
}

func TestReadyz_FailingCheck_503(t *testing.T) {
	h := NewRouter(Deps{Log: quiet, Checks: map[string]ReadinessCheck{
		"db":     func(context.Context) error { return errors.New("down") },
		"vector": func(context.Context) error { return nil },
	}})
	rec, body := do(t, h, http.MethodGet, "/readyz", nil)
	assert.Equal(t, http.StatusServiceUnavailable, rec.Code)
	assert.Equal(t, "not_ready", body["status"])
	assert.Equal(t, []any{"db"}, body["failing"])
}

func TestCorrelationID_EchoedWhenSafe_ReplacedWhenNot(t *testing.T) {
	h := NewRouter(Deps{Log: quiet})
	rec, _ := do(t, h, http.MethodGet, "/healthz", map[string]string{"X-Correlation-ID": "abc-123"})
	assert.Equal(t, "abc-123", rec.Header().Get("X-Correlation-ID"))
	rec, _ = do(t, h, http.MethodGet, "/healthz", map[string]string{"X-Correlation-ID": "bad value\nwith newline"})
	assert.NotEqual(t, "bad value\nwith newline", rec.Header().Get("X-Correlation-ID"))
}

func TestNotFoundAndMethodNotAllowed_UseErrorContract(t *testing.T) {
	h := NewRouter(Deps{Log: quiet})
	rec, body := do(t, h, http.MethodGet, "/nope", nil)
	assert.Equal(t, http.StatusNotFound, rec.Code)
	errObj := body["error"].(map[string]any)
	assert.Equal(t, "NOT_FOUND", errObj["code"])
	assert.NotEmpty(t, errObj["correlation_id"])

	rec, body = do(t, h, http.MethodPost, "/healthz", nil)
	assert.Equal(t, http.StatusMethodNotAllowed, rec.Code)
	assert.Equal(t, "METHOD_NOT_ALLOWED", body["error"].(map[string]any)["code"])
}

func TestWriteErr_MapsTypedErrors(t *testing.T) {
	cases := []struct {
		err    error
		status int
		code   string
	}{
		{ports.Invalid("QUESTION_EMPTY", "question is empty"), 400, "QUESTION_EMPTY"},
		{&ports.SpendBlockedError{Scope: "feature:qa", Reason: "over"}, 402, "SPEND_BLOCKED"},
		{ports.ErrNotFound, 404, "NOT_FOUND"},
		{ports.ErrConflict, 409, "CONFLICT"},
		{ports.Transient(errors.New("x")), 503, "UPSTREAM_UNAVAILABLE"},
		{errors.New("boom secret detail"), 500, "INTERNAL_ERROR"},
	}
	for _, c := range cases {
		r := chi.NewRouter()
		r.Use(correlation(quiet))
		r.Get("/", func(w http.ResponseWriter, r *http.Request) { WriteErr(w, r, c.err) })
		rec, body := do(t, r, http.MethodGet, "/", nil)
		assert.Equal(t, c.status, rec.Code, c.code)
		e := body["error"].(map[string]any)
		assert.Equal(t, c.code, e["code"])
		assert.NotContains(t, e["message"], "secret detail", "internal errors never leak details")
	}
}

func TestRecoverer_PanicBecomes500(t *testing.T) {
	r := chi.NewRouter()
	r.Use(correlation(quiet), recoverer, instrument(observability.NewMetrics()))
	r.Get("/boom", func(http.ResponseWriter, *http.Request) { panic("kaboom") })
	rec, body := do(t, r, http.MethodGet, "/boom", nil)
	require.Equal(t, http.StatusInternalServerError, rec.Code)
	assert.Equal(t, "INTERNAL_ERROR", body["error"].(map[string]any)["code"])
}

func TestMetrics_LabelledByRoutePattern(t *testing.T) {
	m := observability.NewMetrics()
	h := NewRouter(Deps{Log: quiet, Metrics: m})
	do(t, h, http.MethodGet, "/healthz", nil)
	rec := httptest.NewRecorder()
	m.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	assert.Contains(t, rec.Body.String(), `dth_http_requests_total{code="200",route="/healthz"} 1`)
}
