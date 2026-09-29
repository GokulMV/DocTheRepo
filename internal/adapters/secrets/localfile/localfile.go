// Package localfile is a KeyEncrypter whose key-encryption key is a 32-byte file on local disk. It is the
// default for laptops and single-VM installs; cloud deployments use awskms or gcpkms.
package localfile

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// Encrypter wraps data keys with AES-256-GCM under the file key.
type Encrypter struct {
	gcm   cipher.AEAD
	keyID string
}

// Open loads the key file, creating it with 0600 permissions if it does not exist. An existing file with
// permissions wider than 0600 is rejected: a world-readable master key defeats envelope encryption.
func Open(path string) (*Encrypter, error) {
	key, err := os.ReadFile(path)
	switch {
	case errors.Is(err, os.ErrNotExist):
		key = make([]byte, 32)
		if _, err := rand.Read(key); err != nil {
			return nil, fmt.Errorf("generate master key: %w", err)
		}
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			return nil, fmt.Errorf("create key directory: %w", err)
		}
		f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if err != nil {
			return nil, fmt.Errorf("create master key %s: %w", path, err)
		}
		if _, err := f.Write(key); err != nil {
			f.Close()
			return nil, fmt.Errorf("write master key: %w", err)
		}
		if err := f.Close(); err != nil {
			return nil, fmt.Errorf("write master key: %w", err)
		}
	case err != nil:
		return nil, fmt.Errorf("read master key %s: %w", path, err)
	default:
		st, err := os.Stat(path)
		if err != nil {
			return nil, err
		}
		if st.Mode().Perm()&0o077 != 0 {
			return nil, fmt.Errorf("master key %s has permissions %o; run `chmod 600 %s`", path, st.Mode().Perm(), path)
		}
	}
	if len(key) != 32 {
		return nil, fmt.Errorf("master key %s must be exactly 32 bytes, got %d", path, len(key))
	}
	blk, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(blk)
	if err != nil {
		return nil, err
	}
	fp := sha256.Sum256(key)
	return &Encrypter{gcm: gcm, keyID: "local:" + hex.EncodeToString(fp[:4])}, nil
}

// KeyID is a short fingerprint of the key, safe to store and log.
func (e *Encrypter) KeyID() string { return e.keyID }

// WrapKey encrypts a data key.
func (e *Encrypter) WrapKey(_ context.Context, dek []byte) ([]byte, error) {
	nonce := make([]byte, e.gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	return e.gcm.Seal(nonce, nonce, dek, []byte("dth-dek")), nil
}

// UnwrapKey decrypts a data key.
func (e *Encrypter) UnwrapKey(_ context.Context, wrapped []byte) ([]byte, error) {
	ns := e.gcm.NonceSize()
	if len(wrapped) < ns {
		return nil, errors.New("wrapped key too short")
	}
	dek, err := e.gcm.Open(nil, wrapped[:ns], wrapped[ns:], []byte("dth-dek"))
	if err != nil {
		return nil, errors.New("wrapped key failed authentication (wrong master key?)")
	}
	return dek, nil
}
