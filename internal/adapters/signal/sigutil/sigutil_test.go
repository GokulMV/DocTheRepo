package sigutil

import (
	"encoding/base64"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/GokulMV/DocTheRepo/internal/ports"
)

func TestVerifyTokenSchemes(t *testing.T) {
	const s = "s3cret-token"
	ok := []ports.WebhookRequest{
		{Header: map[string][]string{"Authorization": {"Bearer " + s}}},
		{Header: map[string][]string{"authorization": {"token " + s}}},
		{Header: map[string][]string{"Authorization": {"Basic " + base64.StdEncoding.EncodeToString([]byte("alertmanager:"+s))}}},
		{Header: map[string][]string{"X-Dth-Token": {s}}},
		{Query: map[string][]string{"token": {s}}},
	}
	for i, r := range ok {
		assert.NoError(t, VerifyToken(r, s), i)
	}
	bad := []ports.WebhookRequest{
		{},
		{Header: map[string][]string{"Authorization": {"Bearer nope"}}},
		{Header: map[string][]string{"Authorization": {"Basic !!!"}}},
		{Header: map[string][]string{"Authorization": {"Basic " + base64.StdEncoding.EncodeToString([]byte(s))}}}, // no colon: not user:password
		{Query: map[string][]string{"token": {""}}},
	}
	for i, r := range bad {
		assert.ErrorIs(t, VerifyToken(r, s), ports.ErrInvalidSignature, i)
	}
	assert.ErrorIs(t, VerifyToken(ok[0], ""), ports.ErrInvalidSignature)
	assert.ErrorIs(t, VerifyHMAC([]byte("x"), "k", "zz"), ports.ErrInvalidSignature, "non-hex signature")
}

func TestJSONHelpersAndTime(t *testing.T) {
	v, err := Decode([]byte(`{"a":{"b":[{"c":"x"},{"c":42}]},"n":1790000000123,"f":true}`))
	require.NoError(t, err)
	assert.Equal(t, "x", Str(v, "a.b.0.c"))
	assert.Equal(t, "42", Str(v, "a.b.1.c"))
	assert.Equal(t, "", Str(v, "a.b.9.c"))
	assert.Equal(t, "", Str(v, "a.b"), "non-scalars are not strings")
	assert.Equal(t, "true", Str(v, "f"))
	assert.Equal(t, "42", First(v, "missing", "a.b.1.c"))
	_, err = Decode([]byte("nope"))
	var ve *ports.ValidationError
	assert.ErrorAs(t, err, &ve)

	want := time.Date(2026, 9, 21, 14, 13, 20, 0, time.UTC)
	assert.Equal(t, want, Time("2026-09-21T14:13:20Z"))
	assert.Equal(t, want, Time("1790000000"))
	assert.Equal(t, want.Add(123*time.Millisecond), Time("1790000000123"))
	assert.Equal(t, want, Time("1790000000000000000"))
	assert.True(t, Time("soon").IsZero())

	assert.Equal(t, map[string]string{"service": "api", "env": "prod", "flag": "true"}, Tags("service:api, env:prod,flag,"))
	assert.Equal(t, map[string]string{"a": "b"}, Tags([]any{"a:b", 3}))
	assert.Equal(t, map[string]string{"k": "keep", "n": "new"}, Merge(map[string]string{"k": "keep"}, map[string]string{"k": "x", "n": "new", "e": ""}, true))
}
