package webui

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestHandlerServesShellForClientRoutes(t *testing.T) {
	h := Handler()
	for _, p := range []string{"/", "/docs/123", "/ask/abc"} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, p, nil))
		assert.Equal(t, http.StatusOK, rec.Code, p)
		assert.Contains(t, rec.Header().Get("Content-Type"), "text/html")
		assert.Equal(t, CSP, rec.Header().Get("Content-Security-Policy"))
		if Built() {
			assert.Contains(t, rec.Body.String(), `<div id="root">`)
			assert.Equal(t, "no-cache", rec.Header().Get("Cache-Control"))
		} else {
			assert.Contains(t, rec.Body.String(), "built without the web UI")
		}
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/", nil))
	assert.Equal(t, http.StatusMethodNotAllowed, rec.Code)
}

func TestMissingAssetIs404(t *testing.T) {
	rec := httptest.NewRecorder()
	Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/assets/Docs-stale123.js", nil))
	assert.Equal(t, http.StatusNotFound, rec.Code, "a stale chunk must not be answered with the HTML shell")
	assert.NotContains(t, rec.Body.String(), `<div id="root">`)
}
