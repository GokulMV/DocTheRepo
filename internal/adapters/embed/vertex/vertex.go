// Package vertex embeds text with Vertex AI text-embedding models.
package vertex

import (
	"context"

	"golang.org/x/oauth2"

	llmvertex "github.com/GokulMV/DocTheRepo/internal/adapters/llm/vertex"
	"github.com/GokulMV/DocTheRepo/internal/ports"
)

// Embedder implements ports.Embedder.
type Embedder struct{ g *llmvertex.Gemini }

// New builds the embedder (Extra: project, region). ts may be nil for Application Default Credentials.
func New(ctx context.Context, cfg ports.ProviderConfig, ts oauth2.TokenSource) (*Embedder, error) {
	g, err := llmvertex.NewGemini(ctx, cfg, ts)
	if err != nil {
		return nil, err
	}
	return &Embedder{g: g}, nil
}

// Kind returns "vertex".
func (e *Embedder) Kind() string { return "vertex" }

// MaxBatch keeps requests well under Vertex's per-request instance and token limits.
func (e *Embedder) MaxBatch() int { return 100 }

// Embed embeds documents for retrieval.
func (e *Embedder) Embed(ctx context.Context, model string, texts []string) (ports.EmbedResponse, error) {
	return e.g.Embed(ctx, model, texts, "RETRIEVAL_DOCUMENT")
}
