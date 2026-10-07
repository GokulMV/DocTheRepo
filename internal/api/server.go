package api

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"regexp"
	"runtime/debug"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/GokulMV/DocTheRepo/internal/auth"
	"github.com/GokulMV/DocTheRepo/internal/observability"
	"github.com/GokulMV/DocTheRepo/internal/ports"
	"github.com/GokulMV/DocTheRepo/internal/store"
)

// ReadinessCheck probes one dependency; a non-nil error marks the Hub not ready.
type ReadinessCheck func(ctx context.Context) error

// Deps are the collaborators the router needs. Later phases extend this struct.
type Deps struct {
	Log     *slog.Logger
	Metrics *observability.Metrics
	// Checks are run by /readyz, keyed by the name reported in the response.
	Checks map[string]ReadinessCheck
	// Git handles /hooks/github|gitlab/{connector_id}; nil disables git ingress.
	Git GitIngest
	// Signals handles /hooks/{source}/{connector_id} for signal sources; nil disables signal ingress.
	Signals SignalIngress
	// WebhookPerMinute is the per-connector ingress rate (default 6000).
	WebhookPerMinute int
	// Auth enables /api/v1 (nil serves only health and ingress).
	Auth *auth.Service
	// OIDC is single sign-on from the deployment's config (nil: none; owners can still set it up in the UI).
	OIDC *auth.OIDC
	// PublicURL is the Hub's external URL, for the single sign-on callback (empty: from each request).
	PublicURL string
	// SealKeys open secrets the browser sealed to the Hub; RequireSealed refuses plain ones.
	SealKeys      *store.SealKeys
	RequireSealed bool
	// SecureCookies marks cookies Secure (true whenever the public URL is https).
	SecureCookies bool
	// V1 mounts additional authenticated /api/v1 route groups.
	V1 []func(r chi.Router)
	// UI serves the web app for every path the API does not own (nil: API only).
	UI http.Handler
	// Environment names the deployment (nonlive, production, …) for the UI banner.
	Environment string
	// Mailer emails invite and password links (nil: off).
	Mailer Mailer
	// HasIssues reports whether the Issues pages apply: an alert source is connected or an issue exists
	// (nil: always shown).
	HasIssues func(ctx context.Context) bool
	// DocsV2 says docs are written per repository (the Docs page shows documents, not a file tree).
	DocsV2 bool
	// HasSystem reports whether tracked repositories talk to each other (the System page applies).
	HasSystem func(ctx context.Context) bool
	// Settings is the secret-reference policy for /settings/apply (nil: the settings endpoints are off).
	Settings *SettingsPolicy
}

// NewRouter builds the HTTP handler tree.
func NewRouter(d Deps) http.Handler {
	r := chi.NewRouter()
	root := r // the settings endpoints call the API in process through the finished router
	r.Use(securityHeaders, correlation(d.Log), recoverer, instrument(d.Metrics))
	r.Get("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	r.Get("/readyz", readyz(d.Checks))
	perMin := d.WebhookPerMinute
	if perMin <= 0 {
		perMin = 6000
	}
	lim := &connectorLimiter{perMin: perMin}
	if d.Git != nil {
		r.Post("/hooks/{kind:github|gitlab}/{connector_id}", gitHook(d.Git, lim))
	}
	if d.Signals != nil {
		if f, ok := d.Signals.(FirehoseIngress); ok { // a static segment: chi matches it before {source}
			r.Post("/hooks/firehose/{connector_id}", firehoseHook(f, lim))
		}
		r.Post("/hooks/{source}/{connector_id}", signalHook(d.Signals, lim))
	}
	if d.Auth != nil {
		if d.OIDC != nil && d.Auth.OIDC() == nil {
			d.Auth.UseConfigOIDC(d.OIDC) // the caller did not run LoadSignIn (tests)
		}
		ah := &authHandlers{svc: d.Auth, secure: d.SecureCookies, publicURL: d.PublicURL, sealKeys: d.SealKeys, requireSealed: d.RequireSealed, mailer: d.Mailer, environment: d.Environment, hasIssues: d.HasIssues, docsV2: d.DocsV2, hasSystem: d.HasSystem}
		r.Route("/api/v1", func(r chi.Router) {
			r.Get("/openapi.json", openapiHandler)
			r.Group(func(r chi.Router) {
				r.Use(authenticate(d.Auth))
				ah.routes(r)
				for _, mount := range d.V1 {
					mount(r)
				}
				if d.Settings != nil {
					settingsRoutes(*d.Settings, func() http.Handler { return root })(r)
				}
			})
		})
	}
	notFound := func(w http.ResponseWriter, r *http.Request) {
		WriteError(w, r, http.StatusNotFound, "NOT_FOUND", "no such endpoint", nil)
	}
	if d.UI != nil {
		// API and ingress paths keep JSON 404s; everything else is the single-page app.
		r.NotFound(func(w http.ResponseWriter, r *http.Request) {
			if strings.HasPrefix(r.URL.Path, "/api/") || strings.HasPrefix(r.URL.Path, "/hooks/") {
				notFound(w, r)
				return
			}
			d.UI.ServeHTTP(w, r)
		})
	} else {
		r.NotFound(notFound)
	}
	r.MethodNotAllowed(func(w http.ResponseWriter, r *http.Request) {
		WriteError(w, r, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "method not allowed", nil)
	})
	return r
}

// securityHeaders applies baseline browser protections to every response.
func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "same-origin")
		h.Set("X-Frame-Options", "DENY")
		next.ServeHTTP(w, r)
	})
}

func readyz(checks map[string]ReadinessCheck) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
		defer cancel()
		results := make(map[string]string, len(checks))
		var failing []string
		var mu sync.Mutex
		var wg sync.WaitGroup
		for name, check := range checks {
			wg.Add(1)
			go func(name string, check ReadinessCheck) {
				defer wg.Done()
				err := check(ctx)
				mu.Lock()
				defer mu.Unlock()
				if err != nil {
					results[name] = "failing"
					failing = append(failing, name)
					observability.Logger(r.Context()).Warn("readiness check failed", "check", name, "err", err)
					return
				}
				results[name] = "ok"
			}(name, check)
		}
		wg.Wait()
		if len(failing) > 0 {
			WriteJSON(w, http.StatusServiceUnavailable, map[string]any{"status": "not_ready", "failing": failing, "checks": results})
			return
		}
		WriteJSON(w, http.StatusOK, map[string]any{"status": "ready", "checks": results})
	}
}

var safeCorrelation = regexp.MustCompile(`^[A-Za-z0-9._:-]{1,128}$`)

// correlation accepts a caller-supplied X-Correlation-ID (if well-formed) or mints one, echoes it on the
// response, and attaches a request-scoped logger.
func correlation(log *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			id := r.Header.Get("X-Correlation-ID")
			if !safeCorrelation.MatchString(id) {
				id = ports.NewID()
			}
			w.Header().Set("X-Correlation-ID", id)
			ctx := observability.WithCorrelationID(r.Context(), id)
			ctx = observability.WithLogger(ctx, log.With("correlation_id", id, "component", "api"))
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func recoverer(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if p := recover(); p != nil {
				if p == http.ErrAbortHandler {
					panic(p)
				}
				observability.Logger(r.Context()).Error("handler panic", "panic", fmt.Sprint(p), "stack", string(debug.Stack()))
				WriteError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "internal error", nil)
			}
		}()
		next.ServeHTTP(w, r)
	})
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (s *statusRecorder) WriteHeader(code int) {
	s.status = code
	s.ResponseWriter.WriteHeader(code)
}

// Flush keeps streaming (SSE) working through the recorder.
func (s *statusRecorder) Flush() {
	if f, ok := s.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func instrument(m *observability.Metrics) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
			next.ServeHTTP(rec, r)
			// Label by route pattern, never the raw path, so IDs do not explode metric cardinality.
			route := "unmatched"
			if rc := chi.RouteContext(r.Context()); rc != nil && rc.RoutePattern() != "" {
				route = rc.RoutePattern()
			}
			if m != nil {
				m.HTTPRequests.WithLabelValues(route, strconv.Itoa(rec.status)).Inc()
				m.HTTPDuration.WithLabelValues(route).Observe(time.Since(start).Seconds())
			}
			lvl := slog.LevelDebug
			if rec.status >= 500 {
				lvl = slog.LevelError
			}
			observability.Logger(r.Context()).Log(r.Context(), lvl, "http request",
				"method", r.Method, "route", route, "status", rec.status, "duration_ms", time.Since(start).Milliseconds())
		})
	}
}
