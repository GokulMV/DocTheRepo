package api

import (
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"github.com/GokulMV/DocTheRepo/internal/auth"
	"github.com/GokulMV/DocTheRepo/internal/observability"
)

// SessionCookie holds the raw session ID (only its hash is stored).
const SessionCookie = "dth_session"

// CSRFHeader must echo the session's CSRF token on cookie-authenticated mutations.
const CSRFHeader = "X-CSRF-Token"

// authenticate resolves the caller from a bearer PAT or the session cookie. Unauthenticated requests
// continue without a principal; requireRole rejects them where needed.
func authenticate(svc *auth.Service) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if sp := auth.FromContext(r.Context()); sp != nil && sp.Via == "system" {
				next.ServeHTTP(w, r) // an in-process call by the Hub itself (see auth.SystemPrincipal)
				return
			}
			var (
				p   *auth.Principal
				err error
			)
			if h := r.Header.Get("Authorization"); strings.HasPrefix(h, "Bearer ") {
				p, err = svc.TokenPrincipal(r.Context(), strings.TrimPrefix(h, "Bearer "))
				if err != nil {
					writeAuthErr(w, r, err)
					return
				}
			} else if c, cerr := r.Cookie(SessionCookie); cerr == nil {
				p, err = svc.SessionPrincipal(r.Context(), c.Value)
				if err != nil && !errors.Is(err, auth.ErrUnauthenticated) {
					WriteErr(w, r, err)
					return
				}
			}
			if p != nil {
				if p.Via == "session" && isMutation(r.Method) &&
					subtle.ConstantTimeCompare([]byte(r.Header.Get(CSRFHeader)), []byte(p.CSRF)) != 1 {
					WriteError(w, r, http.StatusForbidden, "CSRF_FAILED", "missing or invalid "+CSRFHeader+" header", nil)
					return
				}
				ctx := auth.WithPrincipal(r.Context(), p)
				ctx = observability.WithLogger(ctx, observability.Logger(ctx).With("user_id", p.UserID))
				r = r.WithContext(ctx)
			}
			next.ServeHTTP(w, r)
		})
	}
}

func isMutation(m string) bool {
	return m != http.MethodGet && m != http.MethodHead && m != http.MethodOptions
}

// requireRole rejects callers below min (401 when unauthenticated).
func requireRole(min auth.Role) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			p := auth.FromContext(r.Context())
			if p == nil {
				writeAuthErr(w, r, auth.ErrUnauthenticated)
				return
			}
			if !p.Role.AtLeast(min) {
				WriteError(w, r, http.StatusForbidden, "FORBIDDEN", "this action requires the "+string(min)+" role", nil)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func writeAuthErr(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, auth.ErrUnauthenticated):
		WriteError(w, r, http.StatusUnauthorized, "UNAUTHENTICATED", "sign in or send a valid personal access token", nil)
	case errors.Is(err, auth.ErrBadCredentials):
		WriteError(w, r, http.StatusUnauthorized, "INVALID_CREDENTIALS", err.Error(), nil)
	case errors.Is(err, auth.ErrForbidden):
		WriteError(w, r, http.StatusForbidden, "FORBIDDEN", err.Error(), nil)
	case errors.Is(err, auth.ErrDomainNotAllowed):
		WriteError(w, r, http.StatusForbidden, "DOMAIN_NOT_ALLOWED", err.Error(), nil)
	case errors.Is(err, auth.ErrLastOwner):
		WriteError(w, r, http.StatusConflict, "LAST_OWNER", err.Error(), nil)
	default:
		WriteErr(w, r, err)
	}
}

// --- pagination ---

// MaxLimit caps page sizes (plan § 7).
const MaxLimit = 200

// Page is the cursor pagination envelope.
type Page[T any] struct {
	Items      []T     `json:"items"`
	NextCursor *string `json:"next_cursor"`
}

// pageParams reads ?limit= (default 50, max 200) and decodes ?cursor= into cur.
func pageParams(r *http.Request, cur any) (int, error) {
	limit := 50
	if v := r.URL.Query().Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 {
			return 0, errBadParam("limit must be a positive integer")
		}
		limit = min(n, MaxLimit)
	}
	if c := r.URL.Query().Get("cursor"); c != "" {
		b, err := base64.RawURLEncoding.DecodeString(c)
		if err != nil || json.Unmarshal(b, cur) != nil {
			return 0, errBadParam("invalid cursor")
		}
	}
	return limit, nil
}

// newPage builds a page; when items filled the limit, next is encoded as the cursor.
func newPage[T any](items []T, limit int, next func(last T) any) Page[T] {
	if items == nil {
		items = []T{}
	}
	p := Page[T]{Items: items}
	if len(items) == limit && limit > 0 {
		b, _ := json.Marshal(next(items[len(items)-1]))
		c := base64.RawURLEncoding.EncodeToString(b)
		p.NextCursor = &c
	}
	return p
}

type paramError struct{ msg string }

func (e *paramError) Error() string { return e.msg }

func errBadParam(msg string) error { return &paramError{msg} }

// decodeJSON reads a JSON body (max 1 MB) into v, rejecting unknown fields.
func decodeJSON(w http.ResponseWriter, r *http.Request, v any) error {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return errBadParam("invalid JSON body: " + err.Error())
	}
	return nil
}

// fail writes err, mapping parameter errors to 400 VALIDATION_FAILED.
func fail(w http.ResponseWriter, r *http.Request, err error) {
	var pe *paramError
	if errors.As(err, &pe) {
		WriteError(w, r, http.StatusBadRequest, "VALIDATION_FAILED", pe.msg, nil)
		return
	}
	writeAuthErr(w, r, err)
}

func clientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		return strings.TrimSpace(strings.Split(xff, ",")[0])
	}
	host := r.RemoteAddr
	if i := strings.LastIndex(host, ":"); i > 0 {
		host = host[:i]
	}
	return host
}

func encodeCursor(b []byte) string { return base64.RawURLEncoding.EncodeToString(b) }

var uuidPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

func uuidLike(s string) bool { return uuidPattern.MatchString(s) }
