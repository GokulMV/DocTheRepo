package auth

import (
	"context"
	"errors"
	"fmt"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"

	"github.com/GokulMV/DocTheRepo/internal/config"
)

// OIDC is an OpenID Connect relying party (Okta, Google Workspace, Entra ID, Keycloak): authorization
// code flow with PKCE and a nonce.
type OIDC struct {
	oauth       oauth2.Config
	verifier    *oidc.IDTokenVerifier
	groupsClaim string
}

// NewOIDC discovers the issuer and builds the client.
func NewOIDC(ctx context.Context, c config.OIDCConfig, clientSecret string) (*OIDC, error) {
	p, err := oidc.NewProvider(ctx, c.Issuer)
	if err != nil {
		return nil, fmt.Errorf("oidc discovery for %s: %w", c.Issuer, err)
	}
	scopes := c.Scopes
	if len(scopes) == 0 {
		scopes = []string{oidc.ScopeOpenID, "email", "profile"}
	}
	return &OIDC{
		oauth: oauth2.Config{ClientID: c.ClientID, ClientSecret: clientSecret, RedirectURL: c.RedirectURL,
			Endpoint: p.Endpoint(), Scopes: scopes},
		verifier:    p.Verifier(&oidc.Config{ClientID: c.ClientID}),
		groupsClaim: c.GroupsClaim,
	}, nil
}

// LoginState is kept in a short-lived HttpOnly cookie between the redirect and the callback.
type LoginState struct {
	State    string `json:"s"`
	Nonce    string `json:"n"`
	Verifier string `json:"v"`
	Return   string `json:"r,omitempty"`
	// Redirect is the callback URL used for this login when the client has none configured (single
	// sign-on set up in the UI); the token exchange must send the same one.
	Redirect string `json:"d,omitempty"`
}

// NewLoginState creates fresh state, nonce, and PKCE verifier.
func NewLoginState(returnTo string) LoginState {
	return LoginState{State: randomString(24), Nonce: randomString(24), Verifier: oauth2.GenerateVerifier(), Return: returnTo}
}

// HasRedirect reports whether the client has a configured callback URL.
func (o *OIDC) HasRedirect() bool { return o.oauth.RedirectURL != "" }

func (o *OIDC) config(ls LoginState) *oauth2.Config {
	c := o.oauth
	if ls.Redirect != "" {
		c.RedirectURL = ls.Redirect
	}
	return &c
}

// AuthURL is the IdP authorization URL for ls.
func (o *OIDC) AuthURL(ls LoginState) string {
	return o.config(ls).AuthCodeURL(ls.State, oidc.Nonce(ls.Nonce), oauth2.S256ChallengeOption(ls.Verifier))
}

// Exchange redeems the code, verifies the ID token (signature, issuer, audience, expiry, nonce), and
// returns its claims.
func (o *OIDC) Exchange(ctx context.Context, code string, ls LoginState) (Claims, error) {
	tok, err := o.config(ls).Exchange(ctx, code, oauth2.VerifierOption(ls.Verifier))
	if err != nil {
		return Claims{}, fmt.Errorf("oidc code exchange: %w", err)
	}
	raw, ok := tok.Extra("id_token").(string)
	if !ok || raw == "" {
		return Claims{}, errors.New("oidc: no id_token in token response")
	}
	idt, err := o.verifier.Verify(ctx, raw)
	if err != nil {
		return Claims{}, fmt.Errorf("oidc: invalid id_token: %w", err)
	}
	if idt.Nonce != ls.Nonce {
		return Claims{}, errors.New("oidc: nonce mismatch")
	}
	var c Claims
	if err := idt.Claims(&c); err != nil {
		return Claims{}, err
	}
	if o.groupsClaim != "" {
		var all map[string]any
		if err := idt.Claims(&all); err == nil {
			if gs, ok := all[o.groupsClaim].([]any); ok {
				for _, g := range gs {
					if s, ok := g.(string); ok {
						c.Groups = append(c.Groups, s)
					}
				}
			}
		}
	}
	return c, nil
}
