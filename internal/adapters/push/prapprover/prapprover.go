// Package prapprover lands docs through a PR that a designated human approves with their own review; the
// bot merges only after that review satisfies branch protection. Hard rule (plan § 12): nothing here
// accepts, stores, or uses another person's credentials — there is no code path that could.
package prapprover

import (
	"context"
	"fmt"

	"github.com/GokulMV/DocTheRepo/internal/adapters/push/prautomerge"
	"github.com/GokulMV/DocTheRepo/internal/ports"
)

// Lander implements the pr_with_approver mode.
type Lander struct {
	Opener *prautomerge.Opener
}

// Land validates the approver, then opens a PR requesting their review. An approver who no longer exists
// or lost access (the "approver left the company" case) falls back to an auto-merge PR; the lifecycle
// marks it needs_human if branch protection then refuses the merge.
func (a *Lander) Land(ctx context.Context, host ports.CodeHost, cfg ports.PushConfig, l ports.DocsLanding) (ports.LandResult, error) {
	if cfg.Approver == "" {
		return ports.LandResult{}, ports.Permanent(fmt.Errorf("push mode pr_with_approver needs an approver"))
	}
	ok, err := host.ValidReviewer(ctx, l.Repo, cfg.Approver)
	if err != nil {
		return ports.LandResult{}, fmt.Errorf("validate approver: %w", err)
	}
	if !ok {
		res, err := a.Opener.Land(ctx, host, cfg, l)
		res.Note = fmt.Sprintf("approver %q no longer exists or has no access to %s; fell back to an auto-merge PR", cfg.Approver, l.Repo)
		return res, err
	}
	pr, sup, err := a.Opener.Open(ctx, host, l, ports.PushPRWithApprover, []string{cfg.Approver}, ports.PRStateAwaitingReview)
	if err != nil {
		return ports.LandResult{}, err
	}
	return ports.LandResult{Mode: ports.PushPRWithApprover, PR: &pr, Status: ports.JobPendingApproval, Superseded: sup,
		Note: "waiting for " + cfg.Approver + " to review"}, nil
}
