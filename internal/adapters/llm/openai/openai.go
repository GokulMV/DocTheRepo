// Package openai configures the OpenAI API (api.openai.com) on the shared OpenAI-protocol client.
package openai

import (
	"errors"

	"github.com/GokulMV/DocTheRepo/internal/adapters/llm/openaicompat"
	"github.com/GokulMV/DocTheRepo/internal/ports"
)

// DefaultBaseURL is the public OpenAI API.
const DefaultBaseURL = "https://api.openai.com/v1"

// New builds an OpenAI client. Structured outputs use json_schema; max_completion_tokens replaces the
// deprecated max_tokens. Extra: organization, project, supports_effort, no_temperature.
func New(cfg ports.ProviderConfig) (*openaicompat.Client, error) {
	if cfg.APIKey == "" {
		return nil, errors.New("openai provider needs an API key")
	}
	o := openaicompat.Options{
		Kind: "openai", BaseURL: cfg.BaseURL, MaxTokensField: "max_completion_tokens", JSONMode: "schema", EmbedBatch: 2048,
		Headers: map[string]string{"Authorization": "Bearer " + cfg.APIKey},
	}
	if o.BaseURL == "" {
		o.BaseURL = DefaultBaseURL
	}
	if v := cfg.Extra["organization"]; v != "" {
		o.Headers["OpenAI-Organization"] = v
	}
	if v := cfg.Extra["project"]; v != "" {
		o.Headers["OpenAI-Project"] = v
	}
	openaicompat.ApplyExtra(&o, cfg.Extra)
	return openaicompat.New(o)
}
