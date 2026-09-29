// Package parity runs one shared, table-driven suite against every adapter of a port. An adapter that
// cannot pass a row is a leaked abstraction: fix the port or the adapter, never exempt the row (plan § 10,
// § 16 rule 11).
package parity

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/anthropics/anthropic-sdk-go/option"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/oauth2"

	embedbedrock "github.com/GokulMV/DocTheRepo/internal/adapters/embed/bedrock"
	embedvertex "github.com/GokulMV/DocTheRepo/internal/adapters/embed/vertex"
	"github.com/GokulMV/DocTheRepo/internal/adapters/httpx"
	"github.com/GokulMV/DocTheRepo/internal/adapters/llm/anthropic"
	"github.com/GokulMV/DocTheRepo/internal/adapters/llm/azureopenai"
	"github.com/GokulMV/DocTheRepo/internal/adapters/llm/bedrock"
	"github.com/GokulMV/DocTheRepo/internal/adapters/llm/openai"
	"github.com/GokulMV/DocTheRepo/internal/adapters/llm/openaicompat"
	"github.com/GokulMV/DocTheRepo/internal/adapters/llm/vertex"
	"github.com/GokulMV/DocTheRepo/internal/ports"
	"github.com/GokulMV/DocTheRepo/test/mocks/llmmock"
)

type llmCase struct {
	name string
	// model maps a scenario to the model ID the adapter sends (Bedrock Claude needs the anthropic. prefix).
	model func(scenario string) string
	build func(t *testing.T, url string) ports.LLM
}

var tokens = oauth2.StaticTokenSource(&oauth2.Token{AccessToken: "t"})

func fastHTTP() *httpx.Client {
	c := httpx.New("parity")
	c.MaxRetries = 0
	return c
}

func llmAdapters() []llmCase {
	same := func(s string) string { return s }
	return []llmCase{
		{"anthropic", same, func(t *testing.T, url string) ports.LLM {
			a, err := anthropic.New(ports.ProviderConfig{APIKey: "k", BaseURL: url}, option.WithMaxRetries(0))
			require.NoError(t, err)
			return a
		}},
		{"openai", same, func(t *testing.T, url string) ports.LLM {
			c, err := openai.New(ports.ProviderConfig{APIKey: "k", BaseURL: url + "/v1"})
			require.NoError(t, err)
			return withFastHTTP(t, c, "openai", url+"/v1", map[string]string{"Authorization": "Bearer k"}, "max_completion_tokens", "schema", "")
		}},
		{"azure_openai", same, func(t *testing.T, url string) ports.LLM {
			_, err := azureopenai.New(ports.ProviderConfig{APIKey: "k", BaseURL: url, Extra: map[string]string{"deployment": "d"}})
			require.NoError(t, err)
			c, err := openaicompat.New(openaicompat.Options{Kind: "azure_openai", BaseURL: url + "/openai", ChatPath: "/deployments/d/chat/completions",
				Query: "api-version=x", Headers: map[string]string{"api-key": "k"}, MaxTokensField: "max_completion_tokens", JSONMode: "schema", HTTP: fastHTTP()})
			require.NoError(t, err)
			return c
		}},
		{"openai_compat", same, func(t *testing.T, url string) ports.LLM {
			return withFastHTTP(t, nil, "openai_compat", url+"/v1", nil, "max_tokens", "object", "")
		}},
		{"bedrock/converse", same, func(t *testing.T, url string) ports.LLM { return bedrockAdapter(t, url) }},
		{"bedrock/claude", func(s string) string { return "anthropic." + s }, func(t *testing.T, url string) ports.LLM { return bedrockAdapter(t, url) }},
		{"vertex/gemini", same, func(t *testing.T, url string) ports.LLM {
			g, err := vertex.NewGemini(context.Background(), ports.ProviderConfig{BaseURL: url, Extra: map[string]string{"project": "p", "region": "r"}}, tokens)
			require.NoError(t, err)
			return geminiLLM{g}
		}},
	}
}

// geminiLLM adapts the Gemini REST client to ports.LLM without the Claude half (which needs Google ADC and
// is the same anthropic adapter as the rows above).
type geminiLLM struct{ g *vertex.Gemini }

func (g geminiLLM) Kind() string { return "vertex" }
func (g geminiLLM) Chat(ctx context.Context, r ports.ChatRequest) (ports.ChatResponse, error) {
	return g.g.Chat(ctx, r)
}
func (g geminiLLM) Ping(ctx context.Context, m string) error { return g.g.Ping(ctx, m) }

func withFastHTTP(t *testing.T, _ *openaicompat.Client, kind, base string, headers map[string]string, maxField, jsonMode, _ string) ports.LLM {
	c, err := openaicompat.New(openaicompat.Options{Kind: kind, BaseURL: base, Headers: headers, MaxTokensField: maxField, JSONMode: jsonMode, HTTP: fastHTTP()})
	require.NoError(t, err)
	return c
}

func bedrockAdapter(t *testing.T, url string) ports.LLM {
	t.Helper()
	a, err := bedrock.New(context.Background(), ports.ProviderConfig{APIKey: "k", BaseURL: url,
		Extra: map[string]string{"region": "us-east-1", "access_key_id": "AKID", "secret_access_key": "s"}})
	require.NoError(t, err)
	return a
}

func TestLLMParity(t *testing.T) {
	for _, ad := range llmAdapters() {
		t.Run(ad.name, func(t *testing.T) {
			srv := llmmock.New()
			defer srv.Close()
			llm := ad.build(t, srv.URL)
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			req := func(scenario string) ports.ChatRequest {
				return ports.ChatRequest{Model: ad.model(scenario), System: "sys", MaxOutputTokens: 256,
					JSONSchema: map[string]any{"type": "object"}, Messages: []ports.ChatMessage{{Role: "user", Content: "hi"}}}
			}

			t.Run("success returns text and reported usage", func(t *testing.T) {
				resp, err := llm.Chat(ctx, req(llmmock.OK))
				require.NoError(t, err)
				assert.Equal(t, llmmock.Text, resp.Text)
				assert.True(t, resp.Usage.Reported)
				assert.Equal(t, int64(llmmock.InputTokens), resp.Usage.InputTokens+resp.Usage.CacheReadTokens)
				assert.Equal(t, int64(llmmock.OutputTokens), resp.Usage.OutputTokens)
				assert.NotEmpty(t, resp.Model)
			})
			t.Run("refusal is a RefusalError", func(t *testing.T) {
				_, err := llm.Chat(ctx, req(llmmock.Refusal))
				var re *ports.RefusalError
				assert.ErrorAs(t, err, &re)
			})
			t.Run("truncation is a TruncatedError with partial text", func(t *testing.T) {
				resp, err := llm.Chat(ctx, req(llmmock.Truncated))
				var te *ports.TruncatedError
				require.ErrorAs(t, err, &te)
				assert.Equal(t, `{"answ`, resp.Text)
			})
			t.Run("rate limiting is transient", func(t *testing.T) {
				_, err := llm.Chat(ctx, req(llmmock.RateLimited))
				_, transient := ports.AsTransient(err)
				assert.True(t, transient, "%v", err)
			})
			t.Run("bad credentials are permanent", func(t *testing.T) {
				_, err := llm.Chat(ctx, req(llmmock.Unauthorized))
				var pe *ports.PermanentError
				assert.True(t, errors.As(err, &pe), "%v", err)
			})
			t.Run("streaming callers still receive the full text", func(t *testing.T) {
				r := req(llmmock.OK)
				var got string
				r.OnDelta = func(s string) { got += s }
				resp, err := llm.Chat(ctx, r)
				require.NoError(t, err)
				assert.Equal(t, llmmock.Text, resp.Text)
				assert.Equal(t, llmmock.Text, got, "deltas concatenate to the full text")
				assert.Equal(t, int64(llmmock.OutputTokens), resp.Usage.OutputTokens, "streamed usage is captured")
			})
		})
	}
}

type embedCase struct {
	name  string
	model string
	build func(t *testing.T, url string) ports.Embedder
}

func TestEmbedderParity(t *testing.T) {
	cases := []embedCase{
		{"openai", "text-embedding-3-small", func(t *testing.T, url string) ports.Embedder {
			c, err := openaicompat.New(openaicompat.Options{Kind: "openai", BaseURL: url + "/v1", HTTP: fastHTTP()})
			require.NoError(t, err)
			return c
		}},
		{"ollama", "nomic-embed-text", func(t *testing.T, url string) ports.Embedder {
			c, err := openaicompat.FromConfig(ports.ProviderConfig{Kind: "ollama", BaseURL: url + "/v1"})
			require.NoError(t, err)
			return c
		}},
		{"bedrock/titan", "amazon.titan-embed-text-v2:0", func(t *testing.T, url string) ports.Embedder {
			e, err := embedbedrock.New(context.Background(), ports.ProviderConfig{BaseURL: url,
				Extra: map[string]string{"region": "us-east-1", "access_key_id": "AKID", "secret_access_key": "s"}})
			require.NoError(t, err)
			return e
		}},
		{"vertex", "text-embedding-005", func(t *testing.T, url string) ports.Embedder {
			e, err := embedvertex.New(context.Background(), ports.ProviderConfig{BaseURL: url, Extra: map[string]string{"project": "p", "region": "r"}}, tokens)
			require.NoError(t, err)
			return e
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			srv := llmmock.New()
			defer srv.Close()
			e := c.build(t, srv.URL)
			assert.Greater(t, e.MaxBatch(), 0)
			texts := []string{"alpha", "beta", "gamma"}
			res, err := e.Embed(context.Background(), c.model, texts)
			require.NoError(t, err)
			require.Len(t, res.Vectors, len(texts), "one vector per input")
			assert.Equal(t, 3, res.Dimensions)
			for _, v := range res.Vectors {
				assert.Len(t, v, res.Dimensions, "all vectors share the dimension")
			}
			assert.NotEmpty(t, e.Kind())
			assert.True(t, res.Usage.Reported)
			assert.Greater(t, res.Usage.InputTokens, int64(0))
		})
	}
}
