package main

import (
	"bytes"
	"context"
	"path/filepath"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/GokulMV/DocTheRepo/internal/adapters/secrets/localfile"
	"github.com/GokulMV/DocTheRepo/internal/secrets"
	"github.com/GokulMV/DocTheRepo/internal/store/storetest"
)

// Rotating the local master key moves every stored secret to the new key without touching the secret.
func TestRotateKey(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	oldPath, newPath := filepath.Join(dir, "old.key"), filepath.Join(dir, "new.key")
	url := storetest.URL(t)
	t.Setenv("DTH_DATABASE_URL", url)
	t.Setenv("DTH_LOCAL_KEY_FILE", oldPath)
	oldK, err := localfile.Open(oldPath)
	require.NoError(t, err)
	oldBox := secrets.NewBox(oldK)
	db, err := pgxpool.New(ctx, url)
	require.NoError(t, err)
	defer db.Close()

	const pid = "0b8f7a8e-5d0c-4d6f-9f43-2b1b7a6f4c11"
	key, err := oldBox.Seal(ctx, []byte("sk-team"), secrets.ProviderKeyAAD(pid))
	require.NoError(t, err)
	_, err = db.Exec(ctx, "INSERT INTO llm_providers (id, kind, name, key_ciphertext) VALUES ($1, 'anthropic', 'Team', $2)", pid, key)
	require.NoError(t, err)
	oidc, err := oldBox.Seal(ctx, []byte("client-secret"), []byte("auth_settings:oidc_secret"))
	require.NoError(t, err)
	_, err = db.Exec(ctx, "INSERT INTO auth_settings (id, oidc_secret_ciphertext) VALUES (1, $1) ON CONFLICT (id) DO UPDATE SET oidc_secret_ciphertext = $1", oidc)
	require.NoError(t, err)

	var out bytes.Buffer
	require.NoError(t, rotateKey([]string{"--dry-run", "--to-key-file", newPath}, "", &out), out.String())
	assert.Contains(t, out.String(), "Would move 2 secrets")
	var still []byte
	require.NoError(t, db.QueryRow(ctx, "SELECT key_ciphertext FROM llm_providers WHERE id = $1", pid).Scan(&still))
	assert.Equal(t, key, still, "a dry run writes nothing")

	out.Reset()
	require.NoError(t, rotateKey([]string{"--to-key-file", newPath}, "", &out), out.String())
	assert.Contains(t, out.String(), "Moved 1 model provider keys.")
	assert.Contains(t, out.String(), "Moved 1 single sign-on client secret.")
	assert.Contains(t, out.String(), "DTH_LOCAL_KEY_FILE="+newPath)

	newK, err := localfile.Open(newPath)
	require.NoError(t, err)
	var got []byte
	require.NoError(t, db.QueryRow(ctx, "SELECT key_ciphertext FROM llm_providers WHERE id = $1", pid).Scan(&got))
	pt, err := secrets.NewBox(newK).Open(ctx, got, secrets.ProviderKeyAAD(pid))
	require.NoError(t, err)
	assert.Equal(t, "sk-team", string(pt))
	require.NoError(t, db.QueryRow(ctx, "SELECT oidc_secret_ciphertext FROM auth_settings").Scan(&got))
	pt, err = secrets.NewBox(newK).Open(ctx, got, []byte("auth_settings:oidc_secret"))
	require.NoError(t, err)
	assert.Equal(t, "client-secret", string(pt))

	// Running it again moves nothing; the same key twice and missing flags are refused.
	out.Reset()
	require.NoError(t, rotateKey([]string{"--to-key-file", newPath}, "", &out))
	assert.Contains(t, out.String(), "Moved 0 secrets")
	assert.Error(t, rotateKey([]string{"--to-key-file", oldPath}, "", &out))
	assert.Error(t, rotateKey(nil, "", &out))
	assert.Error(t, rotateKey([]string{"--to-key-file", newPath, "--to-awskms", "k"}, "", &out))
}
