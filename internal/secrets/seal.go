package secrets

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdh"
	"crypto/hkdf"
	"crypto/mlkem"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
)

// Sealed values: a secret the browser (or the CLI) encrypted to the Hub's public sealing key before sending
// it. The key is hybrid — X25519 and ML-KEM-768 (FIPS 203) — so a recorded request stays confidential unless
// both are broken, including by a future quantum computer. The two shared secrets are combined with
// HKDF-SHA256 into an AES-256-GCM key; the purpose (which field) is bound in, so a sealed value cannot be
// replayed into another field.
//
// Wire format: "dthseal1:" + base64url(version(1) | kidLen(1) | kid | x25519 ephemeral(32) |
// ML-KEM-768 ciphertext(1088) | nonce(12) | AES-256-GCM ciphertext+tag)
const (
	SealPrefix  = "dthseal1:"
	SealAlg     = "X25519+ML-KEM-768/HKDF-SHA256/AES-256-GCM"
	sealVersion = 1
)

// ErrSealed is returned for sealed values that are malformed, for another key, or fail authentication.
var ErrSealed = errors.New("sealed value is not valid for this Hub (malformed, expired key, or tampered)")

// IsSealed reports whether v is a sealed value.
func IsSealed(v string) bool { return strings.HasPrefix(v, SealPrefix) }

// SealKey is the Hub's private sealing key.
type SealKey struct {
	ID string
	x  *ecdh.PrivateKey
	dk *mlkem.DecapsulationKey768
}

// PublicSealKey is what clients seal to.
type PublicSealKey struct {
	ID       string `json:"kid"`
	Alg      string `json:"alg"`
	X25519   []byte `json:"x25519"`
	MLKEM768 []byte `json:"mlkem768"`
}

// GenerateSealKey makes a new key pair; Private() is what must be stored (sealed by the Box).
func GenerateSealKey(id string) (*SealKey, error) {
	x, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		return nil, err
	}
	dk, err := mlkem.GenerateKey768()
	if err != nil {
		return nil, err
	}
	return &SealKey{ID: id, x: x, dk: dk}, nil
}

// Private serialises the private key: X25519 private (32) | ML-KEM-768 seed (64).
func (k *SealKey) Private() []byte { return append(append([]byte{}, k.x.Bytes()...), k.dk.Bytes()...) }

// LoadSealKey restores a key from Private().
func LoadSealKey(id string, private []byte) (*SealKey, error) {
	if len(private) != 32+mlkem.SeedSize {
		return nil, errors.New("seal key: wrong private key length")
	}
	x, err := ecdh.X25519().NewPrivateKey(private[:32])
	if err != nil {
		return nil, err
	}
	dk, err := mlkem.NewDecapsulationKey768(private[32:])
	if err != nil {
		return nil, err
	}
	return &SealKey{ID: id, x: x, dk: dk}, nil
}

// Public returns the public half.
func (k *SealKey) Public() PublicSealKey {
	return PublicSealKey{ID: k.ID, Alg: SealAlg, X25519: k.x.PublicKey().Bytes(), MLKEM768: k.dk.EncapsulationKey().Bytes()}
}

func sealInfo(kid, purpose string) []byte { return []byte("dthseal-v1|" + kid + "|" + purpose) }

func sealAEAD(ssKEM, ssX, eph, serverX []byte, info []byte) (cipher.AEAD, error) {
	salt := append(append([]byte{}, eph...), serverX...)
	key, err := hkdf.Key(sha256.New, append(append([]byte{}, ssKEM...), ssX...), salt, string(info), 32)
	if err != nil {
		return nil, err
	}
	defer clear(key)
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

// Seal encrypts plaintext to pub for purpose (used by the CLI and tests; the browser has its own copy).
func Seal(pub PublicSealKey, plaintext []byte, purpose string) (string, error) {
	serverX, err := ecdh.X25519().NewPublicKey(pub.X25519)
	if err != nil {
		return "", fmt.Errorf("seal key: %w", err)
	}
	ek, err := mlkem.NewEncapsulationKey768(pub.MLKEM768)
	if err != nil {
		return "", fmt.Errorf("seal key: %w", err)
	}
	if len(pub.ID) > 255 {
		return "", errors.New("seal key id too long")
	}
	eph, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		return "", err
	}
	ssX, err := eph.ECDH(serverX)
	if err != nil {
		return "", err
	}
	ssKEM, ct := ek.Encapsulate()
	info := sealInfo(pub.ID, purpose)
	aead, err := sealAEAD(ssKEM, ssX, eph.PublicKey().Bytes(), pub.X25519, info)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	var b bytes.Buffer
	b.WriteByte(sealVersion)
	b.WriteByte(byte(len(pub.ID)))
	b.WriteString(pub.ID)
	b.Write(eph.PublicKey().Bytes())
	b.Write(ct)
	b.Write(nonce)
	b.Write(aead.Seal(nil, nonce, plaintext, info))
	return SealPrefix + base64.RawURLEncoding.EncodeToString(b.Bytes()), nil
}

// SealedKeyID returns the key id a sealed value names (to pick the right key), or "".
func SealedKeyID(v string) string {
	raw, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(v, SealPrefix))
	if err != nil || len(raw) < 2 || raw[0] != sealVersion || len(raw) < 2+int(raw[1]) {
		return ""
	}
	return string(raw[2 : 2+int(raw[1])])
}

// Unseal decrypts a sealed value for purpose.
func (k *SealKey) Unseal(v, purpose string) ([]byte, error) {
	if !IsSealed(v) {
		return nil, ErrSealed
	}
	raw, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(v, SealPrefix))
	if err != nil || len(raw) < 2 || raw[0] != sealVersion {
		return nil, ErrSealed
	}
	p := 2 + int(raw[1])
	if len(raw) < p || string(raw[2:p]) != k.ID {
		return nil, ErrSealed
	}
	const ephLen, ctLen, nonceLen = 32, mlkem.CiphertextSize768, 12
	if len(raw) < p+ephLen+ctLen+nonceLen+16 {
		return nil, ErrSealed
	}
	eph := raw[p : p+ephLen]
	ct := raw[p+ephLen : p+ephLen+ctLen]
	nonce := raw[p+ephLen+ctLen : p+ephLen+ctLen+nonceLen]
	body := raw[p+ephLen+ctLen+nonceLen:]
	ephPub, err := ecdh.X25519().NewPublicKey(eph)
	if err != nil {
		return nil, ErrSealed
	}
	ssX, err := k.x.ECDH(ephPub)
	if err != nil {
		return nil, ErrSealed
	}
	ssKEM, err := k.dk.Decapsulate(ct)
	if err != nil {
		return nil, ErrSealed
	}
	info := sealInfo(k.ID, purpose)
	aead, err := sealAEAD(ssKEM, ssX, eph, k.x.PublicKey().Bytes(), info)
	if err != nil {
		return nil, ErrSealed
	}
	out, err := aead.Open(nil, nonce, body, info)
	if err != nil {
		return nil, ErrSealed
	}
	return out, nil
}

// SealKeyAAD binds a stored sealing key's private half to its id.
func SealKeyAAD(id string) []byte { return []byte("seal_key:" + id) }

// Purposes a sealed value may be bound to.
const (
	PurposeProviderKey          = "provider.api_key"
	PurposeConnectorCreds       = "connector.credentials"
	PurposeConnectorWebhook     = "connector.webhook_secret"
	PurposeConnectorOAuthClient = "connector.oauth_client_secret" // a GitHub App's client secret
	PurposeOIDCClientSecret     = "auth.oidc_client_secret"
	PurposeMCPSecret            = "mcp.secret"
	// PurposeAtlassianSecret is the Atlassian OAuth app's client secret (one-time setup).
	PurposeAtlassianSecret = "atlassian.client_secret"
)
