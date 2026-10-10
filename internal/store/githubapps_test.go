package store_test

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/GokulMV/DocTheRepo/internal/adapters/secrets/localfile"
	"github.com/GokulMV/DocTheRepo/internal/ports"
	"github.com/GokulMV/DocTheRepo/internal/secrets"
	"github.com/GokulMV/DocTheRepo/internal/store"
	"github.com/GokulMV/DocTheRepo/internal/store/storetest"
)

func TestConnectors_GitHubAppOAuthClient(t *testing.T) {
	st := storetest.New(t)
	kek, err := localfile.Open(filepath.Join(t.TempDir(), "k"))
	require.NoError(t, err)
	conns := store.NewConnectors(st, secrets.NewBox(kek), secrets.ConnectorCredsAAD, secrets.ConnectorWebhookAAD)
	ctx := context.Background()

	id, err := conns.Create(ctx, store.NewConnector{Type: "github", Name: "GitHub (acme)", Mode: "poll", Credentials: "PEM",
		Config: map[string]string{"app_id": "1", "app_slug": "docs-acme", "owner": "acme", "installation_id": "9"}})
	require.NoError(t, err)
	pat, err := conns.Create(ctx, store.NewConnector{Type: "github", Name: "GitHub token", Credentials: "ghp_x"})
	require.NoError(t, err)

	apps, err := conns.GitHubApps(ctx)
	require.NoError(t, err)
	require.Len(t, apps, 1, "only GitHub Apps, not token connectors")
	require.Equal(t, store.GitHubApp{ConnectorID: id, Name: "GitHub (acme)", Slug: "docs-acme", Owner: "acme", Web: "https://github.com", Installed: true}, apps[0])

	_, err = conns.GitHubAppClient(ctx, id)
	var ve *ports.ValidationError
	require.True(t, errors.As(err, &ve), "an App without a client says how to add one")
	_, err = conns.GitHubAppClient(ctx, pat)
	require.ErrorIs(t, err, ports.ErrNotFound)

	require.NoError(t, conns.SetOAuthClient(ctx, id, "Iv23liAPPCLIENT", "client-s3cret"))
	c, err := conns.GitHubAppClient(ctx, id)
	require.NoError(t, err)
	require.Equal(t, "Iv23liAPPCLIENT", c.ClientID)
	require.Equal(t, "client-s3cret", c.ClientSecret)
	require.True(t, c.OAuth)
	var raw []byte
	require.NoError(t, st.Pool.QueryRow(ctx, `SELECT oauth_client_secret_ct FROM connectors WHERE id = $1`, id).Scan(&raw))
	require.NotContains(t, string(raw), "client-s3cret", "stored sealed")

	// An empty secret keeps the stored one; the listing never carries it.
	require.NoError(t, conns.SetOAuthClient(ctx, id, "Iv23liNEWID", ""))
	c, _ = conns.GitHubAppClient(ctx, id)
	require.Equal(t, "Iv23liNEWID", c.ClientID)
	require.Equal(t, "client-s3cret", c.ClientSecret)
	apps, _ = conns.GitHubApps(ctx)
	require.True(t, apps[0].OAuth)

	// Other connector edits (a settings re-apply replacing the config) keep the client.
	cfg := map[string]string{"app_id": "1", "app_slug": "docs-acme", "owner": "acme", "base_url": "https://ghe.example.com/api/v3/"}
	require.NoError(t, conns.Update(ctx, id, store.ConnectorPatch{Config: &cfg}))
	c, err = conns.GitHubAppClient(ctx, id)
	require.NoError(t, err)
	require.Equal(t, "https://ghe.example.com", c.Web)
	require.False(t, c.Installed)

	require.NoError(t, conns.SetOAuthClient(ctx, id, "", ""))
	apps, _ = conns.GitHubApps(ctx)
	require.False(t, apps[0].OAuth)
	require.ErrorIs(t, conns.SetOAuthClient(ctx, "00000000-0000-0000-0000-000000000000", "Iv23liX", "s"), ports.ErrNotFound)
}
