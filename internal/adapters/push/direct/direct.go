// Package direct lands docs as one commit on the tracked branch. Branch protection rejections are handed
// back as ports.ErrProtectedBranch so the dispatcher can apply the repo's on_reject fallback.
package direct

import (
	"context"
	"errors"
	"fmt"

	"github.com/GokulMV/DocTheRepo/internal/ports"
)

// Lander implements the direct mode.
type Lander struct{}

// Land commits the docs with an optimistic parent. If the branch moved meanwhile but none of the moved
// commits touched the docs being written, the commit is retried on the new head; otherwise the job is
// retried so the docs are re-assembled from the current files (never clobbering a concurrent edit).
func (Lander) Land(ctx context.Context, host ports.CodeHost, _ ports.PushConfig, l ports.DocsLanding) (ports.LandResult, error) {
	for attempt := 0; attempt < 3; attempt++ {
		head, err := host.BranchHead(ctx, l.Repo, l.Branch)
		if err != nil {
			return ports.LandResult{}, err
		}
		sha, err := host.CommitFiles(ctx, l.Repo, ports.CommitRequest{Branch: l.Branch, ParentSHA: head, Message: l.Message, Files: l.Files, Author: l.Author})
		if err == nil {
			return ports.LandResult{Mode: ports.PushDirect, CommitSHA: sha, Status: ports.JobDone}, nil
		}
		if !errors.Is(err, ports.ErrConflict) {
			return ports.LandResult{}, err
		}
		newHead, herr := host.BranchHead(ctx, l.Repo, l.Branch)
		if herr != nil {
			return ports.LandResult{}, herr
		}
		changed, cerr := host.Compare(ctx, l.Repo, head, newHead)
		if cerr != nil {
			return ports.LandResult{}, cerr
		}
		if touches(changed, l.Files) {
			return ports.LandResult{}, ports.Transient(fmt.Errorf("%s changed the docs being written; regenerating: %w", l.Branch, ports.ErrConflict))
		}
	}
	return ports.LandResult{}, ports.Transient(fmt.Errorf("%s keeps moving: %w", l.Branch, ports.ErrConflict))
}

func touches(changed []ports.ChangedFile, files []ports.FileChange) bool {
	ours := map[string]bool{}
	for _, f := range files {
		ours[f.Path] = true
	}
	for _, c := range changed {
		if ours[c.Path] || (c.PreviousPath != "" && ours[c.PreviousPath]) {
			return true
		}
	}
	return false
}
