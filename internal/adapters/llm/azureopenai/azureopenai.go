// Package azureopenai configures Azure OpenAI deployments on the shared OpenAI-protocol client. Azure
// routes by deployment name in the path and versions the API with an api-version query parameter.
package azureopenai

import (
	"errors"
	"net/url"
	"strings"

	"github.com/GokulMV/DocTheRepo/internal/adapters/llm/openaicompat"
	"github.com/GokulMV/DocTheRepo/internal/ports"
)

// DefaultAPIVersion is used when extra.api_version is unset.
const DefaultAPIVersion = "2024-10-21"

// New builds an Azure OpenAI client. BaseURL is the resource endpoint (https://<res>.openai.azure.com).
// Extra: deployment (chat), embedding_deployment, api_version, supports_effort, no_temperature.
// The route's model is sent for logging but Azure serves whatever model the deployment runs.
func New(cfg ports.ProviderConfig) (*openaicompat.Client, error) {
	if cfg.APIKey == "" || cfg.BaseURL == "" {
		return nil, errors.New("azure openai provider needs base_url (resource endpoint) and an API key")
	}
	dep := cfg.Extra["deployment"]
	if dep == "" {
		return nil, errors.New("azure openai provider needs extra.deployment")
	}
	embedDep := cfg.Extra["embedding_deployment"]
	if embedDep == "" {
		embedDep = dep
	}
	ver := cfg.Extra["api_version"]
	if ver == "" {
		ver = DefaultAPIVersion
	}
	o := openaicompat.Options{
		Kind: "azure_openai", BaseURL: strings.TrimSuffix(cfg.BaseURL, "/") + "/openai",
		ChatPath:   "/deployments/" + url.PathEscape(dep) + "/chat/completions",
		EmbedPath:  "/deployments/" + url.PathEscape(embedDep) + "/embeddings",
		ModelsPath: "/models", Query: "api-version=" + url.QueryEscape(ver),
		MaxTokensField: "max_completion_tokens", JSONMode: "schema", EmbedBatch: 2048,
		Headers: map[string]string{"api-key": cfg.APIKey},
	}
	openaicompat.ApplyExtra(&o, cfg.Extra)
	return openaicompat.New(o)
}
