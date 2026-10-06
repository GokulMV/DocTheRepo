// Package mcpclient calls other products' MCP servers over the Streamable HTTP transport: initialize, list
// tools, call a tool. Authentication is pluggable (a bearer token, a header, OAuth, AWS SigV4, Google
// credentials), so the same client talks to Sentry, Atlassian, Datadog, AWS and Google Cloud servers.
package mcpclient

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
)

// ProtocolVersion is the revision the client asks for; servers may answer with an older one they speak.
const ProtocolVersion = "2025-06-18"

// maxBody caps one response (tool results are trimmed further before a model sees them).
const maxBody = 4 << 20

// Authorizer adds credentials to a request; body is the request body (SigV4 signs it).
type Authorizer interface {
	Authorize(ctx context.Context, req *http.Request, body []byte) error
}

// AuthError is a 401 or 403: the server wants credentials (WWWAuthenticate says where to get them).
type AuthError struct {
	Status          int
	WWWAuthenticate string
	Body            string
}

func (e *AuthError) Error() string {
	if e.Status == http.StatusForbidden {
		return "the server refused access (403): the key or account lacks permission"
	}
	return "the server needs sign-in (401)"
}

// Tool is a tool the server offers.
type Tool struct {
	Name        string          `json:"name"`
	Title       string          `json:"title,omitempty"`
	Description string          `json:"description,omitempty"`
	InputSchema json.RawMessage `json:"inputSchema,omitempty"`
	Annotations struct {
		Title           string `json:"title,omitempty"`
		ReadOnlyHint    *bool  `json:"readOnlyHint,omitempty"`
		DestructiveHint *bool  `json:"destructiveHint,omitempty"`
	} `json:"annotations"`
}

// ReadOnly reports whether the server marks the tool as not changing anything.
func (t Tool) ReadOnly() bool {
	return t.Annotations.ReadOnlyHint != nil && *t.Annotations.ReadOnlyHint
}

// Result is a tool's output as text.
type Result struct {
	Text    string
	IsError bool
}

// Client is one MCP server. It keeps the session the server hands out and starts a new one when it expires.
type Client struct {
	URL     string
	HTTP    *http.Client
	Auth    Authorizer
	Headers map[string]string // extra, non-secret headers (e.g. X-Grafana-URL)
	Name    string            // client name sent to the server
	Version string

	mu      sync.Mutex
	session string
	proto   string
	ready   bool
	ids     atomic.Int64
}

type rpcError struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

func (e *rpcError) Error() string { return fmt.Sprintf("server error %d: %s", e.Code, e.Message) }

type rpcResponse struct {
	ID     json.RawMessage `json:"id"`
	Result json.RawMessage `json:"result"`
	Error  *rpcError       `json:"error"`
}

// Initialize starts a session (also done on first use).
func (c *Client) Initialize(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.initLocked(ctx)
}

func (c *Client) initLocked(ctx context.Context) error {
	c.session, c.proto, c.ready = "", "", false
	params := map[string]any{
		"protocolVersion": ProtocolVersion,
		"capabilities":    map[string]any{},
		"clientInfo":      map[string]any{"name": orStr(c.Name, "DocTheRepo Hub"), "version": orStr(c.Version, "dev")},
	}
	var res struct {
		ProtocolVersion string `json:"protocolVersion"`
	}
	sess, err := c.post(ctx, "initialize", params, &res, true)
	if err != nil {
		return err
	}
	c.session, c.proto = sess, orStr(res.ProtocolVersion, ProtocolVersion)
	if _, err := c.post(ctx, "notifications/initialized", nil, nil, false); err != nil {
		return err
	}
	c.ready = true
	return nil
}

// call runs a request, starting (or restarting, when the server forgot the session) a session as needed.
func (c *Client) call(ctx context.Context, method string, params, out any) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.ready {
		if err := c.initLocked(ctx); err != nil {
			return err
		}
	}
	_, err := c.post(ctx, method, params, out, true)
	var se *statusError
	if errors.As(err, &se) && se.status == http.StatusNotFound && c.session != "" {
		if err := c.initLocked(ctx); err != nil {
			return err
		}
		_, err = c.post(ctx, method, params, out, true)
	}
	return err
}

// ListTools returns every tool the server offers.
func (c *Client) ListTools(ctx context.Context) ([]Tool, error) {
	var all []Tool
	cursor := ""
	for page := 0; page < 20; page++ {
		params := map[string]any{}
		if cursor != "" {
			params["cursor"] = cursor
		}
		var res struct {
			Tools      []Tool `json:"tools"`
			NextCursor string `json:"nextCursor"`
		}
		if err := c.call(ctx, "tools/list", params, &res); err != nil {
			return nil, err
		}
		all = append(all, res.Tools...)
		if res.NextCursor == "" {
			break
		}
		cursor = res.NextCursor
	}
	return all, nil
}

// CallTool runs a tool and renders its output as text.
func (c *Client) CallTool(ctx context.Context, name string, args json.RawMessage) (Result, error) {
	if len(bytes.TrimSpace(args)) == 0 {
		args = json.RawMessage(`{}`)
	}
	var res struct {
		Content []struct {
			Type     string `json:"type"`
			Text     string `json:"text"`
			Resource struct {
				URI  string `json:"uri"`
				Text string `json:"text"`
			} `json:"resource"`
			URI  string `json:"uri"`
			Name string `json:"name"`
		} `json:"content"`
		StructuredContent json.RawMessage `json:"structuredContent"`
		IsError           bool            `json:"isError"`
	}
	if err := c.call(ctx, "tools/call", map[string]any{"name": name, "arguments": args}, &res); err != nil {
		return Result{}, err
	}
	var parts []string
	for _, it := range res.Content {
		switch it.Type {
		case "text":
			parts = append(parts, it.Text)
		case "resource":
			if it.Resource.Text != "" {
				parts = append(parts, it.Resource.Text)
			} else {
				parts = append(parts, "[resource "+it.Resource.URI+"]")
			}
		case "resource_link":
			parts = append(parts, "[link "+orStr(it.Name, it.URI)+": "+it.URI+"]")
		default:
			parts = append(parts, "["+it.Type+" omitted]")
		}
	}
	if len(parts) == 0 && len(res.StructuredContent) > 0 {
		parts = append(parts, string(res.StructuredContent))
	}
	return Result{Text: strings.Join(parts, "\n"), IsError: res.IsError}, nil
}

type statusError struct {
	status int
	body   string
}

func (e *statusError) Error() string {
	if e.body != "" {
		return fmt.Sprintf("the server answered %d: %s", e.status, e.body)
	}
	return fmt.Sprintf("the server answered %d", e.status)
}

// post sends one JSON-RPC message. Requests (wantID) wait for their response, which may come as JSON or
// as an event stream; notifications expect 202. It returns the session id the server set, if any.
func (c *Client) post(ctx context.Context, method string, params, out any, wantID bool) (string, error) {
	msg := map[string]any{"jsonrpc": "2.0", "method": method}
	if params != nil {
		msg["params"] = params
	}
	var id int64
	if wantID {
		id = c.ids.Add(1)
		msg["id"] = id
	}
	body, err := json.Marshal(msg)
	if err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.URL, bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	for k, v := range c.Headers {
		req.Header.Set(k, v)
	}
	if c.session != "" {
		req.Header.Set("Mcp-Session-Id", c.session)
	}
	if c.proto != "" {
		req.Header.Set("MCP-Protocol-Version", c.proto)
	}
	if c.Auth != nil {
		if err := c.Auth.Authorize(ctx, req, body); err != nil {
			return "", err
		}
	}
	hc := c.HTTP
	if hc == nil {
		hc = http.DefaultClient
	}
	resp, err := hc.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	sess := resp.Header.Get("Mcp-Session-Id")
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return "", &AuthError{Status: resp.StatusCode, WWWAuthenticate: resp.Header.Get("WWW-Authenticate"), Body: string(b)}
	}
	if resp.StatusCode >= 300 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return "", &statusError{status: resp.StatusCode, body: strings.TrimSpace(string(b))}
	}
	if !wantID {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, maxBody))
		return sess, nil
	}
	ct, _, _ := mime.ParseMediaType(resp.Header.Get("Content-Type"))
	var r rpcResponse
	switch ct {
	case "text/event-stream":
		if r, err = readStream(io.LimitReader(resp.Body, maxBody), id); err != nil {
			return "", err
		}
	default:
		if err := json.NewDecoder(io.LimitReader(resp.Body, maxBody)).Decode(&r); err != nil {
			return "", fmt.Errorf("the server's answer is not JSON-RPC: %w", err)
		}
	}
	if r.Error != nil {
		return "", r.Error
	}
	if out != nil && len(r.Result) > 0 {
		if err := json.Unmarshal(r.Result, out); err != nil {
			return "", fmt.Errorf("read %s result: %w", method, err)
		}
	}
	return sess, nil
}

// readStream reads server-sent events until the response to id arrives; requests and notifications the
// server sends meanwhile are skipped.
func readStream(r io.Reader, id int64) (rpcResponse, error) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64<<10), maxBody)
	var data strings.Builder
	want := fmt.Sprint(id)
	flush := func() (rpcResponse, bool) {
		defer data.Reset()
		if data.Len() == 0 {
			return rpcResponse{}, false
		}
		var m rpcResponse
		if json.Unmarshal([]byte(data.String()), &m) != nil {
			return rpcResponse{}, false
		}
		if strings.Trim(string(m.ID), `"`) == want && (m.Result != nil || m.Error != nil) {
			return m, true
		}
		return rpcResponse{}, false
	}
	for sc.Scan() {
		line := sc.Text()
		switch {
		case line == "":
			if m, ok := flush(); ok {
				return m, nil
			}
		case strings.HasPrefix(line, "data:"):
			if data.Len() > 0 {
				data.WriteByte('\n')
			}
			data.WriteString(strings.TrimPrefix(strings.TrimPrefix(line, "data:"), " "))
		}
	}
	if m, ok := flush(); ok {
		return m, nil
	}
	if err := sc.Err(); err != nil {
		return rpcResponse{}, err
	}
	return rpcResponse{}, errors.New("the server closed the stream without answering")
}

func orStr(s, def string) string {
	if s == "" {
		return def
	}
	return s
}
