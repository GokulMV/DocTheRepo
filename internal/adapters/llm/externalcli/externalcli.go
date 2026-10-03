// Package externalcli runs any headless agent CLI as a documentation engine through the versioned DocGen
// contract: the Hub writes a task file, runs the configured command, and reads a result file. No tool is
// special-cased; anything that honours the contract works (plan § 7.9, § 8).
package externalcli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/GokulMV/DocTheRepo/internal/ports"
	"github.com/GokulMV/DocTheRepo/pkg/contract"
)

// Engine implements ports.DocGenerator.
type Engine struct {
	argv         []string
	timeout      time.Duration
	baseDir      string
	reportsUsage bool
	env          []string
}

// New builds an engine. Extra: command_template (required, must reference {task_file} and
// {result_file}), timeout (default 5m), work_dir (default <tmp>/dth), reports_usage (default true),
// env (comma-separated KEY=VALUE pairs added to the child environment). APIKey, when set, is passed as
// DTH_ENGINE_API_KEY so engines can use the operator's key without it appearing on the command line.
func New(cfg ports.ProviderConfig) (*Engine, error) {
	tmpl := cfg.Extra["command_template"]
	if !strings.Contains(tmpl, "{task_file}") || !strings.Contains(tmpl, "{result_file}") {
		return nil, errors.New("external_cli command_template must reference {task_file} and {result_file}")
	}
	argv, err := SplitArgs(tmpl)
	if err != nil {
		return nil, fmt.Errorf("command_template: %w", err)
	}
	e := &Engine{argv: argv, timeout: 5 * time.Minute, baseDir: filepath.Join(os.TempDir(), "dth"), reportsUsage: cfg.Extra["reports_usage"] != "false"}
	if v := cfg.Extra["timeout"]; v != "" {
		d, err := time.ParseDuration(v)
		if err != nil || d <= 0 {
			return nil, fmt.Errorf("timeout: invalid duration %q", v)
		}
		e.timeout = d
	}
	if v := cfg.Extra["work_dir"]; v != "" {
		e.baseDir = v
	}
	if v := cfg.Extra["env"]; v != "" {
		for _, kv := range strings.Split(v, ",") {
			if kv = strings.TrimSpace(kv); strings.Contains(kv, "=") {
				e.env = append(e.env, kv)
			}
		}
	}
	if cfg.APIKey != "" {
		e.env = append(e.env, "DTH_ENGINE_API_KEY="+cfg.APIKey)
	}
	return e, nil
}

// Kind returns "external_cli".
func (e *Engine) Kind() string { return "external_cli" }

// ReportsUsage reports the configured declaration.
func (e *Engine) ReportsUsage() bool { return e.reportsUsage }

// Generate runs one task. Errors: timeout → transient; non-zero exit → permanent (with the engine's
// status/error when it wrote a result); schema violation → *ports.SchemaError (triggers repair).
func (e *Engine) Generate(ctx context.Context, task contract.DocGenTask) (contract.DocGenResult, error) {
	return e.generateWith(ctx, task, contract.DocGenVersion)
}

func (e *Engine) generateWith(ctx context.Context, task contract.DocGenTask, version string) (contract.DocGenResult, error) {
	var res contract.DocGenResult
	if task.JobID == "" || strings.ContainsAny(task.JobID, `/\`) || strings.Contains(task.JobID, "..") {
		return res, ports.Permanent(fmt.Errorf("invalid job id %q", task.JobID))
	}
	dir := filepath.Join(e.baseDir, task.JobID)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return res, ports.Transient(fmt.Errorf("create work dir: %w", err))
	}
	defer os.RemoveAll(dir) // task/result files live only for the job (plan Appendix D)
	taskPath, resultPath := filepath.Join(dir, "task.json"), filepath.Join(dir, "result.json")
	task.ContractVersion = version
	task.OutputPath = resultPath
	raw, err := json.MarshalIndent(task, "", "  ")
	if err != nil {
		return res, ports.Permanent(fmt.Errorf("encode task: %w", err))
	}
	if err := os.WriteFile(taskPath, raw, 0o600); err != nil {
		return res, ports.Transient(fmt.Errorf("write task file: %w", err))
	}
	out, runErr := e.run(ctx, taskPath, resultPath, dir, task.Model)
	body, readErr := os.ReadFile(resultPath)
	if runErr != nil {
		if errors.Is(runErr, context.DeadlineExceeded) {
			return res, ports.Transient(fmt.Errorf("engine timed out after %s", e.timeout))
		}
		if ctx.Err() != nil {
			return res, ctx.Err()
		}
		msg := fmt.Sprintf("engine exited with error: %v; stderr: %s", runErr, tail(out, 1024))
		if readErr == nil && json.Unmarshal(body, &res) == nil && res.Error != nil {
			msg = fmt.Sprintf("engine reported %s: %s", res.Status, *res.Error)
		}
		return res, ports.Permanent(errors.New(msg))
	}
	if readErr != nil {
		return res, &ports.SchemaError{Problems: []string{"$: engine exited 0 but wrote no result file at output_path"}}
	}
	if probs := contract.Validate(contract.DocGenResultSchema, body); len(probs) > 0 {
		return res, &ports.SchemaError{Problems: probs}
	}
	if err := json.Unmarshal(body, &res); err != nil {
		return res, &ports.SchemaError{Problems: []string{"$: " + err.Error()}}
	}
	if res.Status != "success" {
		msg := "engine reported status error"
		if res.Error != nil {
			msg = *res.Error
		}
		return res, ports.Permanent(errors.New(msg))
	}
	return res, nil
}

func (e *Engine) run(ctx context.Context, taskPath, resultPath, dir, model string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, e.timeout)
	defer cancel()
	args := make([]string, len(e.argv))
	for i, a := range e.argv {
		a = strings.ReplaceAll(a, "{task_file}", taskPath)
		a = strings.ReplaceAll(a, "{result_file}", resultPath)
		a = strings.ReplaceAll(a, "{model}", model)
		args[i] = strings.ReplaceAll(a, "{work_dir}", dir)
	}
	cmd := exec.CommandContext(ctx, args[0], args[1:]...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), e.env...)
	cmd.WaitDelay = 5 * time.Second
	var stderr bytes.Buffer
	cmd.Stderr = &limited{w: &stderr, n: 64 << 10}
	cmd.Stdout = &limited{w: &bytes.Buffer{}, n: 64 << 10}
	err := cmd.Run()
	if ctx.Err() == context.DeadlineExceeded {
		return stderr.Bytes(), context.DeadlineExceeded
	}
	return stderr.Bytes(), err
}

type limited struct {
	w *bytes.Buffer
	n int
}

func (l *limited) Write(p []byte) (int, error) {
	if room := l.n - l.w.Len(); room > 0 {
		if len(p) > room {
			l.w.Write(p[:room])
		} else {
			l.w.Write(p)
		}
	}
	return len(p), nil
}

func tail(b []byte, n int) string {
	s := strings.TrimSpace(string(b))
	if len(s) > n {
		s = "…" + s[len(s)-n:]
	}
	return s
}

// SplitArgs splits a command template into argv with shell-like quoting ('…', "…", backslash escapes) but
// never invokes a shell, so values substituted into arguments cannot inject commands.
func SplitArgs(s string) ([]string, error) {
	var args []string
	var cur strings.Builder
	inArg := false
	var quote rune
	escaped := false
	for _, r := range s {
		switch {
		case escaped:
			cur.WriteRune(r)
			escaped = false
		case r == '\\' && quote != '\'':
			escaped, inArg = true, true
		case quote != 0:
			if r == quote {
				quote = 0
			} else {
				cur.WriteRune(r)
			}
		case r == '\'' || r == '"':
			quote, inArg = r, true
		case r == ' ' || r == '\t' || r == '\n':
			if inArg {
				args = append(args, cur.String())
				cur.Reset()
				inArg = false
			}
		default:
			cur.WriteRune(r)
			inArg = true
		}
	}
	if quote != 0 || escaped {
		return nil, errors.New("unterminated quote or escape")
	}
	if inArg {
		args = append(args, cur.String())
	}
	if len(args) == 0 {
		return nil, errors.New("empty command")
	}
	return args, nil
}
