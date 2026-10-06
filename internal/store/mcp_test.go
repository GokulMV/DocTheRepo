package store_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/GokulMV/DocTheRepo/internal/adapters/secrets/localfile"
	"github.com/GokulMV/DocTheRepo/internal/secrets"
	"github.com/GokulMV/DocTheRepo/internal/store"
	"github.com/GokulMV/DocTheRepo/internal/store/storetest"
)

func TestMCPServers(t *testing.T) {
	st := storetest.New(t)
	kek, err := localfile.Open(filepath.Join(t.TempDir(), "k"))
	require.NoError(t, err)
	m := store.NewMCPServers(st, secrets.NewBox(kek), secrets.MCPSecretAAD, secrets.MCPOAuthAAD)
	ctx := context.Background()

	id, err := m.Create(ctx, store.NewMCPServer{Name: "Sentry", URL: "https://mcp.sentry.dev/mcp", Auth: "oauth", CatalogKey: "sentry"})
	require.NoError(t, err)
	s, err := m.Get(ctx, id)
	require.NoError(t, err)
	require.Equal(t, "editor", s.MinRole, "editors by default")
	require.False(t, s.SignedIn)
	require.Equal(t, "new", s.Status)

	type tok struct{ AccessToken string }
	require.NoError(t, m.SetOAuth(ctx, id, tok{"at-1"}))
	var got tok
	ok, err := m.OAuth(ctx, id, &got)
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, "at-1", got.AccessToken)
	var raw []byte
	require.NoError(t, st.Pool.QueryRow(ctx, `SELECT oauth_ciphertext FROM mcp_servers WHERE id = $1`, id).Scan(&raw))
	require.NotContains(t, string(raw), "at-1", "tokens are stored sealed")

	tools := []store.MCPTool{{Name: "search_issues", ReadOnly: true}, {Name: "resolve_issue"}}
	require.NoError(t, m.SetStatus(ctx, id, "ok", "", tools))
	s, _ = m.Get(ctx, id)
	require.True(t, s.SignedIn)
	require.True(t, s.ToolOn(s.Tools[1]) == false && s.ToolOn(s.Tools[0]), "read-only tools are on, others off")

	choices := map[string]bool{"resolve_issue": true, "search_issues": false}
	require.NoError(t, m.Update(ctx, id, store.MCPPatch{ToolChoices: &choices}))
	s, _ = m.Get(ctx, id)
	require.True(t, s.ToolOn(s.Tools[1]))
	require.False(t, s.ToolOn(s.Tools[0]))

	// A new address drops the old sign-in and tool list.
	u := "https://mcp.sentry.dev/mcp2"
	require.NoError(t, m.Update(ctx, id, store.MCPPatch{URL: &u}))
	ok, _ = m.OAuth(ctx, id, &got)
	require.False(t, ok)
	s, _ = m.Get(ctx, id)
	require.Empty(t, s.Tools)

	kid, err := m.Create(ctx, store.NewMCPServer{Name: "New Relic", URL: "https://mcp.newrelic.com/mcp/", Auth: "header",
		Config: map[string]string{"header_name": "api-key"}, Secret: "NRAK-123", MinRole: "viewer"})
	require.NoError(t, err)
	sec, err := m.Secret(ctx, kid)
	require.NoError(t, err)
	require.Equal(t, "NRAK-123", sec)
	all, err := m.List(ctx)
	require.NoError(t, err)
	require.Len(t, all, 2)
	require.NoError(t, m.Delete(ctx, kid))
}
