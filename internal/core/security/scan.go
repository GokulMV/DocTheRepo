package security

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/GokulMV/DocTheRepo/internal/core/llmgateway"
	"github.com/GokulMV/DocTheRepo/internal/core/spendguard"
	"github.com/GokulMV/DocTheRepo/internal/ports"
	"github.com/GokulMV/DocTheRepo/pkg/contract"
)

// Finding statuses after verification (kryptonite's candidate status never reaches storage).
const (
	StatusConfirmed = "confirmed"
	StatusPlausible = "plausible"
	StatusRejected  = "rejected"
)

// FeatureSecurity is the model route scans use; without one they use the Ask (qa) route.
const FeatureSecurity = llmgateway.FeatureSecurity

// Finding is kryptonite's Finding (references/finding-schema.md) plus where in the code it is and, once
// someone asks for it, the proposed fix.
type Finding struct {
	ID             string   `json:"id"`
	Dimension      string   `json:"dimension"`
	Title          string   `json:"title"`
	Surface        string   `json:"surface"`
	File           string   `json:"file"`
	LineStart      int      `json:"line_start"`
	LineEnd        int      `json:"line_end"`
	Repro          []string `json:"repro"`
	Evidence       string   `json:"evidence"`
	Severity       string   `json:"severity"`
	Exploitability string   `json:"exploitability"`
	Priority       string   `json:"priority"`
	Status         string   `json:"status"`
	FixStatus      string   `json:"fix_status"`
	Fix            *Fix     `json:"fix,omitempty"`
	FixPRURL       string   `json:"fix_pr_url,omitempty"`
	FixError       string   `json:"fix_error,omitempty"`
}

// Plan is what a scan will read and roughly cost, shown before it runs (kryptonite's phase 2 gate).
type Plan struct {
	CommitSHA       string              `json:"commit_sha"`
	Modules         []string            `json:"modules"`
	Files           map[string][]string `json:"files"` // module → files it attacks
	FilesRead       int                 `json:"files_read"`
	EstimatedTokens int64               `json:"estimated_tokens"`
	Cached          []string            `json:"cached,omitempty"` // modules whose inputs are unchanged since a scan
	Skipped         []string            `json:"skipped,omitempty"`
}

// Result reports a finished scan.
type Result struct {
	Plan     Plan           `json:"plan"`
	Findings []Finding      `json:"-"`
	Counts   map[string]int `json:"counts"` // P0..P3 among confirmed+plausible, and rejected
	Verdict  string         `json:"verdict"`
	Tokens   int64          `json:"tokens"`
	Notes    []string       `json:"notes,omitempty"`
}

// Cache keeps a module's verified findings per exact inputs.
type Cache interface {
	ModuleFindings(ctx context.Context, repoID, module, inputsHash string) ([]Finding, bool, error)
	PutModuleFindings(ctx context.Context, repoID, module, inputsHash string, fs []Finding) error
}

// Scanner runs scans.
type Scanner struct {
	GW    *llmgateway.Gateway
	Cache Cache
	// Progress reports module progress (optional).
	Progress func(done, total int, module string)
}

// Target is the repository and commit a scan reads.
type Target struct {
	Repo   ports.RepoConfig
	Host   ports.CodeHost
	Commit string
	// Overrides are file contents already changed by an earlier fix in the same pull request.
	Overrides map[string]string
}

// read loads the candidate files once for every module.
func read(ctx context.Context, t Target, modules []string) (map[string]string, []string, error) {
	tree, err := t.Host.ListTree(ctx, t.Repo.FullName, t.Commit)
	if err != nil {
		return nil, nil, err
	}
	files := map[string]string{}
	var skipped []string
	for _, p := range PathCandidates(tree, modules) {
		b, err := t.Host.GetFile(ctx, t.Repo.FullName, p, t.Commit)
		if errors.Is(err, ports.ErrNotFound) {
			continue
		}
		if err != nil {
			return nil, nil, fmt.Errorf("read %s: %w", p, err)
		}
		s := string(b)
		if strings.ContainsRune(s, 0) {
			continue // binary
		}
		if len(s) > MaxFileBytes {
			s = s[:MaxFileBytes]
			skipped = append(skipped, p+" (read the first 40 KB)")
		}
		files[p] = s
	}
	return files, skipped, nil
}

// PlanScan reads the files each module would attack and estimates the cost (no model call).
func (s *Scanner) PlanScan(ctx context.Context, t Target, modules []string) (Plan, map[string]string, error) {
	files, skipped, err := read(ctx, t, modules)
	if err != nil {
		return Plan{}, nil, err
	}
	p := Plan{CommitSHA: t.Commit, Modules: modules, Files: map[string][]string{}, FilesRead: len(files), Skipped: skipped}
	for _, m := range modules {
		sel := ModuleFiles(m, files)
		p.Files[m] = sel
		if s.Cache != nil {
			if _, ok, _ := s.Cache.ModuleFindings(ctx, t.Repo.ID, m, inputsHash(m, sel, files)); ok {
				p.Cached = append(p.Cached, m)
				continue
			}
		}
		var n int
		for _, f := range sel {
			n += len(files[f])
		}
		// Attacker reads the code once; the verifier rereads excerpts (about half); plus prompts and output.
		p.EstimatedTokens += spendguard.EstimateTokens(strings.Repeat("x", n))*3/2 + 6000
	}
	return p, files, nil
}

// inputsHash identifies a module run: the playbook, the prompts, and every file's content.
func inputsHash(module string, sel []string, files map[string]string) string {
	h := sha256.New()
	fmt.Fprintf(h, "%s\x00%s\x00%s\x00", module, promptVersion, Reference("severity"))
	for _, f := range sel {
		fmt.Fprintf(h, "%s\x00%s\x00", f, files[f])
	}
	return hex.EncodeToString(h.Sum(nil))
}

// feature is the route scans use: security when routed, else qa.
func (s *Scanner) feature(ctx context.Context) string {
	if _, err := s.GW.Route(ctx, FeatureSecurity); err == nil {
		return FeatureSecurity
	}
	return llmgateway.FeatureQA
}

// Run scans: per module, attack then verify, reusing cached results for unchanged inputs.
func (s *Scanner) Run(ctx context.Context, t Target, modules []string, meta llmgateway.CallMeta) (Result, error) {
	plan, files, err := s.PlanScan(ctx, t, modules)
	if err != nil {
		return Result{}, err
	}
	res := Result{Plan: plan, Counts: map[string]int{}}
	feature := s.feature(ctx)
	for i, m := range modules {
		if s.Progress != nil {
			s.Progress(i, len(modules), m)
		}
		mod, ok := ModuleByName(m)
		if !ok || !mod.Static {
			res.Notes = append(res.Notes, fmt.Sprintf("%s skipped: %s", m, mod.Reason))
			continue
		}
		sel := plan.Files[m]
		if len(sel) == 0 {
			res.Notes = append(res.Notes, m+": no files to read in this repository")
			continue
		}
		key := inputsHash(m, sel, files)
		if s.Cache != nil {
			if cached, ok, err := s.Cache.ModuleFindings(ctx, t.Repo.ID, m, key); err == nil && ok {
				res.Findings = append(res.Findings, cached...)
				continue
			}
		}
		cands, used, err := s.attack(ctx, feature, meta, t, mod, sel, files)
		res.Tokens += used
		if err != nil {
			return res, fmt.Errorf("%s: %w", m, err)
		}
		verified, used, err := s.verify(ctx, feature, meta, mod, cands, files)
		res.Tokens += used
		if err != nil {
			return res, fmt.Errorf("%s verify: %w", m, err)
		}
		if s.Cache != nil {
			_ = s.Cache.PutModuleFindings(ctx, t.Repo.ID, m, key, verified)
		}
		res.Findings = append(res.Findings, verified...)
	}
	sort.SliceStable(res.Findings, func(i, j int) bool {
		if res.Findings[i].Priority != res.Findings[j].Priority {
			return res.Findings[i].Priority < res.Findings[j].Priority
		}
		return res.Findings[i].File < res.Findings[j].File
	})
	for _, f := range res.Findings {
		if f.Status == StatusRejected {
			res.Counts["rejected"]++
		} else {
			res.Counts[f.Priority]++
		}
	}
	res.Verdict = Verdict(res.Findings)
	return res, nil
}

// numbered renders a file with line numbers, as the model must cite them.
func numbered(path, body string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "<file path=%q>\n", path)
	for i, line := range strings.Split(body, "\n") {
		fmt.Fprintf(&b, "%5d  %s\n", i+1, line)
	}
	b.WriteString("</file>\n")
	return b.String()
}

// excerpt returns lines around a finding (with numbers), for the verifier.
func excerpt(path, body string, from, to int) string {
	lines := strings.Split(body, "\n")
	if from < 1 {
		from = 1
	}
	if to < from {
		to = from
	}
	lo, hi := max(from-30, 1), min(to+30, len(lines))
	var b strings.Builder
	fmt.Fprintf(&b, "<excerpt path=%q lines=\"%d-%d\">\n", path, lo, hi)
	for i := lo; i <= hi; i++ {
		fmt.Fprintf(&b, "%5d  %s\n", i, lines[i-1])
	}
	b.WriteString("</excerpt>\n")
	return b.String()
}

type attackOut struct {
	Findings []struct {
		Title          string   `json:"title"`
		Surface        string   `json:"surface"`
		File           string   `json:"file"`
		LineStart      int      `json:"line_start"`
		LineEnd        int      `json:"line_end"`
		Repro          []string `json:"repro"`
		Evidence       string   `json:"evidence"`
		Severity       string   `json:"severity"`
		Exploitability string   `json:"exploitability"`
	} `json:"findings"`
}

var attackSchema = contract.Schema{
	"type": "object", "additionalProperties": false, "required": []any{"findings"},
	"properties": contract.Schema{"findings": contract.Schema{"type": "array", "items": contract.Schema{
		"type": "object", "additionalProperties": false,
		"required": []any{"title", "surface", "file", "line_start", "line_end", "repro", "evidence", "severity", "exploitability"},
		"properties": contract.Schema{
			"title": contract.Schema{"type": "string"}, "surface": contract.Schema{"type": "string"}, "file": contract.Schema{"type": "string"},
			"line_start": contract.Schema{"type": "integer"}, "line_end": contract.Schema{"type": "integer"},
			"repro":          contract.Schema{"type": "array", "items": contract.Schema{"type": "string"}},
			"evidence":       contract.Schema{"type": "string"},
			"severity":       contract.Schema{"type": "string", "enum": []any{"critical", "high", "medium", "low", "info"}},
			"exploitability": contract.Schema{"type": "string", "enum": []any{"trivial", "easy", "moderate", "hard"}},
		}}}},
}

func (s *Scanner) attack(ctx context.Context, feature string, meta llmgateway.CallMeta, t Target, mod Module, sel []string, files map[string]string) ([]Finding, int64, error) {
	var code strings.Builder
	for _, f := range sel {
		code.WriteString(numbered(f, files[f]))
	}
	prompt := fmt.Sprintf("Repository: %s at %s\nModule: %s\n\nThe code to attack (line numbers on the left):\n<data>\n%s</data>",
		t.Repo.FullName, short(t.Commit), mod.Name, code.String())
	var out attackOut
	check := func() []string {
		var problems []string
		for i, f := range out.Findings {
			if _, ok := files[f.File]; !ok || !contains(sel, f.File) {
				problems = append(problems, fmt.Sprintf("finding %d cites %q, which is not one of the files shown", i, f.File))
			}
		}
		return problems
	}
	r, err := s.GW.ChatJSONResult(ctx, feature, meta, ports.ChatRequest{System: attackerSystem(mod), MaxOutputTokens: 6000,
		Messages: []ports.ChatMessage{{Role: "user", Content: prompt}}}, attackSchema, &out, check)
	used := r.Usage.Total()
	if err != nil {
		return nil, used, err
	}
	var fs []Finding
	for _, f := range out.Findings {
		fs = append(fs, Finding{Dimension: mod.Name, Title: clip(f.Title, 140), Surface: clip(f.Surface, 300), File: f.File,
			LineStart: f.LineStart, LineEnd: max(f.LineEnd, f.LineStart), Repro: f.Repro, Evidence: clip(f.Evidence, 4000),
			Severity: f.Severity, Exploitability: f.Exploitability})
	}
	return fs, used, nil
}

type verifyOut struct {
	Verdicts []struct {
		Index          int    `json:"index"`
		Status         string `json:"status"`
		Severity       string `json:"severity"`
		Exploitability string `json:"exploitability"`
		Evidence       string `json:"evidence"`
		DuplicateOf    int    `json:"duplicate_of"`
	} `json:"verdicts"`
}

var verifySchema = contract.Schema{
	"type": "object", "additionalProperties": false, "required": []any{"verdicts"},
	"properties": contract.Schema{"verdicts": contract.Schema{"type": "array", "items": contract.Schema{
		"type": "object", "additionalProperties": false,
		"required": []any{"index", "status", "severity", "exploitability", "evidence", "duplicate_of"},
		"properties": contract.Schema{
			"index":          contract.Schema{"type": "integer"},
			"status":         contract.Schema{"type": "string", "enum": []any{"confirmed", "plausible", "rejected"}},
			"severity":       contract.Schema{"type": "string", "enum": []any{"critical", "high", "medium", "low", "info"}},
			"exploitability": contract.Schema{"type": "string", "enum": []any{"trivial", "easy", "moderate", "hard"}},
			"evidence":       contract.Schema{"type": "string"},
			"duplicate_of":   contract.Schema{"type": "integer"},
		}}}},
}

func (s *Scanner) verify(ctx context.Context, feature string, meta llmgateway.CallMeta, mod Module, cands []Finding, files map[string]string) ([]Finding, int64, error) {
	if len(cands) == 0 {
		return nil, 0, nil
	}
	var b strings.Builder
	for i, c := range cands {
		fmt.Fprintf(&b, "<candidate index=\"%d\">\ntitle: %s\nsurface: %s\nfile: %s lines %d-%d\nseverity: %s, exploitability: %s\nrepro:\n- %s\nevidence: %s\n%s</candidate>\n",
			i, c.Title, c.Surface, c.File, c.LineStart, c.LineEnd, c.Severity, c.Exploitability, strings.Join(c.Repro, "\n- "), c.Evidence,
			excerpt(c.File, files[c.File], c.LineStart, c.LineEnd))
	}
	var out verifyOut
	check := func() []string {
		seen := map[int]bool{}
		for _, v := range out.Verdicts {
			seen[v.Index] = true
		}
		var problems []string
		for i := range cands {
			if !seen[i] {
				problems = append(problems, fmt.Sprintf("no verdict for candidate %d", i))
			}
		}
		return problems
	}
	r, err := s.GW.ChatJSONResult(ctx, feature, meta, ports.ChatRequest{System: verifierSystem(mod), MaxOutputTokens: 400 + 300*len(cands),
		Messages: []ports.ChatMessage{{Role: "user", Content: "Candidate findings with the code they cite:\n<data>\n" + b.String() + "</data>"}}},
		verifySchema, &out, check)
	used := r.Usage.Total()
	if err != nil {
		return nil, used, err
	}
	var fs []Finding
	for _, v := range out.Verdicts {
		if v.Index < 0 || v.Index >= len(cands) || (v.DuplicateOf >= 0 && v.DuplicateOf != v.Index && v.DuplicateOf < len(cands)) {
			continue // out of range, or merged into another candidate
		}
		f := cands[v.Index]
		f.Status, f.Severity, f.Exploitability = v.Status, v.Severity, v.Exploitability
		if e := strings.TrimSpace(v.Evidence); e != "" {
			f.Evidence = clip(f.Evidence+"\n\nVerifier: "+e, 6000)
		}
		f.Priority = Priority(f.Severity, f.Exploitability)
		fs = append(fs, f)
	}
	return fs, used, nil
}

func contains(xs []string, x string) bool {
	for _, v := range xs {
		if v == x {
			return true
		}
	}
	return false
}

func clip(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

func short(sha string) string {
	if len(sha) > 12 {
		return sha[:12]
	}
	return sha
}
