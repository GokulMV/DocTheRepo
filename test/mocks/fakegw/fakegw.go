// Package fakegw builds a real llmgateway.Gateway over in-memory routes, a ledger, and fake providers,
// for pipeline tests: the docgen model answers every requested chunk_id, the embedder returns stable
// hash vectors, and a token limit can be set to exercise spend blocking.
package fakegw

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"sync"

	"github.com/GokulMV/DocTheRepo/internal/core/llmgateway"
	"github.com/GokulMV/DocTheRepo/internal/core/spendguard"
	"github.com/GokulMV/DocTheRepo/internal/ports"
	"github.com/GokulMV/DocTheRepo/pkg/contract"
)

// Dims is the fake embedding dimension.
const Dims = 8

// Env is a gateway with inspectable fakes.
type Env struct {
	GW     *llmgateway.Gateway
	Ledger *Ledger
	Model  *LLM
	Emb    *Embedder
	Routes map[string]llmgateway.Route
	mu     sync.Mutex
}

// Options configures New.
type Options struct {
	// TokenLimit, when > 0, is a global daily token ceiling.
	TokenLimit int64
	// NoTriageRoute leaves the triage feature unrouted.
	NoTriageRoute bool
}

// New builds the environment.
func New(o Options) *Env {
	e := &Env{Ledger: &Ledger{}, Model: &LLM{}, Emb: &Embedder{}}
	e.Routes = map[string]llmgateway.Route{
		llmgateway.FeatureDocGen:    {Feature: "docgen", ProviderID: "llm", ProviderKind: "anthropic", Model: "claude-opus-5-5", MaxOutputTokens: 2000, ContextBudget: 12000},
		llmgateway.FeatureEmbedding: {Feature: "embedding", ProviderID: "emb", ProviderKind: "openai", Model: "fake-embed"},
		llmgateway.FeatureQA:        {Feature: "qa", ProviderID: "llm", ProviderKind: "anthropic", Model: "claude-opus-5-5", MaxOutputTokens: 1000},
	}
	if !o.NoTriageRoute {
		e.Routes[llmgateway.FeatureTriage] = llmgateway.Route{Feature: "triage", ProviderID: "llm", ProviderKind: "anthropic", Model: "claude-opus-5-5", MaxOutputTokens: 200}
	}
	var limits []spendguard.Limit
	allowUnlimited := true
	if o.TokenLimit > 0 {
		limits = []spendguard.Limit{{Scope: spendguard.ScopeGlobal, Window: spendguard.WindowDay, MaxTokens: o.TokenLimit}}
	}
	g, err := spendguard.New(limits, nil, allowUnlimited)
	if err != nil {
		panic(err)
	}
	e.GW = llmgateway.New(spendguard.NewEnforcer(g, e.Ledger, nil), e, e, true)
	return e
}

// Route implements llmgateway.Routes.
func (e *Env) Route(_ context.Context, f string) (llmgateway.Route, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	r, ok := e.Routes[f]
	if !ok {
		return r, llmgateway.ErrNoRoute
	}
	return r, nil
}

// LLM implements llmgateway.Providers.
func (e *Env) LLM(context.Context, string) (ports.LLM, error) { return e.Model, nil }

// Embedder implements llmgateway.Providers.
func (e *Env) Embedder(context.Context, string) (ports.Embedder, error) { return e.Emb, nil }

// DocGenerator implements llmgateway.Providers.
func (e *Env) DocGenerator(context.Context, string) (ports.DocGenerator, error) {
	return nil, ports.Permanent(errors.New("no engine"))
}

// SetEmbeddingModel changes the embedding route's model (reindex tests).
func (e *Env) SetEmbeddingModel(model string, dims int) {
	e.mu.Lock()
	defer e.mu.Unlock()
	r := e.Routes[llmgateway.FeatureEmbedding]
	r.Model = model
	e.Routes[llmgateway.FeatureEmbedding] = r
	e.Emb.setDims(dims)
}

// Ledger is an in-memory spend ledger.
type Ledger struct {
	mu      sync.Mutex
	Records []ports.UsageRecord
}

// Spent sums matching records.
func (l *Ledger) Spent(_ context.Context, f ports.SpendFilter) (ports.SpendTotals, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	var t ports.SpendTotals
	for _, r := range l.Records {
		if (f.Feature == "" || r.Feature == f.Feature) && (f.RepoID == "" || r.RepoID == f.RepoID) {
			t.Tokens += r.InputTokens + r.OutputTokens
			t.CostUSD += r.CostUSD
		}
	}
	return t, nil
}

// Record appends a record.
func (l *Ledger) Record(_ context.Context, r ports.UsageRecord) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.Records = append(l.Records, r)
	return nil
}

// Calls counts records for a feature.
func (l *Ledger) Calls(feature string) int {
	l.mu.Lock()
	defer l.mu.Unlock()
	n := 0
	for _, r := range l.Records {
		if r.Feature == feature {
			n++
		}
	}
	return n
}

var chunkLineRE = regexp.MustCompile("(?m)^- chunk_id ([0-9a-f]+): `([^`]*)`")

// LLM answers docgen prompts with one doc per requested chunk_id and triage prompts as not cosmetic.
type LLM struct {
	mu   sync.Mutex
	Reqs []ports.ChatRequest
	// Handler, when set, overrides the default answers.
	Handler func(ports.ChatRequest) (ports.ChatResponse, error)
}

// Kind returns "fake".
func (l *LLM) Kind() string { return "fake" }

// Ping succeeds.
func (l *LLM) Ping(context.Context, string) error { return nil }

// Chat answers a request.
func (l *LLM) Chat(_ context.Context, r ports.ChatRequest) (ports.ChatResponse, error) {
	l.mu.Lock()
	l.Reqs = append(l.Reqs, r)
	h := l.Handler
	l.mu.Unlock()
	if h != nil {
		return h(r)
	}
	prompt := r.Messages[len(r.Messages)-1].Content
	var text string
	switch {
	case strings.Contains(prompt, "Document exactly these chunks"):
		type doc struct {
			ChunkID string `json:"chunk_id"`
			Symbol  string `json:"symbol"`
			Content string `json:"content"`
		}
		out := struct {
			FileSummary string `json:"file_summary"`
			Docs        []doc  `json:"docs"`
		}{FileSummary: "Generated summary."}
		for _, m := range chunkLineRE.FindAllStringSubmatch(prompt, -1) {
			out.Docs = append(out.Docs, doc{ChunkID: m[1], Symbol: m[2], Content: fmt.Sprintf("Documents `%s`.", m[2])})
		}
		b, _ := json.Marshal(out)
		text = string(b)
	case strings.Contains(prompt, "\nQuestion: "):
		// Q&A: cite the first source, plus a fabricated [99] the answer contract must drop. A prompt whose
		// question contains "unanswerable" gets an uncited reply.
		if strings.Contains(prompt, "unanswerable") {
			text = "Nothing relevant here."
		} else {
			text = "The answer is in the first source [1]. A made-up claim [99]."
		}
		if r.OnDelta != nil {
			for _, part := range strings.SplitAfter(text, " ") {
				r.OnDelta(part)
			}
		}
	default:
		b, _ := json.Marshal(contract.TriageVerdict{Cosmetic: false, Confident: true, Reason: "fake"})
		text = string(b)
	}
	return ports.ChatResponse{Text: text, Usage: ports.TokenUsage{InputTokens: int64(len(prompt) / 4), OutputTokens: int64(len(text) / 4), Reported: true}}, nil
}

// DocgenCalls counts docgen requests.
func (l *LLM) DocgenCalls() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	n := 0
	for _, r := range l.Reqs {
		if strings.Contains(r.Messages[len(r.Messages)-1].Content, "Document exactly these chunks") {
			n++
		}
	}
	return n
}

// Embedder returns deterministic unit-ish vectors derived from the text hash.
type Embedder struct {
	mu    sync.Mutex
	dims  int
	Texts int
	// Same maps a text to another whose vector it gets, so tests can make two wordings mean the same.
	Same map[string]string
}

func (e *Embedder) setDims(d int) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.dims = d
}

// Kind returns "fake".
func (e *Embedder) Kind() string { return "fake" }

// MaxBatch returns 16.
func (e *Embedder) MaxBatch() int { return 16 }

// Embed hashes each text into a vector.
func (e *Embedder) Embed(_ context.Context, _ string, texts []string) (ports.EmbedResponse, error) {
	e.mu.Lock()
	d := e.dims
	if d == 0 {
		d = Dims
	}
	e.Texts += len(texts)
	same := e.Same
	e.mu.Unlock()
	r := ports.EmbedResponse{Dimensions: d, Usage: ports.TokenUsage{InputTokens: int64(len(texts) * 10), Reported: true}}
	for _, t := range texts {
		if s, ok := same[t]; ok {
			t = s
		}
		r.Vectors = append(r.Vectors, Vector(t, d))
	}
	return r, nil
}

// Vector is the fake embedding of text.
func Vector(text string, dims int) []float32 {
	h := sha256.Sum256([]byte(text))
	v := make([]float32, dims)
	for i := range v {
		v[i] = float32(h[i%len(h)])/255 + 0.01
	}
	return v
}
