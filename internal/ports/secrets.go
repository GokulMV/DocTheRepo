package ports

import "context"

// KeyEncrypter wraps and unwraps data-encryption keys with a key-encryption key held elsewhere
// (AWS KMS, GCP KMS, or a local key file). Adapters never see the plaintext secrets themselves.
type KeyEncrypter interface {
	// KeyID identifies the key-encryption key; it is stored with each ciphertext so rotation can find it.
	KeyID() string
	WrapKey(ctx context.Context, dek []byte) ([]byte, error)
	UnwrapKey(ctx context.Context, wrapped []byte) ([]byte, error)
}
