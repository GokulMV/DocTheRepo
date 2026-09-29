// Package github implements ports.CodeHost for GitHub.com and GitHub Enterprise Server, authenticating as
// a GitHub App installation (short-lived, auto-refreshed installation tokens; plan § 12). A token mode
// exists for local development before the App is created.
package github

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/bradleyfalzon/ghinstallation/v2"
	gh "github.com/google/go-github/v90/github"

	"github.com/GokulMV/DocTheRepo/internal/ports"
)

// Host implements ports.CodeHost.
type Host struct {
	c             *gh.Client
	webhookSecret []byte
	appMode       bool
	identity      *ports.Identity
}

// New builds the adapter. cfg.APIKey is the App private key (PEM) in app mode, or a token when
// Extra["auth"]="token". Extra: app_id, installation_id, webhook_secret. cfg.BaseURL targets GitHub
// Enterprise Server (https://ghe.example.com/api/v3/).
func New(cfg ports.ProviderConfig, transport http.RoundTripper) (*Host, error) {
	if transport == nil {
		transport = http.DefaultTransport
	}
	h := &Host{webhookSecret: []byte(cfg.Extra["webhook_secret"])}
	var client *http.Client
	switch cfg.Extra["auth"] {
	case "token":
		if cfg.APIKey == "" {
			return nil, errors.New("github token auth needs a token")
		}
		client = &http.Client{Transport: transport, Timeout: 60 * time.Second}
	default:
		appID, err1 := strconv.ParseInt(cfg.Extra["app_id"], 10, 64)
		instID, err2 := strconv.ParseInt(cfg.Extra["installation_id"], 10, 64)
		if err1 != nil || err2 != nil || cfg.APIKey == "" {
			return nil, errors.New("github app auth needs extra.app_id, extra.installation_id and the private key")
		}
		itr, err := ghinstallation.New(transport, appID, instID, []byte(cfg.APIKey))
		if err != nil {
			return nil, fmt.Errorf("github app key: %w", err)
		}
		if cfg.BaseURL != "" {
			itr.BaseURL = strings.TrimSuffix(cfg.BaseURL, "/")
		}
		client = &http.Client{Transport: itr, Timeout: 60 * time.Second}
		h.appMode = true
	}
	opts := []gh.ClientOptionsFunc{gh.WithHTTPClient(client)}
	if cfg.Extra["auth"] == "token" {
		opts = append(opts, gh.WithAuthToken(cfg.APIKey))
	}
	if cfg.BaseURL != "" {
		opts = append(opts, gh.WithEnterpriseURLs(cfg.BaseURL, cfg.BaseURL))
	}
	c, err := gh.NewClient(opts...)
	if err != nil {
		return nil, fmt.Errorf("github client: %w", err)
	}
	h.c = c
	return h, nil
}

// Kind returns "github".
func (h *Host) Kind() string { return "github" }

func split(repo string) (string, string, error) {
	o, r, ok := strings.Cut(repo, "/")
	if !ok || o == "" || r == "" || strings.Contains(r, "/") {
		return "", "", ports.Permanent(fmt.Errorf("invalid GitHub repository %q (want owner/name)", repo))
	}
	return o, r, nil
}

// VerifyWebhook checks X-Hub-Signature-256 in constant time before the body is parsed.
func (h *Host) VerifyWebhook(hdr http.Header, body []byte) error {
	if len(h.webhookSecret) == 0 {
		return ports.ErrInvalidSignature // an unset secret must never accept unsigned payloads
	}
	sig := hdr.Get("X-Hub-Signature-256")
	if sig == "" {
		return ports.ErrInvalidSignature
	}
	if err := gh.ValidateSignature(sig, body, h.webhookSecret); err != nil {
		return ports.ErrInvalidSignature
	}
	return nil
}

// ParseWebhook maps push and pull_request_review events; everything else is ignored.
func (h *Host) ParseWebhook(hdr http.Header, body []byte) (any, error) {
	kind := hdr.Get("X-GitHub-Event")
	switch kind {
	case "push", "pull_request_review":
	default:
		return nil, nil
	}
	ev, err := gh.ParseWebHook(kind, body)
	if err != nil {
		return nil, &ports.ValidationError{Code: "MALFORMED_PAYLOAD", Message: err.Error()}
	}
	switch e := ev.(type) {
	case *gh.PushEvent:
		if !strings.HasPrefix(e.GetRef(), "refs/heads/") {
			return nil, nil // tag pushes are not code changes
		}
		pe := ports.PushEvent{Repo: e.GetRepo().GetFullName(), Branch: strings.TrimPrefix(e.GetRef(), "refs/heads/"),
			BeforeSHA: e.GetBefore(), AfterSHA: e.GetAfter(), Pusher: e.GetPusher().GetName(), Deleted: e.GetDeleted(),
			Forced: e.GetForced(), Delivery: hdr.Get("X-GitHub-Delivery")}
		if s := e.GetSender().GetLogin(); s != "" {
			pe.Authors = append(pe.Authors, s)
		}
		seen := map[string]bool{}
		for _, c := range e.Commits {
			for _, who := range []string{c.GetAuthor().GetLogin(), c.GetAuthor().GetEmail(), c.GetCommitter().GetLogin(), c.GetCommitter().GetEmail()} {
				if who != "" && !seen[who] {
					seen[who] = true
					pe.Authors = append(pe.Authors, who)
				}
			}
			pe.Paths = append(pe.Paths, c.Added...)
			pe.Paths = append(pe.Paths, c.Modified...)
			pe.Paths = append(pe.Paths, c.Removed...)
		}
		return pe, nil
	case *gh.PullRequestReviewEvent:
		return ports.ReviewEvent{Repo: e.GetRepo().GetFullName(), Number: e.GetPullRequest().GetNumber(),
			Approved: strings.EqualFold(e.GetReview().GetState(), "approved"), Reviewer: e.GetReview().GetUser().GetLogin()}, nil
	}
	return nil, nil
}

// RegisterWebhook creates (or updates) a push + review webhook pointing at url.
func (h *Host) RegisterWebhook(ctx context.Context, repo, url, secret string) error {
	o, r, err := split(repo)
	if err != nil {
		return err
	}
	cfg := &gh.HookConfig{URL: gh.Ptr(url), ContentType: gh.Ptr("json"), Secret: gh.Ptr(secret), InsecureSSL: gh.Ptr("0")}
	hook := &gh.Hook{Name: gh.Ptr("web"), Active: gh.Ptr(true), Events: []string{"push", "pull_request_review"}, Config: cfg}
	existing, _, err := h.c.Repositories.ListHooks(ctx, o, r, &gh.ListOptions{PerPage: 100})
	if err != nil {
		return classify(err)
	}
	for _, e := range existing {
		if e.GetConfig().GetURL() == url {
			_, _, err := h.c.Repositories.EditHook(ctx, o, r, e.GetID(), hook)
			return classify(err)
		}
	}
	_, _, err = h.c.Repositories.CreateHook(ctx, o, r, hook)
	return classify(err)
}

// ListRepos lists repositories visible to the installation (or the token's user).
func (h *Host) ListRepos(ctx context.Context) ([]string, error) {
	var out []string
	opt := &gh.ListOptions{PerPage: 100}
	for {
		var names []string
		var resp *gh.Response
		if h.appMode {
			list, r, err := h.c.Apps.ListRepos(ctx, opt)
			if err != nil {
				return nil, classify(err)
			}
			for _, rp := range list.Repositories {
				names = append(names, rp.GetFullName())
			}
			resp = r
		} else {
			list, r, err := h.c.Repositories.ListByAuthenticatedUser(ctx, &gh.RepositoryListByAuthenticatedUserOptions{ListOptions: *opt})
			if err != nil {
				return nil, classify(err)
			}
			for _, rp := range list {
				names = append(names, rp.GetFullName())
			}
			resp = r
		}
		out = append(out, names...)
		if resp == nil || resp.NextPage == 0 {
			return out, nil
		}
		opt.Page = resp.NextPage
	}
}

// DefaultBranch returns the repository's default branch.
func (h *Host) DefaultBranch(ctx context.Context, repo string) (string, error) {
	o, r, err := split(repo)
	if err != nil {
		return "", err
	}
	rp, _, err := h.c.Repositories.Get(ctx, o, r)
	if err != nil {
		return "", classify(err)
	}
	return rp.GetDefaultBranch(), nil
}

// BranchHead returns a branch's head SHA.
func (h *Host) BranchHead(ctx context.Context, repo, branch string) (string, error) {
	o, r, err := split(repo)
	if err != nil {
		return "", err
	}
	ref, _, err := h.c.Git.GetRef(ctx, o, r, "heads/"+branch)
	if err != nil {
		return "", classify(err)
	}
	return ref.GetObject().GetSHA(), nil
}

// compareLimit is the number of files the compare API returns before truncating.
const compareLimit = 300

// Compare lists files changed between two commits. Pure renames (no content change) report similarity
// 100; GitHub does not expose a similarity index for edited renames, so they report 50 and are treated as
// moves. Beyond the compare API's file limit, trees are diffed directly.
func (h *Host) Compare(ctx context.Context, repo, base, head string) ([]ports.ChangedFile, error) {
	o, r, err := split(repo)
	if err != nil {
		return nil, err
	}
	if base == "" || strings.Trim(base, "0") == "" { // new branch: everything at head is "added"
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
	cmp, _, err := h.c.Repositories.CompareCommits(ctx, o, r, base, head, &gh.ListOptions{PerPage: 100})
	if err != nil {
		return nil, classify(err)
	}
	if len(cmp.Files) >= compareLimit {
		return h.treeDiff(ctx, o, r, base, head)
	}
	out := make([]ports.ChangedFile, 0, len(cmp.Files))
	for _, f := range cmp.Files {
		cf := ports.ChangedFile{Path: f.GetFilename()}
		switch f.GetStatus() {
		case "added", "copied":
			cf.Status = ports.FileAdded
		case "removed":
			cf.Status = ports.FileRemoved
		case "renamed":
			cf.Status, cf.PreviousPath = ports.FileRenamed, f.GetPreviousFilename()
			cf.Similarity = 50
			if f.GetChanges() == 0 {
				cf.Similarity = 100
			}
		default:
			cf.Status = ports.FileModified
		}
		out = append(out, cf)
	}
	return out, nil
}

func (h *Host) treeDiff(ctx context.Context, o, r, base, head string) ([]ports.ChangedFile, error) {
	a, err := h.blobMap(ctx, o, r, base)
	if err != nil {
		return nil, err
	}
	b, err := h.blobMap(ctx, o, r, head)
	if err != nil {
		return nil, err
	}
	var out []ports.ChangedFile
	for p, sha := range b {
		if old, ok := a[p]; !ok {
			out = append(out, ports.ChangedFile{Path: p, Status: ports.FileAdded})
		} else if old != sha {
			out = append(out, ports.ChangedFile{Path: p, Status: ports.FileModified})
		}
	}
	for p := range a {
		if _, ok := b[p]; !ok {
			out = append(out, ports.ChangedFile{Path: p, Status: ports.FileRemoved})
		}
	}
	return out, nil
}

func (h *Host) blobMap(ctx context.Context, o, r, ref string) (map[string]string, error) {
	sha, err := h.commitTree(ctx, o, r, ref)
	if err != nil {
		return nil, err
	}
	t, _, err := h.c.Git.GetTree(ctx, o, r, sha, true)
	if err != nil {
		return nil, classify(err)
	}
	if t.GetTruncated() {
		return nil, ports.Permanent(fmt.Errorf("repository tree at %s is too large for the GitHub trees API", ref))
	}
	m := map[string]string{}
	for _, e := range t.Entries {
		if e.GetType() == "blob" {
			m[e.GetPath()] = e.GetSHA()
		}
	}
	return m, nil
}

func (h *Host) commitTree(ctx context.Context, o, r, ref string) (string, error) {
	sha := ref
	if len(ref) != 40 {
		head, err := h.BranchHead(ctx, o+"/"+r, ref)
		if err != nil {
			return "", err
		}
		sha = head
	}
	c, _, err := h.c.Git.GetCommit(ctx, o, r, sha)
	if err != nil {
		return "", classify(err)
	}
	return c.GetTree().GetSHA(), nil
}

// GetFile returns file content at ref (ErrNotFound when absent). Large files fall back to the blob API.
func (h *Host) GetFile(ctx context.Context, repo, path, ref string) ([]byte, error) {
	o, r, err := split(repo)
	if err != nil {
		return nil, err
	}
	fc, _, _, err := h.c.Repositories.GetContents(ctx, o, r, path, &gh.RepositoryContentGetOptions{Ref: ref})
	if err != nil {
		return nil, classify(err)
	}
	if fc == nil {
		return nil, ports.ErrNotFound // a directory
	}
	if fc.GetEncoding() == "none" || (fc.Content == nil && fc.GetSize() > 0) {
		b, _, err := h.c.Git.GetBlobRaw(ctx, o, r, fc.GetSHA())
		return b, classify(err)
	}
	s, err := fc.GetContent()
	if err != nil {
		return nil, ports.Permanent(fmt.Errorf("decode %s: %w", path, err))
	}
	return []byte(s), nil
}

// ListTree lists every file path at ref.
func (h *Host) ListTree(ctx context.Context, repo, ref string) ([]string, error) {
	o, r, err := split(repo)
	if err != nil {
		return nil, err
	}
	m, err := h.blobMap(ctx, o, r, ref)
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(m))
	for p := range m {
		out = append(out, p)
	}
	return out, nil
}

// CommitsForPath lists commits touching path since a time (newest first, up to 30).
func (h *Host) CommitsForPath(ctx context.Context, repo, path string, since time.Time) ([]ports.Commit, error) {
	o, r, err := split(repo)
	if err != nil {
		return nil, err
	}
	list, _, err := h.c.Repositories.ListCommits(ctx, o, r, &gh.CommitsListOptions{Path: path, Since: since, ListOptions: gh.ListOptions{PerPage: 30}})
	if err != nil {
		return nil, classify(err)
	}
	out := make([]ports.Commit, 0, len(list))
	for _, c := range list {
		out = append(out, ports.Commit{SHA: c.GetSHA(), Author: c.GetCommit().GetAuthor().GetName(), Message: c.GetCommit().GetMessage(),
			At: c.GetCommit().GetAuthor().GetDate().Time, URL: c.GetHTMLURL()})
	}
	return out, nil
}

// BotIdentity returns the App's bot user ("<slug>[bot]") or the token's user.
func (h *Host) BotIdentity(ctx context.Context) (ports.Identity, error) {
	if h.identity != nil {
		return *h.identity, nil
	}
	var id ports.Identity
	if h.appMode {
		app, _, err := h.c.Apps.Get(ctx, "")
		if err != nil {
			return id, classify(err)
		}
		login := app.GetSlug() + "[bot]"
		u, _, err := h.c.Users.Get(ctx, login)
		if err != nil {
			return id, classify(err)
		}
		id = ports.Identity{Login: login, Name: app.GetSlug(), Email: fmt.Sprintf("%d+%s@users.noreply.github.com", u.GetID(), login)}
	} else {
		u, _, err := h.c.Users.Get(ctx, "")
		if err != nil {
			return id, classify(err)
		}
		id = ports.Identity{Login: u.GetLogin(), Name: u.GetName(), Email: fmt.Sprintf("%d+%s@users.noreply.github.com", u.GetID(), u.GetLogin())}
	}
	h.identity = &id
	return id, nil
}

// CreateBranch creates refs/heads/name at fromSHA.
func (h *Host) CreateBranch(ctx context.Context, repo, name, fromSHA string) error {
	o, r, err := split(repo)
	if err != nil {
		return err
	}
	_, _, err = h.c.Git.CreateRef(ctx, o, r, gh.CreateRef{Ref: "refs/heads/" + name, SHA: fromSHA})
	if isStatus(err, 422) && strings.Contains(err.Error(), "already exists") {
		return fmt.Errorf("branch %s: %w", name, ports.ErrConflict)
	}
	return classify(err)
}

// DeleteBranch removes a branch; a missing branch is not an error.
func (h *Host) DeleteBranch(ctx context.Context, repo, name string) error {
	o, r, err := split(repo)
	if err != nil {
		return err
	}
	_, err = h.c.Git.DeleteRef(ctx, o, r, "heads/"+name)
	if err = classify(err); errors.Is(err, ports.ErrNotFound) || isStatus(err, 422) {
		return nil
	}
	return err
}

// CommitFiles writes all files in one commit via the Git Data API: tree on top of the parent's tree,
// commit, then a non-forced ref update (so a concurrent push surfaces as ErrConflict, never a clobber).
func (h *Host) CommitFiles(ctx context.Context, repo string, req ports.CommitRequest) (string, error) {
	o, r, err := split(repo)
	if err != nil {
		return "", err
	}
	parent := req.ParentSHA
	if parent == "" {
		if parent, err = h.BranchHead(ctx, repo, req.Branch); err != nil {
			return "", err
		}
	}
	pc, _, err := h.c.Git.GetCommit(ctx, o, r, parent)
	if err != nil {
		return "", classify(err)
	}
	entries := make([]*gh.TreeEntry, 0, len(req.Files))
	for _, f := range req.Files {
		e := &gh.TreeEntry{Path: gh.Ptr(f.Path), Mode: gh.Ptr("100644"), Type: gh.Ptr("blob")}
		if !f.Delete {
			e.Content = gh.Ptr(string(f.Content))
		}
		entries = append(entries, e)
	}
	tree, _, err := h.c.Git.CreateTree(ctx, o, r, pc.GetTree().GetSHA(), entries)
	if err != nil {
		return "", classify(err)
	}
	commit := gh.Commit{Message: gh.Ptr(req.Message), Tree: &gh.Tree{SHA: tree.SHA}, Parents: []*gh.Commit{{SHA: gh.Ptr(parent)}}}
	if req.Author.Name != "" {
		commit.Author = &gh.CommitAuthor{Name: gh.Ptr(req.Author.Name), Email: gh.Ptr(req.Author.Email)}
	}
	nc, _, err := h.c.Git.CreateCommit(ctx, o, r, commit, nil)
	if err != nil {
		return "", classify(err)
	}
	_, _, err = h.c.Git.UpdateRef(ctx, o, r, "heads/"+req.Branch, gh.UpdateRef{SHA: nc.GetSHA(), Force: gh.Ptr(false)})
	if err != nil {
		msg := strings.ToLower(err.Error())
		switch {
		case strings.Contains(msg, "protected branch") || isStatus(err, 403):
			return "", fmt.Errorf("%s: %w", req.Branch, ports.ErrProtectedBranch)
		case strings.Contains(msg, "fast forward"):
			return "", fmt.Errorf("%s moved while committing: %w", req.Branch, ports.ErrConflict)
		}
		return "", classify(err)
	}
	return nc.GetSHA(), nil
}

// OpenPR opens a PR and requests reviewers (users, or teams written as "org/slug").
func (h *Host) OpenPR(ctx context.Context, repo string, req ports.PRRequest) (ports.PullRequest, error) {
	o, r, err := split(repo)
	if err != nil {
		return ports.PullRequest{}, err
	}
	pr, _, err := h.c.PullRequests.Create(ctx, o, r, gh.CreatePullRequest{Title: gh.Ptr(req.Title), Head: req.Head,
		Base: req.Base, Body: gh.Ptr(req.Body)})
	if err != nil {
		return ports.PullRequest{}, classify(err)
	}
	if len(req.Reviewers) > 0 {
		var users, teams []string
		for _, rv := range req.Reviewers {
			if _, slug, ok := strings.Cut(rv, "/"); ok {
				teams = append(teams, slug)
			} else {
				users = append(users, strings.TrimPrefix(rv, "@"))
			}
		}
		if _, _, err := h.c.PullRequests.RequestReviewers(ctx, o, r, pr.GetNumber(), gh.ReviewersRequest{Reviewers: users, TeamReviewers: teams}); err != nil {
			return toPR(pr), classify(err)
		}
	}
	return h.GetPR(ctx, repo, pr.GetNumber())
}

func toPR(p *gh.PullRequest) ports.PullRequest {
	st := ports.PROpen
	switch {
	case p.GetMerged():
		st = ports.PRMerged
	case p.GetState() == "closed":
		st = ports.PRClosed
	}
	return ports.PullRequest{Number: p.GetNumber(), URL: p.GetHTMLURL(), State: st, Head: p.GetHead().GetRef(), Base: p.GetBase().GetRef(),
		HeadSHA: p.GetHead().GetSHA(), Mergeable: p.Mergeable, CreatedAt: p.GetCreatedAt().Time}
}

// GetPR returns PR state including aggregated checks and approval.
func (h *Host) GetPR(ctx context.Context, repo string, number int) (ports.PullRequest, error) {
	o, r, err := split(repo)
	if err != nil {
		return ports.PullRequest{}, err
	}
	p, _, err := h.c.PullRequests.Get(ctx, o, r, number)
	if err != nil {
		return ports.PullRequest{}, classify(err)
	}
	out := toPR(p)
	if out.Checks, err = h.checks(ctx, o, r, out.HeadSHA); err != nil {
		return out, err
	}
	reviews, _, err := h.c.PullRequests.ListReviews(ctx, o, r, number, &gh.ListOptions{PerPage: 100})
	if err != nil {
		return out, classify(err)
	}
	latest := map[string]string{}
	for _, rv := range reviews {
		if st := rv.GetState(); st == "APPROVED" || st == "CHANGES_REQUESTED" || st == "DISMISSED" {
			latest[rv.GetUser().GetLogin()] = st
		}
	}
	for _, st := range latest {
		if st == "CHANGES_REQUESTED" {
			out.Approved = false
			break
		}
		if st == "APPROVED" {
			out.Approved = true
		}
	}
	return out, nil
}

// checks aggregates commit statuses and check runs: any failure → failure, any pending → pending.
func (h *Host) checks(ctx context.Context, o, r, sha string) (ports.ChecksState, error) {
	if sha == "" {
		return ports.ChecksNone, nil
	}
	cs, _, err := h.c.Repositories.GetCombinedStatus(ctx, o, r, sha, nil)
	if err != nil {
		return "", classify(err)
	}
	runs, _, err := h.c.Checks.ListCheckRunsForRef(ctx, o, r, sha, &gh.ListCheckRunsOptions{ListOptions: gh.ListOptions{PerPage: 100}})
	if err != nil && !isStatus(err, 403) { // check runs need the checks permission; statuses alone still work
		return "", classify(err)
	}
	total, pending, failed := cs.GetTotalCount(), false, false
	switch cs.GetState() {
	case "failure", "error":
		failed = true
	case "pending":
		pending = cs.GetTotalCount() > 0
	}
	if runs != nil {
		for _, run := range runs.CheckRuns {
			total++
			if run.GetStatus() != "completed" {
				pending = true
			} else if c := run.GetConclusion(); c != "success" && c != "neutral" && c != "skipped" {
				failed = true
			}
		}
	}
	switch {
	case failed:
		return ports.ChecksFailure, nil
	case pending:
		return ports.ChecksPending, nil
	case total == 0:
		return ports.ChecksNone, nil
	}
	return ports.ChecksSuccess, nil
}

// MergePR squash-merges at the expected head SHA.
func (h *Host) MergePR(ctx context.Context, repo string, number int, sha string) error {
	o, r, err := split(repo)
	if err != nil {
		return err
	}
	_, _, err = h.c.PullRequests.Merge(ctx, o, r, number, "", &gh.PullRequestOptions{SHA: sha, MergeMethod: "squash"})
	switch {
	case isStatus(err, 409):
		return fmt.Errorf("PR #%d head moved: %w", number, ports.ErrConflict)
	case isStatus(err, 405):
		return fmt.Errorf("PR #%d not mergeable yet: %w", number, ports.ErrProtectedBranch)
	}
	return classify(err)
}

// ClosePR comments and closes a PR.
func (h *Host) ClosePR(ctx context.Context, repo string, number int, comment string) error {
	o, r, err := split(repo)
	if err != nil {
		return err
	}
	if comment != "" {
		if _, _, err := h.c.Issues.CreateComment(ctx, o, r, number, &gh.IssueComment{Body: gh.Ptr(comment)}); err != nil {
			return classify(err)
		}
	}
	_, _, err = h.c.PullRequests.Edit(ctx, o, r, number, &gh.PullRequest{State: gh.Ptr("closed")})
	return classify(err)
}

// UpdatePRBranch brings the PR branch up to date with its base.
func (h *Host) UpdatePRBranch(ctx context.Context, repo string, number int) error {
	o, r, err := split(repo)
	if err != nil {
		return err
	}
	_, _, err = h.c.PullRequests.UpdateBranch(ctx, o, r, number, nil)
	var accepted *gh.AcceptedError
	if errors.As(err, &accepted) {
		return nil // 202: the update is scheduled
	}
	if isStatus(err, 422) {
		return fmt.Errorf("PR #%d: %w", number, ports.ErrRebaseConflict)
	}
	return classify(err)
}

// ValidReviewer checks that a user is a collaborator, or that a team ("org/slug") exists with repo access.
func (h *Host) ValidReviewer(ctx context.Context, repo, reviewer string) (bool, error) {
	o, r, err := split(repo)
	if err != nil {
		return false, err
	}
	if org, slug, ok := strings.Cut(reviewer, "/"); ok {
		_, _, err := h.c.Teams.IsTeamRepoBySlug(ctx, strings.TrimPrefix(org, "@"), slug, o, r)
		if errors.Is(classify(err), ports.ErrNotFound) {
			return false, nil
		}
		return err == nil, classify(err)
	}
	ok, _, err := h.c.Repositories.IsCollaborator(ctx, o, r, strings.TrimPrefix(reviewer, "@"))
	if err != nil {
		return false, classify(err)
	}
	return ok, nil
}

// classify maps go-github errors onto typed errors.
func classify(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	var rl *gh.RateLimitError
	if errors.As(err, &rl) {
		return ports.TransientAfter(fmt.Errorf("github rate limit: %w", err), time.Until(rl.Rate.Reset.Time))
	}
	var ab *gh.AbuseRateLimitError
	if errors.As(err, &ab) {
		return ports.TransientAfter(fmt.Errorf("github secondary rate limit: %w", err), ab.GetRetryAfter())
	}
	var er *gh.ErrorResponse
	if errors.As(err, &er) && er.Response != nil {
		switch s := er.Response.StatusCode; {
		case s == http.StatusNotFound:
			return fmt.Errorf("github: %w", ports.ErrNotFound)
		case s == http.StatusTooManyRequests || s >= 500:
			return ports.Transient(fmt.Errorf("github %d: %w", s, err))
		default:
			return ports.Permanent(fmt.Errorf("github %d: %w", s, err))
		}
	}
	return ports.Transient(fmt.Errorf("github: %w", err))
}

func isStatus(err error, status int) bool {
	var er *gh.ErrorResponse
	return errors.As(err, &er) && er.Response != nil && er.Response.StatusCode == status
}
