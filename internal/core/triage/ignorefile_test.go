package triage

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseIgnoreFile(t *testing.T) {
	ignore, keep := ParseIgnoreFile([]byte("# not docs\n\nsecrets.go\n/scripts\ngenerated/\ninternal/legacy/**\n*.sql\n!keep.sql\n"))
	tr, err := New(nil, Options{Ignore: ignore, Keep: keep, IndexOnly: []string{}})
	require.NoError(t, err)
	cases := map[string]bool{
		"secrets.go":                true,
		"pkg/secrets.go":            true,
		"scripts/deploy.sh":         true,
		"tools/scripts/x.sh":        false, // "/scripts" is anchored to the root
		"generated/api.go":          true,
		"a/generated/b/c.go":        true,
		"generated.go":              false, // "generated/" is directories only
		"internal/legacy/old.go":    true,
		"internal/legacy/deep/x.go": true,
		"internal/current/x.go":     false,
		"db/schema.sql":             true,
		"db/keep.sql":               false, // re-included
		"cmd/main.go":               false,
	}
	for p, want := range cases {
		assert.Equal(t, want, tr.ignored(p), p)
	}
}

func TestIgnoreMatcher(t *testing.T) {
	ignored, err := IgnoreMatcher([]byte("secrets_test.go\nfixtures/\n!public.json\n"))
	require.NoError(t, err)
	assert.True(t, ignored("internal/vault/secrets_test.go"))
	assert.True(t, ignored("tests/fixtures/keys.json"))
	assert.False(t, ignored("tests/fixtures/public.json"), "re-included")
	assert.False(t, ignored("internal/vault/vault_test.go"), "tests are not skipped by the built-in list here")
	assert.False(t, ignored(".github/workflows/ci.yml"))
}
