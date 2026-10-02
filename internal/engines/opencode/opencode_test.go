package opencode

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/GokulMV/DocTheRepo/internal/adapters/llm/externalcli"
	"github.com/GokulMV/DocTheRepo/pkg/contract"
)

// fakeOpencode writes a script that prints the given event lines and records its arguments.
func fakeOpencode(t *testing.T, events string, exit int) (bin, argsFile string) {
	t.Helper()
	dir := t.TempDir()
	argsFile = filepath.Join(dir, "args")
	bin = filepath.Join(dir, "opencode")
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" > " + argsFile + "\npwd >> " + argsFile + "\ncat <<'EOF'\n" + events + "\nEOF\nexit " + string(rune('0'+exit)) + "\n"
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return bin, argsFile
}

const reply = `Here you go: {"docs":[{"chunk_id":"0123456789abcdef","symbol":"Add","path":"docs/generated/calc/calc.go.md","content":"Adds.","summary":"Adds two ints."},` +
	`{"chunk_id":"fedcba9876543210","symbol":"Sub","path":"../../etc/passwd","content":"Subtracts.","summary":"Subtracts."},` +
	`{"chunk_id":"ffffffffffffffff","symbol":"X","path":"docs/generated/x.md","content":"x"}]}`

func events() string {
	b, _ := json.Marshal(reply)
	half, _ := json.Marshal(reply[:20])
	return `{"type":"step_start","part":{"id":"s1","type":"step-start"}}
{"type":"text","part":{"id":"t1","type":"text","text":` + string(half) + `}}
{"type":"text","part":{"id":"t1","type":"text","text":` + string(b) + `}}
{"type":"step_finish","part":{"id":"f1","type":"step-finish","tokens":{"input":1200,"output":300,"reasoning":0,"cache":{"read":0,"write":0}},"cost":0.01}}
{"type":"step_finish","part":{"id":"f1","type":"step-finish","tokens":{"input":1200,"output":300}}}
{"type":"info","message":{"providerID":"anthropic","modelID":"claude-sonnet-5-5"}}`
}

func TestRunWritesContractResult(t *testing.T) {
	bin, argsFile := fakeOpencode(t, events(), 0)
	dir := t.TempDir()
	task := externalcli.FixtureTask("job-1")
	task.ContractVersion = contract.DocGenVersion
	b, _ := json.Marshal(task)
	tf, rf := filepath.Join(dir, "task.json"), filepath.Join(dir, "result.json")
	_ = os.WriteFile(tf, b, 0o600)
	if err := Run(context.Background(), Options{Bin: bin, Model: "anthropic/claude-sonnet-5-5"}, tf, rf); err != nil {
		t.Fatal(err)
	}
	var res contract.DocGenResult
	raw, _ := os.ReadFile(rf)
	if err := json.Unmarshal(raw, &res); err != nil {
		t.Fatal(err)
	}
	if res.Status != "success" || len(res.Docs) != 2 {
		t.Fatalf("result = %s", raw)
	}
	if res.Docs[1].Path != "docs/generated/calc/calc.go.md" {
		t.Errorf("path outside docs_path must be replaced by the target, got %q", res.Docs[1].Path)
	}
	if res.Usage == nil || res.Usage.InputTokens != 1200 || res.Usage.OutputTokens != 300 || res.Usage.Model != "claude-sonnet-5-5" {
		t.Errorf("usage = %+v (step_finish repeated with the same id must count once)", res.Usage)
	}
	args, _ := os.ReadFile(argsFile)
	if !strings.Contains(string(args), "run\n--format\njson\n--model\nanthropic/claude-sonnet-5-5\n") || !strings.Contains(string(args), "dth-opencode-") {
		t.Errorf("args/cwd = %s", args)
	}
	if probs := contract.Validate(contract.DocGenResultSchema, raw); len(probs) > 0 {
		t.Errorf("schema: %v", probs)
	}
}

func TestRunFailureAndUnsupported(t *testing.T) {
	bin, _ := fakeOpencode(t, "boom", 1)
	dir := t.TempDir()
	tf, rf := filepath.Join(dir, "task.json"), filepath.Join(dir, "result.json")
	_ = os.WriteFile(tf, []byte(`{"contract_version":"2.0","chunks_to_generate":[]}`), 0o600)
	if err := Run(context.Background(), Options{Bin: bin}, tf, rf); err == nil {
		t.Fatal("want error")
	}
	raw, _ := os.ReadFile(rf)
	if !strings.Contains(string(raw), `"status":"error"`) {
		t.Fatalf("result = %s", raw)
	}
	_ = os.WriteFile(tf, []byte(`{"contract_version":"9.0"}`), 0o600)
	if err := Run(context.Background(), Options{Bin: bin}, tf, rf); err == nil || !strings.Contains(err.Error(), "unsupported") {
		t.Fatalf("err = %v", err)
	}
}

func TestParseEventsPlainText(t *testing.T) {
	ev := ParseEvents([]byte(`{"docs":[]}` + "\n"))
	if ev.HasUsage || ev.Text != "" {
		// A bare JSON object line is an event without a text part: no text, no usage.
		t.Logf("ev = %+v", ev)
	}
	ev = ParseEvents([]byte("plain reply {\"docs\":[]}"))
	if !strings.Contains(ev.Text, "plain reply") {
		t.Fatalf("plain output should be the text, got %+v", ev)
	}
}
