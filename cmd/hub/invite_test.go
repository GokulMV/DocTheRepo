package main

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/GokulMV/DocTheRepo/internal/store/storetest"
)

// Break glass: with nobody able to sign in, an operator where the Hub runs gets a password link.
func TestInviteBreakGlass(t *testing.T) {
	t.Setenv("DTH_DATABASE_URL", storetest.URL(t))
	t.Setenv("DTH_PUBLIC_URL", "https://hub.acme.com/")
	var out bytes.Buffer
	require.NoError(t, invite([]string{"Ann@acme.com"}, "", &out), out.String())
	assert.Contains(t, out.String(), "Created ann@acme.com as owner.")
	assert.Regexp(t, `https://hub\.acme\.com/invite/[A-Za-z0-9_-]{20,}`, out.String())
	out.Reset()
	require.NoError(t, invite([]string{"ann@acme.com"}, "", &out))
	assert.NotContains(t, out.String(), "Created", "an existing user gets a new link")
	assert.Error(t, invite([]string{"x@acme.com", "boss"}, "", &out))
	assert.Error(t, invite(nil, "", &out))
}
