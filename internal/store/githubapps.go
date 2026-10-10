package store

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/GokulMV/DocTheRepo/internal/ports"
	"github.com/GokulMV/DocTheRepo/internal/secrets"
)

// GitHubApp is a GitHub App connector as the MCP connections see it: whether the Hub can sign people in
// with it (an OAuth client ID and secret are stored). Never the secret itself.
type GitHubApp struct {
	ConnectorID string `json:"connector_id"`
	Name        string `json:"name"`
	Slug        string `json:"app_slug"`
	Owner       string `json:"owner"`
	// Web is https://github.com or the GitHub Enterprise Server address.
	Web       string `json:"web"`
	ClientID  string `json:"client_id,omitempty"`
	OAuth     bool   `json:"oauth"` // client ID and secret are stored
	Installed bool   `json:"installed"`
}

// GitHubAppClient is a GitHub App's OAuth client, opened.
type GitHubAppClient struct {
	GitHubApp
	ClientSecret string
}

// GitHubWeb is the web address of a GitHub connector's API base (empty: github.com).
func GitHubWeb(apiBase string) string {
	if apiBase == "" || apiBase == "https://api.github.com/" || apiBase == "https://api.github.com" {
		return "https://github.com"
	}
	return strings.TrimSuffix(strings.TrimSuffix(strings.TrimSuffix(apiBase, "/"), "/api/v3"), "/")
}

// SetOAuthClient stores a GitHub App connector's OAuth client. An empty secret keeps the stored one; an
// empty client ID and secret remove both.
func (c *Connectors) SetOAuthClient(ctx context.Context, id, clientID, secret string) error {
	if !uuidRE.MatchString(id) {
		return ports.ErrNotFound
	}
	clientID = strings.TrimSpace(clientID)
	var tag interface{ RowsAffected() int64 }
	var err error
	switch {
	case clientID == "" && secret == "":
		tag, err = c.s.Pool.Exec(ctx, `UPDATE connectors SET oauth_client_id = '', oauth_client_secret_ct = NULL, updated_at = now() WHERE id = $1`, id)
	case secret == "":
		tag, err = c.s.Pool.Exec(ctx, `UPDATE connectors SET oauth_client_id = $2, updated_at = now() WHERE id = $1`, id, clientID)
	default:
		ct, serr := c.box.Seal(ctx, []byte(secret), secrets.ConnectorOAuthClientAAD(id))
		if serr != nil {
			return serr
		}
		tag, err = c.s.Pool.Exec(ctx, `UPDATE connectors SET oauth_client_id = $2, oauth_client_secret_ct = $3, updated_at = now() WHERE id = $1`, id, clientID, ct)
	}
	if err != nil {
		return fmt.Errorf("store the OAuth client: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ports.ErrNotFound
	}
	return nil
}

const githubAppCols = `id, name, config, enabled, oauth_client_id, oauth_client_secret_ct IS NOT NULL`

// scanGitHubApp reads githubAppCols (then extra) and reports whether the row is an enabled GitHub App.
func scanGitHubApp(row pgx.Row, extra ...any) (GitHubApp, bool, error) {
	var a GitHubApp
	var cfg []byte
	var enabled bool
	if err := row.Scan(append([]any{&a.ConnectorID, &a.Name, &cfg, &enabled, &a.ClientID, &a.OAuth}, extra...)...); err != nil {
		return a, false, err
	}
	m := map[string]string{}
	_ = json.Unmarshal(cfg, &m)
	a.Slug, a.Owner, a.Web, a.Installed = m["app_slug"], m["owner"], GitHubWeb(m["base_url"]), m["installation_id"] != ""
	a.OAuth = a.OAuth && a.ClientID != ""
	return a, enabled && m["app_id"] != "", nil
}

// GitHubApps lists the enabled GitHub App connectors.
func (c *Connectors) GitHubApps(ctx context.Context) ([]GitHubApp, error) {
	rows, err := c.s.Pool.Query(ctx, `SELECT `+githubAppCols+` FROM connectors WHERE type = 'github' AND enabled ORDER BY created_at`)
	if err != nil {
		return nil, fmt.Errorf("list GitHub Apps: %w", err)
	}
	defer rows.Close()
	out := []GitHubApp{}
	for rows.Next() {
		a, app, err := scanGitHubApp(rows)
		if err != nil {
			return nil, err
		}
		if app {
			out = append(out, a)
		}
	}
	return out, rows.Err()
}

// GitHubAppClient opens a GitHub App connector's OAuth client. A missing or disabled connector, or one
// that is not a GitHub App, is ports.ErrNotFound; an App without a client ID and secret is a
// ValidationError that says how to add them.
func (c *Connectors) GitHubAppClient(ctx context.Context, id string) (GitHubAppClient, error) {
	if !uuidRE.MatchString(id) {
		return GitHubAppClient{}, fmt.Errorf("GitHub App %q: %w", id, ports.ErrNotFound)
	}
	var ct []byte
	a, app, err := scanGitHubApp(c.s.Pool.QueryRow(ctx, `SELECT `+githubAppCols+`, oauth_client_secret_ct FROM connectors WHERE id = $1 AND type = 'github'`, id), &ct)
	if IsNoRows(err) || (err == nil && !app) {
		return GitHubAppClient{}, fmt.Errorf("GitHub App %s: %w", id, ports.ErrNotFound)
	}
	if err != nil {
		return GitHubAppClient{}, err
	}
	if !a.OAuth {
		return GitHubAppClient{GitHubApp: a}, &ports.ValidationError{Code: "GITHUB_APP_NO_CLIENT",
			Message: "the GitHub App " + a.Name + " has no client ID and secret stored: add them (from the App's settings page) to sign in with GitHub"}
	}
	b, err := c.box.Open(ctx, ct, secrets.ConnectorOAuthClientAAD(id))
	if err != nil {
		return GitHubAppClient{}, ports.Permanent(fmt.Errorf("decrypt the client secret of GitHub App %q: %w", a.Name, err))
	}
	a.OAuth = true
	return GitHubAppClient{GitHubApp: a, ClientSecret: string(b)}, nil
}
