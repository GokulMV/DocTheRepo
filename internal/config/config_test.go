package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func writeFile(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "dth.yaml")
	require.NoError(t, os.WriteFile(p, []byte(body), 0o600))
	return p
}

func TestLoad_NoFile_UsesCompleteDefaults(t *testing.T) {
	cfg, err := Load(filepath.Join(t.TempDir(), "missing.yaml"))
	require.NoError(t, err)
	assert.Equal(t, Default().Server.Listen, cfg.Server.Listen)
	assert.True(t, cfg.HasRole(RoleAPI) && cfg.HasRole(RoleWorker) && cfg.HasRole(RoleScheduler))
	assert.Equal(t, "docs/generated/", cfg.Docs.DefaultPath)
	assert.Equal(t, 4, cfg.Queue.Concurrency["code_push"])
}

func TestLoad_EmptyFile_UsesDefaults(t *testing.T) {
	cfg, err := Load(writeFile(t, ""))
	require.NoError(t, err)
	assert.Equal(t, 5, cfg.Queue.MaxAttempts)
}

func TestLoad_FileOverridesOnlyGivenKeys(t *testing.T) {
	cfg, err := Load(writeFile(t, "server:\n  listen: \":9999\"\nqueue:\n  max_attempts: 3\n"))
	require.NoError(t, err)
	assert.Equal(t, ":9999", cfg.Server.Listen)
	assert.Equal(t, 3, cfg.Queue.MaxAttempts)
	assert.Equal(t, 5*time.Minute, cfg.Queue.LeaseTTL, "unspecified keys keep defaults")
}

func TestLoad_UnknownKey_FailsLoudly(t *testing.T) {
	_, err := Load(writeFile(t, "servr:\n  listen: x\n"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "servr")
}

func TestLoad_EnvOverridesFile(t *testing.T) {
	t.Setenv("DTH_LISTEN", ":7000")
	t.Setenv("DTH_ROLES", "worker, scheduler")
	t.Setenv("DTH_TRACING_ENABLED", "true")
	t.Setenv("DTH_OTLP_ENDPOINT", "http://otel:4318")
	t.Setenv("DTH_OIDC_ALLOWED_DOMAINS", "acme.com, ,acme.io")
	cfg, err := Load(writeFile(t, "server:\n  listen: \":9999\"\n"))
	require.NoError(t, err)
	assert.Equal(t, ":7000", cfg.Server.Listen)
	assert.Equal(t, []Role{RoleWorker, RoleScheduler}, cfg.Roles)
	assert.False(t, cfg.HasRole(RoleAPI))
	assert.True(t, cfg.Tracing.Enabled)
	assert.Equal(t, []string{"acme.com", "acme.io"}, cfg.Auth.OIDC.AllowedDomains)
}

func TestLoad_BadBoolEnv_Errors(t *testing.T) {
	t.Setenv("DTH_TRACING_ENABLED", "maybe")
	_, err := Load("")
	require.Error(t, err)
}

func TestLoad_DocsPathGetsTrailingSlash(t *testing.T) {
	cfg, err := Load(writeFile(t, "docs:\n  default_path: website/docs\n"))
	require.NoError(t, err)
	assert.Equal(t, "website/docs/", cfg.Docs.DefaultPath)
}

func TestValidate_ReportsEveryProblem(t *testing.T) {
	c := Default()
	c.Roles = []Role{"web"}
	c.Secrets.Provider = "vault"
	c.Auth.Mode = "oidc"
	c.Queue.MaxAttempts = 0
	c.Queue.Concurrency["bogus"] = 1
	c.Logging.Format = "xml"
	c.Tracing.Enabled = true
	c.Server.PublicURL = "not a url"
	err := c.Validate()
	require.Error(t, err)
	for _, want := range []string{"unknown role", "secrets.provider", "auth.oidc", "max_attempts",
		"unknown job type", "logging.format", "otlp_endpoint", "public_url"} {
		assert.Contains(t, err.Error(), want)
	}
}

func TestValidate_KMSProvidersNeedKeyID(t *testing.T) {
	for _, p := range []string{"awskms", "gcpkms"} {
		c := Default()
		c.Secrets.Provider = p
		require.ErrorContains(t, c.Validate(), "kms_key_id")
		c.Secrets.KMSKeyID = "key"
		require.NoError(t, c.Validate())
	}
}

func TestValidate_OIDCComplete_Passes(t *testing.T) {
	c := Default()
	c.Auth.Mode = "oidc"
	c.Auth.OIDC.Issuer = "https://example.okta.com"
	c.Auth.OIDC.ClientID = "abc"
	c.Auth.OIDC.RedirectURL = "https://dth.example.com/api/v1/auth/callback"
	require.NoError(t, c.Validate())
}

func TestValidate_SessionWindows(t *testing.T) {
	c := Default()
	c.Auth.SessionAbsolute = time.Hour
	require.ErrorContains(t, c.Validate(), "session_idle")
}

func TestValidate_ZeroConcurrencyDisablesType(t *testing.T) {
	c := Default()
	c.Queue.Concurrency["reindex"] = 0
	require.NoError(t, c.Validate())
	c.Queue.Concurrency["reindex"] = -1
	require.Error(t, c.Validate())
}

func TestValidateDocsPath(t *testing.T) {
	cases := map[string]bool{
		"docs/generated/":  true,
		"website/docs/ref": true,
		"":                 false,
		"/abs/path":        false,
		".":                false,
		"./":               false,
		"docs/../..":       false,
		"../outside":       false,
		"a/b/../c":         true, // cleans to a/c, no traversal out of the repo
	}
	for in, ok := range cases {
		err := ValidateDocsPath(in)
		assert.Equal(t, ok, err == nil, "path %q: %v", in, err)
	}
}

func TestLoad_UnreadablePath_Errors(t *testing.T) {
	dir := t.TempDir() // reading a directory as a file fails with a non-ENOENT error
	_, err := Load(dir)
	require.Error(t, err)
	assert.True(t, strings.Contains(err.Error(), "read config"))
}
