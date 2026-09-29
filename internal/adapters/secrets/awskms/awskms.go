// Package awskms is a KeyEncrypter backed by an AWS KMS key: data keys are wrapped with KMS Encrypt and
// unwrapped with Decrypt, so the key-encryption key never leaves KMS and every unwrap is in CloudTrail.
package awskms

import (
	"context"
	"errors"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/kms"
)

// API is the subset of the KMS client used (a fake in tests).
type API interface {
	Encrypt(ctx context.Context, in *kms.EncryptInput, opts ...func(*kms.Options)) (*kms.EncryptOutput, error)
	Decrypt(ctx context.Context, in *kms.DecryptInput, opts ...func(*kms.Options)) (*kms.DecryptOutput, error)
}

// Encrypter implements ports.KeyEncrypter.
type Encrypter struct {
	api   API
	keyID string
}

// encryptionContext binds ciphertexts to this use (KMS rejects a decrypt with a different context).
var encryptionContext = map[string]string{"app": "doctherepo-hub", "purpose": "dek"}

// New uses the default AWS credential chain (task role on ECS, IRSA on EKS, env/profile locally).
func New(ctx context.Context, keyID string) (*Encrypter, error) {
	if keyID == "" {
		return nil, errors.New("secrets.kms_key_id (DTH_KMS_KEY_ID) is required for awskms")
	}
	cfg, err := awsconfig.LoadDefaultConfig(ctx)
	if err != nil {
		return nil, fmt.Errorf("load AWS config: %w", err)
	}
	return NewWithAPI(kms.NewFromConfig(cfg), keyID), nil
}

// NewWithAPI wraps an existing client.
func NewWithAPI(api API, keyID string) *Encrypter { return &Encrypter{api: api, keyID: keyID} }

// KeyID returns the KMS key ARN or alias.
func (e *Encrypter) KeyID() string { return "awskms:" + e.keyID }

// WrapKey encrypts a data key.
func (e *Encrypter) WrapKey(ctx context.Context, dek []byte) ([]byte, error) {
	out, err := e.api.Encrypt(ctx, &kms.EncryptInput{KeyId: aws.String(e.keyID), Plaintext: dek, EncryptionContext: encryptionContext})
	if err != nil {
		return nil, fmt.Errorf("kms encrypt: %w", err)
	}
	return out.CiphertextBlob, nil
}

// UnwrapKey decrypts a data key.
func (e *Encrypter) UnwrapKey(ctx context.Context, wrapped []byte) ([]byte, error) {
	out, err := e.api.Decrypt(ctx, &kms.DecryptInput{KeyId: aws.String(e.keyID), CiphertextBlob: wrapped, EncryptionContext: encryptionContext})
	if err != nil {
		return nil, fmt.Errorf("kms decrypt: %w", err)
	}
	return out.Plaintext, nil
}
