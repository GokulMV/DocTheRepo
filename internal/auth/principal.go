// Package auth authenticates users (OIDC for teams, a single owner password in local mode, personal
// access tokens for the API and CLI), manages sessions, and resolves authorization: the four roles and
// the repository ACL that every read of repository content is filtered by (plan § 12).
package auth

import (
	"context"
	"fmt"
	"slices"
)

// Role is a user role; each includes the ones before it.
type Role string

const (
	RoleViewer Role = "viewer"
	RoleEditor Role = "editor"
	RoleAdmin  Role = "admin"
	RoleOwner  Role = "owner"
)

var rank = map[Role]int{RoleViewer: 1, RoleEditor: 2, RoleAdmin: 3, RoleOwner: 4}

// ParseRole validates a role name.
func ParseRole(s string) (Role, error) {
	r := Role(s)
	if rank[r] == 0 {
		return "", fmt.Errorf("unknown role %q (viewer, editor, admin, owner)", s)
	}
	return r, nil
}

// AtLeast reports whether r includes min.
func (r Role) AtLeast(min Role) bool { return rank[r] >= rank[min] }

// Principal is the authenticated caller.
type Principal struct {
	UserID string `json:"id"`
	Email  string `json:"email"`
	Name   string `json:"name"`
	Role   Role   `json:"role"`
	// Via is "session", "token" or "system" (the Hub itself; see SystemPrincipal).
	Via string `json:"-"`
	// CSRF is the session's CSRF token (cookie-authenticated mutations must echo it).
	CSRF      string `json:"-"`
	SessionID []byte `json:"-"`
}

type ctxKey struct{}

// WithPrincipal attaches p to ctx.
func WithPrincipal(ctx context.Context, p *Principal) context.Context {
	return context.WithValue(ctx, ctxKey{}, p)
}

// FromContext returns the caller, or nil when unauthenticated.
func FromContext(ctx context.Context) *Principal {
	p, _ := ctx.Value(ctxKey{}).(*Principal)
	return p
}

// RepoScope is the set of repositories a caller may read.
type RepoScope struct {
	All bool
	IDs []string
}

// Allows reports whether repoID is readable.
func (s RepoScope) Allows(repoID string) bool { return s.All || slices.Contains(s.IDs, repoID) }

// Restrict narrows a requested set of repo IDs to the readable ones. An empty request means "everything
// readable": nil with All, else the caller's IDs. A request naming an unreadable repo returns ok=false so
// the API can answer 403 SCOPE_FORBIDDEN instead of silently dropping it.
func (s RepoScope) Restrict(requested []string) (ids []string, all bool, ok bool) {
	if len(requested) == 0 {
		if s.All {
			return nil, true, true
		}
		return s.IDs, false, true
	}
	for _, r := range requested {
		if !s.Allows(r) {
			return nil, false, false
		}
	}
	return requested, false, true
}

// SystemPrincipal acts with the owner role for the Hub itself (the settings file applied at startup). It
// exists only inside the process: requests from the network can never carry it.
func SystemPrincipal(name string) *Principal {
	return &Principal{Email: name, Name: name, Role: RoleOwner, Via: "system"}
}
