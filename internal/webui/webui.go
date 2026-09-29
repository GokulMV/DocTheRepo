// Package webui serves the React UI embedded into the hub binary: static assets with long-lived caching
// for fingerprinted files, index.html for client-side routes, and strict security headers.
package webui

import (
	"embed"
	"io/fs"
	"net/http"
	"path"
	"strings"
)

// dist holds the Vite build (`make web` copies web/dist here before `go build`).
//
//go:embed all:dist
var dist embed.FS

// CSP allows only same-origin scripts. Styles allow inline attributes because charting and diagram
// libraries set style attributes; no inline scripts are ever used.
const CSP = "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; font-src 'self' data:; " +
	"connect-src 'self'; frame-ancestors 'none'; base-uri 'self'; form-action 'self'"

// Built reports whether the embedded UI exists (false when the binary was built without `make web`).
func Built() bool {
	_, err := fs.Stat(dist, "dist/index.html")
	return err == nil
}

// Handler serves the UI.
func Handler() http.Handler {
	sub, _ := fs.Sub(dist, "dist")
	files := http.FileServer(http.FS(sub))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Security-Policy", CSP)
		p := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
		if p != "" && p != "." {
			if st, err := fs.Stat(sub, p); err == nil && !st.IsDir() {
				if strings.HasPrefix(p, "assets/") {
					w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
				}
				files.ServeHTTP(w, r)
				return
			}
		}
		// Client-side routes (/docs/123, /ask/…) get the app shell; it must never be cached so a new
		// deploy's assets are picked up.
		index, err := fs.ReadFile(sub, "index.html")
		if err != nil {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(notBuilt))
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-cache")
		_, _ = w.Write(index)
	})
}

const notBuilt = `<!doctype html><meta charset="utf-8"><title>DocTheRepo Hub</title>
<body style="font-family:system-ui;margin:3rem"><h1>DocTheRepo Hub is running</h1>
<p>This binary was built without the web UI. Run <code>make web build</code>, or use the API at <code>/api/v1</code>
(spec: <a href="/api/v1/openapi.json">/api/v1/openapi.json</a>).</p></body>`
