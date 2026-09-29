package awskms

import (
	"bytes"
	"context"
	"errors"
	"maps"
	"testing"

	"github.com/aws/aws-sdk-go-v2/service/kms"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/GokulMV/DocTheRepo/internal/secrets"
)

// fakeKMS "encrypts" by prefixing, and enforces the encryption context like KMS does.
type fakeKMS struct{ ctxSeen map[string]string }

func (f *fakeKMS) Encrypt(_ context.Context, in *kms.EncryptInput, _ ...func(*kms.Options)) (*kms.EncryptOutput, error) {
	f.ctxSeen = in.EncryptionContext
	return &kms.EncryptOutput{CiphertextBlob: append([]byte("kms:"), in.Plaintext...)}, nil
}

func (f *fakeKMS) Decrypt(_ context.Context, in *kms.DecryptInput, _ ...func(*kms.Options)) (*kms.DecryptOutput, error) {
	if !maps.Equal(in.EncryptionContext, f.ctxSeen) || !bytes.HasPrefix(in.CiphertextBlob, []byte("kms:")) {
		return nil, errors.New("InvalidCiphertextException")
	}
	return &kms.DecryptOutput{Plaintext: in.CiphertextBlob[4:]}, nil
}

func TestEnvelopeRoundTripThroughKMS(t *testing.T) {
	e := NewWithAPI(&fakeKMS{}, "alias/dth")
	assert.Equal(t, "awskms:alias/dth", e.KeyID())
	box := secrets.NewBox(e)
	ct, err := box.Seal(context.Background(), []byte("ghp_secret"), []byte("aad"))
	require.NoError(t, err)
	pt, err := box.Open(context.Background(), ct, []byte("aad"))
	require.NoError(t, err)
	assert.Equal(t, "ghp_secret", string(pt))
	_, err = e.UnwrapKey(context.Background(), []byte("garbage"))
	assert.Error(t, err)
	_, err = New(context.Background(), "")
	assert.Error(t, err)
}
