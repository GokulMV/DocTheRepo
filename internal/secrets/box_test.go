package secrets_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/GokulMV/DocTheRepo/internal/adapters/secrets/localfile"
	"github.com/GokulMV/DocTheRepo/internal/secrets"
)

func newBox(t *testing.T) (*secrets.Box, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "keys", "master.key")
	kek, err := localfile.Open(path)
	require.NoError(t, err)
	return secrets.NewBox(kek), path
}

func TestBox_SealOpen_RoundTrip(t *testing.T) {
	box, _ := newBox(t)
	ctx := context.Background()
	ct, err := box.Seal(ctx, []byte("sk-ant-secret"), []byte("provider:1:key"))
	require.NoError(t, err)
	assert.NotContains(t, string(ct), "sk-ant-secret")
	pt, err := box.Open(ctx, ct, []byte("provider:1:key"))
	require.NoError(t, err)
	assert.Equal(t, "sk-ant-secret", string(pt))
}

func TestBox_SameSecretTwice_DifferentCiphertexts(t *testing.T) {
	box, _ := newBox(t)
	a, _ := box.Seal(context.Background(), []byte("x"), nil)
	b, _ := box.Seal(context.Background(), []byte("x"), nil)
	assert.NotEqual(t, a, b)
}

func TestBox_WrongAAD_Fails(t *testing.T) {
	box, _ := newBox(t)
	ct, _ := box.Seal(context.Background(), []byte("x"), []byte("connector:A:creds"))
	_, err := box.Open(context.Background(), ct, []byte("connector:B:creds"))
	assert.ErrorIs(t, err, secrets.ErrCorrupt)
}

func TestBox_Tampered_Fails(t *testing.T) {
	box, _ := newBox(t)
	ct, _ := box.Seal(context.Background(), []byte("hello"), nil)
	ct[len(ct)-1] ^= 0xff
	_, err := box.Open(context.Background(), ct, nil)
	assert.ErrorIs(t, err, secrets.ErrCorrupt)
	for _, bad := range [][]byte{nil, {9}, {1, 200}, ct[:6]} {
		_, err := box.Open(context.Background(), bad, nil)
		assert.Error(t, err)
	}
}

func TestBox_DifferentMasterKey_Fails(t *testing.T) {
	a, _ := newBox(t)
	b, _ := newBox(t)
	ct, _ := a.Seal(context.Background(), []byte("x"), nil)
	_, err := b.Open(context.Background(), ct, nil)
	assert.ErrorContains(t, err, "sealed with key")
}

func TestLocalFile_ReopenUsesSameKey(t *testing.T) {
	box, path := newBox(t)
	ct, _ := box.Seal(context.Background(), []byte("persist"), nil)
	kek, err := localfile.Open(path)
	require.NoError(t, err)
	pt, err := secrets.NewBox(kek).Open(context.Background(), ct, nil)
	require.NoError(t, err)
	assert.Equal(t, "persist", string(pt))
	st, _ := os.Stat(path)
	assert.Equal(t, os.FileMode(0o600), st.Mode().Perm())
}

func TestLocalFile_RejectsLoosePermissionsAndBadLength(t *testing.T) {
	dir := t.TempDir()
	loose := filepath.Join(dir, "loose.key")
	require.NoError(t, os.WriteFile(loose, make([]byte, 32), 0o644))
	_, err := localfile.Open(loose)
	assert.ErrorContains(t, err, "chmod 600")

	short := filepath.Join(dir, "short.key")
	require.NoError(t, os.WriteFile(short, make([]byte, 7), 0o600))
	_, err = localfile.Open(short)
	assert.ErrorContains(t, err, "32 bytes")
}
