package gitlab

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/GokulMV/DocTheRepo/internal/ports"
	"github.com/GokulMV/DocTheRepo/test/mocks/gitlabmock"
)

func sp(s string) *string { return &s }

func setup(t *testing.T) (*Host, *gitlabmock.Server) {
	t.Helper()
	m := gitlabmock.New()
	t.Cleanup(m.Close)
	m.CreateProject("acme/shop", "main", map[string]string{"main.go": "package main\n", "docs/README.md": "# hi\n"})
	h, err := New(ports.ProviderConfig{BaseURL: m.BaseURL(), APIKey: "glpat-token", Extra: map[string]string{"webhook_secret": "s3cret"}})
	require.NoError(t, err)
	h.RebasePoll = 5 * time.Millisecond
	return h, m
}

func TestReadOperations(t *testing.T) {
	h, m := setup(t)
	ctx := context.Background()
	assert.Equal(t, "gitlab", h.Kind())
	db, err := h.DefaultBranch(ctx, "acme/shop")
	require.NoError(t, err)
	assert.Equal(t, "main", db)
	base, err := h.BranchHead(ctx, "acme/shop", "main")
	require.NoError(t, err)
	assert.Equal(t, m.Head("acme/shop", "main"), base)

	content := "package main\nfunc Hello() {}\n"
	head := m.Push("acme/shop", "main", map[string]*string{"main.go": &content, "pkg/new.go": sp("package pkg\n"), "docs/README.md": nil}, "alice")
	files, err := h.Compare(ctx, "acme/shop", base, head)
	require.NoError(t, err)
	got := map[string]ports.FileStatus{}
	for _, f := range files {
		got[f.Path] = f.Status
	}
	assert.Equal(t, map[string]ports.FileStatus{"main.go": ports.FileModified, "pkg/new.go": ports.FileAdded, "docs/README.md": ports.FileRemoved}, got)

	b, err := h.GetFile(ctx, "acme/shop", "main.go", head)
	require.NoError(t, err)
	assert.Equal(t, content, string(b))
	_, err = h.GetFile(ctx, "acme/shop", "docs/README.md", head)
	assert.ErrorIs(t, err, ports.ErrNotFound)

	tree, err := h.ListTree(ctx, "acme/shop", head)
	require.NoError(t, err)
	sort.Strings(tree)
	assert.Equal(t, []string{"main.go", "pkg/new.go"}, tree, "directories are not files")

	all, err := h.Compare(ctx, "acme/shop", "0000000000000000000000000000000000000000", head)
	require.NoError(t, err)
	assert.Len(t, all, 2, "a new branch compares everything as added")

	commits, err := h.CommitsForPath(ctx, "acme/shop", "main.go", time.Now().Add(-time.Hour))
	require.NoError(t, err)
	require.NotEmpty(t, commits)
	assert.Equal(t, "alice", commits[0].Author)
	assert.Equal(t, head, commits[0].SHA)

	_, err = h.DefaultBranch(ctx, "acme/missing")
	assert.ErrorIs(t, err, ports.ErrNotFound)
}

func TestListRepos_PaginatesAndFiltersByGroup(t *testing.T) {
	m := gitlabmock.New()
	defer m.Close()
	for i := range 130 {
		m.CreateProject(fmt.Sprintf("acme/svc-%03d", i), "main", map[string]string{"a.go": "package a\n"})
	}
	m.CreateProject("other/x", "main", map[string]string{"a.go": "package a\n"})
	h, err := New(ports.ProviderConfig{BaseURL: m.BaseURL(), APIKey: "t"})
	require.NoError(t, err)
	repos, err := h.ListRepos(context.Background())
	require.NoError(t, err)
	assert.Len(t, repos, 131)

	g, _ := New(ports.ProviderConfig{BaseURL: m.BaseURL(), APIKey: "t", Extra: map[string]string{"group": "acme"}})
	repos, err = g.ListRepos(context.Background())
	require.NoError(t, err)
	assert.Len(t, repos, 130)
	assert.NotContains(t, repos, "other/x")
}

func TestListTree_Paginates(t *testing.T) {
	h, m := setup(t)
	changes := map[string]*string{}
	for i := range 150 {
		changes[fmt.Sprintf("pkg/f%03d.go", i)] = sp("package pkg\n")
	}
	head := m.Push("acme/shop", "main", changes, "alice")
	tree, err := h.ListTree(context.Background(), "acme/shop", head)
	require.NoError(t, err)
	assert.Len(t, tree, 152)
}

func TestCompare_PureRenameHasFullSimilarity(t *testing.T) {
	h, m := setup(t)
	ctx := context.Background()
	base := m.Head("acme/shop", "main")
	head := m.Push("acme/shop", "main", map[string]*string{"main.go": nil, "cmd/main.go": sp("package main\n")}, "alice")
	files, err := h.Compare(ctx, "acme/shop", base, head)
	require.NoError(t, err)
	require.Len(t, files, 1)
	assert.Equal(t, ports.ChangedFile{Path: "cmd/main.go", PreviousPath: "main.go", Status: ports.FileRenamed, Similarity: 100}, files[0])
}

func TestWebhookVerifyAndParse(t *testing.T) {
	h, _ := setup(t)
	body := `{"object_kind":"push","ref":"refs/heads/main","before":"aaa","after":"bbb","user_username":"alice",
	  "project":{"path_with_namespace":"acme/shop"},
	  "commits":[{"id":"bbb","author":{"email":"alice@acme.com"},"added":["a.go"],"modified":["b.go"],"removed":["c.go"]}]}`
	hdr := http.Header{"X-Gitlab-Token": {"s3cret"}, "X-Gitlab-Event": {"Push Hook"}, "X-Gitlab-Event-Uuid": {"u-1"}}
	require.NoError(t, h.VerifyWebhook(hdr, []byte(body)))
	bad := hdr.Clone()
	bad.Set("X-Gitlab-Token", "nope")
	assert.ErrorIs(t, h.VerifyWebhook(bad, []byte(body)), ports.ErrInvalidSignature)
	assert.ErrorIs(t, h.VerifyWebhook(http.Header{}, []byte(body)), ports.ErrInvalidSignature)

	ev, err := h.ParseWebhook(hdr, []byte(body))
	require.NoError(t, err)
	pe := ev.(ports.PushEvent)
	assert.Equal(t, "acme/shop", pe.Repo)
	assert.Equal(t, "main", pe.Branch)
	assert.Equal(t, "bbb", pe.AfterSHA)
	assert.Equal(t, "u-1", pe.Delivery)
	assert.Equal(t, []string{"alice", "alice@acme.com"}, pe.Authors)
	assert.Equal(t, []string{"a.go", "b.go", "c.go"}, pe.Paths)
	assert.False(t, pe.Deleted)

	del := `{"object_kind":"push","ref":"refs/heads/old","before":"aaa","after":"0000000000000000000000000000000000000000","project":{"path_with_namespace":"acme/shop"}}`
	ev, err = h.ParseWebhook(hdr, []byte(del))
	require.NoError(t, err)
	assert.True(t, ev.(ports.PushEvent).Deleted)

	tag := `{"object_kind":"push","ref":"refs/tags/v1","project":{"path_with_namespace":"acme/shop"}}`
	ev, err = h.ParseWebhook(hdr, []byte(tag))
	require.NoError(t, err)
	assert.Nil(t, ev, "tag refs are ignored")

	mr := `{"object_kind":"merge_request","user":{"username":"bob"},"project":{"path_with_namespace":"acme/shop"},"object_attributes":{"iid":7,"action":"approved"}}`
	mrHdr := http.Header{"X-Gitlab-Event": {"Merge Request Hook"}}
	ev, err = h.ParseWebhook(mrHdr, []byte(mr))
	require.NoError(t, err)
	assert.Equal(t, ports.ReviewEvent{Repo: "acme/shop", Number: 7, Approved: true, Reviewer: "bob"}, ev)

	opened := `{"object_kind":"merge_request","project":{"path_with_namespace":"acme/shop"},"object_attributes":{"iid":7,"action":"open"}}`
	ev, err = h.ParseWebhook(mrHdr, []byte(opened))
	require.NoError(t, err)
	assert.Nil(t, ev)
	ev, err = h.ParseWebhook(http.Header{"X-Gitlab-Event": {"Issue Hook"}}, []byte(`{}`))
	assert.NoError(t, err)
	assert.Nil(t, ev)
	_, err = h.ParseWebhook(hdr, []byte(`{`))
	var ve *ports.ValidationError
	assert.ErrorAs(t, err, &ve)

	noSecret, _ := New(ports.ProviderConfig{APIKey: "t"})
	assert.ErrorIs(t, noSecret.VerifyWebhook(hdr, []byte(body)), ports.ErrInvalidSignature, "no secret configured never accepts payloads")
}

func TestCommitFiles_DirectProtectedAndConflict(t *testing.T) {
	h, m := setup(t)
	ctx := context.Background()
	id, err := h.BotIdentity(ctx)
	require.NoError(t, err)
	assert.Equal(t, "project_1_bot", id.Login)

	sha, err := h.CommitFiles(ctx, "acme/shop", ports.CommitRequest{Branch: "main", Message: "docs: update", Author: id,
		Files: []ports.FileChange{{Path: "docs/generated/main.go.md", Content: []byte("# main\n")}, {Path: "docs/README.md", Delete: true},
			{Path: "never/existed.md", Delete: true}}})
	require.NoError(t, err)
	assert.Equal(t, sha, m.Head("acme/shop", "main"))
	got, ok := m.File("acme/shop", "main", "docs/generated/main.go.md")
	assert.True(t, ok)
	assert.Equal(t, "# main\n", got)
	_, ok = m.File("acme/shop", "main", "docs/README.md")
	assert.False(t, ok)
	author, _ := m.CommitMeta(sha)
	assert.Equal(t, id.Name, author)

	sha2, err := h.CommitFiles(ctx, "acme/shop", ports.CommitRequest{Branch: "main", ParentSHA: sha, Message: "docs: again",
		Files: []ports.FileChange{{Path: "docs/generated/main.go.md", Content: []byte("# main v2\n")}}})
	require.NoError(t, err, "existing files are updated, not created")
	got, _ = m.File("acme/shop", "main", "docs/generated/main.go.md")
	assert.Equal(t, "# main v2\n", got)

	noop, err := h.CommitFiles(ctx, "acme/shop", ports.CommitRequest{Branch: "main", Message: "m", Files: []ports.FileChange{{Path: "gone.md", Delete: true}}})
	require.NoError(t, err)
	assert.Equal(t, sha2, noop, "nothing to do returns the head")

	m.Push("acme/shop", "main", map[string]*string{"x.go": sp("package x\n")}, "bob")
	_, err = h.CommitFiles(ctx, "acme/shop", ports.CommitRequest{Branch: "main", ParentSHA: sha2, Message: "m",
		Files: []ports.FileChange{{Path: "docs/generated/a.md", Content: []byte("a")}}})
	assert.ErrorIs(t, err, ports.ErrConflict, "a concurrent push is never clobbered")

	m.Protect("acme/shop", "main")
	_, err = h.CommitFiles(ctx, "acme/shop", ports.CommitRequest{Branch: "main", Message: "m",
		Files: []ports.FileChange{{Path: "docs/generated/a.md", Content: []byte("a")}}})
	assert.ErrorIs(t, err, ports.ErrProtectedBranch)
	_, err = h.CommitFiles(ctx, "acme/shop", ports.CommitRequest{Branch: "nope", Message: "m", Files: []ports.FileChange{{Path: "a", Content: []byte("a")}}})
	assert.ErrorIs(t, err, ports.ErrNotFound)
}

func TestMergeRequestLifecycle(t *testing.T) {
	h, m := setup(t)
	ctx := context.Background()
	m.AddUser("bob")
	m.AddGroup("acme/docs", "carol", "dave")
	base := m.Head("acme/shop", "main")
	require.NoError(t, h.CreateBranch(ctx, "acme/shop", "dth/docs-1", base))
	assert.ErrorIs(t, h.CreateBranch(ctx, "acme/shop", "dth/docs-1", base), ports.ErrConflict)
	sha, err := h.CommitFiles(ctx, "acme/shop", ports.CommitRequest{Branch: "dth/docs-1", Message: "docs",
		Files: []ports.FileChange{{Path: "docs/generated/main.go.md", Content: []byte("x")}}})
	require.NoError(t, err)

	pr, err := h.OpenPR(ctx, "acme/shop", ports.PRRequest{Head: "dth/docs-1", Base: "main", Title: "docs: update", Body: "b", Reviewers: []string{"@bob", "acme/docs"}})
	require.NoError(t, err)
	assert.Equal(t, ports.PROpen, pr.State)
	assert.Equal(t, sha, pr.HeadSHA)
	assert.Equal(t, ports.ChecksNone, pr.Checks)
	assert.False(t, pr.Approved)
	require.NotNil(t, pr.Mergeable)
	assert.Len(t, m.MRs("acme/shop")[0].ReviewerIDs, 3, "group reviewers expand to members")

	m.SetPipeline(sha, "running")
	pr, _ = h.GetPR(ctx, "acme/shop", pr.Number)
	assert.Equal(t, ports.ChecksPending, pr.Checks)
	m.SetPipeline(sha, "success")
	m.Approve("acme/shop", pr.Number, "bob")
	pr, err = h.GetPR(ctx, "acme/shop", pr.Number)
	require.NoError(t, err)
	assert.Equal(t, ports.ChecksSuccess, pr.Checks)
	assert.True(t, pr.Approved)

	assert.ErrorIs(t, h.MergePR(ctx, "acme/shop", pr.Number, "deadbeef"), ports.ErrConflict)
	require.NoError(t, h.MergePR(ctx, "acme/shop", pr.Number, sha))
	pr, _ = h.GetPR(ctx, "acme/shop", pr.Number)
	assert.Equal(t, ports.PRMerged, pr.State)
	got, _ := m.File("acme/shop", "main", "docs/generated/main.go.md")
	assert.Equal(t, "x", got)
	assert.Empty(t, m.Head("acme/shop", "dth/docs-1"), "source branch removed on merge")
	require.NoError(t, h.DeleteBranch(ctx, "acme/shop", "dth/docs-1"), "deleting a missing branch is fine")

	ok, err := h.ValidReviewer(ctx, "acme/shop", "bob")
	require.NoError(t, err)
	assert.True(t, ok)
	ok, err = h.ValidReviewer(ctx, "acme/shop", "mallory")
	require.NoError(t, err)
	assert.False(t, ok, "the approver-left-the-company case")
	ok, _ = h.ValidReviewer(ctx, "acme/shop", "acme/docs")
	assert.True(t, ok)
	ok, err = h.ValidReviewer(ctx, "acme/shop", "acme/ghosts")
	require.NoError(t, err)
	assert.False(t, ok)
}

func TestRebaseCloseAndProtectedMerge(t *testing.T) {
	h, m := setup(t)
	ctx := context.Background()
	m.Protect("acme/shop", "main")
	base := m.Head("acme/shop", "main")
	require.NoError(t, h.CreateBranch(ctx, "acme/shop", "dth/docs-2", base))
	sha, err := h.CommitFiles(ctx, "acme/shop", ports.CommitRequest{Branch: "dth/docs-2", Message: "d", Files: []ports.FileChange{{Path: "docs/generated/x.md", Content: []byte("x")}}})
	require.NoError(t, err)
	pr, err := h.OpenPR(ctx, "acme/shop", ports.PRRequest{Head: "dth/docs-2", Base: "main", Title: "t"})
	require.NoError(t, err)
	assert.ErrorIs(t, h.MergePR(ctx, "acme/shop", pr.Number, sha), ports.ErrProtectedBranch, "unapproved MR on protected target")

	m.Push("acme/shop", "main", map[string]*string{"y.go": sp("package y\n")}, "bob")
	require.NoError(t, h.UpdatePRBranch(ctx, "acme/shop", pr.Number))
	rebased, _ := m.File("acme/shop", "dth/docs-2", "y.go")
	assert.Equal(t, "package y\n", rebased, "the rebase brings in the target's new commits")

	m.ConflictOnRebase = true
	assert.ErrorIs(t, h.UpdatePRBranch(ctx, "acme/shop", pr.Number), ports.ErrRebaseConflict)
	pr, _ = h.GetPR(ctx, "acme/shop", pr.Number)
	require.NotNil(t, pr.Mergeable)
	assert.False(t, *pr.Mergeable)

	m.ConflictOnRebase = false
	m.RebasePolls = 1000
	h.RebaseWait = 20 * time.Millisecond
	_, transient := ports.AsTransient(h.UpdatePRBranch(ctx, "acme/shop", pr.Number))
	assert.True(t, transient, "a rebase that outlasts the wait is retried later")

	require.NoError(t, h.ClosePR(ctx, "acme/shop", pr.Number, "Superseded by a newer docs MR."))
	pr, _ = h.GetPR(ctx, "acme/shop", pr.Number)
	assert.Equal(t, ports.PRClosed, pr.State)
	assert.Equal(t, []string{"Superseded by a newer docs MR."}, m.MRs("acme/shop")[0].Notes)
	assert.Error(t, h.MergePR(ctx, "acme/shop", pr.Number, ""))
}

func TestRegisterWebhook_CreatesThenUpdates(t *testing.T) {
	h, m := setup(t)
	ctx := context.Background()
	require.NoError(t, h.RegisterWebhook(ctx, "acme/shop", "https://dth.example.com/hooks/gitlab/abc", "sec"))
	require.NoError(t, h.RegisterWebhook(ctx, "acme/shop", "https://dth.example.com/hooks/gitlab/abc", "rotated"))
	hooks := m.Hooks("acme/shop")
	require.Len(t, hooks, 1, "re-registering updates in place")
	assert.Equal(t, "rotated", hooks[0]["token"])
	assert.Equal(t, true, hooks[0]["merge_requests_events"])
}

func TestNewAndClassify(t *testing.T) {
	_, err := New(ports.ProviderConfig{})
	assert.Error(t, err)
	assert.Nil(t, classify(nil))
	assert.ErrorIs(t, classify(context.Canceled), context.Canceled)
	_, transient := ports.AsTransient(classify(errors.New("dial tcp: refused")))
	assert.True(t, transient)
}
