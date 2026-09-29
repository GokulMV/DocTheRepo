// Package prautomerge lands docs through a pull/merge request scoped to the generated-docs path, which the
// lifecycle sweep merges once required checks pass. It is the default push mode (plan § 8.17) and also
// provides PR opening and superseding for the approver mode.
package prautomerge

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/GokulMV/DocTheRepo/internal/ports"
)

// BranchPrefix names docs branches; the job ID makes a retried job reuse its own branch and PR.
const BranchPrefix = "dth/docs-"

// Branch returns the docs branch for a job.
func Branch(jobID string) string { return BranchPrefix + jobID }

// Opener opens docs PRs and records them.
type Opener struct {
	PRs ports.PRStore
}

// Open creates the job's docs branch from the tracked branch, commits the files, opens the PR, records it,
// and supersedes older docs PRs whose chunks the new one fully covers. A retried job (branch already
// there) returns its existing PR instead of opening a second one.
func (o *Opener) Open(ctx context.Context, host ports.CodeHost, l ports.DocsLanding, mode ports.PushMode, reviewers []string, state string) (ports.PullRequest, []int, error) {
	branch := Branch(l.JobID)
	if existing, ok, err := o.existing(ctx, host, l, branch); err != nil || ok {
		return existing, nil, err
	}
	base, err := host.BranchHead(ctx, l.Repo, l.Branch)
	if err != nil {
		return ports.PullRequest{}, nil, fmt.Errorf("read %s head: %w", l.Branch, err)
	}
	if err := host.CreateBranch(ctx, l.Repo, branch, base); err != nil && !errors.Is(err, ports.ErrConflict) {
		return ports.PullRequest{}, nil, fmt.Errorf("create docs branch: %w", err)
	}
	if _, err := host.CommitFiles(ctx, l.Repo, ports.CommitRequest{Branch: branch, Message: l.Message, Files: l.Files, Author: l.Author}); err != nil {
		return ports.PullRequest{}, nil, fmt.Errorf("commit docs: %w", err)
	}
	pr, err := host.OpenPR(ctx, l.Repo, ports.PRRequest{Head: branch, Base: l.Branch, Title: l.Title, Body: l.Body, Reviewers: reviewers})
	if err != nil {
		return ports.PullRequest{}, nil, fmt.Errorf("open docs PR: %w", err)
	}
	approver := ""
	if len(reviewers) > 0 {
		approver = strings.Join(reviewers, ",")
	}
	rec := ports.PRRecord{RepoID: l.RepoID, Number: pr.Number, URL: pr.URL, Branch: branch, Base: l.Branch, JobID: l.JobID, Mode: mode,
		State: state, ChunkIDs: l.ChunkIDs, SourcePaths: l.SourcePaths, Approver: approver}
	if err := o.PRs.SavePR(ctx, rec); err != nil {
		return pr, nil, fmt.Errorf("record docs PR: %w", err)
	}
	superseded, err := o.supersede(ctx, host, l, pr.Number)
	return pr, superseded, err
}

// existing finds the PR a previous attempt of this job already opened.
func (o *Opener) existing(ctx context.Context, host ports.CodeHost, l ports.DocsLanding, branch string) (ports.PullRequest, bool, error) {
	active, err := o.PRs.ActivePRs(ctx, l.RepoID)
	if err != nil {
		return ports.PullRequest{}, false, err
	}
	for _, r := range active {
		if r.Branch == branch {
			pr, err := host.GetPR(ctx, l.Repo, r.Number)
			return pr, err == nil, err
		}
	}
	return ports.PullRequest{}, false, nil
}

// supersede closes older active docs PRs whose chunk set the new PR covers entirely. Partially
// overlapping PRs stay open: closing them would drop docs the new PR does not regenerate, and if both
// touch one file the later merge conflicts and the lifecycle regenerates it from current code.
func (o *Opener) supersede(ctx context.Context, host ports.CodeHost, l ports.DocsLanding, newNumber int) ([]int, error) {
	covered := map[string]bool{}
	for _, id := range l.ChunkIDs {
		covered[id] = true
	}
	active, err := o.PRs.ActivePRs(ctx, l.RepoID)
	if err != nil {
		return nil, err
	}
	var closed []int
	for _, r := range active {
		if r.Number == newNumber || len(r.ChunkIDs) == 0 || !subset(r.ChunkIDs, covered) {
			continue
		}
		msg := fmt.Sprintf("Superseded by #%d, which regenerates these docs from newer code.", newNumber)
		if err := host.ClosePR(ctx, l.Repo, r.Number, msg); err != nil && !errors.Is(err, ports.ErrNotFound) {
			return closed, fmt.Errorf("close superseded PR #%d: %w", r.Number, err)
		}
		_ = host.DeleteBranch(ctx, l.Repo, r.Branch)
		if err := o.PRs.SetPRState(ctx, l.RepoID, r.Number, ports.PRStateSuperseded, fmt.Sprintf("superseded by #%d", newNumber)); err != nil {
			return closed, err
		}
		closed = append(closed, r.Number)
	}
	return closed, nil
}

func subset(ids []string, of map[string]bool) bool {
	for _, id := range ids {
		if !of[id] {
			return false
		}
	}
	return true
}

// Land opens an auto-merge docs PR.
func (o *Opener) Land(ctx context.Context, host ports.CodeHost, _ ports.PushConfig, l ports.DocsLanding) (ports.LandResult, error) {
	pr, sup, err := o.Open(ctx, host, l, ports.PushPRAutoMerge, nil, ports.PRStateOpen)
	if err != nil {
		return ports.LandResult{}, err
	}
	return ports.LandResult{Mode: ports.PushPRAutoMerge, PR: &pr, Status: ports.JobDone, Superseded: sup}, nil
}
