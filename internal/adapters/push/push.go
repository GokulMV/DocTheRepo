// Package push dispatches a docs landing to the repo's push mode and applies direct mode's on_reject
// fallback (plan § 8.17). The first lifecycle step runs right after a PR opens, so a green, unprotected
// repo merges without waiting for the sweep.
package push

import (
	"context"
	"errors"
	"fmt"

	"github.com/GokulMV/DocTheRepo/internal/adapters/push/direct"
	"github.com/GokulMV/DocTheRepo/internal/adapters/push/lifecycle"
	"github.com/GokulMV/DocTheRepo/internal/adapters/push/prapprover"
	"github.com/GokulMV/DocTheRepo/internal/adapters/push/prautomerge"
	"github.com/GokulMV/DocTheRepo/internal/ports"
)

// Dispatcher implements ports.Lander.
type Dispatcher struct {
	PRs       ports.PRStore
	Lifecycle *lifecycle.Sweeper
}

// Land lands docs per cfg.Mode.
func (d *Dispatcher) Land(ctx context.Context, host ports.CodeHost, cfg ports.PushConfig, l ports.DocsLanding) (ports.LandResult, error) {
	if len(l.Files) == 0 {
		return ports.LandResult{Mode: cfg.Mode, Status: ports.JobDone, Note: "no doc changes"}, nil
	}
	opener := &prautomerge.Opener{PRs: d.PRs}
	var (
		res ports.LandResult
		err error
	)
	switch cfg.Mode {
	case ports.PushDirect:
		res, err = direct.Lander{}.Land(ctx, host, cfg, l)
		if errors.Is(err, ports.ErrProtectedBranch) {
			switch cfg.OnReject {
			case ports.RejectFailJob:
				return res, ports.Permanent(fmt.Errorf("direct push to %s rejected by branch protection (on_reject: fail_job): %w", l.Branch, err))
			case ports.RejectFallbackApprover:
				res, err = (&prapprover.Lander{Opener: opener}).Land(ctx, host, cfg, l)
			default:
				res, err = opener.Land(ctx, host, cfg, l)
			}
			if err == nil {
				res.Note = joinNote("direct push rejected by branch protection; fell back to "+string(res.Mode), res.Note)
			}
		}
	case ports.PushPRWithApprover:
		res, err = (&prapprover.Lander{Opener: opener}).Land(ctx, host, cfg, l)
	case ports.PushPRAutoMerge, "":
		res, err = opener.Land(ctx, host, cfg, l)
	default:
		return res, ports.Permanent(fmt.Errorf("unknown push mode %q", cfg.Mode))
	}
	if err != nil || res.PR == nil || d.Lifecycle == nil {
		return res, err
	}
	rec, gerr := d.PRs.GetPR(ctx, l.RepoID, res.PR.Number)
	if gerr != nil {
		return res, nil
	}
	state, serr := d.Lifecycle.Step(ctx, host, ports.RepoConfig{ID: l.RepoID, FullName: l.Repo, Push: cfg}, rec)
	if serr == nil && state == ports.PRStateMerged {
		res.PR.State = ports.PRMerged
	}
	return res, nil
}

func joinNote(a, b string) string {
	if b == "" {
		return a
	}
	return a + "; " + b
}
