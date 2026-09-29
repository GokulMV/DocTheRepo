// Package lifecycle drives open docs PRs to a final state (plan § 8.17): merge when green (and approved in
// approver mode), rebase or regenerate on conflict, close and regenerate when stale, fall back or ask for
// a human when the approver is gone. Superseding happens when a PR is opened (prautomerge).
package lifecycle

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/GokulMV/DocTheRepo/internal/ports"
)

// HostResolver returns a repository's config and a CodeHost for it.
type HostResolver func(ctx context.Context, repoID string) (ports.CodeHost, ports.RepoConfig, error)

// Requeuer schedules regeneration of a closed PR's docs against the tracked branch's current head.
type Requeuer func(ctx context.Context, rec ports.PRRecord, reason string) error

// ProtectionGrace is how long an auto-merge PR may be refused by branch protection (checks registering)
// before it is handed to a human.
const ProtectionGrace = time.Hour

// Sweeper runs the lifecycle.
type Sweeper struct {
	PRs     ports.PRStore
	Hosts   HostResolver
	Requeue Requeuer
	Log     *slog.Logger
	Now     func() time.Time
	// OnTransition observes state changes (metrics: docs_pr_rebased_total, docs_pr_stale_closed_total, ...).
	OnTransition func(rec ports.PRRecord, to string)
}

func (s *Sweeper) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

func (s *Sweeper) log() *slog.Logger {
	if s.Log != nil {
		return s.Log
	}
	return slog.Default()
}

// Sweep steps every active docs PR. One PR's failure does not stop the others.
func (s *Sweeper) Sweep(ctx context.Context) error {
	recs, err := s.PRs.AllActivePRs(ctx)
	if err != nil {
		return err
	}
	var errs []error
	for _, rec := range recs {
		host, repo, err := s.Hosts(ctx, rec.RepoID)
		if err == nil {
			_, err = s.Step(ctx, host, repo, rec)
		}
		if err != nil {
			s.log().Warn("docs PR lifecycle step failed", "repo_id", rec.RepoID, "pr", rec.Number, "err", err)
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// OnReview handles an approval webhook immediately instead of waiting for the next sweep.
func (s *Sweeper) OnReview(ctx context.Context, repoID string, number int) (string, error) {
	rec, err := s.PRs.GetPR(ctx, repoID, number)
	if errors.Is(err, ports.ErrNotFound) {
		return "", nil // not a Hub docs PR
	}
	if err != nil || !rec.Active() {
		return rec.State, err
	}
	host, repo, err := s.Hosts(ctx, repoID)
	if err != nil {
		return "", err
	}
	return s.Step(ctx, host, repo, rec)
}

func (s *Sweeper) set(ctx context.Context, rec ports.PRRecord, state, note string) (string, error) {
	if err := s.PRs.SetPRState(ctx, rec.RepoID, rec.Number, state, note); err != nil {
		return rec.State, err
	}
	if s.OnTransition != nil {
		s.OnTransition(rec, state)
	}
	lvl := slog.LevelInfo
	if state == ports.PRStateNeedsHuman {
		lvl = slog.LevelError
	}
	s.log().Log(context.Background(), lvl, "docs PR state changed", "repo_id", rec.RepoID, "pr", rec.Number, "from", rec.State, "to", state, "note", note)
	return state, nil
}

// closeAndRequeue closes the PR, deletes its branch, and regenerates its docs from current code.
func (s *Sweeper) closeAndRequeue(ctx context.Context, host ports.CodeHost, repo ports.RepoConfig, rec ports.PRRecord, state, comment string) (string, error) {
	if err := host.ClosePR(ctx, repo.FullName, rec.Number, comment); err != nil && !errors.Is(err, ports.ErrNotFound) {
		return rec.State, err
	}
	_ = host.DeleteBranch(ctx, repo.FullName, rec.Branch)
	if s.Requeue != nil {
		if err := s.Requeue(ctx, rec, state); err != nil {
			return rec.State, fmt.Errorf("requeue docs for PR #%d: %w", rec.Number, err)
		}
	}
	return s.set(ctx, rec, state, comment)
}

// Step advances one PR and returns its (possibly unchanged) state.
func (s *Sweeper) Step(ctx context.Context, host ports.CodeHost, repo ports.RepoConfig, rec ports.PRRecord) (string, error) {
	pr, err := host.GetPR(ctx, repo.FullName, rec.Number)
	if errors.Is(err, ports.ErrNotFound) {
		return s.set(ctx, rec, ports.PRStateClosed, "PR no longer exists")
	}
	if err != nil {
		return rec.State, err
	}
	switch pr.State {
	case ports.PRMerged:
		_ = host.DeleteBranch(ctx, repo.FullName, rec.Branch)
		return s.set(ctx, rec, ports.PRStateMerged, "")
	case ports.PRClosed:
		return s.set(ctx, rec, ports.PRStateClosed, "closed outside the Hub")
	}
	cfg := repo.Push
	stale := cfg.StaleAfter
	if stale <= 0 {
		stale = 72 * time.Hour
	}
	age := s.now().Sub(rec.OpenedAt)
	if age > stale {
		return s.closeAndRequeue(ctx, host, repo, rec, ports.PRStateStale,
			fmt.Sprintf("Closing: this docs PR has been open longer than %s. The Hub is regenerating these docs from the current code.", stale))
	}

	if rec.Mode == ports.PushPRWithApprover && !pr.Approved {
		ok, err := host.ValidReviewer(ctx, repo.FullName, rec.Approver)
		if err != nil {
			return rec.State, err
		}
		if ok {
			return rec.State, nil // waiting for the approver's own review
		}
		rec.Mode, rec.State = ports.PushPRAutoMerge, ports.PRStateOpen
		rec.Note = fmt.Sprintf("approver %q is no longer valid; falling back to auto-merge", rec.Approver)
		if err := s.PRs.SavePR(ctx, rec); err != nil {
			return rec.State, err
		}
		s.log().Error("docs PR approver is invalid; falling back to auto-merge", "repo", repo.FullName, "pr", rec.Number, "approver", rec.Approver)
	}

	if pr.Mergeable == nil {
		return rec.State, nil // the host is still computing mergeability
	}
	if !*pr.Mergeable {
		switch cfg.ConflictStrategy {
		case ports.ConflictLeaveOpen:
			return s.set(ctx, rec, ports.PRStateNeedsHuman, "docs PR has conflicts (pr_conflict_strategy: leave_open)")
		case ports.ConflictRequeue:
			return s.closeAndRequeue(ctx, host, repo, rec, ports.PRStateRequeued, "Closing: the base branch changed under this docs PR. Regenerating from current code.")
		default:
			err := host.UpdatePRBranch(ctx, repo.FullName, rec.Number)
			if errors.Is(err, ports.ErrRebaseConflict) {
				return s.closeAndRequeue(ctx, host, repo, rec, ports.PRStateRequeued, "Closing: this docs PR could not be rebased cleanly. Regenerating from current code.")
			}
			if err == nil && s.OnTransition != nil {
				s.OnTransition(rec, "rebased")
			}
			return rec.State, err // merge on a later step once checks rerun on the rebased head
		}
	}

	switch pr.Checks {
	case ports.ChecksPending:
		return rec.State, nil
	case ports.ChecksFailure:
		return s.set(ctx, rec, ports.PRStateNeedsHuman, "required checks failed on the docs PR")
	}
	err = host.MergePR(ctx, repo.FullName, rec.Number, pr.HeadSHA)
	switch {
	case err == nil:
		_ = host.DeleteBranch(ctx, repo.FullName, rec.Branch)
		return s.set(ctx, rec, ports.PRStateMerged, "")
	case errors.Is(err, ports.ErrConflict):
		return rec.State, nil // head moved (a rebase landed); next step merges the new head
	case errors.Is(err, ports.ErrProtectedBranch):
		if rec.Mode == ports.PushPRAutoMerge && !pr.Approved && age > ProtectionGrace {
			return s.set(ctx, rec, ports.PRStateNeedsHuman,
				"branch protection requires a review the bot cannot give; use push mode pr_with_approver or let the bot bypass")
		}
		return rec.State, nil
	}
	return rec.State, err
}
