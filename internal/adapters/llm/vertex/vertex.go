// Package vertex is the LLM adapter for Google Vertex AI. Claude models ("claude-*") go through the
// Anthropic SDK's Vertex client; Gemini models go through the Vertex generateContent REST API. Both use
// Application Default Credentials (Workload Identity on Cloud Run/GKE, gcloud ADC locally).
package vertex

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"

	"github.com/GokulMV/DocTheRepo/internal/adapters/httpx"
	"github.com/GokulMV/DocTheRepo/internal/adapters/llm/anthropic"
	"github.com/GokulMV/DocTheRepo/internal/ports"
)

// Scope is the OAuth scope Vertex AI requires.
const Scope = "https://www.googleapis.com/auth/cloud-platform"

// Adapter implements ports.LLM for Vertex AI.
type Adapter struct {
	claude  *anthropic.Adapter
	gemini  *Gemini
	project string
}

// New builds a Vertex adapter. Extra: project, region (e.g. us-central1 or global).
func New(ctx context.Context, cfg ports.ProviderConfig) (*Adapter, error) {
	g, err := NewGemini(ctx, cfg, nil)
	if err != nil {
		return nil, err
	}
	cl, err := anthropic.NewVertex(ctx, cfg)
	if err != nil {
		return nil, err
	}
	return &Adapter{claude: cl, gemini: g}, nil
}

// Kind returns "vertex".
func (a *Adapter) Kind() string { return "vertex" }

// Chat dispatches by model family.
func (a *Adapter) Chat(ctx context.Context, req ports.ChatRequest) (ports.ChatResponse, error) {
	if strings.HasPrefix(req.Model, "claude-") {
		return a.claude.Chat(ctx, req)
	}
	return a.gemini.Chat(ctx, req)
}

// Ping checks access to the model.
func (a *Adapter) Ping(ctx context.Context, model string) error {
	if strings.HasPrefix(model, "claude-") {
		return a.claude.Ping(ctx, model)
	}
	return a.gemini.Ping(ctx, model)
}

// Gemini calls Vertex AI publisher models over REST.
type Gemini struct {
	base   string // https://{region}-aiplatform.googleapis.com/v1/projects/{p}/locations/{r}/publishers/google/models
	tokens oauth2.TokenSource
	http   *httpx.Client
}

// NewGemini builds the REST client. A nil token source uses Application Default Credentials.
func NewGemini(ctx context.Context, cfg ports.ProviderConfig, ts oauth2.TokenSource) (*Gemini, error) {
	project, region := cfg.Extra["project"], cfg.Extra["region"]
	if project == "" || region == "" {
		return nil, errors.New("vertex provider needs extra.project and extra.region")
	}
	host := "https://" + region + "-aiplatform.googleapis.com"
	if region == "global" {
		host = "https://aiplatform.googleapis.com"
	}
	if cfg.BaseURL != "" {
		host = strings.TrimSuffix(cfg.BaseURL, "/")
	}
	if ts == nil {
		var err error
		ts, err = google.DefaultTokenSource(ctx, Scope)
		if err != nil {
			return nil, fmt.Errorf("vertex credentials (Application Default Credentials): %w", err)
		}
	}
	return &Gemini{
		base:   fmt.Sprintf("%s/v1/projects/%s/locations/%s/publishers/google/models", host, project, region),
		tokens: oauth2.ReuseTokenSource(nil, ts), http: httpx.New("vertex"),
	}, nil
}

func (g *Gemini) headers() (map[string]string, error) {
	tok, err := g.tokens.Token()
	if err != nil {
		return nil, ports.Transient(fmt.Errorf("vertex token: %w", err))
	}
	return map[string]string{"Authorization": "Bearer " + tok.AccessToken}, nil
}

type part struct {
	Text string `json:"text,omitempty"`
}

type content struct {
	Role  string `json:"role,omitempty"`
	Parts []part `json:"parts"`
}

// Chat calls generateContent.
func (g *Gemini) Chat(ctx context.Context, req ports.ChatRequest) (ports.ChatResponse, error) {
	body := map[string]any{}
	var contents []content
	for _, m := range req.Messages {
		role := "user"
		if m.Role == "assistant" {
			role = "model"
		}
		contents = append(contents, content{Role: role, Parts: []part{{Text: m.Content}}})
	}
	body["contents"] = contents
	if req.System != "" {
		body["systemInstruction"] = content{Parts: []part{{Text: req.System}}}
	}
	gc := map[string]any{}
	if req.MaxOutputTokens > 0 {
		gc["maxOutputTokens"] = req.MaxOutputTokens
	}
	if req.Temperature != nil {
		gc["temperature"] = *req.Temperature
	}
	if req.JSONSchema != nil {
		gc["responseMimeType"] = "application/json" // JSON mode; the caller validates the shape
	}
	if len(gc) > 0 {
		body["generationConfig"] = gc
	}
	h, err := g.headers()
	if err != nil {
		return ports.ChatResponse{}, err
	}
	var out struct {
		Candidates []struct {
			Content      content `json:"content"`
			FinishReason string  `json:"finishReason"`
		} `json:"candidates"`
		PromptFeedback *struct {
			BlockReason string `json:"blockReason"`
		} `json:"promptFeedback"`
		UsageMetadata *struct {
			PromptTokenCount        int64 `json:"promptTokenCount"`
			CandidatesTokenCount    int64 `json:"candidatesTokenCount"`
			ThoughtsTokenCount      int64 `json:"thoughtsTokenCount"`
			CachedContentTokenCount int64 `json:"cachedContentTokenCount"`
		} `json:"usageMetadata"`
		ModelVersion string `json:"modelVersion"`
	}
	if err := g.http.JSON(ctx, http.MethodPost, g.base+"/"+req.Model+":generateContent", h, body, &out); err != nil {
		return ports.ChatResponse{}, err
	}
	resp := ports.ChatResponse{Model: req.Model}
	if out.ModelVersion != "" {
		resp.Model = out.ModelVersion
	}
	if u := out.UsageMetadata; u != nil {
		resp.Usage = ports.TokenUsage{InputTokens: u.PromptTokenCount - u.CachedContentTokenCount, CacheReadTokens: u.CachedContentTokenCount,
			OutputTokens: u.CandidatesTokenCount + u.ThoughtsTokenCount, Reported: true}
	}
	if out.PromptFeedback != nil && out.PromptFeedback.BlockReason != "" {
		return resp, &ports.RefusalError{Category: strings.ToLower(out.PromptFeedback.BlockReason)}
	}
	if len(out.Candidates) == 0 {
		return resp, ports.Transient(errors.New("vertex returned no candidates"))
	}
	c := out.Candidates[0]
	var b strings.Builder
	for _, p := range c.Content.Parts {
		b.WriteString(p.Text)
	}
	resp.Text, resp.StopReason = b.String(), c.FinishReason
	if req.OnDelta != nil && resp.Text != "" {
		req.OnDelta(resp.Text)
	}
	switch c.FinishReason {
	case "SAFETY", "PROHIBITED_CONTENT", "BLOCKLIST", "SPII", "RECITATION":
		return resp, &ports.RefusalError{Category: strings.ToLower(c.FinishReason)}
	case "MAX_TOKENS":
		return resp, &ports.TruncatedError{MaxOutputTokens: req.MaxOutputTokens}
	}
	return resp, nil
}

// Ping counts tokens for a one-word prompt (free, authenticated, model-specific).
func (g *Gemini) Ping(ctx context.Context, model string) error {
	h, err := g.headers()
	if err != nil {
		return err
	}
	body := map[string]any{"contents": []content{{Role: "user", Parts: []part{{Text: "ping"}}}}}
	return g.http.JSON(ctx, http.MethodPost, g.base+"/"+model+":countTokens", h, body, nil)
}

// Embed calls the text-embedding predict endpoint (used by the embed/vertex adapter).
func (g *Gemini) Embed(ctx context.Context, model string, texts []string, taskType string) (ports.EmbedResponse, error) {
	h, err := g.headers()
	if err != nil {
		return ports.EmbedResponse{}, err
	}
	inst := make([]map[string]string, len(texts))
	for i, t := range texts {
		inst[i] = map[string]string{"content": t, "task_type": taskType}
	}
	var out struct {
		Predictions []struct {
			Embeddings struct {
				Values     []float32 `json:"values"`
				Statistics struct {
					TokenCount float64 `json:"token_count"`
				} `json:"statistics"`
			} `json:"embeddings"`
		} `json:"predictions"`
	}
	if err := g.http.JSON(ctx, http.MethodPost, g.base+"/"+model+":predict", h, map[string]any{"instances": inst}, &out); err != nil {
		return ports.EmbedResponse{}, err
	}
	if len(out.Predictions) != len(texts) {
		return ports.EmbedResponse{}, ports.Transient(fmt.Errorf("vertex returned %d embeddings for %d inputs", len(out.Predictions), len(texts)))
	}
	res := ports.EmbedResponse{Vectors: make([][]float32, len(texts)), Usage: ports.TokenUsage{Reported: true}}
	for i, p := range out.Predictions {
		res.Vectors[i] = p.Embeddings.Values
		res.Usage.InputTokens += int64(p.Embeddings.Statistics.TokenCount)
		if res.Dimensions == 0 {
			res.Dimensions = len(p.Embeddings.Values)
		}
	}
	return res, nil
}
