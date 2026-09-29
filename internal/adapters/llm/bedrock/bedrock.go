// Package bedrock is the LLM adapter for Amazon Bedrock. Claude models ("anthropic.*") go through the
// Anthropic SDK's Mantle client (the Messages API on Bedrock); every other model family (Amazon Nova,
// Meta Llama, Mistral, Cohere, ...) goes through the model-agnostic Converse API.
package bedrock

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime"
	brtypes "github.com/aws/aws-sdk-go-v2/service/bedrockruntime/types"
	"github.com/aws/smithy-go"

	"github.com/GokulMV/DocTheRepo/internal/adapters/llm/anthropic"
	"github.com/GokulMV/DocTheRepo/internal/ports"
)

// Adapter implements ports.LLM for Bedrock.
type Adapter struct {
	claude  *anthropic.Adapter
	runtime *bedrockruntime.Client
}

// New builds a Bedrock adapter. Extra: region (required), profile, access_key_id, secret_access_key,
// session_token (else the default AWS chain: task role, env, shared config). APIKey is a Bedrock API key,
// used by the Claude (Mantle) path.
func New(ctx context.Context, cfg ports.ProviderConfig) (*Adapter, error) {
	rt, err := Runtime(ctx, cfg)
	if err != nil {
		return nil, err
	}
	cl, err := anthropic.NewBedrock(ctx, cfg)
	if err != nil {
		return nil, err
	}
	return &Adapter{claude: cl, runtime: rt}, nil
}

// Runtime builds a bedrock-runtime client from provider config (also used by the embeddings adapter).
func Runtime(ctx context.Context, cfg ports.ProviderConfig) (*bedrockruntime.Client, error) {
	region := cfg.Extra["region"]
	if region == "" {
		return nil, errors.New("bedrock provider needs extra.region")
	}
	opts := []func(*awsconfig.LoadOptions) error{awsconfig.WithRegion(region)}
	if p := cfg.Extra["profile"]; p != "" {
		opts = append(opts, awsconfig.WithSharedConfigProfile(p))
	}
	if ak, sk := cfg.Extra["access_key_id"], cfg.Extra["secret_access_key"]; ak != "" && sk != "" {
		opts = append(opts, awsconfig.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(ak, sk, cfg.Extra["session_token"])))
	}
	awsCfg, err := awsconfig.LoadDefaultConfig(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("load AWS config: %w", err)
	}
	return bedrockruntime.NewFromConfig(awsCfg, func(o *bedrockruntime.Options) {
		if cfg.BaseURL != "" {
			o.BaseEndpoint = aws.String(cfg.BaseURL)
		}
	}), nil
}

// Kind returns "bedrock".
func (a *Adapter) Kind() string { return "bedrock" }

// IsClaude reports whether a Bedrock model ID is a Claude model (served via Mantle).
func IsClaude(model string) bool {
	m := model
	for _, p := range inferenceProfilePrefixes {
		if strings.HasPrefix(m, p) {
			m = strings.TrimPrefix(m, p)
			break
		}
	}
	return strings.HasPrefix(m, "anthropic.")
}

// Cross-region / geographic inference profile prefixes that precede a Bedrock model ID.
var inferenceProfilePrefixes = []string{"global.", "us-gov.", "us.", "eu.", "apac.", "jp.", "au.", "ca."}

// Chat dispatches by model family.
func (a *Adapter) Chat(ctx context.Context, req ports.ChatRequest) (ports.ChatResponse, error) {
	if IsClaude(req.Model) {
		return a.claude.Chat(ctx, req)
	}
	return a.converse(ctx, req)
}

func (a *Adapter) converse(ctx context.Context, req ports.ChatRequest) (ports.ChatResponse, error) {
	in := &bedrockruntime.ConverseInput{ModelId: aws.String(req.Model), InferenceConfig: &brtypes.InferenceConfiguration{}}
	if req.MaxOutputTokens > 0 {
		in.InferenceConfig.MaxTokens = aws.Int32(int32(req.MaxOutputTokens))
	}
	if req.Temperature != nil {
		in.InferenceConfig.Temperature = aws.Float32(float32(*req.Temperature))
	}
	system := req.System
	if req.JSONSchema != nil {
		// Converse has no portable structured-output switch; the caller validates and repairs.
		system += "\n\nRespond with a single JSON object and nothing else."
	}
	if strings.TrimSpace(system) != "" {
		in.System = []brtypes.SystemContentBlock{&brtypes.SystemContentBlockMemberText{Value: strings.TrimSpace(system)}}
	}
	for _, m := range req.Messages {
		role := brtypes.ConversationRoleUser
		if m.Role == "assistant" {
			role = brtypes.ConversationRoleAssistant
		}
		in.Messages = append(in.Messages, brtypes.Message{Role: role, Content: []brtypes.ContentBlock{&brtypes.ContentBlockMemberText{Value: m.Content}}})
	}
	out, err := a.runtime.Converse(ctx, in)
	if err != nil {
		return ports.ChatResponse{}, Classify(err)
	}
	var b strings.Builder
	if msg, ok := out.Output.(*brtypes.ConverseOutputMemberMessage); ok {
		for _, c := range msg.Value.Content {
			if t, ok := c.(*brtypes.ContentBlockMemberText); ok {
				b.WriteString(t.Value)
			}
		}
	}
	resp := ports.ChatResponse{Text: b.String(), Model: req.Model, StopReason: string(out.StopReason)}
	if u := out.Usage; u != nil {
		resp.Usage = ports.TokenUsage{InputTokens: i64(u.InputTokens), OutputTokens: i64(u.OutputTokens),
			CacheReadTokens: i64(u.CacheReadInputTokens), CacheWriteTokens: i64(u.CacheWriteInputTokens), Reported: true}
	}
	if req.OnDelta != nil && resp.Text != "" {
		req.OnDelta(resp.Text) // non-streaming path: deliver the whole text as one delta
	}
	switch out.StopReason {
	case brtypes.StopReasonContentFiltered, brtypes.StopReasonGuardrailIntervened:
		return resp, &ports.RefusalError{Category: string(out.StopReason)}
	case brtypes.StopReasonMaxTokens:
		return resp, &ports.TruncatedError{MaxOutputTokens: req.MaxOutputTokens}
	}
	return resp, nil
}

// Ping sends the smallest possible request to the model.
func (a *Adapter) Ping(ctx context.Context, model string) error {
	if IsClaude(model) {
		return a.claude.Ping(ctx, model)
	}
	_, err := a.converse(ctx, ports.ChatRequest{Model: model, MaxOutputTokens: 8, Messages: []ports.ChatMessage{{Role: "user", Content: "Reply with OK."}}})
	var tr *ports.TruncatedError
	if errors.As(err, &tr) {
		return nil
	}
	return err
}

// Classify maps AWS API errors onto typed errors (the AWS SDK has already retried with backoff).
func Classify(err error) error {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	var ae smithy.APIError
	if errors.As(err, &ae) {
		switch ae.ErrorCode() {
		case "ThrottlingException", "ServiceUnavailableException", "ModelNotReadyException", "InternalServerException",
			"ModelTimeoutException", "ServiceQuotaExceededException":
			return ports.Transient(fmt.Errorf("bedrock %s: %w", ae.ErrorCode(), err))
		default:
			return ports.Permanent(fmt.Errorf("bedrock %s: %w", ae.ErrorCode(), err))
		}
	}
	return ports.Transient(fmt.Errorf("bedrock: %w", err))
}

func i64(p *int32) int64 {
	if p == nil {
		return 0
	}
	return int64(*p)
}
