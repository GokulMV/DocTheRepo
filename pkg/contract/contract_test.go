package contract

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestValidate_DocGenResult(t *testing.T) {
	good := DocGenResult{ContractVersion: "2.0", Status: "success", Docs: []GeneratedDoc{{Path: "docs/a.md", ChunkID: "0123456789abcdef", Symbol: "F", Content: "x"}},
		Usage: &ReportedUsage{InputTokens: 1, OutputTokens: 2}, Extensions: map[string]any{"future": true}}
	b, _ := json.Marshal(good)
	assert.Empty(t, Validate(DocGenResultSchema, b))

	bad := `{"contract_version":"1.0","status":"done","error":7,"docs":[{"path":"","chunk_id":"xyz","symbol":"F"}],"usage":{"input_tokens":-1,"output_tokens":1.5}}`
	probs := Validate(DocGenResultSchema, []byte(bad))
	for _, want := range []string{
		"$.contract_version: must match pattern",
		"$.status: must be one of",
		"$.error: expected [string null], got number",
		`$.docs[0]: missing required field "content"`,
		"$.docs[0].path: must be at least 1 characters",
		"$.docs[0].chunk_id: must match pattern",
		"$.usage.input_tokens: must be >= 0",
		"$.usage.output_tokens: expected integer, got number",
	} {
		found := false
		for _, p := range probs {
			if strings.HasPrefix(p, want) {
				found = true
			}
		}
		assert.True(t, found, "missing %q in %v", want, probs)
	}
	assert.Equal(t, []string{`$: missing required field "docs"`}, Validate(DocGenResultSchema, []byte(`{"contract_version":"2.1","status":"success"}`)))
}

func TestValidate_StrictSchemasRejectExtraFields(t *testing.T) {
	probs := Validate(TriageSchema, []byte(`{"cosmetic":true,"confident":false,"reason":"r","extra":1,"another":2}`))
	assert.Equal(t, []string{`$: unexpected field "another"`, `$: unexpected field "extra"`}, probs)
	assert.Empty(t, Validate(DecodeSchema, []byte(`{"summary":"s","probable_cause":"","impact":"","affected_code":[{"chunk_id":"c","reason":"r"}],
		"next_steps":["a"],"confidence":"high","is_actionable":true,"suggest_known_issue":false}`)))
	assert.NotEmpty(t, Validate(DecodeSchema, []byte(`{"summary":"","confidence":"sure"}`)))
}

func TestValidate_BadJSONAndMisc(t *testing.T) {
	assert.Contains(t, Validate(TriageSchema, []byte(`{`))[0], "not valid JSON")
	assert.Contains(t, Validate(TriageSchema, []byte(`{} {}`))[0], "trailing")
	s := Schema{"type": "array", "minItems": 1, "maxItems": 2, "items": Schema{"type": "string", "maxLength": 2, "const": "ab"}}
	assert.Empty(t, Validate(s, []byte(`["ab"]`)))
	assert.Len(t, Validate(s, []byte(`[]`)), 1)
	assert.Len(t, Validate(s, []byte(`["abc","ab","ab"]`)), 3)
	assert.Empty(t, Validate(Schema{"type": "number", "maximum": 5}, []byte(`4.5`)))
	assert.Len(t, Validate(Schema{"type": "number", "maximum": 5}, []byte(`6`)), 1)
	assert.Empty(t, Validate(Schema{"type": []string{"boolean", "null"}}, []byte(`null`)))
	assert.Len(t, Validate(Schema{"type": "object"}, []byte(`[]`)), 1)
}

func TestExtractJSON(t *testing.T) {
	cases := map[string]string{
		"```json\n{\"a\":1}\n```":               `{"a":1}`,
		"Here you go:\n{\"a\":{\"b\":2}}\nDone": `{"a":{"b":2}}`,
		`{"a":1}`:                               `{"a":1}`,
		"no json here":                          "no json here",
	}
	for in, want := range cases {
		assert.Equal(t, want, string(ExtractJSON(in)), in)
	}
}
