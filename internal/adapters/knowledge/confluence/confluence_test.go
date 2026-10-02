package confluence

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/GokulMV/DocTheRepo/internal/ports"
)

func TestToMarkdown(t *testing.T) {
	in := `<h2>Retry &amp; timeouts</h2><p>Calls to <strong>payments-gw</strong> time out after&nbsp;30s.</p>
<ac:structured-macro ac:name="code"><ac:parameter ac:name="language">go</ac:parameter><ac:plain-text-body><![CDATA[if a > b {
	return err
}]]></ac:plain-text-body></ac:structured-macro>
<ac:structured-macro ac:name="warning"><ac:rich-text-body><p>Do not restart during peak.</p></ac:rich-text-body></ac:structured-macro>
<ul><li>one<ul><li>nested</li></ul></li><li>see <ac:link><ri:page ri:content-title="Checkout runbook" /></ac:link></li></ul>
<table><tbody><tr><th>Code</th><th>Meaning</th></tr><tr><td>503</td><td>gw | down</td></tr></tbody></table>
<ac:structured-macro ac:name="toc" />`
	got := ToMarkdown(in)
	for _, want := range []string{"## Retry & timeouts", "Calls to **payments-gw** time out after 30s.", "```go\nif a > b {\n\treturn err\n}\n```",
		"> **Warning:**", "> Do not restart during peak.", "- one\n  - nested", "- see Checkout runbook", "| Code | Meaning |\n| --- | --- |\n| 503 | gw \\| down |"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
}

func TestPageID(t *testing.T) {
	for in, want := range map[string]string{
		"https://x.atlassian.net/wiki/spaces/ENG/pages/12345/Checkout+runbook": "12345",
		"https://wiki.acme.com/pages/viewpage.action?pageId=987":               "987",
		"https://x.atlassian.net/wiki/spaces/ENG/overview":                     "",
	} {
		if got := PageID(in); got != want {
			t.Errorf("PageID(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestChangedPaginatesAndAdvancesCursor(t *testing.T) {
	var cqls []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer tok" {
			w.WriteHeader(401)
			return
		}
		cqls = append(cqls, r.URL.Query().Get("cql"))
		page := map[string]any{"results": []map[string]any{{"id": "1", "type": "page", "title": "A", "space": map[string]any{"key": "ENG"},
			"version": map[string]any{"when": "2026-10-01T10:00:00Z"}, "body": map[string]any{"storage": map[string]any{"value": "<p>a</p>"}},
			"metadata": map[string]any{"labels": map[string]any{"results": []map[string]any{{"name": "Known-Issue"}}}}, "_links": map[string]any{"webui": "/spaces/ENG/pages/1/A"}}},
			"_links": map[string]any{"next": "/rest/api/content/search?cursor=2"}}
		if r.URL.Query().Get("cursor") == "2" {
			page = map[string]any{"results": []map[string]any{{"id": "2", "type": "page", "title": "B", "space": map[string]any{"key": "ENG"},
				"version": map[string]any{"when": "2026-10-01T11:00:00Z"}}}, "_links": map[string]any{}}
		}
		_ = json.NewEncoder(w).Encode(page)
	}))
	defer srv.Close()
	cc := ports.ConnectorConfig{Type: "confluence", Config: map[string]string{"base_url": srv.URL, "spaces": "ENG", "timezone": "UTC"}, Credentials: "tok"}
	var docs []ports.KnowledgeDoc
	var cursors []string
	err := New().Changed(context.Background(), cc, "ENG", "2026-10-01T09:30:45Z", func(d []ports.KnowledgeDoc, c string) error {
		docs, cursors = append(docs, d...), append(cursors, c)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(docs) != 2 || docs[0].URL != srv.URL+"/spaces/ENG/pages/1/A" || docs[0].Labels[0] != "known-issue" || docs[0].Markdown != "a" {
		t.Fatalf("docs = %+v", docs)
	}
	if cursors[1] != "2026-10-01T11:00:00Z" {
		t.Fatalf("cursors = %v", cursors)
	}
	if !strings.Contains(cqls[0], `lastmodified >= "2026-10-01 09:30"`) || !strings.Contains(cqls[0], `space = "ENG"`) {
		t.Fatalf("cql = %s", cqls[0])
	}
}
