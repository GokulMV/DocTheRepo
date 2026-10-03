package security

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/GokulMV/DocTheRepo/internal/core/llmgateway"
	"github.com/GokulMV/DocTheRepo/internal/ports"
)

// Job types.
const (
	JobScan = ports.JobSecurityScan
	JobFix  = ports.JobSecurityFix
)

// ScanPayload starts a scan (the scan row exists already).
type ScanPayload struct {
	ScanID  string   `json:"scan_id"`
	RepoID  string   `json:"repo_id"`
	Modules []string `json:"modules"`
}

// FixPayload fixes selected findings of one repository in one pull request.
type FixPayload struct {
	RepoID     string   `json:"repo_id"`
	FindingIDs []string `json:"finding_ids"`
}

// Store persists scans and findings (store.Security).
type Store interface {
	Cache
	StartScan(ctx context.Context, scanID, commit string) error
	FinishScan(ctx context.Context, scanID, status, verdict string, summary any, errMsg string) error
	AddFindings(ctx context.Context, scanID, repoID string, fs []Finding) error
	Findings(ctx context.Context, repoID string, ids []string) ([]Finding, error)
	SetFix(ctx context.Context, id, status string, fix *Fix, prURL, errMsg string) error
}

// Jobs runs scan and fix jobs.
type Jobs struct {
	Scanner *Scanner
	Store   Store
	Repos   interface {
		Get(ctx context.Context, id string) (ports.RepoConfig, error)
	}
	Hosts func(ctx context.Context, connectorID string) (ports.CodeHost, error)
}

func (j *Jobs) target(ctx context.Context, repoID string) (Target, error) {
	repo, err := j.Repos.Get(ctx, repoID)
	if err != nil {
		return Target{}, err
	}
	host, err := j.Hosts(ctx, repo.ConnectorID)
	if err != nil {
		return Target{}, err
	}
	head, err := host.BranchHead(ctx, repo.FullName, repo.Branch())
	if err != nil {
		return Target{}, fmt.Errorf("read the head of %s: %w", repo.Branch(), err)
	}
	return Target{Repo: repo, Host: host, Commit: head}, nil
}

// HandleScan runs a scan job.
func (j *Jobs) HandleScan(ctx context.Context, job ports.Job) (ports.Outcome, error) {
	var pl ScanPayload
	if err := json.Unmarshal(job.Payload, &pl); err != nil || pl.ScanID == "" {
		return ports.Outcome{}, ports.Permanent(fmt.Errorf("security scan payload: %v", err))
	}
	fail := func(err error) (ports.Outcome, error) {
		var sb *ports.SpendBlockedError
		status := "failed"
		if errors.As(err, &sb) {
			status = "blocked"
		}
		_ = j.Store.FinishScan(context.WithoutCancel(ctx), pl.ScanID, status, "", nil, err.Error())
		if status == "blocked" {
			return ports.Outcome{Status: ports.JobSpendBlocked, Message: err.Error()}, nil
		}
		return ports.Outcome{}, ports.Permanent(err)
	}
	t, err := j.target(ctx, pl.RepoID)
	if err != nil {
		return fail(err)
	}
	if err := j.Store.StartScan(ctx, pl.ScanID, t.Commit); err != nil {
		return ports.Outcome{}, err
	}
	res, err := j.Scanner.Run(ctx, t, pl.Modules, llmgateway.CallMeta{RepoID: t.Repo.ID, JobID: job.ID})
	if err != nil {
		return fail(err)
	}
	if err := j.Store.AddFindings(ctx, pl.ScanID, t.Repo.ID, res.Findings); err != nil {
		return ports.Outcome{}, err
	}
	summary := map[string]any{"counts": res.Counts, "files_read": res.Plan.FilesRead, "files": res.Plan.Files, "cached": res.Plan.Cached,
		"tokens": res.Tokens, "notes": res.Notes, "skipped": res.Plan.Skipped}
	if err := j.Store.FinishScan(ctx, pl.ScanID, "done", res.Verdict, summary, ""); err != nil {
		return ports.Outcome{}, err
	}
	return ports.Outcome{Status: ports.JobDone, Message: fmt.Sprintf("%s: %d findings", res.Verdict, len(res.Findings)), Result: summary}, nil
}

// HandleFix proposes fixes for the selected findings and opens one pull request with them. It runs only
// because a person selected the findings and clicked Fix; it never merges.
func (j *Jobs) HandleFix(ctx context.Context, job ports.Job) (ports.Outcome, error) {
	var pl FixPayload
	if err := json.Unmarshal(job.Payload, &pl); err != nil || pl.RepoID == "" || len(pl.FindingIDs) == 0 {
		return ports.Outcome{}, ports.Permanent(fmt.Errorf("security fix payload: %v", err))
	}
	findings, err := j.Store.Findings(ctx, pl.RepoID, pl.FindingIDs)
	if err != nil {
		return ports.Outcome{}, err
	}
	t, err := j.target(ctx, pl.RepoID)
	if err != nil {
		for _, f := range findings {
			_ = j.Store.SetFix(ctx, f.ID, "failed", nil, "", err.Error())
		}
		return ports.Outcome{}, ports.Permanent(err)
	}
	meta := llmgateway.CallMeta{RepoID: t.Repo.ID, JobID: job.ID}
	t.Overrides = map[string]string{}
	changed := map[string]string{}
	var fixed []Finding
	var proposals []*Fix
	for _, f := range findings {
		fix, _, err := j.Scanner.ProposeFix(ctx, t, f, meta)
		var sb *ports.SpendBlockedError
		if errors.As(err, &sb) {
			_ = j.Store.SetFix(ctx, f.ID, "failed", nil, "", err.Error())
			return ports.Outcome{Status: ports.JobSpendBlocked, Message: err.Error()}, nil
		}
		if err != nil {
			_ = j.Store.SetFix(ctx, f.ID, "failed", nil, "", err.Error())
			continue
		}
		for _, ff := range append(append([]FixFile{}, fix.Files...), fix.Tests...) {
			changed[ff.Path] = ff.Content
			t.Overrides[ff.Path] = ff.Content
		}
		fx := fix
		_ = j.Store.SetFix(ctx, f.ID, "proposed", &fx, "", "")
		fixed = append(fixed, f)
		proposals = append(proposals, &fx)
	}
	if len(fixed) == 0 {
		return ports.Outcome{Status: ports.JobDone, Message: "no fix could be proposed (see each finding)"}, nil
	}
	pr, err := j.openPR(ctx, t, job.ID, fixed, proposals, changed)
	if err != nil {
		for _, f := range fixed {
			_ = j.Store.SetFix(ctx, f.ID, "failed", nil, "", "the fix was designed but the pull request could not be opened: "+err.Error())
		}
		return ports.Outcome{}, err
	}
	for i, f := range fixed {
		_ = j.Store.SetFix(ctx, f.ID, "pr_opened", proposals[i], pr.URL, "")
	}
	return ports.Outcome{Status: ports.JobDone, Message: "opened " + pr.URL, Result: map[string]any{"pr": pr.URL, "fixed": len(fixed)}}, nil
}

func (j *Jobs) openPR(ctx context.Context, t Target, jobID string, fixed []Finding, proposals []*Fix, changed map[string]string) (ports.PullRequest, error) {
	branch := "dth/security-fix-" + strings.ReplaceAll(jobID, "-", "")[:12]
	if err := t.Host.CreateBranch(ctx, t.Repo.FullName, branch, t.Commit); err != nil {
		return ports.PullRequest{}, fmt.Errorf("create branch %s: %w", branch, err)
	}
	bot, err := t.Host.BotIdentity(ctx)
	if err != nil {
		return ports.PullRequest{}, err
	}
	files := make([]ports.FileChange, 0, len(changed))
	for p, c := range changed {
		files = append(files, ports.FileChange{Path: p, Content: []byte(c)})
	}
	title := fmt.Sprintf("security: fix %d finding%s from the Hub's security scan", len(fixed), plural(len(fixed)))
	if _, err := t.Host.CommitFiles(ctx, t.Repo.FullName, ports.CommitRequest{Branch: branch, ParentSHA: t.Commit, Message: title + "\n\n" + prList(fixed),
		Files: files, Author: bot}); err != nil {
		return ports.PullRequest{}, fmt.Errorf("commit: %w", err)
	}
	var body strings.Builder
	fmt.Fprintf(&body, "Fixes proposed by DocTheRepo Hub for findings a person selected on the Security page. Each comes with a\n")
	fmt.Fprintf(&body, "vuln-closed test and a feature-intact test. **Nothing was merged automatically: review it, and let CI run the tests.**\n\n")
	for i, f := range fixed {
		fmt.Fprintf(&body, "### %s · %s: %s\n\n", f.Priority, f.Dimension, f.Title)
		fmt.Fprintf(&body, "- Where: `%s` lines %d-%d (%s)\n- Verified: %s, severity %s, exploitability %s\n", f.File, f.LineStart, f.LineEnd, f.Surface, f.Status, f.Severity, f.Exploitability)
		if p := proposals[i]; p != nil {
			var tests []string
			for _, tf := range p.Tests {
				tests = append(tests, "`"+tf.Path+"`")
			}
			fmt.Fprintf(&body, "- Tests: %s\n\n**Risk:** %s\n\n", strings.Join(tests, ", "), strings.TrimSpace(p.Risk))
		}
	}
	fmt.Fprintf(&body, "---\nScan method: [kryptonite](https://github.com/levitasOrg/kryptonite) attack modules, applied statically by the Hub.\n")
	return t.Host.OpenPR(ctx, t.Repo.FullName, ports.PRRequest{Head: branch, Base: t.Repo.Branch(), Title: title, Body: body.String()})
}

func prList(fs []Finding) string {
	var b strings.Builder
	for _, f := range fs {
		fmt.Fprintf(&b, "- %s %s: %s (%s:%d)\n", f.Priority, f.Dimension, f.Title, f.File, f.LineStart)
	}
	return b.String()
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}
