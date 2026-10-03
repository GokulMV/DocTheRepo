package security

import (
	"context"
	"errors"
	"fmt"
	"path"
	"strings"

	"github.com/GokulMV/DocTheRepo/internal/core/llmgateway"
	"github.com/GokulMV/DocTheRepo/internal/ports"
	"github.com/GokulMV/DocTheRepo/pkg/contract"
)

// MaxFixFileBytes caps a file the fixer rewrites (it returns whole files).
const MaxFixFileBytes = 60 << 10

// Fix is a proposed fix: whole new files, two tests, and the risk note.
type Fix struct {
	Files []FixFile `json:"files"`
	Tests []FixFile `json:"tests"`
	Risk  string    `json:"risk"`
}

// FixFile is one file with its complete new content.
type FixFile struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

var fixSchema = contract.Schema{
	"type": "object", "additionalProperties": false, "required": []any{"files", "tests", "risk"},
	"properties": contract.Schema{
		"files": fileList, "tests": fileList, "risk": contract.Schema{"type": "string"},
	},
}

var fileList = contract.Schema{"type": "array", "items": contract.Schema{
	"type": "object", "additionalProperties": false, "required": []any{"path", "content"},
	"properties": contract.Schema{"path": contract.Schema{"type": "string"}, "content": contract.Schema{"type": "string"}},
}}

// forbidden are paths a fix never writes: CI, deployment, lockfiles, and anything outside the repository.
func forbidden(p string) string {
	clean := path.Clean(p)
	lp := strings.ToLower(clean)
	switch {
	case p == "" || strings.HasPrefix(clean, "/") || strings.HasPrefix(clean, "..") || strings.Contains(clean, "/../"):
		return "is outside the repository"
	case strings.HasPrefix(lp, ".github/") || strings.HasPrefix(lp, ".gitlab") || strings.HasPrefix(lp, ".circleci/"):
		return "is CI configuration"
	case strings.HasSuffix(lp, ".lock") || strings.HasSuffix(lp, "-lock.json") || strings.HasSuffix(lp, "go.sum") || strings.HasSuffix(lp, "lock.yaml"):
		return "is a lockfile"
	}
	return ""
}

// ErrNoFix is returned when the fixer declines (its risk note says why).
var ErrNoFix = errors.New("no safe fix proposed")

// ProposeFix asks the fixer for one finding: the cited file, nearby test files for conventions.
func (s *Scanner) ProposeFix(ctx context.Context, t Target, f Finding, meta llmgateway.CallMeta) (Fix, int64, error) {
	var body []byte
	if o, ok := t.Overrides[f.File]; ok {
		body = []byte(o)
	} else {
		b, err := t.Host.GetFile(ctx, t.Repo.FullName, f.File, t.Commit)
		if err != nil {
			return Fix{}, 0, fmt.Errorf("read %s: %w", f.File, err)
		}
		body = b
	}
	if len(body) > MaxFixFileBytes {
		return Fix{}, 0, fmt.Errorf("%s is larger than %d KB, too large to rewrite safely; fix it by hand", f.File, MaxFixFileBytes>>10)
	}
	var ctxFiles strings.Builder
	ctxFiles.WriteString(numbered(f.File, string(body)))
	shown := map[string]bool{f.File: true}
	// Test conventions: a test file next to the source, if the repository has one.
	tree, _ := t.Host.ListTree(ctx, t.Repo.FullName, t.Commit)
	for _, cand := range testNeighbours(f.File, tree) {
		if b, err := t.Host.GetFile(ctx, t.Repo.FullName, cand, t.Commit); err == nil && len(b) <= MaxFixFileBytes {
			ctxFiles.WriteString(numbered(cand, string(b)))
			shown[cand] = true
			break
		}
	}
	prompt := fmt.Sprintf("Repository: %s at %s\n\nFinding (verified %s, priority %s):\n- dimension: %s\n- title: %s\n- surface: %s\n- file: %s lines %d-%d\n- repro:\n  - %s\n- evidence: %s\n\nFiles:\n<data>\n%s</data>",
		t.Repo.FullName, short(t.Commit), f.Status, f.Priority, f.Dimension, f.Title, f.Surface, f.File, f.LineStart, f.LineEnd,
		strings.Join(f.Repro, "\n  - "), f.Evidence, ctxFiles.String())
	var out Fix
	check := func() []string {
		var problems []string
		for _, ff := range append(append([]FixFile{}, out.Files...), out.Tests...) {
			if why := forbidden(ff.Path); why != "" {
				problems = append(problems, fmt.Sprintf("%s %s and must not be changed", ff.Path, why))
			}
			if strings.TrimSpace(ff.Content) == "" {
				problems = append(problems, ff.Path+" has empty content: return the complete file")
			}
		}
		if len(out.Files) > 0 && len(out.Tests) == 0 {
			problems = append(problems, "add the vuln-closed and feature-intact tests")
		}
		return problems
	}
	r, err := s.GW.ChatJSONResult(ctx, s.feature(ctx), meta, ports.ChatRequest{System: fixerSystem, MaxOutputTokens: 16000,
		Messages: []ports.ChatMessage{{Role: "user", Content: prompt}}}, fixSchema, &out, check)
	used := r.Usage.Total()
	if err != nil {
		return Fix{}, used, err
	}
	if len(out.Files) == 0 {
		return out, used, fmt.Errorf("%w: %s", ErrNoFix, strings.TrimSpace(out.Risk))
	}
	return out, used, nil
}

// testNeighbours lists likely test files for a source file, nearest first.
func testNeighbours(file string, tree []string) []string {
	dir, base := path.Dir(file), path.Base(file)
	ext := path.Ext(base)
	stem := strings.TrimSuffix(base, ext)
	want := []string{path.Join(dir, stem+"_test"+ext), path.Join(dir, stem+".test"+ext), path.Join(dir, stem+".spec"+ext),
		path.Join(dir, "test_"+base), path.Join(dir, "tests", "test_"+base)}
	have := map[string]bool{}
	for _, p := range tree {
		have[p] = true
	}
	var out []string
	for _, w := range want {
		if have[w] {
			out = append(out, w)
		}
	}
	for _, p := range tree { // any test file in the same directory shows the conventions
		if path.Dir(p) == dir && (strings.Contains(p, "_test.") || strings.Contains(p, ".test.") || strings.Contains(p, ".spec.") || strings.HasPrefix(path.Base(p), "test_")) && !contains(out, p) {
			out = append(out, p)
		}
	}
	return out
}
