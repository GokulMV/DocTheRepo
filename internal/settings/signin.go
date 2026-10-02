package settings

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/GokulMV/DocTheRepo/internal/secrets"
)

type remoteSignIn struct {
	State struct {
		Password    bool `json:"password"`
		PasswordSet bool `json:"password_set"`
		Settings    *struct {
			Provider       string   `json:"provider"`
			Issuer         string   `json:"issuer"`
			ClientID       string   `json:"client_id"`
			HasSecret      bool     `json:"has_secret"`
			AllowedDomains []string `json:"allowed_domains"`
			GroupsClaim    string   `json:"groups_claim"`
		} `json:"settings"`
	} `json:"state"`
}

// auth applies sign-in settings in one request, so "passwords off" and the SSO that replaces them land
// together (the Hub refuses a change that leaves no way in, and checks the issuer before saving).
func (a *applier) auth(ctx context.Context, doc Document) error {
	if doc.Auth == nil || (doc.Auth.Password == nil && doc.Auth.SSO == nil) {
		return nil
	}
	var cur remoteSignIn
	if err := a.api.Do(ctx, "GET", "/auth/settings", nil, &cur); err != nil {
		return a.fail("auth", "sign-in", fmt.Errorf("%w (sign-in settings need the owner role)", err))
	}
	body, fields := map[string]any{}, []string{}
	if p := doc.Auth.Password; p != nil && (!cur.State.PasswordSet || cur.State.Password != *p) {
		body["password"], fields = *p, append(fields, "password")
	}
	if s := doc.Auth.SSO; s != nil {
		domains := s.AllowedDomains
		if domains == nil {
			domains = []string{}
		}
		have := cur.State.Settings
		same := have != nil && have.Provider == s.Provider && strings.TrimRight(have.Issuer, "/") == strings.TrimRight(s.Issuer, "/") &&
			have.ClientID == s.ClientID && slices.Equal(have.AllowedDomains, normDomains(domains)) && have.GroupsClaim == s.GroupsClaim
		if !same || s.ClientSecret.Set {
			sso := map[string]any{"provider": s.Provider, "issuer": s.Issuer, "client_id": s.ClientID, "allowed_domains": domains, "groups_claim": s.GroupsClaim}
			f := []string{"sso"}
			if s.ClientSecret.Set { // write-only: always re-applied
				v, err := a.seal(ctx, s.ClientSecret.Value, secrets.PurposeOIDCClientSecret)
				if err != nil {
					return a.fail("auth", "sign-in", err)
				}
				sso["client_secret"], f = v, append(f, "client_secret")
			}
			body["sso"], fields = sso, append(fields, f...)
		}
	}
	if len(fields) == 0 {
		a.record(Change{Kind: "auth", Name: "sign-in", Action: "unchanged"})
		return nil
	}
	if !a.dry {
		if err := a.api.Do(ctx, "PUT", "/auth/settings", body, nil); err != nil {
			return a.fail("auth", "sign-in", err)
		}
	}
	a.record(Change{Kind: "auth", Name: "sign-in", Action: "update", Fields: fields})
	return nil
}

func normDomains(in []string) []string {
	out := []string{}
	for _, d := range in {
		d = strings.ToLower(strings.TrimPrefix(strings.TrimSpace(d), "@"))
		if d != "" && !slices.Contains(out, d) {
			out = append(out, d)
		}
	}
	return out
}

type remoteUser struct {
	ID       string `json:"id"`
	Email    string `json:"email"`
	Name     string `json:"name"`
	Role     string `json:"role"`
	Disabled bool   `json:"disabled"`
}

func (a *applier) listUsers(ctx context.Context) ([]remoteUser, error) {
	var all []remoteUser
	cursor := ""
	for {
		var page struct {
			Items      []remoteUser `json:"items"`
			NextCursor *string      `json:"next_cursor"`
		}
		path := "/users?limit=200"
		if cursor != "" {
			path += "&cursor=" + cursor
		}
		if err := a.api.Do(ctx, "GET", path, nil, &page); err != nil {
			return nil, err
		}
		all = append(all, page.Items...)
		if page.NextCursor == nil || *page.NextCursor == "" {
			return all, nil
		}
		cursor = *page.NextCursor
	}
}

// users adds the people listed and updates their name, role or disabled flag. It never removes anyone.
func (a *applier) users(ctx context.Context, doc Document) error {
	if len(doc.Users) == 0 {
		return nil
	}
	have, err := a.listUsers(ctx)
	if err != nil {
		return a.fail("user", "users", err)
	}
	for _, u := range doc.Users {
		email := strings.ToLower(strings.TrimSpace(u.Email))
		role := u.Role
		if role == "" {
			role = "viewer"
		}
		i := slices.IndexFunc(have, func(r remoteUser) bool { return strings.EqualFold(r.Email, email) })
		if i < 0 {
			if !a.dry {
				var out struct {
					User remoteUser `json:"user"`
				}
				if err := a.api.Do(ctx, "POST", "/users", map[string]any{"email": email, "name": u.Name, "role": role}, &out); err != nil {
					return a.fail("user", email, err)
				}
				if u.Disabled != nil && *u.Disabled {
					if err := a.api.Do(ctx, "PATCH", "/users/"+out.User.ID, map[string]any{"disabled": true}, nil); err != nil {
						return a.fail("user", email, err)
					}
				}
			}
			a.record(Change{Kind: "user", Name: email, Action: "create", Fields: []string{"role"}})
			continue
		}
		cur := have[i]
		patch, fields := map[string]any{}, []string{}
		if u.Name != "" && u.Name != cur.Name {
			patch["name"], fields = u.Name, append(fields, "name")
		}
		if u.Role != "" && u.Role != cur.Role {
			patch["role"], fields = u.Role, append(fields, "role")
		}
		if u.Disabled != nil && *u.Disabled != cur.Disabled {
			patch["disabled"], fields = *u.Disabled, append(fields, "disabled")
		}
		if err := a.update(ctx, "user", email, "/users/"+cur.ID, patch, fields); err != nil {
			return err
		}
	}
	return nil
}

// exportAuth adds sign-in settings and users to an export when the caller may read them.
func exportAuth(ctx context.Context, api API, doc *Document) {
	var cur remoteSignIn
	if err := api.Do(ctx, "GET", "/auth/settings", nil, &cur); err == nil {
		au := &Auth{}
		if cur.State.PasswordSet {
			p := cur.State.Password
			au.Password = &p
		}
		if s := cur.State.Settings; s != nil {
			au.SSO = &SSO{Provider: s.Provider, Issuer: s.Issuer, ClientID: s.ClientID, AllowedDomains: s.AllowedDomains, GroupsClaim: s.GroupsClaim}
			if s.HasSecret {
				au.SSO.ClientSecret = Secret{Value: "${env:DTH_SECRET_OIDC_CLIENT_SECRET}", Set: true}
			}
		}
		if au.Password != nil || au.SSO != nil {
			doc.Auth = au
		}
	} else if !errors.Is(err, ErrAPI) {
		return
	}
	a := &applier{api: api}
	if us, err := a.listUsers(ctx); err == nil {
		for _, u := range us {
			du := User{Email: u.Email, Name: u.Name, Role: u.Role}
			if u.Disabled {
				t := true
				du.Disabled = &t
			}
			doc.Users = append(doc.Users, du)
		}
	}
}
