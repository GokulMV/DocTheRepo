// Package openaicompat speaks the OpenAI Chat Completions and Embeddings protocol, which OpenAI, Azure
// OpenAI, Ollama, vLLM, LiteLLM, and most self-hosted gateways implement. The openai and azureopenai
// packages are thin constructors over this client.
package openaicompat

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/GokulMV/DocTheRepo/internal/adapters/httpx"
	"github.com/GokulMV/DocTheRepo/internal/ports"
)

// Options select protocol dialect details that differ between servers.
type Options struct {
	Kind    string // reported by Kind(): openai, azure_openai, openai_compat, ollama
	BaseURL string // e.g. https://api.openai.com/v1 or http://localhost:11434/v1
	// Headers are sent on every request (auth goes here: Authorization or api-key).
	Headers map[string]string
	// Query is appended to every URL (Azure's api-version).
	Query string
	// ChatPath/EmbedPath override the endpoint paths (Azure uses per-deployment paths).
	ChatPath, EmbedPath, ModelsPath string
	// MaxTokensField is "max_completion_tokens" (OpenAI/Azure) or "max_tokens" (most compatible servers).
	MaxTokensField string
	// JSONMode is "schema" (response_format json_schema), "object" (json_object), or "none".
	JSONMode string
	// SupportsEffort sends reasoning_effort for reasoning models.
	SupportsEffort bool
	// NoTemperature suppresses temperature (reasoning models reject it).
	NoTemperature bool
	// EmbedBatch is the maximum inputs per embeddings call.
	EmbedBatch int
	HTTP       *httpx.Client
}

// Client implements ports.LLM and ports.Embedder.
type Client struct{ o Options }

// New builds a client from explicit options.
func New(o Options) (*Client, error) {
	if o.BaseURL == "" {
		return nil, errors.New("openai-compatible provider needs a base URL")
	}
	o.BaseURL = strings.TrimSuffix(o.BaseURL, "/")
	if o.ChatPath == "" {
		o.ChatPath = "/chat/completions"
	}
	if o.EmbedPath == "" {
		o.EmbedPath = "/embeddings"
	}
	if o.ModelsPath == "" {
		o.ModelsPath = "/models"
	}
	if o.MaxTokensField == "" {
		o.MaxTokensField = "max_tokens"
	}
	if o.JSONMode == "" {
		o.JSONMode = "object"
	}
	if o.EmbedBatch <= 0 {
		o.EmbedBatch = 256
	}
	if o.HTTP == nil {
		o.HTTP = httpx.New(o.Kind)
	}
	return &Client{o: o}, nil
}

// FromConfig builds a generic OpenAI-compatible client (kind openai_compat or ollama). Extra keys:
// max_tokens_field, json_mode, supports_effort, no_temperature, embed_batch.
func FromConfig(cfg ports.ProviderConfig) (*Client, error) {
	o := Options{Kind: cfg.Kind, BaseURL: cfg.BaseURL, Headers: map[string]string{}}
	if o.Kind == "ollama" && o.BaseURL == "" {
		o.BaseURL = "http://localhost:11434/v1"
	}
	if cfg.APIKey != "" {
		o.Headers["Authorization"] = "Bearer " + cfg.APIKey
	}
	ApplyExtra(&o, cfg.Extra)
	return New(o)
}

// ApplyExtra maps provider Extra settings onto Options.
func ApplyExtra(o *Options, extra map[string]string) {
	if v := extra["max_tokens_field"]; v != "" {
		o.MaxTokensField = v
	}
	if v := extra["json_mode"]; v != "" {
		o.JSONMode = v
	}
	if extra["supports_effort"] == "true" {
		o.SupportsEffort = true
	}
	if extra["no_temperature"] == "true" {
		o.NoTemperature = true
	}
	if v := extra["embed_batch"]; v != "" {
		fmt.Sscan(v, &o.EmbedBatch)
	}
}

// Kind returns the provider kind.
func (c *Client) Kind() string { return c.o.Kind }

// MaxBatch is the embeddings batch limit.
func (c *Client) MaxBatch() int { return c.o.EmbedBatch }

func (c *Client) url(path string) string {
	u := c.o.BaseURL + path
	if c.o.Query != "" {
		u += "?" + c.o.Query
	}
	return u
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatResponse struct {
	Model   string `json:"model"`
	Choices []struct {
		Message struct {
			Content string `json:"content"`
			Refusal string `json:"refusal"`
		} `json:"message"`
		Delta struct {
			Content string `json:"content"`
			Refusal string `json:"refusal"`
		} `json:"delta"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
	Usage *struct {
		PromptTokens        int64 `json:"prompt_tokens"`
		CompletionTokens    int64 `json:"completion_tokens"`
		PromptTokensDetails struct {
			CachedTokens int64 `json:"cached_tokens"`
		} `json:"prompt_tokens_details"`
	} `json:"usage"`
}

// Chat sends a chat completion (streaming when OnDelta is set).
func (c *Client) Chat(ctx context.Context, req ports.ChatRequest) (ports.ChatResponse, error) {
	body := map[string]any{"model": req.Model}
	var msgs []chatMessage
	if req.System != "" {
		msgs = append(msgs, chatMessage{Role: "system", Content: req.System})
	}
	for _, m := range req.Messages {
		msgs = append(msgs, chatMessage{Role: m.Role, Content: m.Content})
	}
	body["messages"] = msgs
	if req.MaxOutputTokens > 0 {
		body[c.o.MaxTokensField] = req.MaxOutputTokens
	}
	if req.Temperature != nil && !c.o.NoTemperature {
		body["temperature"] = *req.Temperature
	}
	if req.Effort != "" && c.o.SupportsEffort {
		body["reasoning_effort"] = mapEffort(req.Effort)
	}
	if req.JSONSchema != nil {
		switch c.o.JSONMode {
		case "schema":
			body["response_format"] = map[string]any{"type": "json_schema",
				"json_schema": map[string]any{"name": "output", "schema": req.JSONSchema}}
		case "object":
			body["response_format"] = map[string]any{"type": "json_object"}
		}
	}
	if req.OnDelta != nil {
		body["stream"] = true
		body["stream_options"] = map[string]any{"include_usage": true}
		return c.stream(ctx, req, body)
	}
	var out chatResponse
	if err := c.o.HTTP.JSON(ctx, http.MethodPost, c.url(c.o.ChatPath), c.o.Headers, body, &out); err != nil {
		return ports.ChatResponse{}, err
	}
	if len(out.Choices) == 0 {
		return ports.ChatResponse{}, ports.Transient(fmt.Errorf("%s returned no choices", c.o.Kind))
	}
	ch := out.Choices[0]
	resp := ports.ChatResponse{Text: ch.Message.Content, Model: nz(out.Model, req.Model), StopReason: ch.FinishReason, Usage: usageOf(out)}
	return resp, finishError(ch.FinishReason, ch.Message.Refusal, req.MaxOutputTokens)
}

func (c *Client) stream(ctx context.Context, req ports.ChatRequest, body map[string]any) (ports.ChatResponse, error) {
	payload, _ := json.Marshal(body)
	httpResp, err := c.o.HTTP.Do(ctx, func() (*http.Request, error) {
		r, err := http.NewRequest(http.MethodPost, c.url(c.o.ChatPath), strings.NewReader(string(payload)))
		if err != nil {
			return nil, err
		}
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Accept", "text/event-stream")
		for k, v := range c.o.Headers {
			r.Header.Set(k, v)
		}
		return r, nil
	})
	if err != nil {
		return ports.ChatResponse{}, err
	}
	defer httpResp.Body.Close()
	var text, refusal strings.Builder
	resp := ports.ChatResponse{Model: req.Model}
	sc := bufio.NewScanner(io.LimitReader(httpResp.Body, httpx.MaxBody))
	sc.Buffer(make([]byte, 64*1024), 4*1024*1024)
	events := 0
	for sc.Scan() {
		line := sc.Text()
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		events++
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "[DONE]" {
			break
		}
		var ev chatResponse
		if err := json.Unmarshal([]byte(data), &ev); err != nil {
			return resp, ports.Transient(fmt.Errorf("%s stream: bad event: %w", c.o.Kind, err))
		}
		if ev.Model != "" {
			resp.Model = ev.Model
		}
		if ev.Usage != nil {
			resp.Usage = usageOf(ev)
		}
		for _, ch := range ev.Choices {
			if ch.Delta.Content != "" {
				text.WriteString(ch.Delta.Content)
				req.OnDelta(ch.Delta.Content)
			}
			refusal.WriteString(ch.Delta.Refusal)
			if ch.FinishReason != "" {
				resp.StopReason = ch.FinishReason
			}
		}
	}
	if err := sc.Err(); err != nil {
		if ctx.Err() != nil {
			return resp, ctx.Err()
		}
		return resp, ports.Transient(fmt.Errorf("%s stream: %w", c.o.Kind, err))
	}
	if events == 0 {
		// The server (or a proxy) answered without streaming; an empty reply must not look like success.
		return resp, ports.Transient(fmt.Errorf("%s stream contained no events", c.o.Kind))
	}
	resp.Text = text.String()
	return resp, finishError(resp.StopReason, refusal.String(), req.MaxOutputTokens)
}

func finishError(finish, refusal string, max int) error {
	switch {
	case refusal != "" || finish == "content_filter":
		return &ports.RefusalError{Category: "content_filter", Explanation: refusal}
	case finish == "length":
		return &ports.TruncatedError{MaxOutputTokens: max}
	}
	return nil
}

func usageOf(r chatResponse) ports.TokenUsage {
	if r.Usage == nil {
		return ports.TokenUsage{}
	}
	cached := r.Usage.PromptTokensDetails.CachedTokens
	return ports.TokenUsage{InputTokens: r.Usage.PromptTokens - cached, CacheReadTokens: cached,
		OutputTokens: r.Usage.CompletionTokens, Reported: true}
}

// mapEffort folds the Hub's five effort levels onto the three OpenAI reasoning levels.
func mapEffort(e string) string {
	switch e {
	case "xhigh", "max":
		return "high"
	}
	return e
}

// Embed embeds up to MaxBatch texts.
func (c *Client) Embed(ctx context.Context, model string, texts []string) (ports.EmbedResponse, error) {
	if len(texts) > c.o.EmbedBatch {
		return ports.EmbedResponse{}, ports.Permanent(fmt.Errorf("batch of %d exceeds %d", len(texts), c.o.EmbedBatch))
	}
	var out struct {
		Data []struct {
			Index     int       `json:"index"`
			Embedding []float32 `json:"embedding"`
		} `json:"data"`
		Usage *struct {
			PromptTokens int64 `json:"prompt_tokens"`
			TotalTokens  int64 `json:"total_tokens"`
		} `json:"usage"`
	}
	body := map[string]any{"model": model, "input": texts, "encoding_format": "float"}
	if err := c.o.HTTP.JSON(ctx, http.MethodPost, c.url(c.o.EmbedPath), c.o.Headers, body, &out); err != nil {
		return ports.EmbedResponse{}, err
	}
	if len(out.Data) != len(texts) {
		return ports.EmbedResponse{}, ports.Transient(fmt.Errorf("%s returned %d embeddings for %d inputs", c.o.Kind, len(out.Data), len(texts)))
	}
	res := ports.EmbedResponse{Vectors: make([][]float32, len(texts))}
	for _, d := range out.Data {
		if d.Index < 0 || d.Index >= len(texts) {
			return ports.EmbedResponse{}, ports.Transient(fmt.Errorf("%s returned embedding index %d out of range", c.o.Kind, d.Index))
		}
		res.Vectors[d.Index] = d.Embedding
	}
	for _, v := range res.Vectors {
		if len(v) == 0 {
			return ports.EmbedResponse{}, ports.Transient(fmt.Errorf("%s returned an empty embedding", c.o.Kind))
		}
		if res.Dimensions == 0 {
			res.Dimensions = len(v)
		} else if len(v) != res.Dimensions {
			return ports.EmbedResponse{}, ports.Permanent(fmt.Errorf("%s returned mixed embedding dimensions", c.o.Kind))
		}
	}
	if out.Usage != nil {
		res.Usage = ports.TokenUsage{InputTokens: nzi(out.Usage.PromptTokens, out.Usage.TotalTokens), Reported: true}
	}
	return res, nil
}

// Ping lists models (a free, authenticated call).
func (c *Client) Ping(ctx context.Context, model string) error {
	return c.o.HTTP.JSON(ctx, http.MethodGet, c.url(c.o.ModelsPath), c.o.Headers, nil, nil)
}

func nz(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

func nzi(a, b int64) int64 {
	if a != 0 {
		return a
	}
	return b
}
