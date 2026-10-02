package main

import (
	"context"
	"encoding/base64"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/GokulMV/DocTheRepo/internal/adapters/secrets/localfile"
	"github.com/GokulMV/DocTheRepo/internal/api"
	"github.com/GokulMV/DocTheRepo/internal/config"
	"github.com/GokulMV/DocTheRepo/internal/observability"
	"github.com/GokulMV/DocTheRepo/internal/queue"
	"github.com/GokulMV/DocTheRepo/internal/secrets"
	"github.com/GokulMV/DocTheRepo/internal/store/storetest"
)

// TestSealedSecrets: the browser seals a provider key to the Hub's hybrid key; the Hub opens it only to
// store it encrypted, never returns it (only a hint), refuses a value sealed for another field, can require
// sealing, and keeps opening values sealed just before a key rotation.
func TestSealedSecrets(t *testing.T) {
	ctx := context.Background()
	st := storetest.New(t)
	kek, err := localfile.Open(filepath.Join(t.TempDir(), "k"))
	require.NoError(t, err)
	cfg := config.Default()
	cfg.Auth.Mode = "local"
	log := slog.New(slog.DiscardHandler)
	m := observability.NewMetrics()
	a, err := wire(ctx, cfg, st, secrets.NewBox(kek), queue.New(st, queue.Options{}), log, m)
	require.NoError(t, err)
	_, err = a.auth.BootstrapOwner(ctx, "owner@acme.com", "correct horse battery staple")
	require.NoError(t, err)
	srv := httptest.NewServer(api.NewRouter(api.Deps{Log: log, Metrics: m, Auth: a.auth, V1: a.v1Routes()}))
	defer srv.Close()
	jar, _ := cookiejar.New(nil)
	c := &apiClient{t: t, base: srv.URL + "/api/v1", c: &http.Client{Jar: jar}}
	code, out := c.call("POST", "/auth/local/login", map[string]string{"email": "owner@acme.com", "password": "correct horse battery staple"})
	require.Equal(t, http.StatusOK, code, out)
	c.csrf = out["csrf_token"].(string)

	pubKey := func() secrets.PublicSealKey {
		code, out := c.call("GET", "/seal/key", nil)
		require.Equal(t, http.StatusOK, code, out)
		x, _ := base64.StdEncoding.DecodeString(out["x25519"].(string))
		k, _ := base64.StdEncoding.DecodeString(out["mlkem768"].(string))
		assert.Equal(t, secrets.SealAlg, out["alg"])
		return secrets.PublicSealKey{ID: out["kid"].(string), X25519: x, MLKEM768: k}
	}
	pub := pubKey()
	assert.Equal(t, pub.ID, pubKey().ID, "one active key")

	key := "sk-live-0123456789abcdefghij-WXYZ"
	sealed, err := secrets.Seal(pub, []byte(key), secrets.PurposeProviderKey)
	require.NoError(t, err)
	code, out = c.call("POST", "/providers", map[string]any{"kind": "openai_compat", "name": "p1", "base_url": "http://127.0.0.1:1/v1", "api_key": sealed})
	require.Equal(t, http.StatusCreated, code, out)
	pid := out["id"].(string)
	pc, _, err := a.provSrc.ProviderConfig(ctx, pid)
	require.NoError(t, err)
	assert.Equal(t, key, pc.APIKey, "stored as the real key (encrypted at rest), not the sealed envelope")

	code, out = c.call("GET", "/providers", nil)
	require.Equal(t, http.StatusOK, code)
	p0 := out["items"].([]any)[0].(map[string]any)
	assert.Equal(t, "WXYZ", p0["key_meta"].(map[string]any)["hint"])
	assert.NotContains(t, srv.URL, key)
	for k, v := range p0 {
		assert.NotContains(t, k+toString(v), "0123456789abcdef", "the key is never returned")
	}

	// Sealed for another field: refused.
	wrong, _ := secrets.Seal(pub, []byte("x"), secrets.PurposeConnectorCreds)
	code, out = c.call("PATCH", "/providers/"+pid, map[string]any{"api_key": wrong})
	assert.Equal(t, http.StatusBadRequest, code, out)

	// Rotation: a value sealed to the old key (an open tab) still works for a day; new seals use the new key.
	code, out = c.call("POST", "/seal/rotate", nil)
	require.Equal(t, http.StatusOK, code, out)
	assert.NotEqual(t, pub.ID, pubKey().ID)
	old, _ := secrets.Seal(pub, []byte("sk-rotated-0123456789abcdefgh-ABCD"), secrets.PurposeProviderKey)
	code, out = c.call("PATCH", "/providers/"+pid, map[string]any{"api_key": old})
	assert.Equal(t, http.StatusNoContent, code, out)

	// Requiring sealed secrets refuses plain ones.
	a.cfg.Settings.RequireSealed = true
	srv2 := httptest.NewServer(api.NewRouter(api.Deps{Log: log, Metrics: m, Auth: a.auth, V1: a.v1Routes()}))
	defer srv2.Close()
	c.base = srv2.URL + "/api/v1"
	code, out = c.call("PATCH", "/providers/"+pid, map[string]any{"api_key": "plain-text-key"})
	assert.Equal(t, http.StatusBadRequest, code)
	assert.Equal(t, "SEAL_REQUIRED", out["error"].(map[string]any)["code"])
}

func toString(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case map[string]any:
		s := ""
		for k, x := range t {
			s += k + toString(x)
		}
		return s
	}
	return ""
}
