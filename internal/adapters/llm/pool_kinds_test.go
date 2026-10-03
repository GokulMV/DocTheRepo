package llm

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/GokulMV/DocTheRepo/internal/ports"
)

func TestOpencodeAndGitHubModelsKinds(t *testing.T) {
	assert.Contains(t, Kinds(), "opencode")
	assert.Contains(t, Kinds(), "github_models")
	assert.Equal(t, OpencodeCommand, withOpencodeDefaults(ports.ProviderConfig{}).Extra["command_template"])
	custom := withOpencodeDefaults(ports.ProviderConfig{Extra: map[string]string{"command_template": "x {task_file} {result_file}"}})
	assert.Equal(t, "x {task_file} {result_file}", custom.Extra["command_template"], "an explicit command wins")

	get := func(kind string) Factories { regMu.RLock(); defer regMu.RUnlock(); return registry[kind] }
	eng, err := get("opencode").DocGen(context.Background(), ports.ProviderConfig{Kind: "opencode"})
	require.NoError(t, err)
	assert.Equal(t, "external_cli", eng.Kind())
	_, err = get("github_models").LLM(context.Background(), ports.ProviderConfig{Kind: "github_models", APIKey: "ghp_x"})
	require.NoError(t, err)
}
