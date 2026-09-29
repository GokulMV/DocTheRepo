// Package gcpkms is a KeyEncrypter backed by a Cloud KMS key, called over the REST API with Application
// Default Credentials (the Cloud Run / GKE workload identity).
package gcpkms

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"

	"github.com/GokulMV/DocTheRepo/internal/adapters/httpx"
)

// Scope is the Cloud KMS OAuth scope.
const Scope = "https://www.googleapis.com/auth/cloudkms"

// aad binds ciphertexts to this use.
var aad = []byte("doctherepo-hub:dek")

// Encrypter implements ports.KeyEncrypter.
type Encrypter struct {
	name string // projects/p/locations/l/keyRings/r/cryptoKeys/k
	base string
	ts   oauth2.TokenSource
	http *httpx.Client
}

// New uses Application Default Credentials. keyName is the full crypto key resource name.
func New(ctx context.Context, keyName string) (*Encrypter, error) {
	ts, err := google.DefaultTokenSource(ctx, Scope)
	if err != nil {
		return nil, fmt.Errorf("google credentials: %w", err)
	}
	return NewWithTokenSource(keyName, "https://cloudkms.googleapis.com", ts, nil)
}

// NewWithTokenSource builds an encrypter against base (tests use a mock server).
func NewWithTokenSource(keyName, base string, ts oauth2.TokenSource, c *httpx.Client) (*Encrypter, error) {
	if !strings.HasPrefix(keyName, "projects/") || !strings.Contains(keyName, "/cryptoKeys/") {
		return nil, errors.New("secrets.kms_key_id must be projects/<p>/locations/<l>/keyRings/<r>/cryptoKeys/<k> for gcpkms")
	}
	if c == nil {
		c = httpx.New("gcpkms")
	}
	return &Encrypter{name: keyName, base: strings.TrimRight(base, "/"), ts: ts, http: c}, nil
}

// KeyID returns the key resource name.
func (e *Encrypter) KeyID() string { return "gcpkms:" + e.name }

func (e *Encrypter) call(ctx context.Context, verb string, in, out any) error {
	tok, err := e.ts.Token()
	if err != nil {
		return fmt.Errorf("google token: %w", err)
	}
	return e.http.JSON(ctx, http.MethodPost, fmt.Sprintf("%s/v1/%s:%s", e.base, e.name, verb),
		map[string]string{"Authorization": "Bearer " + tok.AccessToken}, in, out)
}

// WrapKey encrypts a data key.
func (e *Encrypter) WrapKey(ctx context.Context, dek []byte) ([]byte, error) {
	var out struct {
		Ciphertext string `json:"ciphertext"`
	}
	if err := e.call(ctx, "encrypt", map[string]string{"plaintext": base64.StdEncoding.EncodeToString(dek),
		"additionalAuthenticatedData": base64.StdEncoding.EncodeToString(aad)}, &out); err != nil {
		return nil, fmt.Errorf("cloud kms encrypt: %w", err)
	}
	return base64.StdEncoding.DecodeString(out.Ciphertext)
}

// UnwrapKey decrypts a data key.
func (e *Encrypter) UnwrapKey(ctx context.Context, wrapped []byte) ([]byte, error) {
	var out struct {
		Plaintext string `json:"plaintext"`
	}
	if err := e.call(ctx, "decrypt", map[string]string{"ciphertext": base64.StdEncoding.EncodeToString(wrapped),
		"additionalAuthenticatedData": base64.StdEncoding.EncodeToString(aad)}, &out); err != nil {
		return nil, fmt.Errorf("cloud kms decrypt: %w", err)
	}
	return base64.StdEncoding.DecodeString(out.Plaintext)
}
