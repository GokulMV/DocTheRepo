package main

import (
	"bytes"
	"context"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/GokulMV/DocTheRepo/internal/api"
	"github.com/GokulMV/DocTheRepo/internal/core/rag"
)

func (c *apiClient) upload(collection string, files map[string]string) (int, map[string]any) {
	c.t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	_ = mw.WriteField("collection", collection)
	for name, body := range files {
		w, _ := mw.CreateFormFile("files", name)
		_, _ = w.Write([]byte(body))
	}
	_ = mw.Close()
	req, _ := http.NewRequest(http.MethodPost, c.base+"/library/uploads", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.Header.Set(api.CSRFHeader, c.csrf)
	resp, err := c.c.Do(req)
	require.NoError(c.t, err)
	defer resp.Body.Close()
	var out map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&out)
	return resp.StatusCode, out
}

// Uploaded documents become shared team knowledge: listed in the Library, readable in the Hub, found by
// Ask, replaced by a re-upload, and removed on request.
func TestUploadsEndToEnd(t *testing.T) {
	ctx := context.Background()
	env := newSignalEnv(t, "suggest")
	a, st, c := env.a, env.st, env.c
	require.NoError(t, a.docs.SeedShelves(ctx))

	code, out := c.upload("Runbooks", map[string]string{
		"db-failover.md": "# Database failover\n\nPromote the replica with `pg_ctl promote`, then repoint the payments service.\n",
		"refunds.html":   "<html><head><title>Refund rules</title></head><body><h1>Refunds</h1><p>Refunds are idempotent per order.</p></body></html>",
		"spec.pdf":       "%PDF-1.7",
	})
	require.Equal(t, http.StatusCreated, code, out)
	docs := out["documents"].([]any)
	require.Len(t, docs, 2)
	refused := out["refused"].([]any)
	require.Len(t, refused, 1)
	assert.Contains(t, refused[0].(map[string]any)["error"], "export it as Markdown")

	code, out = c.call("GET", "/library/uploads", nil)
	require.Equal(t, http.StatusOK, code, out)
	items := out["items"].([]any)
	require.Len(t, items, 2)
	var failoverID string
	for _, it := range items {
		m := it.(map[string]any)
		assert.Equal(t, "Runbooks", m["collection"])
		if m["title"] == "Database failover" {
			failoverID = m["id"].(string)
		}
	}
	require.NotEmpty(t, failoverID)
	code, out = c.call("GET", "/library/uploads/"+failoverID, nil)
	require.Equal(t, http.StatusOK, code, out)
	assert.Contains(t, out["body"], "pg_ctl promote")

	// Searchable like any doc, under the Hub's own link.
	hits, err := a.qa.FullText(ctx, "promote replica failover", rag.Scope{All: true}, 5)
	require.NoError(t, err)
	require.NotEmpty(t, hits)
	var url string
	require.NoError(t, st.Pool.QueryRow(ctx, `SELECT url FROM knowledge_docs WHERE external_id = $1`, failoverID).Scan(&url))
	assert.Equal(t, "/library/uploads/"+failoverID, url)

	// The same file name in the same collection replaces the document.
	code, out = c.upload("Runbooks", map[string]string{"db-failover.md": "# Database failover\n\nUse the managed failover button.\n"})
	require.Equal(t, http.StatusCreated, code, out)
	assert.Equal(t, failoverID, out["documents"].([]any)[0].(map[string]any)["id"])
	_, out = c.call("GET", "/library/uploads/"+failoverID, nil)
	assert.Contains(t, out["body"], "managed failover")
	assert.NotContains(t, out["body"], "pg_ctl")

	// The internal holder of uploads is not listed as a connector.
	_, out = c.call("GET", "/connectors", nil)
	for _, it := range out["items"].([]any) {
		assert.NotEqual(t, "upload", it.(map[string]any)["type"])
	}

	code, _ = c.call("DELETE", "/library/uploads/"+failoverID, nil)
	assert.Equal(t, http.StatusNoContent, code)
	code, _ = c.call("GET", "/library/uploads/"+failoverID, nil)
	assert.Equal(t, http.StatusNotFound, code)
	var live int
	require.NoError(t, st.Pool.QueryRow(ctx, `SELECT count(*) FROM chunks WHERE source = 'upload' AND path LIKE '%'||$1||'%' AND deleted_at IS NULL`, failoverID).Scan(&live))
	assert.Zero(t, live, "its chunks leave search")
}
