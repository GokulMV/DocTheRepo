package auth

import (
	"context"
	"errors"
	"net/url"
	"strings"

	"github.com/GokulMV/DocTheRepo/internal/config"
	"github.com/GokulMV/DocTheRepo/internal/ports"
	"github.com/GokulMV/DocTheRepo/internal/store"
	"github.com/GokulMV/DocTheRepo/internal/store/gen"
)

// Sealer encrypts the stored client secret (the Hub's secrets.Box).
type Sealer interface {
	Seal(ctx context.Context, plaintext, aad []byte) ([]byte, error)
	Open(ctx context.Context, ciphertext, aad []byte) ([]byte, error)
}

// oidcSecretAAD binds the stored client secret to its purpose.
var oidcSecretAAD = []byte("auth_settings:oidc_client_secret")

// liveOIDC is the single sign-on client in use and the email domains it admits.
type liveOIDC struct {
	client  *OIDC
	domains []string
	// fromSettings: built from the settings saved in the UI (not the deployment's config).
	fromSettings bool
}

// OIDC returns the single sign-on client in use, or nil when single sign-on is off.
func (s *Service) OIDC() *OIDC {
	if l := s.oidc.Load(); l != nil {
		return l.client
	}
	return nil
}

// UseConfigOIDC installs the client built from the deployment's config (its allowed domains apply).
func (s *Service) UseConfigOIDC(o *OIDC) {
	if o != nil {
		s.oidc.Store(&liveOIDC{client: o, domains: s.cfg.OIDC.AllowedDomains})
	}
}

func (s *Service) allowedDomains() []string {
	if l := s.oidc.Load(); l != nil {
		return l.domains
	}
	return s.cfg.OIDC.AllowedDomains
}

// SSOSettings is single sign-on as saved in the UI. The client secret is never returned.
type SSOSettings struct {
	Provider       string   `json:"provider"`
	Issuer         string   `json:"issuer"`
	ClientID       string   `json:"client_id"`
	HasSecret      bool     `json:"has_secret"`
	AllowedDomains []string `json:"allowed_domains"`
	GroupsClaim    string   `json:"groups_claim"`
}

// SignInState is what the sign-in settings page shows.
type SignInState struct {
	// Password is whether password sign-in is on now; PasswordDefault is what it is without a setting.
	Password        bool `json:"password"`
	PasswordDefault bool `json:"password_default"`
	PasswordSet     bool `json:"password_set"`
	// SSO is on now, configured either here (Source "settings") or in the deployment's config ("config").
	SSO       bool         `json:"sso"`
	SSOSource string       `json:"sso_source,omitempty"`
	Settings  *SSOSettings `json:"settings,omitempty"`
	// ConfigIssuer is the issuer from the deployment's config, if any (settings saved here take precedence).
	ConfigIssuer string `json:"config_issuer,omitempty"`
}

func (s *Service) settingsRow(ctx context.Context) (gen.AuthSetting, bool, error) {
	row, err := s.st.Q.GetAuthSettings(ctx)
	if store.IsNoRows(err) {
		return gen.AuthSetting{}, false, nil
	}
	return row, err == nil, err
}

// SignIn reports the sign-in settings.
func (s *Service) SignIn(ctx context.Context) (SignInState, error) {
	row, ok, err := s.settingsRow(ctx)
	if err != nil {
		return SignInState{}, err
	}
	st := SignInState{Password: s.PasswordEnabled(ctx), PasswordDefault: s.cfg.Mode == "local", PasswordSet: ok && row.PasswordEnabled != nil}
	if s.cfg.Mode == "oidc" {
		st.ConfigIssuer = s.cfg.OIDC.Issuer
	}
	if ok && row.OidcIssuer != "" {
		domains := row.OidcAllowedDomains
		if domains == nil {
			domains = []string{}
		}
		st.Settings = &SSOSettings{Provider: row.OidcProvider, Issuer: row.OidcIssuer, ClientID: row.OidcClientID, HasSecret: len(row.OidcSecretCiphertext) > 0,
			AllowedDomains: domains, GroupsClaim: row.OidcGroupsClaim}
	}
	if l := s.oidc.Load(); l != nil {
		st.SSO = true
		st.SSOSource = "config"
		if l.fromSettings {
			st.SSOSource = "settings"
		}
	}
	return st, nil
}

// SaveSignIn is a change to the sign-in settings. A nil SSO keeps single sign-on as it is; ClearSSO
// removes the settings saved here. An empty ClientSecret keeps the stored one.
type SaveSignIn struct {
	Password     *bool
	SSO          *SSOSettings
	ClientSecret string
	ClearSSO     bool
}

// ErrLockout refuses a change that would leave no way to sign in.
var ErrLockout = &ports.ValidationError{Code: "LOCKOUT", Message: "this would leave no way to sign in: keep passwords on, or set up single sign-on first"}

// UpdateSignIn saves sign-in settings (owners only, checked by the caller). New single sign-on settings
// are tried first (the issuer's discovery document must load), so a typo never replaces a working setup.
func (s *Service) UpdateSignIn(ctx context.Context, in SaveSignIn) error {
	row, _, err := s.settingsRow(ctx)
	if err != nil {
		return err
	}
	next := gen.UpsertAuthSettingsParams{PasswordEnabled: row.PasswordEnabled, OidcProvider: row.OidcProvider, OidcIssuer: row.OidcIssuer,
		OidcClientID: row.OidcClientID, OidcSecretCiphertext: row.OidcSecretCiphertext, OidcAllowedDomains: row.OidcAllowedDomains,
		OidcGroupsClaim: row.OidcGroupsClaim}
	if next.OidcAllowedDomains == nil {
		next.OidcAllowedDomains = []string{}
	}
	if in.Password != nil {
		next.PasswordEnabled = in.Password
	}
	var built *liveOIDC
	switch {
	case in.ClearSSO:
		next.OidcProvider, next.OidcIssuer, next.OidcClientID, next.OidcSecretCiphertext, next.OidcAllowedDomains, next.OidcGroupsClaim = "", "", "", nil, []string{}, ""
	case in.SSO != nil:
		c := *in.SSO
		c.Issuer = strings.TrimRight(strings.TrimSpace(c.Issuer), "/")
		c.ClientID = strings.TrimSpace(c.ClientID)
		if u, err := url.Parse(c.Issuer); err != nil || u.Scheme != "https" && !(u.Scheme == "http" && isLocalHost(u.Hostname())) || u.Host == "" {
			return &ports.ValidationError{Code: "VALIDATION_FAILED", Message: "the issuer must be an https URL, e.g. https://accounts.google.com"}
		}
		if c.ClientID == "" {
			return &ports.ValidationError{Code: "VALIDATION_FAILED", Message: "enter the client ID"}
		}
		secret := in.ClientSecret
		if secret != "" {
			if s.Box == nil {
				return errors.New("auth: no secret box configured")
			}
			ct, err := s.Box.Seal(ctx, []byte(secret), oidcSecretAAD)
			if err != nil {
				return err
			}
			next.OidcSecretCiphertext = ct
		} else if next.OidcIssuer != "" && next.OidcSecretCiphertext != nil {
			if secret, err = s.openSecret(ctx, next.OidcSecretCiphertext); err != nil {
				return err
			}
		}
		if secret == "" {
			return &ports.ValidationError{Code: "VALIDATION_FAILED", Message: "enter the client secret"}
		}
		domains := cleanDomains(c.AllowedDomains)
		next.OidcProvider, next.OidcIssuer, next.OidcClientID, next.OidcAllowedDomains, next.OidcGroupsClaim = c.Provider, c.Issuer, c.ClientID, domains, strings.TrimSpace(c.GroupsClaim)
		o, err := NewOIDC(ctx, config.OIDCConfig{Issuer: c.Issuer, ClientID: c.ClientID, GroupsClaim: next.OidcGroupsClaim}, secret)
		if err != nil {
			return &ports.ValidationError{Code: "OIDC_DISCOVERY_FAILED", Message: "could not reach the issuer's OpenID configuration (" + c.Issuer +
				"/.well-known/openid-configuration); check the issuer URL. " + err.Error()}
		}
		built = &liveOIDC{client: o, domains: domains, fromSettings: true}
	}

	// The result must leave a way in.
	password := s.cfg.Mode == "local"
	if next.PasswordEnabled != nil {
		password = *next.PasswordEnabled
	}
	sso := built != nil || (!in.ClearSSO && s.oidc.Load() != nil) || (in.ClearSSO && s.configOIDC != nil)
	if !password && !sso {
		return ErrLockout
	}
	if err := s.st.Q.UpsertAuthSettings(ctx, next); err != nil {
		return err
	}
	switch {
	case built != nil:
		s.oidc.Store(built)
	case in.ClearSSO:
		if s.configOIDC != nil {
			s.oidc.Store(&liveOIDC{client: s.configOIDC, domains: s.cfg.OIDC.AllowedDomains})
		} else {
			s.oidc.Store(nil)
		}
	}
	return nil
}

// LoadSignIn installs single sign-on from saved settings at startup; without any, the client from the
// deployment's config (if one) stays in use. configOIDC is that client, or nil.
func (s *Service) LoadSignIn(ctx context.Context, configOIDC *OIDC) error {
	s.configOIDC = configOIDC
	s.UseConfigOIDC(configOIDC)
	row, ok, err := s.settingsRow(ctx)
	if err != nil || !ok || row.OidcIssuer == "" {
		return err
	}
	secret, err := s.openSecret(ctx, row.OidcSecretCiphertext)
	if err != nil {
		return err
	}
	o, err := NewOIDC(ctx, config.OIDCConfig{Issuer: row.OidcIssuer, ClientID: row.OidcClientID, GroupsClaim: row.OidcGroupsClaim}, secret)
	if err != nil {
		return err
	}
	s.oidc.Store(&liveOIDC{client: o, domains: row.OidcAllowedDomains, fromSettings: true})
	return nil
}

func (s *Service) openSecret(ctx context.Context, ct []byte) (string, error) {
	if len(ct) == 0 {
		return "", nil
	}
	if s.Box == nil {
		return "", errors.New("auth: no secret box configured")
	}
	b, err := s.Box.Open(ctx, ct, oidcSecretAAD)
	return string(b), err
}

func cleanDomains(in []string) []string {
	out := []string{}
	seen := map[string]bool{}
	for _, d := range in {
		d = strings.ToLower(strings.TrimPrefix(strings.TrimSpace(d), "@"))
		if d != "" && !seen[d] {
			seen[d] = true
			out = append(out, d)
		}
	}
	return out
}

func isLocalHost(h string) bool { return h == "localhost" || h == "127.0.0.1" || h == "::1" }
