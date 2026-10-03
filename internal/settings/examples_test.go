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
