// Package gitlab implements ports.CodeHost for GitLab.com and self-managed GitLab with a project or group
// access token. Merge requests map onto the same PR semantics as GitHub (plan rule 6d).
package gitlab

import (
	"context"
	"crypto/subtle"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	gl "gitlab.com/gitlab-org/api/client-go"

	"github.com/GokulMV/DocTheRepo/internal/ports"
)

// Host implements ports.CodeHost.
type Host struct {
	c             *gl.Client
	webhookSecret []byte
	group         string
	identity      *ports.Identity
	// RebaseWait bounds how long UpdatePRBranch waits for GitLab's asynchronous rebase.
	RebaseWait time.Duration
	// RebasePoll is the interval between rebase status checks.
	RebasePoll time.Duration
}

// New builds the adapter. cfg.APIKey is the access token; cfg.BaseURL is the instance URL for self-managed
// GitLab (default https://gitlab.com). Extra: webhook_secret, group (for repository discovery).
func New(cfg ports.ProviderConfig, opts ...gl.ClientOptionFunc) (*Host, error) {
	if cfg.APIKey == "" {
		return nil, errors.New("gitlab needs an access token")
	}
	o := []gl.ClientOptionFunc{gl.WithCustomRetryMax(2)}
	if cfg.BaseURL != "" {
		o = append(o, gl.WithBaseURL(cfg.BaseURL))
	}
	c, err := gl.NewClient(cfg.APIKey, append(o, opts...)...)
	if err != nil {
		return nil, fmt.Errorf("gitlab client: %w", err)
	}
	return &Host{c: c, webhookSecret: []byte(cfg.Extra["webhook_secret"]), group: cfg.Extra["group"], RebaseWait: 30 * time.Second, RebasePoll: 500 * time.Millisecond}, nil
}

// Kind returns "gitlab".
func (h *Host) Kind() string { return "gitlab" }

// VerifyWebhook compares X-Gitlab-Token with the configured secret in constant time.
func (h *Host) VerifyWebhook(hdr http.Header, _ []byte) error {
	tok := hdr.Get("X-Gitlab-Token")
	if len(h.webhookSecret) == 0 || tok == "" || subtle.ConstantTimeCompare([]byte(tok), h.webhookSecret) != 1 {
		return ports.ErrInvalidSignature
	}
	return nil
}

// ParseWebhook maps Push Hook and Merge Request Hook (approved) events.
func (h *Host) ParseWebhook(hdr http.Header, body []byte) (any, error) {
	et := gl.EventType(hdr.Get("X-Gitlab-Event"))
	if et != gl.EventTypePush && et != gl.EventTypeMergeRequest {
		return nil, nil
	}
	ev, err := gl.ParseWebhook(et, body)
	if err != nil {
		return nil, &ports.ValidationError{Code: "MALFORMED_PAYLOAD", Message: err.Error()}
	}
	switch e := ev.(type) {
	case *gl.PushEvent:
		if !strings.HasPrefix(e.Ref, "refs/heads/") {
			return nil, nil
		}
		pe := ports.PushEvent{Repo: e.Project.PathWithNamespace, Branch: strings.TrimPrefix(e.Ref, "refs/heads/"),
			BeforeSHA: e.Before, AfterSHA: e.After, Pusher: e.UserUsername, Deleted: strings.Trim(e.After, "0") == "",
			Delivery: hdr.Get("X-Gitlab-Event-UUID")}
		if e.UserUsername != "" {
			pe.Authors = append(pe.Authors, e.UserUsername)
		}
		seen := map[string]bool{}
		for _, c := range e.Commits {
			if em := c.Author.Email; em != "" && !seen[em] {
				seen[em] = true
				pe.Authors = append(pe.Authors, em)
			}
			pe.Paths = append(append(append(pe.Paths, c.Added...), c.Modified...), c.Removed...)
		}
		return pe, nil
	case *gl.MergeEvent:
		if e.ObjectAttributes.Action != "approved" {
			return nil, nil
		}
		re := ports.ReviewEvent{Repo: e.Project.PathWithNamespace, Number: int(e.ObjectAttributes.IID), Approved: true}
		if e.User != nil {
			re.Reviewer = e.User.Username
		}
		return re, nil
	}
	return nil, nil
}

// RegisterWebhook adds (or updates) a push + merge request hook.
func (h *Host) RegisterWebhook(ctx context.Context, repo, url, secret string) error {
	hooks, _, err := h.c.Projects.ListProjectHooks(repo, &gl.ListProjectHooksOptions{}, gl.WithContext(ctx))
	if err != nil {
		return classify(err)
	}
	for _, hk := range hooks {
		if hk.URL == url {
			_, _, err := h.c.Projects.EditProjectHook(repo, hk.ID, &gl.EditProjectHookOptions{URL: gl.Ptr(url), Token: gl.Ptr(secret),
				PushEvents: gl.Ptr(true), MergeRequestsEvents: gl.Ptr(true), EnableSSLVerification: gl.Ptr(true)}, gl.WithContext(ctx))
			return classify(err)
		}
	}
	_, _, err = h.c.Projects.AddProjectHook(repo, &gl.AddProjectHookOptions{URL: gl.Ptr(url), Token: gl.Ptr(secret),
		PushEvents: gl.Ptr(true), MergeRequestsEvents: gl.Ptr(true), EnableSSLVerification: gl.Ptr(true)}, gl.WithContext(ctx))
	return classify(err)
}

// ListRepos lists projects in the configured group (including subgroups), or the token's memberships.
func (h *Host) ListRepos(ctx context.Context) ([]string, error) {
	var out []string
	page := int64(1)
	for {
		var projects []*gl.Project
		var resp *gl.Response
		var err error
		lo := gl.ListOptions{PerPage: 100, Page: page}
		if h.group != "" {
			projects, resp, err = h.c.Groups.ListGroupProjects(h.group, &gl.ListGroupProjectsOptions{ListOptions: lo, IncludeSubGroups: gl.Ptr(true), Archived: gl.Ptr(false)}, gl.WithContext(ctx))
		} else {
			projects, resp, err = h.c.Projects.ListProjects(&gl.ListProjectsOptions{ListOptions: lo, Membership: gl.Ptr(true), Archived: gl.Ptr(false)}, gl.WithContext(ctx))
		}
		if err != nil {
			return nil, classify(err)
		}
		for _, p := range projects {
			out = append(out, p.PathWithNamespace)
		}
		if resp == nil || resp.NextPage == 0 {
			return out, nil
		}
		page = resp.NextPage
	}
}

// DefaultBranch returns the project's default branch.
func (h *Host) DefaultBranch(ctx context.Context, repo string) (string, error) {
	p, _, err := h.c.Projects.GetProject(repo, &gl.GetProjectOptions{}, gl.WithContext(ctx))
	if err != nil {
		return "", classify(err)
	}
	return p.DefaultBranch, nil
}

// BranchHead returns a branch's head SHA.
func (h *Host) BranchHead(ctx context.Context, repo, branch string) (string, error) {
	b, _, err := h.c.Branches.GetBranch(repo, branch, gl.WithContext(ctx))
	if err != nil {
		return "", classify(err)
	}
	return b.Commit.ID, nil
}

// Compare lists files changed between two commits. GitLab reports renames; an empty diff means a pure rename.
func (h *Host) Compare(ctx context.Context, repo, base, head string) ([]ports.ChangedFile, error) {
	if base == "" || strings.Trim(base, "0") == "" {
		paths, err := h.ListTree(ctx, repo, head)
		if err != nil {
			return nil, err
		}
		out := make([]ports.ChangedFile, len(paths))
		for i, p := range paths {
			out[i] = ports.ChangedFile{Path: p, Status: ports.FileAdded}
		}
		return out, nil
	}
	cmp, _, err := h.c.Repositories.Compare(repo, &gl.CompareOptions{From: gl.Ptr(base), To: gl.Ptr(head), Straight: gl.Ptr(false)}, gl.WithContext(ctx))
	if err != nil {
		return nil, classify(err)
	}
	if cmp.CompareTimeout {
		return nil, ports.Transient(fmt.Errorf("gitlab compare of %s..%s timed out", base, head))
	}
	out := make([]ports.ChangedFile, 0, len(cmp.Diffs))
	for _, d := range cmp.Diffs {
		cf := ports.ChangedFile{Path: d.NewPath, Status: ports.FileModified}
		switch {
		case d.NewFile:
			cf.Status = ports.FileAdded
		case d.DeletedFile:
			cf.Status, cf.Path = ports.FileRemoved, d.OldPath
		case d.RenamedFile:
			cf.Status, cf.PreviousPath, cf.Similarity = ports.FileRenamed, d.OldPath, 50
			if strings.TrimSpace(d.Diff) == "" {
				cf.Similarity = 100
			}
		}
		out = append(out, cf)
	}
	return out, nil
}

// GetFile returns raw file content at ref.
func (h *Host) GetFile(ctx context.Context, repo, path, ref string) ([]byte, error) {
	b, _, err := h.c.RepositoryFiles.GetRawFile(repo, path, &gl.GetRawFileOptions{Ref: gl.Ptr(ref)}, gl.WithContext(ctx))
	if err != nil {
		return nil, classify(err)
	}
	return b, nil
}

// ListTree lists all file paths at ref.
func (h *Host) ListTree(ctx context.Context, repo, ref string) ([]string, error) {
	var out []string
	opt := &gl.ListTreeOptions{Ref: gl.Ptr(ref), Recursive: gl.Ptr(true), ListOptions: gl.ListOptions{PerPage: 100, Page: 1}}
	for {
		nodes, resp, err := h.c.Repositories.ListTree(repo, opt, gl.WithContext(ctx))
		if err != nil {
			return nil, classify(err)
		}
		for _, n := range nodes {
			if n.Type == "blob" {
				out = append(out, n.Path)
			}
		}
		if resp == nil || resp.NextPage == 0 {
			return out, nil
		}
		opt.Page = resp.NextPage
	}
}

// CommitsForPath lists recent commits touching a path.
func (h *Host) CommitsForPath(ctx context.Context, repo, path string, since time.Time) ([]ports.Commit, error) {
	list, _, err := h.c.Commits.ListCommits(repo, &gl.ListCommitsOptions{Path: gl.Ptr(path), Since: &since, ListOptions: gl.ListOptions{PerPage: 30}}, gl.WithContext(ctx))
	if err != nil {
		return nil, classify(err)
	}
	out := make([]ports.Commit, 0, len(list))
	for _, c := range list {
		at := time.Time{}
		if c.AuthoredDate != nil {
			at = *c.AuthoredDate
		}
		out = append(out, ports.Commit{SHA: c.ID, Author: c.AuthorName, Message: c.Message, At: at, URL: c.WebURL})
	}
	return out, nil
}

// BotIdentity returns the token's user (a project/group bot user for access tokens).
func (h *Host) BotIdentity(ctx context.Context) (ports.Identity, error) {
	if h.identity != nil {
		return *h.identity, nil
	}
	u, _, err := h.c.Users.CurrentUser(gl.WithContext(ctx))
	if err != nil {
		return ports.Identity{}, classify(err)
	}
	email := u.PublicEmail
	if email == "" {
		email = u.Email
	}
	h.identity = &ports.Identity{Login: u.Username, Name: u.Name, Email: email}
	return *h.identity, nil
}

// CreateBranch creates a branch at fromSHA.
func (h *Host) CreateBranch(ctx context.Context, repo, name, fromSHA string) error {
	_, _, err := h.c.Branches.CreateBranch(repo, &gl.CreateBranchOptions{Branch: gl.Ptr(name), Ref: gl.Ptr(fromSHA)}, gl.WithContext(ctx))
	if isStatus(err, 400) && strings.Contains(strings.ToLower(err.Error()), "already exists") {
		return fmt.Errorf("branch %s: %w", name, ports.ErrConflict)
	}
	return classify(err)
}

// DeleteBranch removes a branch; a missing branch is not an error.
func (h *Host) DeleteBranch(ctx context.Context, repo, name string) error {
	_, err := h.c.Branches.DeleteBranch(repo, name, gl.WithContext(ctx))
	if errors.Is(classify(err), ports.ErrNotFound) {
		return nil
	}
	return classify(err)
}

// CommitFiles writes all changes in one commit via the Commits API. GitLab needs create vs update per
// file, so existence is checked at the branch head first. When ParentSHA is set and the branch moved,
// ErrConflict is returned instead of committing on top of someone else's push.
func (h *Host) CommitFiles(ctx context.Context, repo string, req ports.CommitRequest) (string, error) {
	head, err := h.BranchHead(ctx, repo, req.Branch)
	if err != nil {
		return "", err
	}
	if req.ParentSHA != "" && head != req.ParentSHA {
		return "", fmt.Errorf("%s moved while committing: %w", req.Branch, ports.ErrConflict)
	}
	actions := make([]*gl.CommitActionOptions, 0, len(req.Files))
	for _, f := range req.Files {
		meta, _, err := h.c.RepositoryFiles.GetFileMetaData(repo, f.Path, &gl.GetFileMetaDataOptions{Ref: gl.Ptr(head)}, gl.WithContext(ctx))
		exists := err == nil
		if err != nil && !errors.Is(classify(err), ports.ErrNotFound) {
			return "", classify(err)
		}
		a := &gl.CommitActionOptions{FilePath: gl.Ptr(f.Path)}
		switch {
		case f.Delete && !exists:
			continue
		case f.Delete:
			a.Action = gl.Ptr(gl.FileDelete)
		case exists:
			a.Action, a.Content = gl.Ptr(gl.FileUpdate), gl.Ptr(string(f.Content))
		default:
			a.Action, a.Content = gl.Ptr(gl.FileCreate), gl.Ptr(string(f.Content))
		}
		if exists && meta != nil {
			a.LastCommitID = gl.Ptr(meta.LastCommitID) // GitLab rejects the action if the file changed since
		}
		actions = append(actions, a)
	}
	if len(actions) == 0 {
		return head, nil
	}
	opt := &gl.CreateCommitOptions{Branch: gl.Ptr(req.Branch), CommitMessage: gl.Ptr(req.Message), Actions: actions}
	if req.Author.Name != "" {
		opt.AuthorName, opt.AuthorEmail = gl.Ptr(req.Author.Name), gl.Ptr(req.Author.Email)
	}
	c, _, err := h.c.Commits.CreateCommit(repo, opt, gl.WithContext(ctx))
	if err != nil {
		msg := strings.ToLower(err.Error())
		switch {
		case isStatus(err, 403) || strings.Contains(msg, "not allowed to push") || strings.Contains(msg, "protected branch"):
			return "", fmt.Errorf("%s: %w", req.Branch, ports.ErrProtectedBranch)
		case strings.Contains(msg, "has been modified") || isStatus(err, 409):
			return "", fmt.Errorf("%s: %w", req.Branch, ports.ErrConflict)
		}
		return "", classify(err)
	}
	return c.ID, nil
}

// OpenPR opens a merge request. Reviewers are usernames; "group/path" expands to that group's members.
func (h *Host) OpenPR(ctx context.Context, repo string, req ports.PRRequest) (ports.PullRequest, error) {
	opt := &gl.CreateMergeRequestOptions{Title: gl.Ptr(req.Title), Description: gl.Ptr(req.Body), SourceBranch: gl.Ptr(req.Head),
		TargetBranch: gl.Ptr(req.Base), RemoveSourceBranch: gl.Ptr(true), Squash: gl.Ptr(true)}
	if ids, err := h.reviewerIDs(ctx, req.Reviewers); err != nil {
		return ports.PullRequest{}, err
	} else if len(ids) > 0 {
		opt.ReviewerIDs = &ids
	}
	mr, _, err := h.c.MergeRequests.CreateMergeRequest(repo, opt, gl.WithContext(ctx))
	if err != nil {
		return ports.PullRequest{}, classify(err)
	}
	return h.GetPR(ctx, repo, int(mr.IID))
}

func (h *Host) reviewerIDs(ctx context.Context, reviewers []string) ([]int64, error) {
	var ids []int64
	for _, rv := range reviewers {
		rv = strings.TrimPrefix(rv, "@")
		if strings.Contains(rv, "/") {
			members, _, err := h.c.Groups.ListGroupMembers(rv, &gl.ListGroupMembersOptions{ListOptions: gl.ListOptions{PerPage: 20}}, gl.WithContext(ctx))
			if err != nil {
				return nil, classify(err)
			}
			for _, m := range members {
				ids = append(ids, m.ID)
			}
			continue
		}
		users, _, err := h.c.Users.ListUsers(&gl.ListUsersOptions{Username: gl.Ptr(rv)}, gl.WithContext(ctx))
		if err != nil {
			return nil, classify(err)
		}
		for _, u := range users {
			ids = append(ids, u.ID)
		}
	}
	return ids, nil
}

// GetPR returns merge request state with pipeline and approval status.
func (h *Host) GetPR(ctx context.Context, repo string, number int) (ports.PullRequest, error) {
	mr, _, err := h.c.MergeRequests.GetMergeRequest(repo, int64(number), &gl.GetMergeRequestsOptions{IncludeRebaseInProgress: gl.Ptr(true)}, gl.WithContext(ctx))
	if err != nil {
		return ports.PullRequest{}, classify(err)
	}
	out := ports.PullRequest{Number: int(mr.IID), URL: mr.WebURL, Head: mr.SourceBranch, Base: mr.TargetBranch, HeadSHA: mr.SHA, Checks: ports.ChecksNone}
	if mr.CreatedAt != nil {
		out.CreatedAt = *mr.CreatedAt
	}
	switch mr.State {
	case "merged":
		out.State = ports.PRMerged
	case "closed", "locked":
		out.State = ports.PRClosed
	default:
		out.State = ports.PROpen
	}
	switch mr.DetailedMergeStatus {
	case "checking", "unchecked", "preparing", "approvals_syncing":
	case "conflict", "need_rebase":
		f := false
		out.Mergeable = &f
	default:
		t := !mr.HasConflicts
		out.Mergeable = &t
	}
	if p := mr.HeadPipeline; p != nil {
		switch p.Status {
		case "success":
			out.Checks = ports.ChecksSuccess
		case "failed", "canceled":
			out.Checks = ports.ChecksFailure
		case "skipped", "manual":
			out.Checks = ports.ChecksNone
		default:
			out.Checks = ports.ChecksPending
		}
	}
	appr, _, err := h.c.MergeRequestApprovals.GetConfiguration(repo, int64(number), gl.WithContext(ctx))
	if err != nil && !errors.Is(classify(err), ports.ErrNotFound) {
		return out, classify(err)
	}
	if appr != nil {
		out.Approved = appr.Approved && len(appr.ApprovedBy) > 0
	}
	return out, nil
}

// MergePR accepts a merge request at the expected head SHA (squash, source branch removed).
func (h *Host) MergePR(ctx context.Context, repo string, number int, sha string) error {
	_, _, err := h.c.MergeRequests.AcceptMergeRequest(repo, int64(number), &gl.AcceptMergeRequestOptions{SHA: gl.Ptr(sha),
		Squash: gl.Ptr(true), ShouldRemoveSourceBranch: gl.Ptr(true)}, gl.WithContext(ctx))
	switch {
	case isStatus(err, 409):
		return fmt.Errorf("MR !%d head moved: %w", number, ports.ErrConflict)
	case isStatus(err, 405) || isStatus(err, 406) || isStatus(err, 422):
		return fmt.Errorf("MR !%d not mergeable yet: %w", number, ports.ErrProtectedBranch)
	}
	return classify(err)
}

// ClosePR comments on and closes a merge request.
func (h *Host) ClosePR(ctx context.Context, repo string, number int, comment string) error {
	if comment != "" {
		if _, _, err := h.c.Notes.CreateMergeRequestNote(repo, int64(number), &gl.CreateMergeRequestNoteOptions{Body: gl.Ptr(comment)}, gl.WithContext(ctx)); err != nil {
			return classify(err)
		}
	}
	_, _, err := h.c.MergeRequests.UpdateMergeRequest(repo, int64(number), &gl.UpdateMergeRequestOptions{StateEvent: gl.Ptr("close")}, gl.WithContext(ctx))
	return classify(err)
}

// UpdatePRBranch rebases the MR source branch onto its target and waits for GitLab's async rebase.
func (h *Host) UpdatePRBranch(ctx context.Context, repo string, number int) error {
	if _, err := h.c.MergeRequests.RebaseMergeRequest(repo, int64(number), &gl.RebaseMergeRequestOptions{}, gl.WithContext(ctx)); err != nil {
		if isStatus(err, 403) || isStatus(err, 409) {
			return fmt.Errorf("MR !%d: %w", number, ports.ErrRebaseConflict)
		}
		return classify(err)
	}
	deadline := time.Now().Add(h.RebaseWait)
	for {
		mr, _, err := h.c.MergeRequests.GetMergeRequest(repo, int64(number), &gl.GetMergeRequestsOptions{IncludeRebaseInProgress: gl.Ptr(true)}, gl.WithContext(ctx))
		if err != nil {
			return classify(err)
		}
		if !mr.RebaseInProgress {
			if mr.MergeError != "" {
				return fmt.Errorf("MR !%d: %s: %w", number, mr.MergeError, ports.ErrRebaseConflict)
			}
			return nil
		}
		if time.Now().After(deadline) {
			return ports.Transient(fmt.Errorf("MR !%d rebase still in progress", number))
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(h.RebasePoll):
		}
	}
}

// ValidReviewer checks that a user exists, or that a group ("group/path") exists and has members.
func (h *Host) ValidReviewer(ctx context.Context, repo, reviewer string) (bool, error) {
	ids, err := h.reviewerIDs(ctx, []string{reviewer})
	if errors.Is(err, ports.ErrNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return len(ids) > 0, nil
}

func classify(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	if errors.Is(err, gl.ErrNotFound) {
		return fmt.Errorf("gitlab: %w", ports.ErrNotFound)
	}
	var er *gl.ErrorResponse
	if errors.As(err, &er) && er.Response != nil {
		switch s := er.Response.StatusCode; {
		case s == http.StatusNotFound:
			return fmt.Errorf("gitlab: %w", ports.ErrNotFound)
		case s == http.StatusTooManyRequests || s >= 500:
			return ports.Transient(fmt.Errorf("gitlab %d: %w", s, err))
		default:
			return ports.Permanent(fmt.Errorf("gitlab %d: %w", s, err))
		}
	}
	return ports.Transient(fmt.Errorf("gitlab: %w", err))
}

func isStatus(err error, status int) bool {
	var er *gl.ErrorResponse
	return errors.As(err, &er) && er.Response != nil && er.Response.StatusCode == status
}
