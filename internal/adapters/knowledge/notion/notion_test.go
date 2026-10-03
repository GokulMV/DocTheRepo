package notion

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/GokulMV/DocTheRepo/internal/adapters/httpx"
	"github.com/GokulMV/DocTheRepo/internal/ports"
)

func rt(s string, ann map[string]bool, href string) map[string]any {
	a := map[string]any{}
	for k, v := range ann {
		a[k] = v
	}
	m := map[string]any{"plain_text": s, "annotations": a}
	if href != "" {
		m["href"] = href
	}
	return m
}

func blk(typ string, body map[string]any, children bool) map[string]any {
	return map[string]any{"id": "b-" + typ, "type": typ, "has_children": children, typ: body}
}

func fakeNotion(t *testing.T) *httptest.Server {
	pages := []map[string]any{
		{"id": "p2", "url": "https://www.notion.so/Runbook-p2", "last_edited_time": "2026-09-02T10:00:00.000Z",
			"properties": map[string]any{"Name": map[string]any{"type": "title", "title": []any{rt("Payments runbook", nil, "")}}}},
		{"id": "p1", "url": "https://www.notion.so/Old-p1", "last_edited_time": "2026-08-01T10:00:00.000Z",
			"properties": map[string]any{"title": map[string]any{"type": "title", "title": []any{rt("Old page", nil, "")}}}},
		{"id": "p0", "archived": true, "last_edited_time": "2026-07-01T10:00:00.000Z", "properties": map[string]any{}},
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/search", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer secret_x" || r.Header.Get("Notion-Version") != Version {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		var in map[string]any
		_ = json.NewDecoder(r.Body).Decode(&in)
		if in["start_cursor"] == "c2" {
			_ = json.NewEncoder(w).Encode(map[string]any{"results": pages[2:], "has_more": false})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"results": pages[:2], "has_more": true, "next_cursor": "c2"})
	})
	mux.HandleFunc("/blocks/p2/children", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"results": []any{
			blk("heading_1", map[string]any{"rich_text": []any{rt("When payments fail", nil, "")}}, false),
			blk("paragraph", map[string]any{"rich_text": []any{rt("Check the ", nil, ""), rt("gateway", map[string]bool{"bold": true}, ""), rt(" status ", nil, ""), rt("page", nil, "https://status.example")}}, false),
			blk("numbered_list_item", map[string]any{"rich_text": []any{rt("Drain", nil, "")}}, false),
			blk("numbered_list_item", map[string]any{"rich_text": []any{rt("Fail over", nil, "")}}, true),
			blk("code", map[string]any{"language": "bash", "rich_text": []any{rt("kubectl rollout restart deploy/pay", nil, "")}}, false),
			blk("to_do", map[string]any{"checked": true, "rich_text": []any{rt("Tell support", nil, "")}}, false),
		}})
	})
	mux.HandleFunc("/blocks/b-numbered_list_item/children", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"results": []any{blk("bulleted_list_item", map[string]any{"rich_text": []any{rt("Promote the replica", nil, "")}}, false)}})
	})
	mux.HandleFunc("/blocks/p1/children", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"results": []any{blk("paragraph", map[string]any{"rich_text": []any{rt("Old text", nil, "")}}, false)}})
	})
	mux.HandleFunc("/pages/0123456789abcdef0123456789abcdef", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(pages[1])
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func TestNotionSync(t *testing.T) {
	srv := fakeNotion(t)
	s := &Source{Base: srv.URL, HTTP: httpx.New("notion")}
	cc := ports.ConnectorConfig{Type: "notion", Credentials: "secret_x"}
	var got []ports.KnowledgeDoc
	var cursors []string
	err := s.Changed(context.Background(), cc, Space, "", func(docs []ports.KnowledgeDoc, cursor string) error {
		got = append(got, docs...)
		cursors = append(cursors, cursor)
		return nil
	})
	require.NoError(t, err)
	require.Len(t, got, 2, "archived pages are skipped")
	assert.Equal(t, "Old page", got[0].Title, "oldest first")
	d := got[1]
	assert.Equal(t, ports.SourceNotion, d.Source)
	assert.Equal(t, "p2", d.ExternalID)
	assert.Equal(t, Space, d.Space)
	assert.Equal(t, "https://www.notion.so/Runbook-p2", d.URL)
	for _, want := range []string{"## When payments fail", "Check the **gateway** status [page](https://status.example)", "1. Drain", "2. Fail over",
		"  - Promote the replica", "```bash\nkubectl rollout restart deploy/pay\n```", "- [x] Tell support"} {
		assert.Contains(t, d.Markdown, want)
	}
	assert.Equal(t, []string{"2026-09-02T10:00:00Z"}, cursors)

	// An incremental sync stops at the cursor.
	got = nil
	require.NoError(t, s.Changed(context.Background(), cc, Space, "2026-08-15T00:00:00Z", func(docs []ports.KnowledgeDoc, _ string) error {
		got = append(got, docs...)
		return nil
	}))
	require.Len(t, got, 1)
	assert.Equal(t, "p2", got[0].ExternalID)

	ids, err := s.IDs(context.Background(), cc, Space)
	require.NoError(t, err)
	assert.Equal(t, []string{"p2", "p1"}, ids)

	assert.True(t, s.Owns(cc, "https://www.notion.so/acme/Old-page-0123456789abcdef0123456789abcdef"))
	assert.True(t, s.Owns(cc, "https://acme.notion.site/x"))
	assert.False(t, s.Owns(cc, "https://acme.atlassian.net/wiki"))
	doc, err := s.Fetch(context.Background(), cc, "https://www.notion.so/acme/Old-page-0123456789abcdef0123456789abcdef")
	require.NoError(t, err)
	assert.Equal(t, "Old page", doc.Title)

	_, err = (&Source{Base: srv.URL, HTTP: httpx.New("notion")}).IDs(context.Background(), ports.ConnectorConfig{Credentials: "wrong"}, Space)
	var v *ports.ValidationError
	require.ErrorAs(t, err, &v)
	assert.True(t, strings.Contains(v.Message, "rejected"))
	_, err = s.IDs(context.Background(), ports.ConnectorConfig{}, Space)
	require.ErrorAs(t, err, &v)
}
