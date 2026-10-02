package settings

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
)

// HTTPAPI is an API over HTTP: Base is the /api/v1 URL, Header is added to every request (a bearer
// token, or the caller's session cookie and CSRF header).
type HTTPAPI struct {
	Base   string
	Header http.Header
	Client *http.Client
}

// Do sends one JSON request.
func (h *HTTPAPI) Do(ctx context.Context, method, path string, in, out any) error {
	var body io.Reader
	if in != nil {
		b, err := json.Marshal(in)
		if err != nil {
			return err
		}
		body = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(h.Base, "/")+path, body)
	if err != nil {
		return err
	}
	for k, vs := range h.Header {
		for _, v := range vs {
			req.Header.Add(k, v)
		}
	}
	req.Header.Set("Accept", "application/json")
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	c := h.Client
	if c == nil {
		c = http.DefaultClient
	}
	resp, err := c.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if resp.StatusCode >= 300 {
		var e struct {
			Error struct {
				Code    string `json:"code"`
				Message string `json:"message"`
			} `json:"error"`
		}
		if json.Unmarshal(raw, &e) == nil && e.Error.Code != "" {
			return fmt.Errorf("%w: %s: %s", ErrAPI, e.Error.Code, e.Error.Message)
		}
		return fmt.Errorf("%w: HTTP %d", ErrAPI, resp.StatusCode)
	}
	if out != nil && len(raw) > 0 {
		return json.Unmarshal(raw, out)
	}
	return nil
}

// HandlerClient is an http.Client whose requests are served in process by h (no network).
func HandlerClient(h http.Handler) *http.Client {
	return &http.Client{Transport: handlerTransport{h}}
}

type handlerTransport struct{ h http.Handler }

func (t handlerTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	rec := httptest.NewRecorder()
	t.h.ServeHTTP(rec, req)
	return rec.Result(), nil
}
