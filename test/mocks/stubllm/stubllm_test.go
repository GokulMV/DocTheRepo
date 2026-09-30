package stubllm_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/GokulMV/DocTheRepo/internal/adapters/llm/openaicompat"
	"github.com/GokulMV/DocTheRepo/internal/core/docgen"
	"github.com/GokulMV/DocTheRepo/internal/core/llmgateway"
	"github.com/GokulMV/DocTheRepo/internal/core/rag"
	"github.com/GokulMV/DocTheRepo/internal/core/spendguard"
	"github.com/GokulMV/DocTheRepo/internal/ports"
	"github.com/GokulMV/DocTheRepo/test/mocks/fakegw"
	"github.com/GokulMV/DocTheRepo/test/mocks/stubllm"
)

// env is a real gateway whose only provider is the real OpenAI-compatible adapter pointed at the stub, so
// docgen and Q&A run their production prompts end to end.
type env struct {
	client *openaicompat.Client
}

func (e env) Route(_ context.Context, f string) (llmgateway.Route, error) {
	switch f {
	case llmgateway.FeatureDocGen, llmgateway.FeatureQA, llmgateway.FeatureTriage:
		return llmgateway.Route{Feature: f, ProviderID: "stub", ProviderKind: "openai_compat", Model: "stub", MaxOutputTokens: 2000, ContextBudget: 8000}, nil
	case llmgateway.FeatureEmbedding:
		return llmgateway.Route{Feature: f, ProviderID: "stub", ProviderKind: "openai_compat", Model: "stub-embed"}, nil
	}
	return llmgateway.Route{}, llmgateway.ErrNoRoute
}
func (e env) LLM(context.Context, string) (ports.LLM, error)           { return e.client, nil }
func (e env) Embedder(context.Context, string) (ports.Embedder, error) { return e.client, nil }
func (e env) DocGenerator(context.Context, string) (ports.DocGenerator, error) {
	return nil, errors.New("no engine")
}

func newGateway(t *testing.T, s *stubllm.Server) *llmgateway.Gateway {
	t.Helper()
	c, err := openaicompat.New(openaicompat.Options{Kind: "openai_compat", BaseURL: s.URL + "/v1", MaxTokensField: "max_tokens", JSONMode: "schema"})
	require.NoError(t, err)
	g, err := spendguard.New(nil, nil, true)
	require.NoError(t, err)
	e := env{client: c}
	return llmgateway.New(spendguard.NewEnforcer(g, &fakegw.Ledger{}, nil), e, e, true)
}

func TestDocGenAnswersEveryRequestedChunk(t *testing.T) {
	s := stubllm.New()
	defer s.Close()
	gen := &docgen.Generator{GW: newGateway(t, s)}
	res, err := gen.Generate(context.Background(), llmgateway.CallMeta{}, docgen.Request{Repo: "acme/shop", SourcePath: "refund.go",
		Targets: []docgen.Target{{Chunk: ports.Chunk{ID: "c1", Symbol: "RetryRefund"}, ChangeType: "added"},
			{Chunk: ports.Chunk{ID: "c2", Symbol: "refundWindow"}, ChangeType: "modified"}}})
	require.NoError(t, err, "the output passes the DocGen schema and the requested-chunk check")
	require.Len(t, res.Sections, 2)
	assert.Contains(t, res.Sections[0].Body, "retry refund")
	assert.Contains(t, res.Sections[1].Body, "refund window")
	assert.Contains(t, res.Summary, "refund.go")
	assert.Equal(t, 1, s.Calls(stubllm.DocGen))
}

func TestQACitesMatchingSourcesThroughTheCitationContract(t *testing.T) {
	s := stubllm.New()
	defer s.Close()
	gw := newGateway(t, s)
	chunks := []ports.Chunk{
		{ID: "a", Scope: "acme/shop", Path: "cart.go", Symbol: "AddItem", Content: "func AddItem(cart *Cart, sku string) {}"},
		{ID: "b", Scope: "acme/shop", Path: "refund.go", Symbol: "RetryRefund", Content: "// RetryRefund retries failed refunds with backoff.\nfunc RetryRefund() {}"},
	}
	ask := func(q string) (string, []rag.Citation) {
		var streamed string
		resp, err := gw.Chat(context.Background(), llmgateway.FeatureQA, llmgateway.CallMeta{}, ports.ChatRequest{System: rag.System,
			Messages: []ports.ChatMessage{{Role: "user", Content: rag.Prompt(q, chunks)}}, OnDelta: func(d string) { streamed += d }})
		require.NoError(t, err)
		assert.Equal(t, resp.Text, streamed, "streamed deltas add up to the answer")
		return rag.Cite(resp.Text, chunks)
	}
	text, cits := ask("How are failed refunds retried?")
	require.Len(t, cits, 1)
	assert.Equal(t, "refund.go", cits[0].Path)
	assert.Contains(t, text, "[1]")

	text, cits = ask("What is the weather on Mars?")
	assert.Equal(t, rag.NotFoundAnswer, text)
	assert.Empty(t, cits)
}

func TestEmbeddingsAreSemanticEnoughAndDeterministic(t *testing.T) {
	cos := func(a, b []float32) float64 {
		var d float64
		for i := range a {
			d += float64(a[i] * b[i])
		}
		return d
	}
	q := stubllm.Embed("how are refunds retried")
	near := stubllm.Embed("func RetryRefund() // retries failed refunds")
	far := stubllm.Embed("func AddItem(cart *Cart, sku string)")
	assert.Greater(t, cos(q, near), cos(q, far))
	assert.Equal(t, q, stubllm.Embed("how are refunds retried"))
	assert.InDelta(t, 1.0, cos(q, q), 1e-5)

	s := stubllm.New()
	defer s.Close()
	c, err := openaicompat.New(openaicompat.Options{Kind: "openai_compat", BaseURL: s.URL + "/v1"})
	require.NoError(t, err)
	res, err := c.Embed(context.Background(), "stub-embed", []string{"a b", "refund"})
	require.NoError(t, err)
	require.Len(t, res.Vectors, 2)
	assert.Equal(t, stubllm.Dims, res.Dimensions)
}

func TestDelayAndConcurrencyCounters(t *testing.T) {
	s := stubllm.New()
	defer s.Close()
	s.SetDelay(stubllm.Triage, 150*time.Millisecond)
	gw := newGateway(t, s)
	done := make(chan struct{})
	for range 3 {
		go func() {
			_, _ = gw.Chat(context.Background(), llmgateway.FeatureTriage, llmgateway.CallMeta{}, ports.ChatRequest{System: "You classify a file change.",
				Messages: []ports.ChatMessage{{Role: "user", Content: "x"}}})
			done <- struct{}{}
		}()
	}
	start := time.Now()
	for range 3 {
		<-done
	}
	assert.GreaterOrEqual(t, time.Since(start), 150*time.Millisecond)
	st := s.Snapshot()[stubllm.Triage]
	assert.Equal(t, 3, st.Calls)
	assert.Equal(t, 3, st.MaxInFlight)
	assert.Zero(t, st.InFlight)
	s.Reset()
	assert.Zero(t, s.Calls(stubllm.Triage))
}
