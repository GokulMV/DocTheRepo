package ports

import (
	"context"
	"time"
)

// PushMode is how generated docs land in a repository (plan § 8.17).
type PushMode string

const (
	PushDirect         PushMode = "direct"
	PushPRAutoMerge    PushMode = "pr_auto_merge"
	PushPRWithApprover PushMode = "pr_with_approver"
)

// OnReject values: what a direct push does when branch protection rejects it.
const (
	RejectFallbackAutoMerge = "fallback_pr_auto_merge"
	RejectFallbackApprover  = "fallback_pr_with_approver"
	RejectFailJob           = "fail_job"
)

// Conflict strategies for a docs PR that became unmergeable.
const (
	ConflictAutoRebase = "auto_rebase"
	ConflictRequeue    = "requeue"
	ConflictLeaveOpen  = "leave_open"
)

// PushConfig is a repository's landing configuration.
type PushConfig struct {
	Mode             PushMode
	OnReject         string
	Approver         string
	ConflictStrategy string
	StaleAfter       time.Duration
}

// DocsLanding is one set of generated doc files to land on a repository's tracked branch.
type DocsLanding struct {
	RepoID string
	Repo   string
	Branch string // the tracked branch the docs describe
	JobID  string
	Files  []FileChange
	// ChunkIDs and SourcePaths are the code chunks/files these docs cover (superseding and requeue).
	ChunkIDs    []string
	SourcePaths []string
	Message     string
	Title       string
	Body        string
	Author      Identity
}

// PR record states.
const (
	PRStateOpen           = "open"
	PRStateAwaitingReview = "awaiting_review"
	PRStateMerged         = "merged"
	PRStateClosed         = "closed"
	PRStateSuperseded     = "superseded"
	PRStateStale          = "stale"
	PRStateRequeued       = "requeued"
	PRStateNeedsHuman     = "needs_human"
)

// LandResult reports how docs were landed.
type LandResult struct {
	Mode      PushMode     `json:"mode"`
	CommitSHA string       `json:"commit_sha,omitempty"`
	PR        *PullRequest `json:"pr,omitempty"`
	// Status is the job status the landing implies: done, pending_approval, or needs_human.
	Status JobStatus `json:"status"`
	Note   string    `json:"note,omitempty"`
	// Superseded lists older docs PRs this landing closed.
	Superseded []int `json:"superseded,omitempty"`
}

// PRRecord is a docs PR the Hub opened and tracks through its lifecycle.
type PRRecord struct {
	RepoID      string    `json:"repo_id"`
	Number      int       `json:"number"`
	URL         string    `json:"url"`
	Branch      string    `json:"branch"`
	Base        string    `json:"base"`
	JobID       string    `json:"job_id,omitempty"`
	Mode        PushMode  `json:"mode"`
	State       string    `json:"state"`
	ChunkIDs    []string  `json:"chunk_ids"`
	SourcePaths []string  `json:"source_paths"`
	Approver    string    `json:"approver,omitempty"`
	Note        string    `json:"note,omitempty"`
	OpenedAt    time.Time `json:"opened_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// Active reports whether the lifecycle sweep still manages the PR.
func (r PRRecord) Active() bool { return r.State == PRStateOpen || r.State == PRStateAwaitingReview }

// PRStore persists docs PRs.
type PRStore interface {
	SavePR(ctx context.Context, r PRRecord) error
	GetPR(ctx context.Context, repoID string, number int) (PRRecord, error)
	ActivePRs(ctx context.Context, repoID string) ([]PRRecord, error)
	AllActivePRs(ctx context.Context) ([]PRRecord, error)
	SetPRState(ctx context.Context, repoID string, number int, state, note string) error
}

// Lander lands generated docs on a repository with the configured push mode.
type Lander interface {
	Land(ctx context.Context, host CodeHost, cfg PushConfig, l DocsLanding) (LandResult, error)
}
