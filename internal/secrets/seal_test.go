package secrets

import (
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSealRoundTripAndBindings(t *testing.T) {
	k, err := GenerateSealKey("k1")
	require.NoError(t, err)
	pub := k.Public()
	assert.Len(t, pub.X25519, 32)
	assert.Len(t, pub.MLKEM768, 1184)

	v, err := Seal(pub, []byte("sk-ant-secret"), PurposeProviderKey)
	require.NoError(t, err)
	assert.True(t, IsSealed(v))
	assert.NotContains(t, v, "sk-ant")
	assert.Equal(t, "k1", SealedKeyID(v))

	got, err := k.Unseal(v, PurposeProviderKey)
	require.NoError(t, err)
	assert.Equal(t, "sk-ant-secret", string(got))

	_, err = k.Unseal(v, PurposeConnectorCreds)
	assert.ErrorIs(t, err, ErrSealed, "bound to its field")

	other, _ := GenerateSealKey("k1")
	_, err = other.Unseal(v, PurposeProviderKey)
	assert.ErrorIs(t, err, ErrSealed, "another key with the same id cannot open it")

	tampered := v[:len(v)-3] + strings.Repeat("A", 3)
	_, err = k.Unseal(tampered, PurposeProviderKey)
	assert.ErrorIs(t, err, ErrSealed)

	// Restored from storage, the key still opens it.
	again, err := LoadSealKey("k1", k.Private())
	require.NoError(t, err)
	got, err = again.Unseal(v, PurposeProviderKey)
	require.NoError(t, err)
	assert.Equal(t, "sk-ant-secret", string(got))
}

func TestSealEachValueIsFresh(t *testing.T) {
	k, _ := GenerateSealKey("k1")
	a, _ := Seal(k.Public(), []byte("same"), PurposeProviderKey)
	b, _ := Seal(k.Public(), []byte("same"), PurposeProviderKey)
	assert.NotEqual(t, a, b, "ephemeral keys and nonces make every seal different")
}

// TestUnsealsWhatTheBrowserSealed opens a value web/src/lib/seal.ts sealed to a key made for that test run.
// web/tests/seal.test.ts writes both to $SEAL_VECTOR_DIR; CI runs it first (job seal-interop). No key is
// kept in the repository, so the test skips when the directory is not given.
func TestUnsealsWhatTheBrowserSealed(t *testing.T) {
	dir := os.Getenv("SEAL_VECTOR_DIR")
	if dir == "" {
		t.Skip("set SEAL_VECTOR_DIR after running SEAL_VECTOR_DIR=<dir> npx vitest run tests/seal.test.ts in web/")
	}
	var key struct {
		KID        string `json:"kid"`
		PrivateHex string `json:"private_hex"`
	}
	var vec struct{ Purpose, Plaintext, Sealed string }
	b, err := os.ReadFile(filepath.Join(dir, "seal_key.json"))
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(b, &key))
	b, err = os.ReadFile(filepath.Join(dir, "browser_sealed.json"))
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(b, &vec))
	priv, err := hex.DecodeString(key.PrivateHex)
	require.NoError(t, err)
	k, err := LoadSealKey(key.KID, priv)
	require.NoError(t, err)
	got, err := k.Unseal(vec.Sealed, vec.Purpose)
	require.NoError(t, err)
	assert.Equal(t, vec.Plaintext, string(got))
}
