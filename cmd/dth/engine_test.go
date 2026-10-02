package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestEngineOpencodeConformance runs the Hub's external_cli conformance suite against
// `dth engine opencode` with a fake opencode that streams JSON events.
func TestEngineOpencodeConformance(t *testing.T) {
	dir := t.TempDir()
	dth := filepath.Join(dir, "dth")
	out, err := exec.Command("go", "build", "-o", dth, ".").CombinedOutput()
	require.NoError(t, err, string(out))

	reply := `{"docs":[{"chunk_id":"0123456789abcdef","symbol":"Add","path":"docs/generated/calc/calc.go.md","content":"Adds a and b.","summary":"Adds."},` +
		`{"chunk_id":"fedcba9876543210","symbol":"Sub","path":"docs/generated/calc/calc.go.md","content":"Subtracts b from a.","summary":"Subtracts."}]}`
	text, _ := json.Marshal(reply)
	events := `{"type":"text","part":{"id":"t1","type":"text","text":` + string(text) + `}}` + "\n" +
		`{"type":"step_finish","part":{"id":"f1","type":"step-finish","tokens":{"input":900,"output":120}}}`
	fake := filepath.Join(dir, "opencode")
	require.NoError(t, os.WriteFile(fake, []byte("#!/bin/sh\ncat <<'EOF'\n"+events+"\nEOF\n"), 0o755))

	got, err := runCLI(t, "adapter-test", dth, "engine", "opencode", "--opencode", fake, "{task_file}", "{result_file}")
	require.NoError(t, err, got)
	assert.Contains(t, got, "Conforms to DocGen v2.")
}

func TestMCPAgainstHub(t *testing.T) {
	h := newFakeHub(t)
	var out, errOut bytes.Buffer
	root := newRoot(&out, &errOut)
	root.SetIn(strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18"}}` + "\n" +
		`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"ask","arguments":{"question":"refunds?","repos":["acme/shop"]}}}` + "\n"))
	root.SetArgs([]string{"mcp", "--server", h.URL, "--token", "dth_pat_ok"})
	require.NoError(t, root.ExecuteContext(context.Background()), errOut.String())
	assert.Contains(t, out.String(), `"serverInfo":{"name":"doctherepo-hub"`)
	assert.Contains(t, out.String(), `"text":"x"`)
	assert.Equal(t, []any{"r1"}, h.body["POST /ask"]["scope"].(map[string]any)["repo_ids"])

	t.Setenv("DTH_TOKEN", "")
	t.Setenv("DTH_CLI_CONFIG", filepath.Join(t.TempDir(), "none.json"))
	_, err := runCLI(t, "mcp", "--server", h.URL)
	assert.ErrorContains(t, err, "not signed in")
}
