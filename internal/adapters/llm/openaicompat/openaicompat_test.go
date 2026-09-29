package openaicompat_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/GokulMV/DocTheRepo/internal/adapters/httpx"
	"github.com/GokulMV/DocTheRepo/internal/adapters/llm/azureopenai"
	"github.com/GokulMV/DocTheRepo/internal/adapters/llm/openai"
	"github.com/GokulMV/DocTheRepo/internal/adapters/llm/openaicompat"
	"github.com/GokulMV/DocTheRepo/internal/ports"
)

type rec struct {
	path, query string
	header      http.Header
	body        map[string]any
}

func mock(t *testing.T, handler func(w http.ResponseWriter, r *http.Request, got *rec)) (*httptest.Server, *rec) {
	t.Helper()
	got := &rec{}
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got.path, got.query, got.header = r.URL.Path, r.URL.RawQuery, r.Header.Clone()
		raw, _ := io.ReadAll(r.Body)
		got.body = map[string]any{}
		_ = json.Unmarshal(raw, &got.body)
		handler(w, r, got)
	}))
	t.Cleanup(s.Close)
	return s, got
}

const chatOK = `{"model":"gpt-x-2026","choices":[{"message":{"content":"{\"a\":1}"},"finish_reason":"stop"}],
  "usage":{"prompt_tokens":100,"completion_tokens":20,"prompt_tokens_details":{"cached_tokens":40}}}`

func TestOpenAI_ChatShape(t *testing.T) {
	s, got := mock(t, func(w http.ResponseWriter, r *http.Request, _ *rec) { _, _ = io.WriteString(w, chatOK) })
	c, err := openai.New(ports.ProviderConfig{APIKey: "sk-o", BaseURL: s.URL + "/v1", Extra: map[string]string{"organization": "org-1", "supports_effort": "true"}})
	require.NoError(t, err)
	temp := 0.2
	resp, err := c.Chat(context.Background(), ports.ChatRequest{Model: "gpt-x", System: "sys", MaxOutputTokens: 500, Effort: "xhigh",
		Temperature: &temp, JSONSchema: map[string]any{"type": "object"}, Messages: []ports.ChatMessage{{Role: "user", Content: "q"}}})
	require.NoError(t, err)
	assert.Equal(t, `{"a":1}`, resp.Text)
	assert.Equal(t, "gpt-x-2026", resp.Model)
	assert.Equal(t, ports.TokenUsage{InputTokens: 60, CacheReadTokens: 40, OutputTokens: 20, Reported: true}, resp.Usage)
	assert.Equal(t, "/v1/chat/completions", got.path)
	assert.Equal(t, "Bearer sk-o", got.header.Get("Authorization"))
	assert.Equal(t, "org-1", got.header.Get("OpenAI-Organization"))
	assert.EqualValues(t, 500, got.body["max_completion_tokens"])
	assert.NotContains(t, got.body, "max_tokens")
	assert.Equal(t, "high", got.body["reasoning_effort"], "xhigh folds onto OpenAI's top level")
	assert.Equal(t, 0.2, got.body["temperature"])
	rf := got.body["response_format"].(map[string]any)
	assert.Equal(t, "json_schema", rf["type"])
	msgs := got.body["messages"].([]any)
	assert.Equal(t, "system", msgs[0].(map[string]any)["role"])
	_, err = openai.New(ports.ProviderConfig{})
	assert.Error(t, err)
}

func TestAzure_PathsQueryAndAuth(t *testing.T) {
	s, got := mock(t, func(w http.ResponseWriter, r *http.Request, _ *rec) {
		if strings.HasSuffix(r.URL.Path, "/embeddings") {
			_, _ = io.WriteString(w, `{"data":[{"index":0,"embedding":[0.1,0.2]}],"usage":{"prompt_tokens":3}}`)
			return
		}
		_, _ = io.WriteString(w, chatOK)
	})
	c, err := azureopenai.New(ports.ProviderConfig{APIKey: "az", BaseURL: s.URL + "/", Extra: map[string]string{"deployment": "gpt prod", "embedding_deployment": "emb", "no_temperature": "true"}})
	require.NoError(t, err)
	temp := 1.0
	_, err = c.Chat(context.Background(), ports.ChatRequest{Model: "gpt", Temperature: &temp, Messages: []ports.ChatMessage{{Role: "user", Content: "q"}}})
	require.NoError(t, err)
	assert.Equal(t, "/openai/deployments/gpt prod/chat/completions", got.path)
	assert.Equal(t, "api-version="+azureopenai.DefaultAPIVersion, got.query)
	assert.Equal(t, "az", got.header.Get("api-key"))
	assert.NotContains(t, got.body, "temperature")
	e, err := c.Embed(context.Background(), "emb", []string{"x"})
	require.NoError(t, err)
	assert.Equal(t, "/openai/deployments/emb/embeddings", got.path)
	assert.Equal(t, 2, e.Dimensions)
	_, err = azureopenai.New(ports.ProviderConfig{APIKey: "a", BaseURL: "x"})
	assert.ErrorContains(t, err, "deployment")
	_, err = azureopenai.New(ports.ProviderConfig{})
	assert.Error(t, err)
}

func TestCompat_OllamaDefaultsJSONObjectAndStreaming(t *testing.T) {
	s, got := mock(t, func(w http.ResponseWriter, r *http.Request, _ *rec) {
		w.Header().Set("Content-Type", "text/event-stream")
		for _, e := range []string{
			`{"model":"llama3","choices":[{"delta":{"content":"Hel"}}]}`,
			`{"choices":[{"delta":{"content":"lo"},"finish_reason":"stop"}]}`,
			`{"choices":[],"usage":{"prompt_tokens":9,"completion_tokens":2}}`,
		} {
			_, _ = io.WriteString(w, "data: "+e+"\n\n")
		}
		_, _ = io.WriteString(w, "data: [DONE]\n\n")
	})
	c, err := openaicompat.FromConfig(ports.ProviderConfig{Kind: "ollama", BaseURL: s.URL + "/v1"})
	require.NoError(t, err)
	assert.Equal(t, "ollama", c.Kind())
	var sb strings.Builder
	resp, err := c.Chat(context.Background(), ports.ChatRequest{Model: "llama3", MaxOutputTokens: 100, Effort: "high",
		JSONSchema: map[string]any{"type": "object"}, Messages: []ports.ChatMessage{{Role: "user", Content: "q"}},
		OnDelta: func(s string) { sb.WriteString(s) }})
	require.NoError(t, err)
	assert.Equal(t, "Hello", sb.String())
	assert.Equal(t, "Hello", resp.Text)
	assert.Equal(t, "llama3", resp.Model)
	assert.Equal(t, int64(9), resp.Usage.InputTokens)
	assert.Equal(t, "stop", resp.StopReason)
	assert.EqualValues(t, 100, got.body["max_tokens"], "compatible servers get max_tokens")
	assert.NotContains(t, got.body, "reasoning_effort", "effort is only sent when supported")
	assert.Equal(t, map[string]any{"type": "json_object"}, got.body["response_format"])
	assert.Equal(t, true, got.body["stream"])
	assert.Empty(t, got.header.Get("Authorization"), "local Ollama needs no key")

	d, err := openaicompat.FromConfig(ports.ProviderConfig{Kind: "ollama"})
	require.NoError(t, err)
	assert.Equal(t, 256, d.MaxBatch())
	_, err = openaicompat.FromConfig(ports.ProviderConfig{Kind: "openai_compat"})
	assert.Error(t, err, "a generic server needs a base URL")
}

func TestCompat_RefusalTruncationAndEmptyChoices(t *testing.T) {
	bodies := map[string]string{
		"refusal": `{"choices":[{"message":{"content":"","refusal":"I can't help"},"finish_reason":"stop"}]}`,
		"filter":  `{"choices":[{"message":{"content":""},"finish_reason":"content_filter"}]}`,
		"length":  `{"choices":[{"message":{"content":"part"},"finish_reason":"length"}]}`,
		"empty":   `{"choices":[]}`,
	}
	for name, body := range bodies {
		s, _ := mock(t, func(w http.ResponseWriter, r *http.Request, _ *rec) { _, _ = io.WriteString(w, body) })
		c, _ := openaicompat.FromConfig(ports.ProviderConfig{Kind: "openai_compat", BaseURL: s.URL, Extra: map[string]string{"json_mode": "none"}})
		resp, err := c.Chat(context.Background(), ports.ChatRequest{Model: "m", MaxOutputTokens: 7, JSONSchema: map[string]any{}, Messages: []ports.ChatMessage{{Role: "user", Content: "q"}}})
		switch name {
		case "refusal", "filter":
			var re *ports.RefusalError
			assert.ErrorAs(t, err, &re, name)
		case "length":
			var te *ports.TruncatedError
			require.ErrorAs(t, err, &te)
			assert.Equal(t, 7, te.MaxOutputTokens)
			assert.Equal(t, "part", resp.Text)
		case "empty":
			_, ok := ports.AsTransient(err)
			assert.True(t, ok)
		}
	}
}

func TestEmbed_OrderingValidationAndBatch(t *testing.T) {
	s, got := mock(t, func(w http.ResponseWriter, r *http.Request, _ *rec) {
		_, _ = io.WriteString(w, `{"data":[{"index":1,"embedding":[3,4]},{"index":0,"embedding":[1,2]}],"usage":{"total_tokens":5}}`)
	})
	c, _ := openaicompat.New(openaicompat.Options{Kind: "openai_compat", BaseURL: s.URL, EmbedBatch: 2})
	r, err := c.Embed(context.Background(), "e", []string{"a", "b"})
	require.NoError(t, err)
	assert.Equal(t, [][]float32{{1, 2}, {3, 4}}, r.Vectors, "results are placed by index, not arrival order")
	assert.Equal(t, int64(5), r.Usage.InputTokens)
	assert.Equal(t, []any{"a", "b"}, got.body["input"])
	_, err = c.Embed(context.Background(), "e", []string{"a", "b", "c"})
	assert.Error(t, err)
	_, err = c.Embed(context.Background(), "e", []string{"only-one"})
	assert.ErrorContains(t, err, "2 embeddings for 1")

	bad, _ := mock(t, func(w http.ResponseWriter, r *http.Request, _ *rec) {
		_, _ = io.WriteString(w, `{"data":[{"index":0,"embedding":[1]},{"index":1,"embedding":[1,2]}]}`)
	})
	c2, _ := openaicompat.New(openaicompat.Options{Kind: "x", BaseURL: bad.URL})
	_, err = c2.Embed(context.Background(), "e", []string{"a", "b"})
	assert.ErrorContains(t, err, "mixed")
}

func TestHTTPX_RetriesThenClassifies(t *testing.T) {
	var n atomic.Int32
	s, _ := mock(t, func(w http.ResponseWriter, r *http.Request, _ *rec) {
		if n.Add(1) < 3 {
			w.Header().Set("Retry-After", "0.01")
			w.WriteHeader(429)
			return
		}
		_, _ = io.WriteString(w, `{"ok":true}`)
	})
	cl := httpx.New("svc")
	cl.BaseDelay = time.Millisecond
	var out map[string]bool
	require.NoError(t, cl.JSON(context.Background(), http.MethodGet, s.URL, nil, nil, &out))
	assert.True(t, out["ok"])
	assert.Equal(t, int32(3), n.Load())

	perm, _ := mock(t, func(w http.ResponseWriter, r *http.Request, _ *rec) {
		w.WriteHeader(401)
		_, _ = io.WriteString(w, `{"error":"bad key"}`)
	})
	err := cl.JSON(context.Background(), http.MethodGet, perm.URL, nil, nil, nil)
	var pe *ports.PermanentError
	require.ErrorAs(t, err, &pe)
	assert.Equal(t, 401, httpx.StatusOf(err))
	assert.Contains(t, err.Error(), "bad key")

	always, _ := mock(t, func(w http.ResponseWriter, r *http.Request, _ *rec) { w.WriteHeader(503) })
	err = cl.JSON(context.Background(), http.MethodGet, always.URL, nil, nil, nil)
	_, isT := ports.AsTransient(err)
	assert.True(t, isT)

	long, _ := mock(t, func(w http.ResponseWriter, r *http.Request, _ *rec) {
		w.Header().Set("Retry-After", "3600")
		w.WriteHeader(429)
	})
	start := time.Now()
	err = cl.JSON(context.Background(), http.MethodGet, long.URL, nil, nil, nil)
	te, _ := ports.AsTransient(err)
	require.NotNil(t, te)
	assert.Equal(t, time.Hour, te.RetryAfter, "long waits are handed to the queue, not slept on a worker")
	assert.Less(t, time.Since(start), 2*time.Second)

	garbage, _ := mock(t, func(w http.ResponseWriter, r *http.Request, _ *rec) { _, _ = io.WriteString(w, "not json") })
	assert.Error(t, cl.JSON(context.Background(), http.MethodGet, garbage.URL, nil, nil, &out))
}

func TestRetryAfterParsing(t *testing.T) {
	h := http.Header{}
	h.Set("retry-after-ms", "1500")
	assert.Equal(t, 1500*time.Millisecond, httpx.RetryAfter(h))
	h = http.Header{}
	h.Set("Retry-After", time.Now().Add(10*time.Second).UTC().Format(http.TimeFormat))
	assert.InDelta(t, float64(10*time.Second), float64(httpx.RetryAfter(h)), float64(2*time.Second))
	assert.Zero(t, httpx.RetryAfter(http.Header{}))
	assert.True(t, httpx.Retryable(502))
	assert.False(t, httpx.Retryable(404))
}

func TestPing(t *testing.T) {
	s, got := mock(t, func(w http.ResponseWriter, r *http.Request, _ *rec) { _, _ = io.WriteString(w, `{"data":[]}`) })
	c, _ := openai.New(ports.ProviderConfig{APIKey: "k", BaseURL: s.URL})
	require.NoError(t, c.Ping(context.Background(), "gpt"))
	assert.Equal(t, "/models", got.path)
}

func TestCompat_StreamWithoutEventsIsAnError(t *testing.T) {
	s, _ := mock(t, func(w http.ResponseWriter, r *http.Request, _ *rec) { _, _ = io.WriteString(w, chatOK) })
	c, _ := openaicompat.FromConfig(ports.ProviderConfig{Kind: "openai_compat", BaseURL: s.URL})
	_, err := c.Chat(context.Background(), ports.ChatRequest{Model: "m", Messages: []ports.ChatMessage{{Role: "user", Content: "q"}}, OnDelta: func(string) {}})
	_, transient := ports.AsTransient(err)
	assert.True(t, transient, "%v", err)
}
