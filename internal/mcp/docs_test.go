package mcp

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

// The document tools pass on confidence, and tell the agent which sections to check against the code.
func TestReadDocumentCarriesConfidence(t *testing.T) {
	call := func(_ context.Context, _, path string, _, out any) error {
		var v any
		switch {
		case strings.HasPrefix(path, "/repos"):
			v = map[string]any{"items": []any{map[string]any{"id": "r1", "full_name": "acme/shop"}}}
		case strings.HasPrefix(path, "/repo-docs/find"):
			if !strings.Contains(path, "type=module") || !strings.Contains(path, "key=internal-payments") {
				t.Errorf("path = %s", path)
			}
			v = map[string]any{"title": "internal/payments", "at_a_glance": "Moves money.", "confidence": 0.66, "label": "medium", "why": []string{"no citations to the code"},
				"calibrated": true, "source_sha": "abcdef1234", "changed": 0.4, "gaps": []string{"refund limits"},
				"sections": []any{map[string]any{"title": "How it works", "markdown": "It charges.", "label": "low", "why": []string{"the cited code does not clearly support it"}}}}
		case strings.HasPrefix(path, "/repo-docs"):
			v = map[string]any{"items": []any{map[string]any{"type": "module", "key": "internal-payments", "title": "internal/payments", "group": "Modules", "label": "medium", "confidence": 0.66, "status": "ok", "at_a_glance": "Moves money."}}}
		}
		b, _ := json.Marshal(v)
		return json.Unmarshal(b, out)
	}
	tools := map[string]Tool{}
	for _, tl := range HubTools(call) {
		tools[tl.Name] = tl
	}
	list, err := tools["list_docs"].Call(context.Background(), json.RawMessage(`{"repo":"acme/shop"}`))
	if err != nil || !strings.Contains(list, `key "internal-payments"`) || !strings.Contains(list, "confidence medium 66%") {
		t.Fatalf("list_docs = %q, %v", list, err)
	}
	doc, err := tools["read_document"].Call(context.Background(), json.RawMessage(`{"repo":"acme/shop","type":"module","key":"internal-payments"}`))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Confidence: medium (66%), judged by a calibrated model", "Why: no citations to the code", "40% of its code changed since",
		"(Check against the code: low confidence: the cited code does not clearly support it.)", "## Not determined from the code"} {
		if !strings.Contains(doc, want) {
			t.Errorf("read_document lacks %q:\n%s", want, doc)
		}
	}
}
