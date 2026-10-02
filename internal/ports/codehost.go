package ports

import (
	"context"
	"net/http"
	"time"
)

// PushEvent is a normalized push from any git host (webhook or polling).
type PushEvent struct {
	Repo      string `json:"repo"` // full name, e.g. "acme/shop" or "group/sub/project"
	Branch    string `json:"branch"`
	BeforeSHA string `json:"before_sha"`
	AfterSHA  string `json:"after_sha"`
	// Authors are the commit author/committer logins and emails in the push (bot-loop guard input).
	Authors []string `json:"authors,omitempty"`
	Pusher  string   `json:"pusher,omitempty"`
	// Paths are changed paths reported by the webhook (may be empty; the pipeline compares commits anyway).
	Paths    []string `json:"paths,omitempty"`
	Deleted  bool     `json:"deleted,omitempty"` // branch deletion
	Forced   bool     `json:"forced,omitempty"`
	Delivery string   `json:"delivery,omitempty"` // webhook delivery id, for idempotency and logs
}

// ReviewEvent is a pull/merge request review or state change relevant to the docs PR lifecycle.
type ReviewEvent struct {
	Repo     string `json:"repo"`
	Number   int    `json:"number"`
	Approved bool   `json:"approved"`
	Reviewer string `json:"reviewer"`
}

// Identity is the Hub's own bot identity on a git host.
type Identity struct {
	Login string `json:"login"`
	Name  string `json:"name"`
	Email string `json:"email"`
}

// FileChange is one file written by a commit. Delete removes the path.
type FileChange struct {
	Path    string `json:"path"`
	Content []byte `json:"-"`
	Delete  bool   `json:"delete,omitempty"`
}

// CommitRequest writes files to a branch in one commit.
type CommitRequest struct {
	Branch string
	// ParentSHA, when set, makes the write fail with ErrConflict if the branch moved (optimistic lock).
	ParentSHA string
	Message   string
	Files     []FileChange
	Author    Identity
}

// PRState is a pull/merge request state.
type PRState string

const (
	PROpen   PRState = "open"
	PRMerged PRState = "merged"
	PRClosed PRState = "closed"
)

// ChecksState summarises required status checks.
type ChecksState string

const (
	ChecksPending ChecksState = "pending"
	ChecksSuccess ChecksState = "success"
	ChecksFailure ChecksState = "failure"
	ChecksNone    ChecksState = "none"
)

// PullRequest is a PR (GitHub) or MR (GitLab) — the same concept behind one type (plan rule 6d).
type PullRequest struct {
	Number    int         `json:"number"`
	URL       string      `json:"url"`
	State     PRState     `json:"state"`
	Head      string      `json:"head"`
	Base      string      `json:"base"`
	HeadSHA   string      `json:"head_sha"`
	Mergeable *bool       `json:"mergeable,omitempty"` // nil while the host is still computing
	Checks    ChecksState `json:"checks"`
	Approved  bool        `json:"approved"`
	CreatedAt time.Time   `json:"created_at"`
}

// PRRequest opens a PR/MR.
type PRRequest struct {
	Head      string
	Base      string
	Title     string
	Body      string
	Reviewers []string // users or teams ("org/team" on GitHub)
}

// Commit is a commit touching a path (for decode context).
type Commit struct {
	SHA     string    `json:"sha"`
	Author  string    `json:"author"`
	Message string    `json:"message"`
	At      time.Time `json:"at"`
	URL     string    `json:"url"`
}

// AppInstallation is implemented by hosts that act through an installed app (a GitHub App). When a host is
// not in app mode, the methods report applied=false and change nothing.
type AppInstallation interface {
	// SetInstallationSuspended suspends (true) or resumes (false) the app's access to the repositories.
	SetInstallationSuspended(ctx context.Context, suspended bool) (applied bool, err error)
	// Uninstall removes the app from the account or organization; its access ends immediately. The app
	// itself remains (no API deletes it); settingsURL is where its owner can delete it.
	Uninstall(ctx context.Context) (settingsURL string, applied bool, err error)
}

// CodeHost is a git hosting provider (GitHub, GitLab). Adapters own auth, pagination, and the mapping of
// host-specific protection/merge semantics onto these operations.
type CodeHost interface {
	Kind() string

	// Webhooks.
	VerifyWebhook(h http.Header, body []byte) error
	// ParseWebhook returns a PushEvent, a ReviewEvent, or (nil, nil) for events the Hub ignores.
	ParseWebhook(h http.Header, body []byte) (any, error)
	RegisterWebhook(ctx context.Context, repo, url, secret string) error

	// Repository reads.
	ListRepos(ctx context.Context) ([]string, error)
	DefaultBranch(ctx context.Context, repo string) (string, error)
	BranchHead(ctx context.Context, repo, branch string) (string, error)
	Compare(ctx context.Context, repo, base, head string) ([]ChangedFile, error)
	// GetFile returns (nil, ErrNotFound) when the path does not exist at ref.
	GetFile(ctx context.Context, repo, path, ref string) ([]byte, error)
	ListTree(ctx context.Context, repo, ref string) ([]string, error)
	CommitsForPath(ctx context.Context, repo, path string, since time.Time) ([]Commit, error)
	BotIdentity(ctx context.Context) (Identity, error)

	// Writes. CommitFiles returns ErrProtectedBranch when branch protection rejects a direct write.
	CreateBranch(ctx context.Context, repo, name, fromSHA string) error
	DeleteBranch(ctx context.Context, repo, name string) error
	CommitFiles(ctx context.Context, repo string, req CommitRequest) (string, error)

	// Pull/merge requests.
	OpenPR(ctx context.Context, repo string, req PRRequest) (PullRequest, error)
	GetPR(ctx context.Context, repo string, number int) (PullRequest, error)
	MergePR(ctx context.Context, repo string, number int, sha string) error
	ClosePR(ctx context.Context, repo string, number int, comment string) error
	// UpdatePRBranch rebases the PR branch onto its base; ErrRebaseConflict when it cannot.
	UpdatePRBranch(ctx context.Context, repo string, number int) error
	// ValidReviewer reports whether a user/team exists and can review in the repo.
	ValidReviewer(ctx context.Context, repo, reviewer string) (bool, error)
}
