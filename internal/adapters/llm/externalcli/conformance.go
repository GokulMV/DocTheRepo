package externalcli

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/GokulMV/DocTheRepo/internal/ports"
	"github.com/GokulMV/DocTheRepo/pkg/contract"
)

// Check is one conformance check result.
type Check struct {
	Name   string `json:"name"`
	Passed bool   `json:"passed"`
	// Warning marks a check that passed with a caveat (e.g. usage not reported → unguarded spend).
	Warning string `json:"warning,omitempty"`
	Detail  string `json:"detail,omitempty"`
}

// Report is the outcome of `dth adapter-test`.
type Report struct {
	Command string  `json:"command"`
	Checks  []Check `json:"checks"`
	Passed  bool    `json:"passed"`
	// Unguarded is true when the engine does not report usage: the spend guard can only estimate it,
	// and running it requires spend.allow_unreported_usage.
	Unguarded bool `json:"unguarded"`
}

// FixtureTask is the fixed conformance task: two small Go symbols to document.
func FixtureTask(jobID string) contract.DocGenTask {
	return contract.DocGenTask{
		JobID: jobID, Repo: "dth/conformance", CommitSHA: "0000000000000000000000000000000000000000", DocsPath: "docs/generated/",
		ChunksToGenerate: []contract.ChunkToGenerate{
			{ChunkID: "0123456789abcdef", Symbol: "Add", FilePath: "calc/calc.go", ChangeType: "added", TargetDocPath: "docs/generated/calc/calc.go.md"},
			{ChunkID: "fedcba9876543210", Symbol: "Sub", FilePath: "calc/calc.go", ChangeType: "changed", TargetDocPath: "docs/generated/calc/calc.go.md"},
		},
		Context: "## Changed code\n\n### calc/calc.go — Add (chunk 0123456789abcdef)\n```go\n// Add returns a+b.\nfunc Add(a, b int) int { return a + b }\n```\n\n" +
			"### calc/calc.go — Sub (chunk fedcba9876543210)\n```go\nfunc Sub(a, b int) int { return a - b }\n```\n",
		MaxOutputTokensPerChunk: 400,
	}
}

// Conformance runs the fixture task and an induced failure against an engine configuration and reports
// whether it honours the contract: exit 0 with a schema-valid result covering every requested chunk,
// docs only under the docs path, usage reported, and a non-zero exit for an unsupported contract major.
func Conformance(ctx context.Context, cfg ports.ProviderConfig) Report {
	rep := Report{Command: cfg.Extra["command_template"], Passed: true}
	add := func(c Check) {
		rep.Checks = append(rep.Checks, c)
		if !c.Passed {
			rep.Passed = false
		}
	}
	eng, err := New(cfg)
	if err != nil {
		add(Check{Name: "configuration", Detail: err.Error()})
		return rep
	}
	add(Check{Name: "configuration", Passed: true})

	res, err := eng.Generate(ctx, FixtureTask("conformance-ok"))
	var se *ports.SchemaError
	switch {
	case errors.As(err, &se):
		add(Check{Name: "result matches schema", Detail: strings.Join(se.Problems, "; ")})
		return rep
	case err != nil:
		add(Check{Name: "exits 0 and writes a result", Detail: err.Error()})
		return rep
	}
	add(Check{Name: "exits 0 and writes a result", Passed: true})
	add(Check{Name: "result matches schema", Passed: true})

	covered := map[string]bool{}
	outside := []string{}
	for _, d := range res.Docs {
		covered[d.ChunkID] = true
		if !strings.HasPrefix(d.Path, "docs/generated/") {
			outside = append(outside, d.Path)
		}
	}
	var missing []string
	for _, c := range FixtureTask("").ChunksToGenerate {
		if !covered[c.ChunkID] {
			missing = append(missing, c.ChunkID)
		}
	}
	add(Check{Name: "documents every requested chunk", Passed: len(missing) == 0, Detail: joinOr(missing, "missing: ")})
	add(Check{Name: "writes only under the docs path", Passed: len(outside) == 0, Detail: joinOr(outside, "outside: ")})
	if res.Usage == nil {
		rep.Unguarded = true
		add(Check{Name: "reports token usage", Passed: true,
			Warning: "no usage reported: spend is estimated, not measured; running requires spend.allow_unreported_usage: true"})
	} else {
		add(Check{Name: "reports token usage", Passed: true, Detail: fmt.Sprintf("%d in / %d out (%s %s)", res.Usage.InputTokens, res.Usage.OutputTokens, res.Usage.Provider, res.Usage.Model)})
	}

	bad := FixtureTask("conformance-fail")
	_, err = eng.generateVersion(ctx, bad, "99.0")
	add(Check{Name: "exits non-zero for an unsupported contract major version", Passed: err != nil && !errors.As(err, &se),
		Detail: fmt.Sprint(err)})
	return rep
}

// generateVersion runs a task declaring a specific contract version (used to induce failure).
func (e *Engine) generateVersion(ctx context.Context, task contract.DocGenTask, version string) (contract.DocGenResult, error) {
	return e.generateWith(ctx, task, version)
}

func joinOr(xs []string, prefix string) string {
	if len(xs) == 0 {
		return ""
	}
	return prefix + strings.Join(xs, ", ")
}
