package auth

import (
	"context"
	"net/mail"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/GokulMV/DocTheRepo/internal/ports"
	"github.com/GokulMV/DocTheRepo/internal/store"
	"github.com/GokulMV/DocTheRepo/internal/store/gen"
)

// InviteTTL is how long an invite or password-reset link works.
const InviteTTL = 7 * 24 * time.Hour

// CreateUser adds a person. They sign in with single sign-on (matched by email on their first login) or
// set a password from an invite link. Only an owner may add an owner.
func (s *Service) CreateUser(ctx context.Context, actor *Principal, email, name string, role Role) (User, error) {
	addr, err := mail.ParseAddress(strings.TrimSpace(email))
	if err != nil || addr.Name != "" || !strings.Contains(addr.Address, "@") {
		return User{}, &ports.ValidationError{Code: "VALIDATION_FAILED", Message: "enter an email address like ann@example.com"}
	}
	email = strings.ToLower(addr.Address)
	if role == RoleOwner && actor.Role != RoleOwner {
		return User{}, ErrForbidden
	}
	name = strings.TrimSpace(name)
	if len(name) > 200 {
		return User{}, &ports.ValidationError{Code: "VALIDATION_FAILED", Message: "name is longer than 200 characters"}
	}
	if _, err := s.st.Q.GetUserByEmail(ctx, email); err == nil {
		return User{}, &ports.ValidationError{Code: "USER_EXISTS", Message: email + " is already a user"}
	} else if !store.IsNoRows(err) {
		return User{}, err
	}
	u, err := s.st.Q.CreateUser(ctx, gen.CreateUserParams{ID: ports.NewID(), Email: email, Name: name, Role: gen.UserRole(role)})
	if err != nil {
		return User{}, err
	}
	return toUser(u), nil
}

// CreateInvite makes a one-time link with which the user sets a password: the first one for a new
// user, or a new one (a reset) later. It returns the raw token, which is shown once; only its hash is
// stored. Older open links stay valid until one of them is used.
func (s *Service) CreateInvite(ctx context.Context, actor *Principal, userID string) (string, time.Time, error) {
	u, err := s.st.Q.GetUser(ctx, userID)
	if store.IsNoRows(err) {
		return "", time.Time{}, ports.ErrNotFound
	}
	if err != nil {
		return "", time.Time{}, err
	}
	if Role(u.Role) == RoleOwner && actor.Role != RoleOwner {
		return "", time.Time{}, ErrForbidden // an admin must not be able to take over an owner's account
	}
	if u.Disabled {
		return "", time.Time{}, &ports.ValidationError{Code: "USER_DISABLED", Message: "enable the user first"}
	}
	raw := randomString(32)
	exp := s.Now().Add(InviteTTL)
	var by *string
	if actor.UserID != "" {
		by = &actor.UserID
	}
	if err := s.st.Q.CreateInvite(ctx, gen.CreateInviteParams{TokenHash: hashSecret(raw), UserID: userID, CreatedBy: by, ExpiresAt: exp}); err != nil {
		return "", time.Time{}, err
	}
	return raw, exp, nil
}

// InviteInfo is what the set-password page shows.
type InviteInfo struct {
	Email     string    `json:"email"`
	Name      string    `json:"name"`
	ExpiresAt time.Time `json:"expires_at"`
}

func (s *Service) invite(ctx context.Context, raw string) (gen.GetInviteRow, error) {
	if raw == "" {
		return gen.GetInviteRow{}, ErrInviteInvalid
	}
	inv, err := s.st.Q.GetInvite(ctx, hashSecret(raw))
	if store.IsNoRows(err) {
		return gen.GetInviteRow{}, ErrInviteInvalid
	}
	if err != nil {
		return gen.GetInviteRow{}, err
	}
	if inv.UsedAt != nil || !s.Now().Before(inv.ExpiresAt) || inv.Disabled {
		return gen.GetInviteRow{}, ErrInviteInvalid
	}
	return inv, nil
}

// Invite checks a link without using it.
func (s *Service) Invite(ctx context.Context, raw string) (InviteInfo, error) {
	inv, err := s.invite(ctx, raw)
	if err != nil {
		return InviteInfo{}, err
	}
	return InviteInfo{Email: inv.Email, Name: inv.Name, ExpiresAt: inv.ExpiresAt}, nil
}

// AcceptInvite sets the user's password from a link and retires all their open links. Existing
// sessions end, since a reset usually means the old password should stop working everywhere.
func (s *Service) AcceptInvite(ctx context.Context, raw, password string) (User, error) {
	inv, err := s.invite(ctx, raw)
	if err != nil {
		return User{}, err
	}
	hash, err := HashPassword(password)
	if err != nil {
		return User{}, &ports.ValidationError{Code: "VALIDATION_FAILED", Message: err.Error()}
	}
	var out gen.User
	err = s.st.InTx(ctx, func(q *gen.Queries, _ pgx.Tx) error {
		if err := q.UseInvites(ctx, inv.UserID); err != nil {
			return err
		}
		if err := q.SetUserPassword(ctx, gen.SetUserPasswordParams{ID: inv.UserID, PasswordHash: &hash}); err != nil {
			return err
		}
		if err := q.DeleteUserSessions(ctx, inv.UserID); err != nil {
			return err
		}
		if err := q.TouchUserLogin(ctx, gen.TouchUserLoginParams{ID: inv.UserID}); err != nil {
			return err
		}
		out, err = q.GetUser(ctx, inv.UserID)
		return err
	})
	if err != nil {
		return User{}, err
	}
	return toUser(out), nil
}

// DeleteUser removes a person with their sessions, tokens, questions and grants; audit entries keep the
// action without the actor. Nobody can remove themselves, only an owner can remove an owner, and the
// last active owner stays.
func (s *Service) DeleteUser(ctx context.Context, actor *Principal, id string) error {
	if id == actor.UserID {
		return &ports.ValidationError{Code: "CANNOT_REMOVE_SELF", Message: "you cannot remove yourself; ask another admin"}
	}
	target, err := s.st.Q.GetUser(ctx, id)
	if store.IsNoRows(err) {
		return ports.ErrNotFound
	}
	if err != nil {
		return err
	}
	if Role(target.Role) == RoleOwner {
		if actor.Role != RoleOwner {
			return ErrForbidden
		}
		if !target.Disabled {
			n, err := s.st.Q.CountOwners(ctx)
			if err != nil {
				return err
			}
			if n <= 1 {
				return ErrLastOwner
			}
		}
	}
	n, err := s.st.Q.DeleteUser(ctx, id)
	if err != nil {
		return err
	}
	if n == 0 {
		return ports.ErrNotFound
	}
	return nil
}

// PasswordEnabled reports whether people may sign in with a password: the sign-in setting when an owner
// has set one, otherwise on in local mode and off in OIDC mode.
func (s *Service) PasswordEnabled(ctx context.Context) bool {
	if st, err := s.st.Q.GetAuthSettings(ctx); err == nil && st.PasswordEnabled != nil {
		return *st.PasswordEnabled
	}
	return s.cfg.Mode == "local"
}
