// Package opencode runs opencode (https://opencode.ai) as a DocGen-contract engine, so the Hub's
// external_cli provider can generate docs with whatever model opencode is configured for:
//
//	command_template: dth engine opencode --model anthropic/claude-sonnet-5-5 {task_file} {result_file}
//
// It turns the task into one prompt, runs `opencode run --format json` in an empty scratch directory (the
// agent must not touch a repository: the Hub lands docs itself), reads the newline-delimited JSON events
// for the reply text and token usage, and writes a contract result. The event format is read defensively:
// text comes from parts of type "text", usage from any object carrying tokens.input/tokens.output.
package opencode

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path"
	"strings"
	"time"

	"github.com/GokulMV/DocTheRepo/pkg/contract"
)

// Options configure a run.
type Options struct {
	// Bin is the opencode executable (default "opencode", or $OPENCODE_BIN).
	Bin string
	// Model is passed as --model (provider/model); empty uses opencode's configured default.
	Model string
	// Agent is passed as --agent when set.
	Agent string
	// Timeout bounds one opencode run (default 10 minutes; the Hub's own timeout also applies).
	Timeout time.Duration
}

// ExitUnsupported is the exit code for a task with an unsupported contract major version.
const ExitUnsupported = 3

// ErrUnsupported marks an unsupported contract version.
var ErrUnsupported = errors.New("unsupported DocGen contract version")

// Run executes one task file and writes the result file. It returns ErrUnsupported (exit 3) for a
// contract major it does not speak, and an error (exit 1) when opencode fails; a result file with
// status "error" is written in that case when possible.
func Run(ctx context.Context, o Options, taskFile, resultFile string) error {
	raw, err := os.ReadFile(taskFile)
	if err != nil {
		return fmt.Errorf("read task: %w", err)
	}
	var task contract.DocGenTask
	if err := json.Unmarshal(raw, &task); err != nil {
		return fmt.Errorf("parse task: %w", err)
	}
	if !strings.HasPrefix(task.ContractVersion, "2.") {
		return fmt.Errorf("%w %q", ErrUnsupported, task.ContractVersion)
	}
	res, err := generate(ctx, o, task)
	if err != nil {
		msg := err.Error()
		_ = write(resultFile, contract.DocGenResult{ContractVersion: task.ContractVersion, Status: "error", Error: &msg, Docs: []contract.GeneratedDoc{}})
		return err
	}
	return write(resultFile, res)
}

func write(p string, res contract.DocGenResult) error {
	b, err := json.Marshal(res)
	if err != nil {
		return err
	}
	return os.WriteFile(p, b, 0o600)
}

func generate(ctx context.Context, o Options, task contract.DocGenTask) (contract.DocGenResult, error) {
	bin := o.Bin
	if bin == "" {
		bin = os.Getenv("OPENCODE_BIN")
	}
	if bin == "" {
		bin = "opencode"
	}
	timeout := o.Timeout
	if timeout <= 0 {
		timeout = 10 * time.Minute
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	work, err := os.MkdirTemp("", "dth-opencode-")
	if err != nil {
		return contract.DocGenResult{}, err
	}
	defer os.RemoveAll(work)
	args := []string{"run", "--format", "json"}
	if o.Model != "" {
		args = append(args, "--model", o.Model)
	}
	if o.Agent != "" {
		args = append(args, "--agent", o.Agent)
	}
	args = append(args, Prompt(task))
	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Dir = work
	cmd.Env = os.Environ()
	if k := os.Getenv("DTH_ENGINE_API_KEY"); k != "" && os.Getenv("OPENCODE_API_KEY_ENV") != "" {
		// Lets an operator hand the Hub-stored key to opencode under the variable its provider expects.
		cmd.Env = append(cmd.Env, os.Getenv("OPENCODE_API_KEY_ENV")+"="+k)
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return contract.DocGenResult{}, fmt.Errorf("opencode timed out after %s", timeout)
		}
		return contract.DocGenResult{}, fmt.Errorf("opencode failed: %v: %s", err, tail(stderr.String(), 500))
	}
	ev := ParseEvents(stdout.Bytes())
	docs, err := ExtractDocs(ev.Text, task)
	if err != nil {
		return contract.DocGenResult{}, err
	}
	res := contract.DocGenResult{ContractVersion: task.ContractVersion, Status: "success", Docs: docs}
	if ev.HasUsage {
		res.Usage = &contract.ReportedUsage{InputTokens: ev.InputTokens, OutputTokens: ev.OutputTokens,
			Provider: "opencode/" + orStr(ev.Provider, "unknown"), Model: orStr(ev.Model, orStr(o.Model, "default"))}
	}
	return res, nil
}

// Prompt is the single instruction sent to opencode.
func Prompt(t contract.DocGenTask) string {
	var b strings.Builder
	b.WriteString(`You are a documentation generator. Do not use tools, do not read or write files, and do not run commands:
everything you need is below. Write reference documentation in Markdown for each requested code symbol.

Reply with exactly one JSON object and nothing else:
{"docs": [{"chunk_id": "<chunk id>", "symbol": "<symbol>", "path": "<target doc path>", "content": "<markdown>", "summary": "<one sentence>"}]}
Include one entry per requested chunk, copying chunk_id, symbol, and path exactly as listed.
`)
	if t.MaxOutputTokensPerChunk > 0 {
		fmt.Fprintf(&b, "Keep each content under about %d tokens.\n", t.MaxOutputTokensPerChunk)
	}
	if len(t.RepairErrors) > 0 {
		b.WriteString("\nYour previous reply was rejected for these reasons; fix them:\n")
		for _, e := range t.RepairErrors {
			b.WriteString("- " + e + "\n")
		}
	}
	fmt.Fprintf(&b, "\nRepository: %s at %s\n\nRequested chunks:\n", t.Repo, t.CommitSHA)
	for _, c := range t.ChunksToGenerate {
		fmt.Fprintf(&b, "- chunk_id=%s symbol=%s file=%s change=%s path=%s\n", c.ChunkID, c.Symbol, c.FilePath, c.ChangeType, c.TargetDocPath)
	}
	b.WriteString("\n" + t.Context)
	return b.String()
}

// Events is what was read from opencode's JSON event stream.
type Events struct {
	Text                      string
	InputTokens, OutputTokens int64
	HasUsage                  bool
	Provider, Model           string
}

// ParseEvents reads newline-delimited JSON events. Text parts are keyed by id so streamed updates of the
// same part count once (the last version wins); token counts are summed once per object id. Output that
// is not JSON events at all is taken as the reply text.
func ParseEvents(out []byte) Events {
	var ev Events
	texts := map[string]string{}
	var order []string
	usage := map[string][2]int64{}
	parsed := 0
	for i, line := range bytes.Split(out, []byte("\n")) {
		line = bytes.TrimSpace(line)
		if len(line) == 0 || line[0] != '{' {
			continue
		}
		var v any
		if json.Unmarshal(line, &v) != nil {
			continue
		}
		parsed++
		walk(v, func(m map[string]any) {
			id, _ := m["id"].(string)
			if t, _ := m["type"].(string); t == "text" {
				if s, ok := m["text"].(string); ok {
					key := id
					if key == "" {
						key = fmt.Sprintf("line-%d", i)
					}
					if _, seen := texts[key]; !seen {
						order = append(order, key)
					}
					texts[key] = s
				}
			}
			if tok, ok := m["tokens"].(map[string]any); ok {
				in, okIn := num(tok["input"])
				outT, okOut := num(tok["output"])
				if okIn || okOut {
					key := id
					if key == "" {
						key = fmt.Sprintf("line-%d", i)
					}
					usage[key] = [2]int64{in, outT}
				}
			}
			for _, k := range []string{"providerID", "provider_id"} {
				if s, ok := m[k].(string); ok && s != "" {
					ev.Provider = s
				}
			}
			for _, k := range []string{"modelID", "model_id"} {
				if s, ok := m[k].(string); ok && s != "" {
					ev.Model = s
				}
			}
		})
	}
	if parsed == 0 {
		ev.Text = string(out)
		return ev
	}
	var b strings.Builder
	for _, k := range order {
		b.WriteString(texts[k])
	}
	ev.Text = b.String()
	for _, u := range usage {
		ev.InputTokens += u[0]
		ev.OutputTokens += u[1]
		ev.HasUsage = true
	}
	return ev
}

func walk(v any, f func(map[string]any)) {
	switch x := v.(type) {
	case map[string]any:
		f(x)
		for _, c := range x {
			walk(c, f)
		}
	case []any:
		for _, c := range x {
			walk(c, f)
		}
	}
}

func num(v any) (int64, bool) {
	f, ok := v.(float64)
	return int64(f), ok
}

// ExtractDocs finds the JSON object in the reply and maps it to contract docs, filling a missing path or
// symbol from the task and dropping entries for chunks that were not requested.
func ExtractDocs(text string, t contract.DocGenTask) ([]contract.GeneratedDoc, error) {
	var out struct {
		Docs []contract.GeneratedDoc `json:"docs"`
	}
	found := false
	for i := strings.IndexByte(text, '{'); i >= 0 && i < len(text); {
		dec := json.NewDecoder(strings.NewReader(text[i:]))
		if err := dec.Decode(&out); err == nil && out.Docs != nil {
			found = true
			break
		}
		j := strings.IndexByte(text[i+1:], '{')
		if j < 0 {
			break
		}
		i += j + 1
	}
	if !found {
		return nil, fmt.Errorf("opencode reply contained no {\"docs\": [...]} object: %s", tail(text, 300))
	}
	want := map[string]contract.ChunkToGenerate{}
	for _, c := range t.ChunksToGenerate {
		want[c.ChunkID] = c
	}
	docs := make([]contract.GeneratedDoc, 0, len(out.Docs))
	for _, d := range out.Docs {
		c, ok := want[d.ChunkID]
		if !ok {
			continue
		}
		if d.Path == "" || !strings.HasPrefix(path.Clean(d.Path), strings.TrimSuffix(t.DocsPath, "/")) {
			d.Path = c.TargetDocPath
		}
		if d.Symbol == "" {
			d.Symbol = c.Symbol
		}
		docs = append(docs, d)
	}
	return docs, nil
}

func tail(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) > n {
		return "…" + s[len(s)-n:]
	}
	return s
}

func orStr(s, d string) string {
	if s != "" {
		return s
	}
	return d
}
