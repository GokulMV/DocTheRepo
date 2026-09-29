package bedrock_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	embedbedrock "github.com/GokulMV/DocTheRepo/internal/adapters/embed/bedrock"
	"github.com/GokulMV/DocTheRepo/internal/adapters/llm/bedrock"
	"github.com/GokulMV/DocTheRepo/internal/ports"
)

func cfg(url string, extra map[string]string) ports.ProviderConfig {
	e := map[string]string{"region": "us-east-1", "access_key_id": "AKIDEXAMPLE", "secret_access_key": "secret"}
	for k, v := range extra {
		e[k] = v
	}
	return ports.ProviderConfig{Kind: "bedrock", APIKey: "bedrock-api-key", BaseURL: url, Extra: e}
}

func TestBedrock_ConverseForNonClaude_MantleForClaude(t *testing.T) {
	var paths []string
	var converseBody map[string]any
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		raw, _ := io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasSuffix(r.URL.Path, "/converse"):
			assert.Contains(t, r.Header.Get("Authorization"), "AWS4-HMAC-SHA256", "Converse is SigV4-signed")
			_ = json.Unmarshal(raw, &converseBody)
			_, _ = io.WriteString(w, `{"output":{"message":{"role":"assistant","content":[{"text":"{\"x\":1}"}]}},"stopReason":"end_turn",
			  "usage":{"inputTokens":40,"outputTokens":8,"totalTokens":48,"cacheReadInputTokens":5}}`)
		case r.URL.Path == "/v1/messages":
			_, _ = io.WriteString(w, `{"id":"m","type":"message","role":"assistant","model":"anthropic.claude-opus-5-5","content":[{"type":"text","text":"claude"}],"stop_reason":"end_turn","usage":{"input_tokens":1,"output_tokens":1}}`)
		default:
			w.WriteHeader(404)
		}
	}))
	defer s.Close()
	a, err := bedrock.New(context.Background(), cfg(s.URL, nil))
	require.NoError(t, err)
	assert.Equal(t, "bedrock", a.Kind())

	var delta string
	resp, err := a.Chat(context.Background(), ports.ChatRequest{Model: "meta.llama4-maverick-v1:0", System: "sys", MaxOutputTokens: 300,
		JSONSchema: map[string]any{"type": "object"}, Messages: []ports.ChatMessage{{Role: "user", Content: "q"}},
		OnDelta: func(s string) { delta = s }})
	require.NoError(t, err)
	assert.Equal(t, `{"x":1}`, resp.Text)
	assert.Equal(t, `{"x":1}`, delta)
	assert.Equal(t, ports.TokenUsage{InputTokens: 40, OutputTokens: 8, CacheReadTokens: 5, Reported: true}, resp.Usage)
	assert.Contains(t, paths[0], "/model/meta.llama4-maverick-v1:0/converse")
	sys := converseBody["system"].([]any)[0].(map[string]any)["text"].(string)
	assert.Contains(t, sys, "single JSON object", "Converse gets a JSON instruction instead of native structured output")
	assert.EqualValues(t, 300, converseBody["inferenceConfig"].(map[string]any)["maxTokens"])

	resp, err = a.Chat(context.Background(), ports.ChatRequest{Model: "us.anthropic.claude-opus-5-5", Messages: []ports.ChatMessage{{Role: "user", Content: "q"}}})
	require.NoError(t, err)
	assert.Equal(t, "claude", resp.Text)
	assert.Equal(t, "/v1/messages", paths[len(paths)-1], "Claude on Bedrock uses the Messages API (Mantle)")
}

func TestBedrock_StopReasonsAndErrors(t *testing.T) {
	stop := "content_filtered"
	status := 200
	code := ""
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if code != "" {
			w.Header().Set("X-Amzn-ErrorType", code)
		}
		w.WriteHeader(status)
		if status != 200 {
			_, _ = io.WriteString(w, `{"message":"nope"}`)
			return
		}
		_, _ = io.WriteString(w, `{"output":{"message":{"role":"assistant","content":[{"text":"t"}]}},"stopReason":"`+stop+`","usage":{"inputTokens":1,"outputTokens":1}}`)
	}))
	defer s.Close()
	a, err := bedrock.New(context.Background(), cfg(s.URL, nil))
	require.NoError(t, err)
	req := ports.ChatRequest{Model: "amazon.nova-pro-v1:0", MaxOutputTokens: 5, Messages: []ports.ChatMessage{{Role: "user", Content: "q"}}}
	_, err = a.Chat(context.Background(), req)
	var re *ports.RefusalError
	assert.ErrorAs(t, err, &re)
	stop = "max_tokens"
	_, err = a.Chat(context.Background(), req)
	var te *ports.TruncatedError
	assert.ErrorAs(t, err, &te)
	require.NoError(t, a.Ping(context.Background(), "amazon.nova-pro-v1:0"), "truncation proves access")

	status, code = 400, "ValidationException"
	_, err = a.Chat(context.Background(), req)
	var pe *ports.PermanentError
	assert.ErrorAs(t, err, &pe)
	assert.Contains(t, err.Error(), "ValidationException")
}

func TestIsClaudeAndConfig(t *testing.T) {
	assert.True(t, bedrock.IsClaude("anthropic.claude-opus-5-5"))
	assert.True(t, bedrock.IsClaude("eu.anthropic.claude-sonnet-5-5"))
	assert.True(t, bedrock.IsClaude("global.anthropic.claude-opus-5-5"))
	assert.False(t, bedrock.IsClaude("meta.llama3-70b-instruct-v1:0"))
	_, err := bedrock.New(context.Background(), ports.ProviderConfig{})
	assert.ErrorContains(t, err, "region")
	_, err = bedrock.Runtime(context.Background(), ports.ProviderConfig{})
	assert.Error(t, err)
}

func TestBedrockEmbeddings_TitanAndCohere(t *testing.T) {
	var bodies []map[string]any
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		m := map[string]any{}
		_ = json.Unmarshal(raw, &m)
		bodies = append(bodies, m)
		w.Header().Set("Content-Type", "application/json")
		if strings.Contains(r.URL.Path, "cohere") {
			_, _ = io.WriteString(w, `{"embeddings":[[1,0],[0,1]]}`)
			return
		}
		_, _ = io.WriteString(w, `{"embedding":[0.5,0.5,0.5],"inputTextTokenCount":4}`)
	}))
	defer s.Close()
	e, err := embedbedrock.New(context.Background(), cfg(s.URL, map[string]string{"dimensions": "256"}))
	require.NoError(t, err)
	assert.Equal(t, "bedrock", e.Kind())
	assert.Equal(t, 96, e.MaxBatch())
	r, err := e.Embed(context.Background(), "amazon.titan-embed-text-v2:0", []string{"a", "b"})
	require.NoError(t, err)
	assert.Len(t, r.Vectors, 2)
	assert.Equal(t, 3, r.Dimensions)
	assert.Equal(t, int64(8), r.Usage.InputTokens)
	assert.EqualValues(t, 256, bodies[0]["dimensions"])
	assert.Equal(t, true, bodies[0]["normalize"])

	r, err = e.Embed(context.Background(), "cohere.embed-english-v3", []string{"a", "b"})
	require.NoError(t, err)
	assert.Equal(t, [][]float32{{1, 0}, {0, 1}}, r.Vectors)
	assert.False(t, r.Usage.Reported, "Cohere reports no usage; the gateway records an estimate")
	assert.Equal(t, "search_document", bodies[len(bodies)-1]["input_type"])
	_, err = e.Embed(context.Background(), "cohere.embed-english-v3", []string{"only one"})
	assert.Error(t, err)
}
