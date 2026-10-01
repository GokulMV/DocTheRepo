// Package llm wires provider kinds to adapter factories and serves built adapters to the LLM gateway.
// Adding a provider kind is one Register call here plus its adapter package; the gateway and the core
// never switch on provider kind.
package llm

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	embedbedrock "github.com/GokulMV/DocTheRepo/internal/adapters/embed/bedrock"
	embedvertex "github.com/GokulMV/DocTheRepo/internal/adapters/embed/vertex"
	"github.com/GokulMV/DocTheRepo/internal/adapters/llm/anthropic"
	"github.com/GokulMV/DocTheRepo/internal/adapters/llm/azureopenai"
	"github.com/GokulMV/DocTheRepo/internal/adapters/llm/bedrock"
	"github.com/GokulMV/DocTheRepo/internal/adapters/llm/externalcli"
	"github.com/GokulMV/DocTheRepo/internal/adapters/llm/jev"
	"github.com/GokulMV/DocTheRepo/internal/adapters/llm/openai"
	"github.com/GokulMV/DocTheRepo/internal/adapters/llm/openaicompat"
	"github.com/GokulMV/DocTheRepo/internal/adapters/llm/vertex"
	"github.com/GokulMV/DocTheRepo/internal/ports"
)

// Factories build adapters from decrypted provider config. A kind may support any subset.
type Factories struct {
	LLM      func(ctx context.Context, cfg ports.ProviderConfig) (ports.LLM, error)
	Embedder func(ctx context.Context, cfg ports.ProviderConfig) (ports.Embedder, error)
	DocGen   func(ctx context.Context, cfg ports.ProviderConfig) (ports.DocGenerator, error)
}

var (
	regMu    sync.RWMutex
	registry = map[string]Factories{}
)

// Register adds or replaces the factories for a provider kind.
func Register(kind string, f Factories) {
	regMu.Lock()
	defer regMu.Unlock()
	registry[kind] = f
}

func factories(kind string) (Factories, bool) {
	regMu.RLock()
	defer regMu.RUnlock()
	f, ok := registry[kind]
	return f, ok
}

// Kinds lists registered provider kinds.
func Kinds() []string {
	regMu.RLock()
	defer regMu.RUnlock()
	out := make([]string, 0, len(registry))
	for k := range registry {
		out = append(out, k)
	}
	return out
}

func init() {
	openAIish := func(build func(ports.ProviderConfig) (*openaicompat.Client, error)) Factories {
		return Factories{
			LLM:      func(_ context.Context, c ports.ProviderConfig) (ports.LLM, error) { return build(c) },
			Embedder: func(_ context.Context, c ports.ProviderConfig) (ports.Embedder, error) { return build(c) },
		}
	}
	Register("anthropic", Factories{LLM: func(_ context.Context, c ports.ProviderConfig) (ports.LLM, error) { return anthropic.New(c) }})
	Register("openai", openAIish(openai.New))
	Register("azure_openai", openAIish(azureopenai.New))
	Register("openai_compat", openAIish(openaicompat.FromConfig))
	Register("ollama", openAIish(openaicompat.FromConfig))
	Register("bedrock", Factories{
		LLM: func(ctx context.Context, c ports.ProviderConfig) (ports.LLM, error) { return bedrock.New(ctx, c) },
		Embedder: func(ctx context.Context, c ports.ProviderConfig) (ports.Embedder, error) {
			return embedbedrock.New(ctx, c)
		},
	})
	Register("vertex", Factories{
		LLM: func(ctx context.Context, c ports.ProviderConfig) (ports.LLM, error) { return vertex.New(ctx, c) },
		Embedder: func(ctx context.Context, c ports.ProviderConfig) (ports.Embedder, error) {
			return embedvertex.New(ctx, c, nil)
		},
	})
	// TypeSafe Jev: decisions only (plan Phase 11.5); route it to the "decide" feature.
	Register("jev", Factories{LLM: func(_ context.Context, c ports.ProviderConfig) (ports.LLM, error) { return jev.New(c) }})
	Register("external_cli", Factories{DocGen: func(_ context.Context, c ports.ProviderConfig) (ports.DocGenerator, error) { return externalcli.New(c) }})
}

// Source loads decrypted provider configuration by ID (store + secrets box in production).
type Source interface {
	ProviderConfig(ctx context.Context, id string) (cfg ports.ProviderConfig, enabled bool, err error)
}

type entry struct {
	at       time.Time
	llm      ports.LLM
	embedder ports.Embedder
	docgen   ports.DocGenerator
}

// Pool builds adapters on demand and caches them; it implements llmgateway.Providers.
type Pool struct {
	src   Source
	ttl   time.Duration
	mu    sync.Mutex
	cache map[string]*entry
	now   func() time.Time
}

// NewPool returns a pool. Cached adapters are rebuilt after ttl (credential rotation, e.g. AWS roles) or
// immediately after Invalidate.
func NewPool(src Source, ttl time.Duration) *Pool {
	if ttl <= 0 {
		ttl = 15 * time.Minute
	}
	return &Pool{src: src, ttl: ttl, cache: map[string]*entry{}, now: time.Now}
}

// Invalidate drops a provider's cached adapters (call after the provider is edited or deleted).
func (p *Pool) Invalidate(id string) {
	p.mu.Lock()
	delete(p.cache, id)
	p.mu.Unlock()
}

// ErrUnsupported means the provider kind cannot serve the requested capability (e.g. embeddings on anthropic).
var ErrUnsupported = errors.New("provider kind does not support this capability")

func (p *Pool) get(ctx context.Context, id string, want string) (*entry, error) {
	p.mu.Lock()
	e, ok := p.cache[id]
	if ok && p.now().Sub(e.at) < p.ttl {
		if (want == "llm" && e.llm != nil) || (want == "embed" && e.embedder != nil) || (want == "docgen" && e.docgen != nil) {
			p.mu.Unlock()
			return e, nil
		}
	}
	p.mu.Unlock()

	cfg, enabled, err := p.src.ProviderConfig(ctx, id)
	if err != nil {
		return nil, err
	}
	if !enabled {
		return nil, ports.Permanent(fmt.Errorf("provider %q is disabled", cfg.Name))
	}
	f, ok := factories(cfg.Kind)
	if !ok {
		return nil, ports.Permanent(fmt.Errorf("unknown provider kind %q", cfg.Kind))
	}
	ne := &entry{at: p.now()}
	switch want {
	case "llm":
		if f.LLM == nil {
			return nil, ports.Permanent(fmt.Errorf("%s: chat: %w", cfg.Kind, ErrUnsupported))
		}
		ne.llm, err = f.LLM(ctx, cfg)
	case "embed":
		if f.Embedder == nil {
			return nil, ports.Permanent(fmt.Errorf("%s: embeddings: %w", cfg.Kind, ErrUnsupported))
		}
		ne.embedder, err = f.Embedder(ctx, cfg)
	case "docgen":
		if f.DocGen == nil {
			return nil, ports.Permanent(fmt.Errorf("%s: documentation engine: %w", cfg.Kind, ErrUnsupported))
		}
		ne.docgen, err = f.DocGen(ctx, cfg)
	}
	if err != nil {
		return nil, ports.Permanent(fmt.Errorf("build %s provider %q: %w", cfg.Kind, cfg.Name, err))
	}
	p.mu.Lock()
	if old, ok := p.cache[id]; ok && p.now().Sub(old.at) < p.ttl { // keep other capabilities already built
		if ne.llm == nil {
			ne.llm = old.llm
		}
		if ne.embedder == nil {
			ne.embedder = old.embedder
		}
		if ne.docgen == nil {
			ne.docgen = old.docgen
		}
	}
	p.cache[id] = ne
	p.mu.Unlock()
	return ne, nil
}

// LLM returns the chat adapter for a provider.
func (p *Pool) LLM(ctx context.Context, id string) (ports.LLM, error) {
	e, err := p.get(ctx, id, "llm")
	if err != nil {
		return nil, err
	}
	return e.llm, nil
}

// Embedder returns the embedding adapter for a provider.
func (p *Pool) Embedder(ctx context.Context, id string) (ports.Embedder, error) {
	e, err := p.get(ctx, id, "embed")
	if err != nil {
		return nil, err
	}
	return e.embedder, nil
}

// DocGenerator returns the documentation engine for a provider.
func (p *Pool) DocGenerator(ctx context.Context, id string) (ports.DocGenerator, error) {
	e, err := p.get(ctx, id, "docgen")
	if err != nil {
		return nil, err
	}
	return e.docgen, nil
}

// Test builds an adapter from unsaved config and pings it (POST /providers/{id}/test).
func Test(ctx context.Context, cfg ports.ProviderConfig, model string) (time.Duration, error) {
	f, ok := factories(cfg.Kind)
	if !ok {
		return 0, fmt.Errorf("unknown provider kind %q", cfg.Kind)
	}
	start := time.Now()
	switch {
	case f.LLM != nil:
		l, err := f.LLM(ctx, cfg)
		if err != nil {
			return 0, err
		}
		err = l.Ping(ctx, model)
		return time.Since(start), err
	case f.DocGen != nil:
		rep := externalcli.Conformance(ctx, cfg)
		if !rep.Passed {
			return time.Since(start), fmt.Errorf("engine failed conformance: %+v", rep.Checks)
		}
		return time.Since(start), nil
	}
	return 0, ErrUnsupported
}
