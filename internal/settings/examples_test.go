package settings

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// The per-environment examples shipped in deploy/environments load and validate.
func TestEnvironmentExamplesLoad(t *testing.T) {
	r := &Resolver{
		Getenv: func(string) (string, bool) { return "", false },
		AWSSecret: func(_ context.Context, id string) (string, error) {
			return `{"api_key":"k","token":"t","webhook_secret":"w"}`, nil
		},
	}
	for _, env := range []string{"nonlive", "production"} {
		p := filepath.Join("..", "..", "deploy", "environments", env, "settings.yaml")
		b, err := os.ReadFile(p)
		require.NoError(t, err)
		d, err := Load(context.Background(), []Source{{Name: p, Data: b}}, r)
		require.NoError(t, err, env)
		require.NoError(t, d.Validate(), env)
		require.NotEmpty(t, d.Repos, env)
	}
}

// The fill-in template in deploy/templates parses, validates and names an owner, before anyone fills it in.
func TestTemplateChecks(t *testing.T) {
	p := filepath.Join("..", "..", "deploy", "templates", "hub.yaml")
	b, err := os.ReadFile(p)
	require.NoError(t, err)
	rep, err := Check(context.Background(), []Source{{Name: p, Data: b}}, CheckOptions{RequireOwner: true})
	require.NoError(t, err)
	require.Len(t, rep.Owners, 1)
	require.True(t, rep.SSO)
	require.Contains(t, rep.References, "${env:DTH_SECRET_OIDC_CLIENT_SECRET}")
}
