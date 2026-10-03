package ports

import (
	"context"
	"fmt"
)

// ChatMessage is one conversation turn. Role is "user" or "assistant".
type ChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// ChatRequest is a provider-neutral chat completion request.
type ChatRequest struct {
	Model           string
	System          string
	Messages        []ChatMessage
	MaxOutputTokens int
	// Effort is the reasoning effort for providers that support it (low|medium|high|xhigh|max); empty
	// leaves the provider default.
	Effort string
	// JSONSchema, when set, asks for a JSON object matching the schema (native structured output where
	// the provider supports it; the caller still validates).
	JSONSchema map[string]any
	// Temperature is sent only to providers/models that accept it.
	Temperature *float64
	// OnDelta, when set, streams text deltas as they arrive; the response still carries the full text.
	OnDelta func(text string)
}

// TokenUsage is provider-reported consumption. Reported is false when the provider returned nothing,
// in which case the caller records its own estimate.
type TokenUsage struct {
	InputTokens      int64 `json:"input_tokens"`
	OutputTokens     int64 `json:"output_tokens"`
	CacheReadTokens  int64 `json:"cache_read_tokens,omitempty"`
	CacheWriteTokens int64 `json:"cache_write_tokens,omitempty"`
	Reported         bool  `json:"reported"`
}

// Total is billed input (including cache reads/writes) plus output.
func (u TokenUsage) Total() int64 {
	return u.InputTokens + u.OutputTokens + u.CacheReadTokens + u.CacheWriteTokens
}

// ChatResponse is the provider-neutral result.
type ChatResponse struct {
	Text string `json:"text"`
	// Model is the model that actually served the request (it can differ from the requested one when a
	// provider-side fallback re-served a refused request).
	Model      string     `json:"model"`
	StopReason string     `json:"stop_reason"`
	Usage      TokenUsage `json:"usage"`
}

// RefusalError reports that the model declined the request (a policy refusal, not a transport failure).
// Retrying the same request will not help, so it is permanent from the job's point of view.
type RefusalError struct {
	Category    string
	Explanation string
}

func (e *RefusalError) Error() string {
	if e.Category == "" {
		return "model declined the request"
	}
	return fmt.Sprintf("model declined the request (%s)", e.Category)
}

// TruncatedError reports output cut off at max_output_tokens.
type TruncatedError struct{ MaxOutputTokens int }

func (e *TruncatedError) Error() string {
	return fmt.Sprintf("output truncated at max_output_tokens=%d", e.MaxOutputTokens)
}

// LLM is a chat-capable model provider (one configured provider account).
type LLM interface {
	// Kind is the provider kind (anthropic, openai, azure_openai, bedrock, vertex, openai_compat, ollama).
	Kind() string
	Chat(ctx context.Context, req ChatRequest) (ChatResponse, error)
	// Ping performs the cheapest authenticated call that proves the key works.
	Ping(ctx context.Context, model string) error
}

// EmbedResponse is the result of one embedding call.
type EmbedResponse struct {
	Vectors    [][]float32 `json:"-"`
	Dimensions int         `json:"dimensions"`
	Usage      TokenUsage  `json:"usage"`
}

// Embedder turns texts into vectors.
type Embedder interface {
	Kind() string
	Embed(ctx context.Context, model string, texts []string) (EmbedResponse, error)
	// MaxBatch is the largest number of texts one call may carry.
	MaxBatch() int
}

// ProviderConfig is everything a provider adapter factory needs, decrypted just before construction.
type ProviderConfig struct {
	ID      string
	Kind    string
	Name    string
	BaseURL string
	APIKey  string
	// Extra holds kind-specific settings: region, project, deployment, api_version, command_template...
	Extra map[string]string
}

// SchemaError reports that an engine's output did not match its contract; it triggers the single
// repair-retry (plan § 8, DocGen contract versioning).
type SchemaError struct{ Problems []string }

func (e *SchemaError) Error() string {
	return fmt.Sprintf("output failed schema validation: %v", e.Problems)
}

// DecisionOption is one answer a decision question allows.
type DecisionOption struct {
	ID          string `json:"id"`
	Description string `json:"description"`
}

// DecisionQuestion is a typed classification question (plan Phase 11.5): pick one option given context.
type DecisionQuestion struct {
	// Task names the question (issue_actionability, change_cosmetic, …) for routing, prompts and evals.
	Task string `json:"task"`
	// Question is the instruction in plain language ("Does this issue need engineering attention?").
	Question string           `json:"question,omitempty"`
	Context  string           `json:"context"`
	Options  []DecisionOption `json:"options"`
}

// Decision is a probability per option. Calibrated says the probabilities come from a model trained to be
// calibrated (a native decision model such as TypeSafe Jev); false means they are self-reported by a chat
// model and must be treated as rough.
type Decision struct {
	Probabilities map[string]float64 `json:"probabilities"`
	Choice        string             `json:"choice"`
	P             float64            `json:"p"`
	Calibrated    bool               `json:"calibrated"`
	Model         string             `json:"model,omitempty"`
	Usage         TokenUsage         `json:"usage"`
}

// Decider is implemented by providers with a native decision model. Chat providers do not need it: the
// gateway asks them for JSON probabilities instead.
type Decider interface {
	Decide(ctx context.Context, model string, q DecisionQuestion) (Decision, error)
}

// JudgeQuestion is one yes/no question about a judgment's shared state.
type JudgeQuestion struct {
	ID           string `json:"id"`
	Instructions string `json:"instructions"`
}

// JudgeRequest asks many yes/no questions about one state in a single call (the Ask evidence sifter asks
// two per retrieved piece of source), so the shared state is paid for once.
type JudgeRequest struct {
	Task      string          `json:"task"`
	State     string          `json:"state"`
	Questions []JudgeQuestion `json:"questions"`
}

// Judgment is the probability of "yes" per question ID. Calibrated is as for Decision.
type Judgment struct {
	P          map[string]float64 `json:"p"`
	Calibrated bool               `json:"calibrated"`
	Model      string             `json:"model,omitempty"`
	Usage      TokenUsage         `json:"usage"`
}

// Judger is implemented by providers with a native yes/no model (TypeSafe Jev). Chat providers answer
// judgments through a JSON contract instead.
type Judger interface {
	Judge(ctx context.Context, model string, r JudgeRequest) (Judgment, error)
}
