package localfile

import (
	"context"
	"encoding/base64"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOpenCreatesPrivateKeyAndReopens(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "master.key")
	e1, err := Open(path)
	require.NoError(t, err)
	st, err := os.Stat(path)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), st.Mode().Perm())

	wrapped, err := e1.WrapKey(context.Background(), []byte("data-key"))
	require.NoError(t, err)
	e2, err := Open(path)
	require.NoError(t, err)
	assert.Equal(t, e1.KeyID(), e2.KeyID())
	got, err := e2.UnwrapKey(context.Background(), wrapped)
	require.NoError(t, err)
	assert.Equal(t, []byte("data-key"), got)
}

func TestOpenRejectsWidePermissionsAndBadLength(t *testing.T) {
	dir := t.TempDir()
	wide := filepath.Join(dir, "wide")
	require.NoError(t, os.WriteFile(wide, make([]byte, 32), 0o644))
	_, err := Open(wide)
	assert.ErrorContains(t, err, "chmod 600")

	short := filepath.Join(dir, "short")
	require.NoError(t, os.WriteFile(short, make([]byte, 16), 0o600))
	_, err = Open(short)
	assert.ErrorContains(t, err, "exactly 32 bytes")
}

func TestFromBase64MatchesFileKey(t *testing.T) {
	path := filepath.Join(t.TempDir(), "k")
	fileEnc, err := Open(path)
	require.NoError(t, err)
	raw, err := os.ReadFile(path)
	require.NoError(t, err)

	envEnc, err := FromBase64(base64.StdEncoding.EncodeToString(raw) + "\n")
	require.NoError(t, err)
	assert.Equal(t, fileEnc.KeyID(), envEnc.KeyID(), "same key, same fingerprint")
	wrapped, err := fileEnc.WrapKey(context.Background(), []byte("dek"))
	require.NoError(t, err)
	got, err := envEnc.UnwrapKey(context.Background(), wrapped)
	require.NoError(t, err)
	assert.Equal(t, []byte("dek"), got)

	_, err = FromBase64("not base64!")
	assert.ErrorContains(t, err, "base64")
	_, err = FromBase64(base64.StdEncoding.EncodeToString([]byte("short")))
	assert.ErrorContains(t, err, "32 bytes")
}
