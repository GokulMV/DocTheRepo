package anthropic

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/anthropics/anthropic-sdk-go/option"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/GokulMV/DocTheRepo/internal/ports"
)

type captured struct {
	mu     sync.Mutex
	body   map[string]any
	header http.Header
	path   string
}

func (c *captured) set(r *http.Request) {
	c.mu.Lock()
	defer c.mu.Unlock()
	raw, _ := io.ReadAll(r.Body)
	c.body = map[string]any{}
	_ = json.Unmarshal(raw, &c.body)
	c.header = r.Header.Clone()
	c.path = r.URL.Path
}

func message(stop, text string) string {
	return fmt.Sprintf(`{"id":"msg_1","type":"message","role":"assistant","model":"claude-opus-5-5",
	  "content":[{"type":"thinking","thinking":"","signature":"s"},{"type":"text","text":%q}],
	  "stop_reason":%q,"stop_details":{"type":"refusal","category":"cyber","explanation":"declined"},
	  "usage":{"input_tokens":120,"output_tokens":45,"cache_read_input_tokens":30,"cache_creation_input_tokens":10}}`, text, stop)
}

func server(t *testing.T, c *captured, status int, body string, hdr map[string]string) *httptest.Server {
	t.Helper()
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c.set(r)
		for k, v := range hdr {
			w.Header().Set(k, v)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(s.Close)
	return s
}

func adapter(t *testing.T, url string, extra map[string]string) *Adapter {
	t.Helper()
	a, err := New(ports.ProviderConfig{Kind: "anthropic", APIKey: "sk-test", BaseURL: url, Extra: extra}, option.WithMaxRetries(0))
	require.NoError(t, err)
	return a
}

func TestChat_RequestShapeAndResponse(t *testing.T) {
	c := &captured{}
	s := server(t, c, 200, message("end_turn", `{"ok":true}`), nil)
	a := adapter(t, s.URL, nil)
	temp := 0.7
	resp, err := a.Chat(context.Background(), ports.ChatRequest{
		Model: "claude-opus-5-5", System: "You write docs.", MaxOutputTokens: 8000, Effort: "medium",
		Temperature: &temp, JSONSchema: map[string]any{"type": "object"},
		Messages: []ports.ChatMessage{{Role: "user", Content: "hi"}, {Role: "assistant", Content: "draft"}, {Role: "user", Content: "fix"}},
	})
	require.NoError(t, err)
	assert.Equal(t, `{"ok":true}`, resp.Text, "thinking blocks are not part of the text")
	assert.Equal(t, "claude-opus-5-5", resp.Model)
	assert.Equal(t, ports.TokenUsage{InputTokens: 120, OutputTokens: 45, CacheReadTokens: 30, CacheWriteTokens: 10, Reported: true}, resp.Usage)
	assert.Equal(t, int64(205), resp.Usage.Total())

	assert.Equal(t, "/v1/messages", c.path)
	assert.Equal(t, "sk-test", c.header.Get("X-Api-Key"))
	b := c.body
	assert.Equal(t, "claude-opus-5-5", b["model"])
	assert.EqualValues(t, 8000, b["max_tokens"])
	assert.NotContains(t, b, "temperature", "current models reject sampling parameters")
	assert.NotContains(t, b, "thinking", "thinking is left at the model default")
	assert.Equal(t, map[string]any{"effort": "medium", "format": map[string]any{"type": "json_schema", "schema": map[string]any{"type": "object"}}}, b["output_config"])
	assert.Equal(t, "default", b["fallbacks"], "refusal fallbacks are on by default for supporting models")
	assert.Contains(t, c.header.Get("Anthropic-Beta"), "server-side-fallback-2026-07-01")
	msgs := b["messages"].([]any)
	require.Len(t, msgs, 3)
	assert.Equal(t, "assistant", msgs[1].(map[string]any)["role"])
	eph := map[string]any{"type": "ephemeral"}
	assert.Equal(t, []any{map[string]any{"type": "text", "text": "You write docs.", "cache_control": eph}}, b["system"],
		"the system prompt is cached")
	block := func(i int) map[string]any { return msgs[i].(map[string]any)["content"].([]any)[0].(map[string]any) }
	assert.Equal(t, eph, block(2)["cache_control"], "the conversation so far is cached for the next turn")
	assert.NotContains(t, block(0), "cache_control", "only the last message carries a breakpoint")
}

func TestChat_FallbacksOnlyWhereSupported(t *testing.T) {
	c := &captured{}
	s := server(t, c, 200, message("end_turn", "x"), nil)
	_, err := adapter(t, s.URL, nil).Chat(context.Background(), ports.ChatRequest{Model: "claude-haiku-4-5", Messages: []ports.ChatMessage{{Role: "user", Content: "x"}}})
	require.NoError(t, err)
	assert.NotContains(t, c.body, "fallbacks")
	assert.EqualValues(t, 16000, c.body["max_tokens"], "a missing max defaults to 16000, not a lowball")
	_, err = adapter(t, s.URL, map[string]string{"fallbacks": "off"}).Chat(context.Background(), ports.ChatRequest{Model: "claude-opus-5-5", Messages: []ports.ChatMessage{{Role: "user", Content: "x"}}})
	require.NoError(t, err)
	assert.NotContains(t, c.body, "fallbacks", "operators can turn fallbacks off")
	assert.NotContains(t, c.body, "output_config")
}

func TestChat_RefusalAndTruncationAreTyped(t *testing.T) {
	c := &captured{}
	s := server(t, c, 200, message("refusal", ""), nil)
	resp, err := adapter(t, s.URL, nil).Chat(context.Background(), ports.ChatRequest{Model: "claude-opus-5-5", Messages: []ports.ChatMessage{{Role: "user", Content: "x"}}})
	var ref *ports.RefusalError
	require.ErrorAs(t, err, &ref)
	assert.Equal(t, "cyber", ref.Category)
	assert.Equal(t, "declined", ref.Explanation)
	assert.True(t, resp.Usage.Reported, "usage of a refused call is still reported for the ledger")

	s2 := server(t, c, 200, message("max_tokens", "partial"), nil)
	resp, err = adapter(t, s2.URL, nil).Chat(context.Background(), ports.ChatRequest{Model: "claude-opus-5-5", MaxOutputTokens: 100, Messages: []ports.ChatMessage{{Role: "user", Content: "x"}}})
	var tr *ports.TruncatedError
	require.ErrorAs(t, err, &tr)
	assert.Equal(t, 100, tr.MaxOutputTokens)
	assert.Equal(t, "partial", resp.Text)
}

func TestChat_ErrorClassification(t *testing.T) {
	cases := []struct {
		status    int
		hdr       map[string]string
		transient bool
		after     time.Duration
	}{
		{429, map[string]string{"retry-after": "7"}, true, 7 * time.Second},
		{529, nil, true, 0},
		{500, nil, true, 0},
		{400, nil, false, 0},
		{401, nil, false, 0},
		{404, nil, false, 0},
	}
	for _, tc := range cases {
		c := &captured{}
		s := server(t, c, tc.status, `{"type":"error","error":{"type":"x_error","message":"nope"}}`, tc.hdr)
		_, err := adapter(t, s.URL, nil).Chat(context.Background(), ports.ChatRequest{Model: "claude-opus-5-5", Messages: []ports.ChatMessage{{Role: "user", Content: "x"}}})
		require.Error(t, err, tc.status)
		te, isT := ports.AsTransient(err)
		assert.Equal(t, tc.transient, isT, "status %d: %v", tc.status, err)
		if isT {
			assert.Equal(t, tc.after, te.RetryAfter)
		} else {
			var pe *ports.PermanentError
			assert.True(t, errors.As(err, &pe))
		}
		assert.Contains(t, err.Error(), fmt.Sprint(tc.status))
	}
	_, err := adapter(t, "http://127.0.0.1:1", nil).Chat(context.Background(), ports.ChatRequest{Model: "m", Messages: []ports.ChatMessage{{Role: "user", Content: "x"}}})
	_, isT := ports.AsTransient(err)
	assert.True(t, isT, "connection failures are transient")
	_, err = adapter(t, "http://x", nil).Chat(context.Background(), ports.ChatRequest{})
	assert.Error(t, err)
}

func TestChat_StreamingDeliversDeltasAndUsage(t *testing.T) {
	events := []string{
		`event: message_start` + "\n" + `data: {"type":"message_start","message":{"id":"m","type":"message","role":"assistant","model":"claude-opus-5-5","content":[],"stop_reason":null,"usage":{"input_tokens":50,"output_tokens":1}}}`,
		`event: content_block_start` + "\n" + `data: {"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`,
		`event: content_block_delta` + "\n" + `data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Hello "}}`,
		`event: content_block_delta` + "\n" + `data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"world"}}`,
		`event: content_block_stop` + "\n" + `data: {"type":"content_block_stop","index":0}`,
		`event: message_delta` + "\n" + `data: {"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":12}}`,
		`event: message_stop` + "\n" + `data: {"type":"message_stop"}`,
	}
	c := &captured{}
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c.set(r)
		w.Header().Set("Content-Type", "text/event-stream")
		for _, e := range events {
			_, _ = io.WriteString(w, e+"\n\n")
		}
	}))
	defer s.Close()
	var got strings.Builder
	resp, err := adapter(t, s.URL, nil).Chat(context.Background(), ports.ChatRequest{
		Model: "claude-opus-5-5", Messages: []ports.ChatMessage{{Role: "user", Content: "x"}},
		OnDelta: func(d string) { got.WriteString(d) },
	})
	require.NoError(t, err)
	assert.Equal(t, true, c.body["stream"])
	assert.Equal(t, "Hello world", got.String())
	assert.Equal(t, "Hello world", resp.Text)
	assert.Equal(t, int64(50), resp.Usage.InputTokens)
	assert.Equal(t, int64(12), resp.Usage.OutputTokens)
	assert.Equal(t, "end_turn", resp.StopReason)
}

func TestPing_UsesModelsEndpoint(t *testing.T) {
	c := &captured{}
	s := server(t, c, 200, `{"id":"claude-opus-5-5","type":"model","display_name":"Claude Opus 5.5","created_at":"2026-01-01T00:00:00Z"}`, nil)
	require.NoError(t, adapter(t, s.URL, nil).Ping(context.Background(), "claude-opus-5-5"))
	assert.Equal(t, "/v1/models/claude-opus-5-5", c.path)
	bad := server(t, c, 401, `{"type":"error","error":{"type":"authentication_error","message":"bad key"}}`, nil)
	assert.Error(t, adapter(t, bad.URL, nil).Ping(context.Background(), "claude-opus-5-5"))
}

func TestBedrockAndVertexConstruction(t *testing.T) {
	_, err := New(ports.ProviderConfig{})
	assert.Error(t, err)
	_, err = NewBedrock(context.Background(), ports.ProviderConfig{})
	assert.ErrorContains(t, err, "region")
	_, err = NewVertex(context.Background(), ports.ProviderConfig{Extra: map[string]string{"region": "us-east5"}})
	assert.ErrorContains(t, err, "project")

	// Bedrock (Mantle) speaks the same Messages API; with an API key and a base URL it can be exercised
	// against the mock. Refusal fallbacks are never sent there (Claude API only).
	c := &captured{}
	s := server(t, c, 200, message("end_turn", "ok"), nil)
	b, err := NewBedrock(context.Background(), ports.ProviderConfig{APIKey: "bedrock-key", BaseURL: s.URL, Extra: map[string]string{"region": "us-east-1"}}, option.WithMaxRetries(0))
	require.NoError(t, err)
	assert.Equal(t, "bedrock", b.Kind())
	resp, err := b.Chat(context.Background(), ports.ChatRequest{Model: "anthropic.claude-opus-5-5", Messages: []ports.ChatMessage{{Role: "user", Content: "x"}}})
	require.NoError(t, err)
	assert.Equal(t, "ok", resp.Text)
	assert.NotContains(t, c.body, "fallbacks")
	assert.Equal(t, "anthropic", adapter(t, s.URL, nil).Kind())

	// Ping without a Models endpoint falls back to a tiny message; truncation still proves access.
	s2 := server(t, c, 200, message("max_tokens", ""), nil)
	b2, _ := NewBedrock(context.Background(), ports.ProviderConfig{APIKey: "k", BaseURL: s2.URL, Extra: map[string]string{"region": "us-east-1"}}, option.WithMaxRetries(0))
	require.NoError(t, b2.Ping(context.Background(), "anthropic.claude-opus-5-5"))
}

func TestChat_StreamWithoutEventsIsAnError(t *testing.T) {
	c := &captured{}
	s := server(t, c, 200, message("end_turn", "not a stream"), nil) // JSON where SSE was requested
	_, err := adapter(t, s.URL, nil).Chat(context.Background(), ports.ChatRequest{Model: "claude-opus-5-5",
		Messages: []ports.ChatMessage{{Role: "user", Content: "x"}}, OnDelta: func(string) {}})
	_, transient := ports.AsTransient(err)
	assert.True(t, transient, "an empty stream must never look like an empty successful reply: %v", err)
}

// A route can set an effort the model does not accept (Haiku): the request is retried without it, the
// model is remembered, and Haiku never gets it in the first place.
func TestChat_EffortDroppedForModelsThatRejectIt(t *testing.T) {
	var mu sync.Mutex
	var calls, withEffort int
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var body map[string]any
		_ = json.Unmarshal(raw, &body)
		oc, _ := body["output_config"].(map[string]any)
		mu.Lock()
		calls++
		if oc["effort"] != nil {
			withEffort++
		}
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		if oc["effort"] != nil {
			w.WriteHeader(400)
			_, _ = io.WriteString(w, `{"type":"error","error":{"type":"invalid_request_error","message":"This model does not support the effort parameter."}}`)
			return
		}
		_, _ = io.WriteString(w, message("end_turn", "ok"))
	}))
	defer s.Close()
	a := adapter(t, s.URL, nil)
	ask := func(model string) {
		t.Helper()
		resp, err := a.Chat(context.Background(), ports.ChatRequest{Model: model, Effort: "medium", Messages: []ports.ChatMessage{{Role: "user", Content: "hi"}}})
		require.NoError(t, err)
		assert.Equal(t, "ok", resp.Text)
	}
	ask("claude-future-model")
	assert.Equal(t, 2, calls, "rejected once, then retried without effort")
	ask("claude-future-model")
	assert.Equal(t, 3, calls, "remembered: no wasted request")
	ask("claude-haiku-4-5-20251001")
	assert.Equal(t, 4, calls)
	assert.Equal(t, 1, withEffort, "Haiku 4.5 never gets the effort parameter")
	ask("claude-haiku-5-5")
	assert.Equal(t, 2, withEffort, "Claude Haiku 5.5 takes it")
}
