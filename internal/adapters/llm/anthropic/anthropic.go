// Package anthropic is the LLM adapter for Claude, built on the official Anthropic Go SDK. One adapter
// serves three platforms — the Claude API, Amazon Bedrock (Mantle), and Google Vertex AI — because all
// three expose the same Messages API surface through the SDK.
package anthropic

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	sdk "github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/bedrock"
	"github.com/anthropics/anthropic-sdk-go/option"
	"github.com/anthropics/anthropic-sdk-go/shared/constant"
	"github.com/anthropics/anthropic-sdk-go/vertex"

	"github.com/GokulMV/DocTheRepo/internal/ports"
)

// DefaultModel is the seeded default route model for Claude providers.
const DefaultModel = "claude-opus-5-5"

// Platform identifies where Claude is served from.
type Platform string

const (
	PlatformClaudeAPI Platform = "claude_api"
	PlatformBedrock   Platform = "bedrock"
	PlatformVertex    Platform = "vertex"
)

// Models that accept server-side refusal fallbacks (`fallbacks: "default"`), which only the Claude API
// serves; on Bedrock and Vertex a refusal is returned to the caller as a RefusalError.
var fallbackModels = map[string]bool{
	"claude-fable-5-1": true, "claude-opus-5-5": true, "claude-opus-5": true, "claude-sonnet-5-5": true,
}

// Adapter implements ports.LLM for Claude.
type Adapter struct {
	platform  Platform
	messages  *sdk.BetaMessageService
	models    *sdk.ModelService // nil on platforms without the Models API
	fallbacks bool
	// noEffort remembers models that rejected the effort parameter, so it is not sent to them again.
	noEffort sync.Map
}

// effortUnsupported reports models known not to accept the effort parameter (it is ignored for them
// rather than failing every request). Others are learned from their first rejection.
func (a *Adapter) effortUnsupported(model string) bool {
	if _, ok := a.noEffort.Load(model); ok {
		return true
	}
	m := strings.ToLower(model)
	return strings.Contains(m, "haiku") || strings.HasPrefix(m, "claude-3")
}

// isEffortRejection reports the API's "this model does not support the effort parameter" error.
func isEffortRejection(err error) bool {
	var apiErr *sdk.Error
	if !errors.As(err, &apiErr) || apiErr.StatusCode != 400 {
		return false
	}
	return strings.Contains(strings.ToLower(apiErr.Error()), "effort")
}

// New builds a Claude API adapter. Extra["fallbacks"]="off" disables refusal fallbacks.
func New(cfg ports.ProviderConfig, extra ...option.RequestOption) (*Adapter, error) {
	if cfg.APIKey == "" {
		return nil, errors.New("anthropic provider needs an API key")
	}
	opts := []option.RequestOption{option.WithAPIKey(cfg.APIKey), option.WithMaxRetries(2)}
	if cfg.BaseURL != "" {
		opts = append(opts, option.WithBaseURL(cfg.BaseURL))
	}
	opts = append(opts, extra...)
	c := sdk.NewClient(opts...)
	return &Adapter{platform: PlatformClaudeAPI, messages: &c.Beta.Messages, models: &c.Models,
		fallbacks: cfg.Extra["fallbacks"] != "off"}, nil
}

// NewBedrock builds an adapter for Claude on Amazon Bedrock via the Mantle Messages endpoint. Model IDs
// carry the "anthropic." prefix. Credentials: an API key (Bedrock API key), or the AWS credential chain
// (task role, profile, env) when no key is configured.
func NewBedrock(ctx context.Context, cfg ports.ProviderConfig, extra ...option.RequestOption) (*Adapter, error) {
	region := cfg.Extra["region"]
	if region == "" {
		return nil, errors.New("bedrock provider needs extra.region")
	}
	mc, err := bedrock.NewMantleClient(ctx, bedrock.MantleClientConfig{
		APIKey: cfg.APIKey, AWSRegion: region, AWSProfile: cfg.Extra["profile"], BaseURL: cfg.BaseURL,
	}, append([]option.RequestOption{option.WithMaxRetries(2)}, extra...)...)
	if err != nil {
		return nil, fmt.Errorf("bedrock client: %w", err)
	}
	return &Adapter{platform: PlatformBedrock, messages: &mc.Beta.Messages}, nil
}

// NewVertex builds an adapter for Claude on Google Vertex AI (Application Default Credentials).
func NewVertex(ctx context.Context, cfg ports.ProviderConfig, extra ...option.RequestOption) (*Adapter, error) {
	region, project := cfg.Extra["region"], cfg.Extra["project"]
	if region == "" || project == "" {
		return nil, errors.New("vertex provider needs extra.region and extra.project")
	}
	opts := []option.RequestOption{vertex.WithGoogleAuth(ctx, region, project), option.WithMaxRetries(2)}
	if cfg.BaseURL != "" {
		opts = append(opts, option.WithBaseURL(cfg.BaseURL))
	}
	c := sdk.NewClient(append(opts, extra...)...)
	return &Adapter{platform: PlatformVertex, messages: &c.Beta.Messages}, nil
}

// Kind returns the provider kind.
func (a *Adapter) Kind() string {
	switch a.platform {
	case PlatformBedrock:
		return "bedrock"
	case PlatformVertex:
		return "vertex"
	}
	return "anthropic"
}

// Streaming keeps long generations under HTTP timeouts.
const streamAbove = 16000

// Chat sends one Messages request. Thinking is left at the model default (always on for current
// models; they reject a disabled setting) and temperature is never sent (current models reject sampling
// parameters). Effort is set explicitly when the route specifies it.
func (a *Adapter) Chat(ctx context.Context, req ports.ChatRequest) (ports.ChatResponse, error) {
	if req.Model == "" {
		return ports.ChatResponse{}, ports.Permanent(errors.New("no model specified"))
	}
	maxTokens := int64(req.MaxOutputTokens)
	if maxTokens <= 0 {
		maxTokens = 16000
	}
	p := sdk.BetaMessageNewParams{Model: sdk.Model(req.Model), MaxTokens: maxTokens}
	// Prompt caching: a breakpoint after the system prompt, and one after the last message, so the next
	// turn of a conversation (or the next file of a docs job, which shares the system prompt) reads the
	// repeated prefix from the cache at a tenth of the input price. A prefix shorter than the model's
	// minimum is simply not cached.
	if req.System != "" {
		p.System = []sdk.BetaTextBlockParam{{Text: req.System, CacheControl: sdk.NewBetaCacheControlEphemeralParam()}}
	}
	for i, m := range req.Messages {
		text := sdk.BetaTextBlockParam{Text: m.Content}
		if i == len(req.Messages)-1 {
			text.CacheControl = sdk.NewBetaCacheControlEphemeralParam()
		}
		block := sdk.BetaContentBlockParamUnion{OfText: &text}
		if m.Role == "assistant" {
			p.Messages = append(p.Messages, sdk.BetaMessageParam{Role: sdk.BetaMessageParamRoleAssistant, Content: []sdk.BetaContentBlockParamUnion{block}})
		} else {
			p.Messages = append(p.Messages, sdk.NewBetaUserMessage(block))
		}
	}
	if req.Effort != "" && !a.effortUnsupported(req.Model) {
		p.OutputConfig.Effort = sdk.BetaOutputConfigEffort(req.Effort)
	}
	if req.JSONSchema != nil {
		p.OutputConfig.Format = sdk.BetaJSONOutputFormatParam{Schema: req.JSONSchema}
	}
	if a.fallbacks && a.platform == PlatformClaudeAPI && fallbackModels[req.Model] {
		p.Fallbacks = sdk.BetaFallbacksParamUnion{OfDefault: constant.ValueOf[constant.Default]()}
		p.Betas = append(p.Betas, sdk.AnthropicBetaServerSideFallback2026_07_01)
	}

	msg, err := a.send(ctx, p, req.OnDelta, maxTokens)
	if err != nil && p.OutputConfig.Effort != "" && isEffortRejection(err) {
		// The route sets an effort this model does not accept: remember that, and send it without.
		a.noEffort.Store(req.Model, true)
		p.OutputConfig.Effort = ""
		msg, err = a.send(ctx, p, req.OnDelta, maxTokens)
	}
	if err != nil {
		return ports.ChatResponse{}, err
	}
	return toResponse(msg, int(maxTokens))
}

// send performs one request, streaming long or incremental generations.
func (a *Adapter) send(ctx context.Context, p sdk.BetaMessageNewParams, onDelta func(string), maxTokens int64) (*sdk.BetaMessage, error) {
	if onDelta == nil && maxTokens <= streamAbove {
		m, err := a.messages.New(ctx, p)
		if err != nil {
			return nil, classify(err)
		}
		return m, nil
	}
	stream := a.messages.NewStreaming(ctx, p)
	acc := sdk.BetaMessage{}
	for stream.Next() {
		ev := stream.Current()
		if err := acc.Accumulate(ev); err != nil {
			return nil, ports.Transient(fmt.Errorf("claude stream: %w", err))
		}
		if onDelta != nil {
			if d, ok := ev.AsAny().(sdk.BetaRawContentBlockDeltaEvent); ok {
				if t, ok := d.Delta.AsAny().(sdk.BetaTextDelta); ok {
					onDelta(t.Text)
				}
			}
		}
	}
	if err := stream.Err(); err != nil {
		return nil, classify(err)
	}
	if acc.ID == "" {
		// No message_start event: the upstream (or a proxy) did not stream. Never treat that as an
		// empty but successful reply.
		return nil, ports.Transient(errors.New("claude stream contained no events"))
	}
	return &acc, nil
}

func toResponse(msg *sdk.BetaMessage, maxTokens int) (ports.ChatResponse, error) {
	var b strings.Builder
	for _, block := range msg.Content {
		if t, ok := block.AsAny().(sdk.BetaTextBlock); ok {
			b.WriteString(t.Text)
		}
	}
	u := msg.Usage
	resp := ports.ChatResponse{
		Text: b.String(), Model: string(msg.Model), StopReason: string(msg.StopReason),
		Usage: ports.TokenUsage{InputTokens: u.InputTokens, OutputTokens: u.OutputTokens,
			CacheReadTokens: u.CacheReadInputTokens, CacheWriteTokens: u.CacheCreationInputTokens, Reported: true},
	}
	switch msg.StopReason {
	case sdk.BetaStopReasonRefusal:
		// Usage is still returned so the caller can record what the refused attempt cost.
		return resp, &ports.RefusalError{Category: string(msg.StopDetails.Category), Explanation: msg.StopDetails.Explanation}
	case sdk.BetaStopReasonMaxTokens:
		return resp, &ports.TruncatedError{MaxOutputTokens: maxTokens}
	}
	return resp, nil
}

// Ping verifies the key and model. On the Claude API it uses the free Models endpoint; elsewhere it
// sends the smallest possible message.
func (a *Adapter) Ping(ctx context.Context, model string) error {
	if a.models != nil {
		if _, err := a.models.Get(ctx, model, sdk.ModelGetParams{}); err != nil {
			return classify(err)
		}
		return nil
	}
	_, err := a.Chat(ctx, ports.ChatRequest{Model: model, MaxOutputTokens: 64, Effort: "low",
		Messages: []ports.ChatMessage{{Role: "user", Content: "Reply with OK."}}})
	var tr *ports.TruncatedError
	if errors.As(err, &tr) {
		return nil // reaching the model is the point; a truncated reply still proves access
	}
	return err
}

// classify maps SDK errors onto the Hub's typed errors. The SDK has already retried retryable statuses
// (honouring retry-after); what reaches here is final for this attempt.
func classify(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	var apierr *sdk.Error
	if !errors.As(err, &apierr) {
		return ports.Transient(fmt.Errorf("claude request failed: %w", err)) // network/transport
	}
	msg := fmt.Errorf("claude API %d (%s, request %s): %w", apierr.StatusCode, apierr.Type(), apierr.RequestID, err)
	switch {
	case apierr.StatusCode == http.StatusTooManyRequests || apierr.StatusCode == 529 ||
		apierr.StatusCode == http.StatusRequestTimeout || apierr.StatusCode >= 500:
		return ports.TransientAfter(msg, retryAfter(apierr.Response))
	default: // 400 invalid request, 401/403 auth or permission, 402 billing, 404 model
		return ports.Permanent(msg)
	}
}

func retryAfter(r *http.Response) time.Duration {
	if r == nil {
		return 0
	}
	if s, err := strconv.ParseFloat(r.Header.Get("retry-after"), 64); err == nil && s > 0 {
		return time.Duration(s * float64(time.Second))
	}
	return 0
}
