// Package secrets implements envelope encryption for connector credentials and LLM keys: each secret is
// encrypted with a fresh AES-256-GCM data key, and that data key is wrapped by a KeyEncrypter.
package secrets

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"

	"github.com/GokulMV/DocTheRepo/internal/ports"
)

const formatV1 byte = 1

// Box seals and opens secrets.
type Box struct{ kek ports.KeyEncrypter }

// NewBox returns a Box using kek to wrap data keys.
func NewBox(kek ports.KeyEncrypter) *Box { return &Box{kek: kek} }

// ErrCorrupt is returned for ciphertexts that are malformed or fail authentication.
var ErrCorrupt = errors.New("secret ciphertext is corrupt or was bound to a different record")

// Seal encrypts plaintext. aad binds the ciphertext to its owner (e.g. "connector:<id>:creds") so a
// ciphertext copied onto another row fails to open.
//
// Layout: version(1) | keyIDLen(1) | keyID | wrappedLen(2, BE) | wrappedDEK | nonce(12) | ciphertext+tag
func (b *Box) Seal(ctx context.Context, plaintext, aad []byte) ([]byte, error) {
	dek := make([]byte, 32)
	if _, err := rand.Read(dek); err != nil {
		return nil, fmt.Errorf("generate data key: %w", err)
	}
	defer clear(dek)
	wrapped, err := b.kek.WrapKey(ctx, dek)
	if err != nil {
		return nil, fmt.Errorf("wrap data key: %w", err)
	}
	gcm, err := newGCM(dek)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, fmt.Errorf("generate nonce: %w", err)
	}
	keyID := b.kek.KeyID()
	if len(keyID) > 255 || len(wrapped) > 65535 {
		return nil, errors.New("key id or wrapped key too long")
	}
	out := make([]byte, 0, 4+len(keyID)+len(wrapped)+len(nonce)+len(plaintext)+gcm.Overhead())
	out = append(out, formatV1, byte(len(keyID)))
	out = append(out, keyID...)
	out = binary.BigEndian.AppendUint16(out, uint16(len(wrapped)))
	out = append(out, wrapped...)
	out = append(out, nonce...)
	return gcm.Seal(out, nonce, plaintext, aad), nil
}

// Open decrypts a ciphertext produced by Seal with the same aad.
func (b *Box) Open(ctx context.Context, blob, aad []byte) ([]byte, error) {
	if len(blob) < 2 || blob[0] != formatV1 {
		return nil, ErrCorrupt
	}
	p := 1
	kl := int(blob[p])
	p++
	if len(blob) < p+kl+2 {
		return nil, ErrCorrupt
	}
	keyID := string(blob[p : p+kl])
	p += kl
	if keyID != b.kek.KeyID() {
		return nil, fmt.Errorf("secret was sealed with key %q but the configured key is %q", keyID, b.kek.KeyID())
	}
	wl := int(binary.BigEndian.Uint16(blob[p:]))
	p += 2
	if len(blob) < p+wl+12 {
		return nil, ErrCorrupt
	}
	dek, err := b.kek.UnwrapKey(ctx, blob[p:p+wl])
	if err != nil {
		return nil, fmt.Errorf("unwrap data key: %w", err)
	}
	defer clear(dek)
	p += wl
	gcm, err := newGCM(dek)
	if err != nil {
		return nil, err
	}
	nonce := blob[p : p+gcm.NonceSize()]
	pt, err := gcm.Open(nil, nonce, blob[p+gcm.NonceSize():], aad)
	if err != nil {
		return nil, ErrCorrupt
	}
	return pt, nil
}

func newGCM(key []byte) (cipher.AEAD, error) {
	blk, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("init cipher: %w", err)
	}
	return cipher.NewGCM(blk)
}

// ProviderKeyAAD binds an LLM provider's key ciphertext to its row.
func ProviderKeyAAD(providerID string) []byte { return []byte("llm_provider:" + providerID + ":key") }

// ConnectorCredsAAD binds a connector's credential ciphertext to its row.
func ConnectorCredsAAD(connectorID string) []byte {
	return []byte("connector:" + connectorID + ":creds")
}

// ConnectorWebhookAAD binds a connector's webhook secret ciphertext to its row.
func ConnectorWebhookAAD(connectorID string) []byte {
	return []byte("connector:" + connectorID + ":webhook")
}

// ConnectorOAuthClientAAD binds a connector's OAuth client secret (a GitHub App's) to its row.
func ConnectorOAuthClientAAD(connectorID string) []byte {
	return []byte("connector:" + connectorID + ":oauth_client")
}

// MCPSecretAAD binds an MCP connection's key (token, header value, cloud credentials) to its row.
func MCPSecretAAD(id string) []byte { return []byte("mcp_server:" + id + ":secret") }

// MCPOAuthAAD binds an MCP connection's OAuth client and tokens to its row.
func MCPOAuthAAD(id string) []byte { return []byte("mcp_server:" + id + ":oauth") }

// KeyIDOf returns the key that sealed blob, without opening it.
func KeyIDOf(blob []byte) (string, error) {
	if len(blob) < 2 || blob[0] != formatV1 || len(blob) < 2+int(blob[1]) {
		return "", ErrCorrupt
	}
	return string(blob[2 : 2+int(blob[1])]), nil
}

// Rewrap moves a sealed secret from one key-encryption key to another: the data key is unwrapped with
// from and wrapped with to, and the encrypted secret itself is untouched (no plaintext is handled, and
// the binding to its record stays). A blob already under to is returned as is with changed false.
func Rewrap(ctx context.Context, blob []byte, from, to ports.KeyEncrypter) (out []byte, changed bool, err error) {
	keyID, err := KeyIDOf(blob)
	if err != nil {
		return nil, false, err
	}
	if keyID == to.KeyID() {
		return blob, false, nil
	}
	if keyID != from.KeyID() {
		return nil, false, fmt.Errorf("secret was sealed with key %q, which is neither the current key %q nor the new key %q", keyID, from.KeyID(), to.KeyID())
	}
	p := 2 + len(keyID)
	if len(blob) < p+2 {
		return nil, false, ErrCorrupt
	}
	wl := int(binary.BigEndian.Uint16(blob[p:]))
	p += 2
	if len(blob) < p+wl+12 {
		return nil, false, ErrCorrupt
	}
	dek, err := from.UnwrapKey(ctx, blob[p:p+wl])
	if err != nil {
		return nil, false, fmt.Errorf("unwrap data key: %w", err)
	}
	defer clear(dek)
	wrapped, err := to.WrapKey(ctx, dek)
	if err != nil {
		return nil, false, fmt.Errorf("wrap data key: %w", err)
	}
	newID := to.KeyID()
	if len(newID) > 255 || len(wrapped) > 65535 {
		return nil, false, errors.New("key id or wrapped key too long")
	}
	rest := blob[p+wl:]
	out = make([]byte, 0, 4+len(newID)+len(wrapped)+len(rest))
	out = append(out, formatV1, byte(len(newID)))
	out = append(out, newID...)
	out = binary.BigEndian.AppendUint16(out, uint16(len(wrapped)))
	out = append(out, wrapped...)
	return append(out, rest...), true, nil
}
