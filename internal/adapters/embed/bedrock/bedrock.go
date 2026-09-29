// Package bedrock embeds text with Amazon Bedrock embedding models: Amazon Titan Text Embeddings
// (one text per call) and Cohere Embed (batched).
package bedrock

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime"

	llmbedrock "github.com/GokulMV/DocTheRepo/internal/adapters/llm/bedrock"
	"github.com/GokulMV/DocTheRepo/internal/ports"
)

// Embedder implements ports.Embedder.
type Embedder struct {
	rt         *bedrockruntime.Client
	dimensions int
}

// New builds the embedder. Extra: region (+ AWS credential keys as for the LLM adapter), dimensions
// (Titan v2 supports 256, 512, 1024).
func New(ctx context.Context, cfg ports.ProviderConfig) (*Embedder, error) {
	rt, err := llmbedrock.Runtime(ctx, cfg)
	if err != nil {
		return nil, err
	}
	e := &Embedder{rt: rt}
	if v := cfg.Extra["dimensions"]; v != "" {
		fmt.Sscan(v, &e.dimensions)
	}
	return e, nil
}

// Kind returns "bedrock".
func (e *Embedder) Kind() string { return "bedrock" }

// MaxBatch is Cohere's per-request limit; Titan requests are issued one text at a time inside Embed.
func (e *Embedder) MaxBatch() int { return 96 }

func isCohere(model string) bool { return strings.Contains(model, "cohere.") }

// Embed returns one vector per text, in order.
func (e *Embedder) Embed(ctx context.Context, model string, texts []string) (ports.EmbedResponse, error) {
	res := ports.EmbedResponse{Vectors: make([][]float32, 0, len(texts)), Usage: ports.TokenUsage{Reported: true}}
	if isCohere(model) {
		body, _ := json.Marshal(map[string]any{"texts": texts, "input_type": "search_document", "truncate": "END"})
		var out struct {
			Embeddings [][]float32 `json:"embeddings"`
		}
		if err := e.invoke(ctx, model, body, &out); err != nil {
			return res, err
		}
		if len(out.Embeddings) != len(texts) {
			return res, ports.Transient(fmt.Errorf("bedrock returned %d embeddings for %d inputs", len(out.Embeddings), len(texts)))
		}
		res.Vectors = out.Embeddings
		res.Usage.Reported = false // Cohere on Bedrock does not return token counts
	} else {
		for _, t := range texts {
			req := map[string]any{"inputText": t, "normalize": true}
			if e.dimensions > 0 {
				req["dimensions"] = e.dimensions
			}
			body, _ := json.Marshal(req)
			var out struct {
				Embedding           []float32 `json:"embedding"`
				InputTextTokenCount int64     `json:"inputTextTokenCount"`
			}
			if err := e.invoke(ctx, model, body, &out); err != nil {
				return res, err
			}
			res.Vectors = append(res.Vectors, out.Embedding)
			res.Usage.InputTokens += out.InputTextTokenCount
		}
	}
	for _, v := range res.Vectors {
		if res.Dimensions == 0 {
			res.Dimensions = len(v)
		}
		if len(v) == 0 || len(v) != res.Dimensions {
			return res, ports.Permanent(fmt.Errorf("bedrock returned empty or mixed-dimension embeddings"))
		}
	}
	return res, nil
}

func (e *Embedder) invoke(ctx context.Context, model string, body []byte, out any) error {
	resp, err := e.rt.InvokeModel(ctx, &bedrockruntime.InvokeModelInput{
		ModelId: aws.String(model), Body: body, ContentType: aws.String("application/json"), Accept: aws.String("application/json"),
	})
	if err != nil {
		return llmbedrock.Classify(err)
	}
	if err := json.Unmarshal(resp.Body, out); err != nil {
		return ports.Transient(fmt.Errorf("bedrock: decode embedding response: %w", err))
	}
	return nil
}
