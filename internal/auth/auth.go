package auth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/GokulMV/DocTheRepo/internal/config"
	"github.com/GokulMV/DocTheRepo/internal/ports"
	"github.com/GokulMV/DocTheRepo/internal/store"
	"github.com/GokulMV/DocTheRepo/internal/store/gen"
)

// Errors returned to the API layer.
var (
	ErrUnauthenticated  = errors.New("authentication required")
	ErrForbidden        = errors.New("insufficient role")
	ErrBadCredentials   = errors.New("invalid email or password")
	ErrDomainNotAllowed = errors.New("this account's email domain is not allowed")
	ErrLastOwner        = errors.New("the last active owner cannot be demoted or disabled")
)

// Service owns users, sessions, tokens, and authorization lookups.
type Service struct {
	st  *store.Store
	cfg config.AuthConfig
	Now func() time.Time
}

// New returns the auth service.
func New(st *store.Store, cfg config.AuthConfig) *Service {
	return &Service{st: st, cfg: cfg, Now: time.Now}
}

// Config returns the auth configuration.
func (s *Service) Config() config.AuthConfig { return s.cfg }

// User is a user as the API shows it.
type User struct {
	ID          string     `json:"id"`
	Email       string     `json:"email"`
	Name        string     `json:"name"`
	Role        Role       `json:"role"`
	Disabled    bool       `json:"disabled"`
	SSO         bool       `json:"sso"`
	CreatedAt   time.Time  `json:"created_at"`
	LastLoginAt *time.Time `json:"last_login_at,omitempty"`
}

func toUser(u gen.User) User {
	return User{ID: u.ID, Email: u.Email, Name: u.Name, Role: Role(u.Role), Disabled: u.Disabled, SSO: u.OidcSubject != nil,
		CreatedAt: u.CreatedAt, LastLoginAt: u.LastLoginAt}
}

// --- sessions ---

// Session is a new browser session: the raw ID goes into the cookie, the CSRF token to the UI.
type Session struct {
	ID        string
	CSRF      string
	ExpiresAt time.Time
}

// CreateSession starts a session (rotated on every login: a new random ID each time).
func (s *Service) CreateSession(ctx context.Context, userID, ip, userAgent string) (Session, error) {
	now := s.Now()
	sess := Session{ID: randomString(32), CSRF: randomString(24), ExpiresAt: now.Add(s.cfg.SessionAbsolute)}
	err := s.st.Q.CreateSession(ctx, gen.CreateSessionParams{IDHash: hashSecret(sess.ID), UserID: userID, CsrfToken: sess.CSRF,
		ExpiresAt: sess.ExpiresAt, IdleUntil: now.Add(s.cfg.SessionIdle), Ip: ip, UserAgent: truncate(userAgent, 300)})
	return sess, err
}

// SessionPrincipal resolves a session cookie, sliding its idle expiry. Expired or unknown sessions and
// disabled users are ErrUnauthenticated.
func (s *Service) SessionPrincipal(ctx context.Context, rawID string) (*Principal, error) {
	if rawID == "" {
		return nil, ErrUnauthenticated
	}
	h := hashSecret(rawID)
	row, err := s.st.Q.GetSession(ctx, h)
	if store.IsNoRows(err) {
		return nil, ErrUnauthenticated
	}
	if err != nil {
		return nil, err
	}
	now := s.Now()
	if row.Disabled || now.After(row.ExpiresAt) || now.After(row.IdleUntil) {
		_ = s.st.Q.DeleteSession(ctx, h)
		return nil, ErrUnauthenticated
	}
	if next := now.Add(s.cfg.SessionIdle); next.Sub(row.IdleUntil) > time.Minute {
		_ = s.st.Q.SlideSession(ctx, gen.SlideSessionParams{IDHash: h, IdleUntil: minTime(next, row.ExpiresAt)})
	}
	return &Principal{UserID: row.UserID, Email: row.Email, Name: row.Name, Role: Role(row.Role), Via: "session", CSRF: row.CsrfToken, SessionID: h}, nil
}

// EndSession deletes a session.
func (s *Service) EndSession(ctx context.Context, p *Principal) error {
	if p == nil || p.SessionID == nil {
		return nil
	}
	return s.st.Q.DeleteSession(ctx, p.SessionID)
}

// GCSessions deletes expired sessions.
func (s *Service) GCSessions(ctx context.Context) error {
	_, err := s.st.Q.GCSessions(ctx)
	return err
}

// --- personal access tokens ---

// Token is a PAT as listed (never the secret).
type Token struct {
	ID         string     `json:"id"`
	Name       string     `json:"name"`
	Scopes     []string   `json:"scopes"`
	ExpiresAt  *time.Time `json:"expires_at,omitempty"`
	LastUsedAt *time.Time `json:"last_used_at,omitempty"`
	CreatedAt  time.Time  `json:"created_at"`
}

// CreateToken issues a PAT and returns the secret, which is shown once.
func (s *Service) CreateToken(ctx context.Context, userID, name string, ttl time.Duration) (string, Token, error) {
	name = strings.TrimSpace(name)
	if name == "" || len(name) > 100 {
		return "", Token{}, &ports.ValidationError{Code: "VALIDATION_FAILED", Message: "name is required (max 100 characters)"}
	}
	raw := TokenPrefix + randomString(32)
	t := Token{ID: ports.NewID(), Name: name, Scopes: []string{}, CreatedAt: s.Now()}
	if ttl > 0 {
		exp := s.Now().Add(ttl)
		t.ExpiresAt = &exp
	}
	err := s.st.Q.CreateToken(ctx, gen.CreateTokenParams{ID: t.ID, UserID: userID, Name: name, TokenHash: hashSecret(raw), Scopes: t.Scopes, ExpiresAt: t.ExpiresAt})
	return raw, t, err
}

// TokenPrincipal resolves a bearer token.
func (s *Service) TokenPrincipal(ctx context.Context, raw string) (*Principal, error) {
	if !strings.HasPrefix(raw, TokenPrefix) {
		return nil, ErrUnauthenticated
	}
	row, err := s.st.Q.GetTokenByHash(ctx, hashSecret(raw))
	if store.IsNoRows(err) {
		return nil, ErrUnauthenticated
	}
	if err != nil {
		return nil, err
	}
	if row.Disabled || (row.ExpiresAt != nil && s.Now().After(*row.ExpiresAt)) {
		return nil, ErrUnauthenticated
	}
	_ = s.st.Q.TouchToken(ctx, row.ID)
	return &Principal{UserID: row.UserID, Email: row.Email, Name: row.Name, Role: Role(row.Role), Via: "token"}, nil
}

// ListTokens lists a user's tokens.
func (s *Service) ListTokens(ctx context.Context, userID string) ([]Token, error) {
	rows, err := s.st.Q.ListTokens(ctx, userID)
	if err != nil {
		return nil, err
	}
	out := make([]Token, len(rows))
	for i, r := range rows {
		out[i] = Token{ID: r.ID, Name: r.Name, Scopes: r.Scopes, ExpiresAt: r.ExpiresAt, LastUsedAt: r.LastUsedAt, CreatedAt: r.CreatedAt}
	}
	return out, nil
}

// DeleteToken revokes one of the user's tokens.
func (s *Service) DeleteToken(ctx context.Context, userID, id string) error {
	n, err := s.st.Q.DeleteToken(ctx, gen.DeleteTokenParams{ID: id, UserID: userID})
	if err == nil && n == 0 {
		return ports.ErrNotFound
	}
	return err
}

// --- users ---

// BootstrapOwner creates (or resets the password of) the local-mode owner. Used by `dth up`.
func (s *Service) BootstrapOwner(ctx context.Context, email, password string) (User, error) {
	hash, err := HashPassword(password)
	if err != nil {
		return User{}, &ports.ValidationError{Code: "VALIDATION_FAILED", Message: err.Error()}
	}
	u, err := s.st.Q.GetUserByEmail(ctx, email)
	if store.IsNoRows(err) {
		u, err = s.st.Q.CreateUser(ctx, gen.CreateUserParams{ID: ports.NewID(), Email: email, Name: "Owner", PasswordHash: &hash, Role: gen.UserRoleOwner})
		return toUser(u), err
	}
	if err != nil {
		return User{}, err
	}
	if err := s.st.Q.SetUserPassword(ctx, gen.SetUserPasswordParams{ID: u.ID, PasswordHash: &hash}); err != nil {
		return User{}, err
	}
	role := gen.UserRoleOwner
	u, err = s.st.Q.UpdateUser(ctx, gen.UpdateUserParams{ID: u.ID, Role: &role})
	return toUser(u), err
}

// PasswordLogin checks local credentials. The same error is returned for unknown users and wrong
// passwords, and a dummy hash is verified for unknown users so timing does not reveal which.
func (s *Service) PasswordLogin(ctx context.Context, email, password string) (User, error) {
	u, err := s.st.Q.GetUserByEmail(ctx, strings.TrimSpace(email))
	if store.IsNoRows(err) || (err == nil && (u.PasswordHash == nil || u.Disabled)) {
		VerifyPassword(dummyHash, password)
		return User{}, ErrBadCredentials
	}
	if err != nil {
		return User{}, err
	}
	if !VerifyPassword(*u.PasswordHash, password) {
		return User{}, ErrBadCredentials
	}
	_ = s.st.Q.TouchUserLogin(ctx, gen.TouchUserLoginParams{ID: u.ID})
	return toUser(u), nil
}

var dummyHash, _ = HashPassword("not-a-real-password-for-timing")

// Claims are the OIDC ID token claims the Hub uses.
type Claims struct {
	Subject       string   `json:"sub"`
	Email         string   `json:"email"`
	EmailVerified *bool    `json:"email_verified"`
	Name          string   `json:"name"`
	Groups        []string `json:"-"`
}

// UpsertOIDCUser signs in an IdP user: domain allow-list, verified email, first user ever becomes owner,
// new users are viewers, and IdP groups are synced for group-based repo access.
func (s *Service) UpsertOIDCUser(ctx context.Context, c Claims) (User, error) {
	email := strings.ToLower(strings.TrimSpace(c.Email))
	if c.Subject == "" || email == "" {
		return User{}, &ports.ValidationError{Code: "OIDC_CLAIMS_MISSING", Message: "the identity provider did not return sub and email"}
	}
	if c.EmailVerified != nil && !*c.EmailVerified {
		return User{}, &ports.ValidationError{Code: "EMAIL_NOT_VERIFIED", Message: "verify your email with the identity provider first"}
	}
	if len(s.cfg.OIDC.AllowedDomains) > 0 {
		domain := email[strings.LastIndex(email, "@")+1:]
		ok := false
		for _, d := range s.cfg.OIDC.AllowedDomains {
			ok = ok || strings.EqualFold(d, domain)
		}
		if !ok {
			return User{}, ErrDomainNotAllowed
		}
	}
	var out User
	u, err := s.st.Q.GetUserBySubject(ctx, &c.Subject)
	switch {
	case store.IsNoRows(err):
		u, err = s.st.Q.GetUserByEmail(ctx, email)
		switch {
		case store.IsNoRows(err):
			n, cerr := s.st.Q.CountUsers(ctx)
			if cerr != nil {
				return User{}, cerr
			}
			role := gen.UserRoleViewer
			if n == 0 {
				role = gen.UserRoleOwner
			}
			u, err = s.st.Q.CreateUser(ctx, gen.CreateUserParams{ID: ports.NewID(), Email: email, Name: c.Name, OidcSubject: &c.Subject, Role: role})
			if err != nil {
				return User{}, err
			}
		case err != nil:
			return User{}, err
		default: // existing account (e.g. the local owner) now signing in with SSO: link it
			if err := s.st.Q.LinkUserSubject(ctx, gen.LinkUserSubjectParams{ID: u.ID, Subject: &c.Subject, Name: c.Name}); err != nil {
				return User{}, err
			}
		}
	case err != nil:
		return User{}, err
	}
	if u.Disabled {
		return User{}, ErrUnauthenticated
	}
	if err := s.st.Q.TouchUserLogin(ctx, gen.TouchUserLoginParams{ID: u.ID, Name: c.Name}); err != nil {
		return User{}, err
	}
	if err := s.syncGroups(ctx, u.ID, c.Groups); err != nil {
		return User{}, err
	}
	out = toUser(u)
	if c.Name != "" {
		out.Name = c.Name
	}
	return out, nil
}

func (s *Service) syncGroups(ctx context.Context, userID string, groups []string) error {
	if err := s.st.Q.ClearUserGroups(ctx, userID); err != nil {
		return err
	}
	for _, g := range groups {
		if g = strings.TrimSpace(g); g == "" {
			continue
		}
		gid, err := s.st.Q.UpsertGroup(ctx, gen.UpsertGroupParams{ID: ports.NewID(), Name: g})
		if err != nil {
			return err
		}
		if err := s.st.Q.AddGroupMember(ctx, gen.AddGroupMemberParams{GroupID: gid, UserID: userID}); err != nil {
			return err
		}
	}
	return nil
}

// GetUser loads a user.
func (s *Service) GetUser(ctx context.Context, id string) (User, error) {
	u, err := s.st.Q.GetUser(ctx, id)
	if store.IsNoRows(err) {
		return User{}, ports.ErrNotFound
	}
	return toUser(u), err
}

// ListUsers pages users by email.
func (s *Service) ListUsers(ctx context.Context, after string, limit int) ([]User, error) {
	rows, err := s.st.Q.ListUsers(ctx, gen.ListUsersParams{After: after, Lim: int32(limit)})
	if err != nil {
		return nil, err
	}
	out := make([]User, len(rows))
	for i, r := range rows {
		out[i] = toUser(r)
	}
	return out, nil
}

// UpdateUser changes a user's role or disabled flag. Only an owner may grant or revoke owner, and the
// last active owner cannot be demoted or disabled. Disabling ends the user's sessions.
func (s *Service) UpdateUser(ctx context.Context, actor *Principal, id string, role *Role, disabled *bool) (User, error) {
	target, err := s.st.Q.GetUser(ctx, id)
	if store.IsNoRows(err) {
		return User{}, ports.ErrNotFound
	}
	if err != nil {
		return User{}, err
	}
	touchesOwner := Role(target.Role) == RoleOwner || (role != nil && *role == RoleOwner)
	if touchesOwner && actor.Role != RoleOwner {
		return User{}, ErrForbidden
	}
	losesOwner := Role(target.Role) == RoleOwner && !target.Disabled &&
		((role != nil && *role != RoleOwner) || (disabled != nil && *disabled))
	if losesOwner {
		n, err := s.st.Q.CountOwners(ctx)
		if err != nil {
			return User{}, err
		}
		if n <= 1 {
			return User{}, ErrLastOwner
		}
	}
	p := gen.UpdateUserParams{ID: id}
	if role != nil {
		r := gen.UserRole(*role)
		p.Role = &r
	}
	if disabled != nil {
		p.Disabled = disabled
	}
	u, err := s.st.Q.UpdateUser(ctx, p)
	if err != nil {
		return User{}, err
	}
	if disabled != nil && *disabled {
		_ = s.st.Q.DeleteUserSessions(ctx, id)
	}
	return toUser(u), nil
}

// SetRepoAccess replaces a user's direct repository grants.
func (s *Service) SetRepoAccess(ctx context.Context, userID string, repoIDs []string, level string) error {
	if level == "" {
		level = "read"
	}
	if level != "read" && level != "admin" {
		return &ports.ValidationError{Code: "VALIDATION_FAILED", Message: "level must be read or admin"}
	}
	return s.st.InTx(ctx, func(q *gen.Queries, _ pgx.Tx) error {
		if err := q.ClearRepoAccess(ctx, userID); err != nil {
			return err
		}
		for _, r := range repoIDs {
			if err := q.GrantRepoAccess(ctx, gen.GrantRepoAccessParams{UserID: userID, RepoID: r, Level: gen.RepoAccessLevel(level)}); err != nil {
				return fmt.Errorf("grant %s: %w", r, err)
			}
		}
		return nil
	})
}

// RepoScope resolves what p may read: admins and owners read everything; with
// auth.all_users_read_all_repos (the default) so does everyone; otherwise direct and group grants.
func (s *Service) RepoScope(ctx context.Context, p *Principal) (RepoScope, error) {
	if p == nil {
		return RepoScope{}, ErrUnauthenticated
	}
	if p.Role.AtLeast(RoleAdmin) || s.cfg.AllUsersReadAllRepos {
		return RepoScope{All: true}, nil
	}
	ids, err := s.st.Q.UserRepoIDs(ctx, p.UserID)
	if err != nil {
		return RepoScope{}, err
	}
	return RepoScope{IDs: ids}, nil
}

// --- audit ---

// AuditEntry is one audit record.
type AuditEntry struct {
	ID         string          `json:"id"`
	At         time.Time       `json:"at"`
	ActorID    string          `json:"actor_user_id,omitempty"`
	Action     string          `json:"action"`
	TargetType string          `json:"target_type,omitempty"`
	TargetID   string          `json:"target_id,omitempty"`
	Details    json.RawMessage `json:"details"`
	IP         string          `json:"ip,omitempty"`
}

// Audit records an admin action. Secrets must never be placed in details.
func (s *Service) Audit(ctx context.Context, actor *Principal, action, targetType, targetID string, details any, ip string) error {
	b, err := json.Marshal(details)
	if err != nil || details == nil {
		b = []byte("{}")
	}
	var aid *string
	if actor != nil {
		aid = &actor.UserID
	}
	return s.st.Q.InsertAudit(ctx, gen.InsertAuditParams{ID: ports.NewID(), ActorUserID: aid, Action: action, TargetType: targetType,
		TargetID: targetID, Details: b, Ip: ip})
}

// ListAudit pages the audit log newest first (before = the previous page's last timestamp).
func (s *Service) ListAudit(ctx context.Context, actor, action string, since time.Time, before *time.Time, limit int) ([]AuditEntry, error) {
	var a *string
	if actor != "" {
		a = &actor
	}
	rows, err := s.st.Q.ListAudit(ctx, gen.ListAuditParams{Actor: a, Action: action, Since: since, Before: before, Lim: int32(limit)})
	if err != nil {
		return nil, err
	}
	out := make([]AuditEntry, len(rows))
	for i, r := range rows {
		e := AuditEntry{ID: r.ID, At: r.At, Action: r.Action, TargetType: r.TargetType, TargetID: r.TargetID, Details: r.Details, IP: r.Ip}
		if r.ActorUserID != nil {
			e.ActorID = *r.ActorUserID
		}
		out[i] = e
	}
	return out, nil
}

func minTime(a, b time.Time) time.Time {
	if a.Before(b) {
		return a
	}
	return b
}

func truncate(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}
	return s
}
