package jira

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/GokulMV/DocTheRepo/internal/ports"
)

func TestRichTextADF(t *testing.T) {
	adf := `{"type":"doc","content":[
	 {"type":"heading","attrs":{"level":2},"content":[{"type":"text","text":"Cause"}]},
	 {"type":"paragraph","content":[{"type":"text","text":"Pool ","marks":[]},{"type":"text","text":"exhausted","marks":[{"type":"strong"}]},
	   {"type":"text","text":" see "},{"type":"text","text":"docs","marks":[{"type":"link","attrs":{"href":"https://x"}}]}]},
	 {"type":"bulletList","content":[{"type":"listItem","content":[{"type":"paragraph","content":[{"type":"text","text":"one"}]}]}]},
	 {"type":"codeBlock","attrs":{"language":"sql"},"content":[{"type":"text","text":"select 1;"}]}]}`
	got := RichText(json.RawMessage(adf))
	for _, want := range []string{"## Cause", "Pool **exhausted** see [docs](https://x)", "- one", "```sql\nselect 1;\n```"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
	if RichText(json.RawMessage(`"plain *wiki*"`)) != "plain *wiki*" {
		t.Error("v2 string kept as written")
	}
}

func TestIssueKey(t *testing.T) {
	for in, want := range map[string]string{
		"https://acme.atlassian.net/browse/ENG-12":                     "ENG-12",
		"https://acme.atlassian.net/jira/software?selectedIssue=OPS-3": "OPS-3",
		"https://acme.atlassian.net/browse/":                           "",
	} {
		if got := IssueKey(in); got != want {
			t.Errorf("IssueKey(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestChangedV3UsesProfileZoneAndTokens(t *testing.T) {
	var jqls []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.Header.Get("Authorization"), "Basic ") {
			w.WriteHeader(401)
			return
		}
		switch r.URL.Path {
		case "/rest/api/3/myself":
			_, _ = w.Write([]byte(`{"timeZone":"Europe/Berlin"}`))
		case "/rest/api/3/search/jql":
			jqls = append(jqls, r.URL.Query().Get("jql"))
			if r.URL.Query().Get("nextPageToken") == "" {
				_, _ = w.Write([]byte(`{"issues":[{"key":"ENG-1","fields":{"summary":"Pool exhausted","labels":["known-issue"],
				  "status":{"name":"Done","statusCategory":{"key":"done"}},"updated":"2026-10-01T12:00:00.000+0000","issuetype":{"name":"Bug"},
				  "project":{"key":"ENG"},"description":{"type":"doc","content":[{"type":"paragraph","content":[{"type":"text","text":"boom"}]}]}}}],
				  "nextPageToken":"p2","isLast":false}`))
				return
			}
			_, _ = w.Write([]byte(`{"issues":[],"isLast":true}`))
		default:
			w.WriteHeader(404)
		}
	}))
	defer srv.Close()
	cc := ports.ConnectorConfig{Type: "jira", Config: map[string]string{"base_url": srv.URL, "projects": "ENG", "email": "a@b.c"}, Credentials: "tok"}
	var docs []ports.KnowledgeDoc
	err := New().Changed(context.Background(), cc, "ENG", "2026-10-01T10:00:00Z", func(d []ports.KnowledgeDoc, _ string) error {
		docs = append(docs, d...)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(docs) != 1 || !docs[0].Done || docs[0].Title != "ENG-1: Pool exhausted" || !strings.Contains(docs[0].Markdown, "## Description\n\nboom") {
		t.Fatalf("docs = %+v", docs)
	}
	if !strings.Contains(jqls[0], `updated >= "2026-10-01 12:00"`) { // 10:00 UTC in Berlin (CEST)
		t.Fatalf("jql = %s", jqls[0])
	}
}
