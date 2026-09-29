package vertex_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/oauth2"

	embedvertex "github.com/GokulMV/DocTheRepo/internal/adapters/embed/vertex"
	"github.com/GokulMV/DocTheRepo/internal/adapters/llm/vertex"
	"github.com/GokulMV/DocTheRepo/internal/ports"
)

var tokens = oauth2.StaticTokenSource(&oauth2.Token{AccessToken: "ya29.test"})

func cfg(url string) ports.ProviderConfig {
	return ports.ProviderConfig{Kind: "vertex", BaseURL: url, Extra: map[string]string{"project": "acme-prod", "region": "us-central1"}}
}

func TestGemini_GenerateContent(t *testing.T) {
	var body map[string]any
	var path, auth string
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path, auth = r.URL.Path, r.Header.Get("Authorization")
		raw, _ := io.ReadAll(r.Body)
		body = map[string]any{}
		_ = json.Unmarshal(raw, &body)
		_, _ = io.WriteString(w, `{"candidates":[{"content":{"role":"model","parts":[{"text":"{\"a\":"},{"text":"1}"}]},"finishReason":"STOP"}],
		  "usageMetadata":{"promptTokenCount":100,"candidatesTokenCount":10,"thoughtsTokenCount":5,"cachedContentTokenCount":20},"modelVersion":"gemini-3-pro-001"}`)
	}))
	defer s.Close()
	g, err := vertex.NewGemini(context.Background(), cfg(s.URL), tokens)
	require.NoError(t, err)
	temp := 0.1
	resp, err := g.Chat(context.Background(), ports.ChatRequest{Model: "gemini-3-pro", System: "sys", MaxOutputTokens: 900, Temperature: &temp,
		JSONSchema: map[string]any{"type": "object"}, Messages: []ports.ChatMessage{{Role: "user", Content: "q"}, {Role: "assistant", Content: "a"}}})
	require.NoError(t, err)
	assert.Equal(t, `{"a":1}`, resp.Text)
	assert.Equal(t, "gemini-3-pro-001", resp.Model)
	assert.Equal(t, ports.TokenUsage{InputTokens: 80, CacheReadTokens: 20, OutputTokens: 15, Reported: true}, resp.Usage,
		"thinking tokens are billed as output")
	assert.Equal(t, "/v1/projects/acme-prod/locations/us-central1/publishers/google/models/gemini-3-pro:generateContent", path)
	assert.Equal(t, "Bearer ya29.test", auth)
	gc := body["generationConfig"].(map[string]any)
	assert.Equal(t, "application/json", gc["responseMimeType"])
	assert.EqualValues(t, 900, gc["maxOutputTokens"])
	contents := body["contents"].([]any)
	assert.Equal(t, "model", contents[1].(map[string]any)["role"])
}

func TestGemini_SafetyTruncationAndBlocked(t *testing.T) {
	bodies := map[string]string{
		"safety":  `{"candidates":[{"content":{"parts":[]},"finishReason":"SAFETY"}]}`,
		"max":     `{"candidates":[{"content":{"parts":[{"text":"cut"}]},"finishReason":"MAX_TOKENS"}]}`,
		"blocked": `{"promptFeedback":{"blockReason":"PROHIBITED_CONTENT"}}`,
		"empty":   `{"candidates":[]}`,
	}
	for name, b := range bodies {
		s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = io.WriteString(w, b) }))
		g, _ := vertex.NewGemini(context.Background(), cfg(s.URL), tokens)
		_, err := g.Chat(context.Background(), ports.ChatRequest{Model: "gemini-3-flash", MaxOutputTokens: 3, Messages: []ports.ChatMessage{{Role: "user", Content: "q"}}})
		switch name {
		case "safety", "blocked":
			var re *ports.RefusalError
			assert.ErrorAs(t, err, &re, name)
		case "max":
			var te *ports.TruncatedError
			assert.ErrorAs(t, err, &te)
		case "empty":
			_, ok := ports.AsTransient(err)
			assert.True(t, ok)
		}
		s.Close()
	}
}

func TestGemini_GlobalHostPingAndEmbeddings(t *testing.T) {
	_, err := vertex.NewGemini(context.Background(), ports.ProviderConfig{}, tokens)
	assert.Error(t, err)
	var path string
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		if r.URL.Path[len(r.URL.Path)-8:] == ":predict" {
			_, _ = io.WriteString(w, `{"predictions":[{"embeddings":{"values":[0.1,0.2],"statistics":{"token_count":3}}},{"embeddings":{"values":[0.3,0.4],"statistics":{"token_count":2}}}]}`)
			return
		}
		_, _ = io.WriteString(w, `{"totalTokens":1}`)
	}))
	defer s.Close()
	g, _ := vertex.NewGemini(context.Background(), cfg(s.URL), tokens)
	require.NoError(t, g.Ping(context.Background(), "gemini-3-flash"))
	assert.Contains(t, path, ":countTokens")

	e, err := embedvertex.New(context.Background(), cfg(s.URL), tokens)
	require.NoError(t, err)
	assert.Equal(t, "vertex", e.Kind())
	assert.Equal(t, 100, e.MaxBatch())
	r, err := e.Embed(context.Background(), "text-embedding-005", []string{"a", "b"})
	require.NoError(t, err)
	assert.Equal(t, 2, r.Dimensions)
	assert.Equal(t, int64(5), r.Usage.InputTokens)
	_, err = e.Embed(context.Background(), "text-embedding-005", []string{"a"})
	assert.Error(t, err, "count mismatch")
}
