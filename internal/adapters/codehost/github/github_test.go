package github

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"net/http"
	"sort"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/GokulMV/DocTheRepo/internal/ports"
	"github.com/GokulMV/DocTheRepo/test/mocks/githubmock"
)

func sp(s string) *string { return &s }

func setup(t *testing.T) (*Host, *githubmock.Server) {
	t.Helper()
	m := githubmock.New()
	t.Cleanup(m.Close)
	m.CreateRepo("acme/shop", "main", map[string]string{"main.go": "package main\n", "docs/README.md": "# hi\n"})
	h, err := New(ports.ProviderConfig{BaseURL: m.APIURL(), APIKey: "ghp_token", Extra: map[string]string{"auth": "token", "webhook_secret": "s3cret"}}, nil)
	require.NoError(t, err)
	return h, m
}

func TestReadOperations(t *testing.T) {
	h, m := setup(t)
	ctx := context.Background()
	assert.Equal(t, "github", h.Kind())
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
	assert.Equal(t, []string{"main.go", "pkg/new.go"}, tree)

	all, err := h.Compare(ctx, "acme/shop", "0000000000000000000000000000000000000000", head)
	require.NoError(t, err)
	assert.Len(t, all, 2, "a new branch compares everything as added")

	commits, err := h.CommitsForPath(ctx, "acme/shop", "main.go", time.Now().Add(-time.Hour))
	require.NoError(t, err)
	require.NotEmpty(t, commits)
	assert.Equal(t, "alice", commits[0].Author)

	repos, err := h.ListRepos(ctx)
	require.NoError(t, err, "token mode lists the user's repositories")
	assert.Equal(t, []string{"acme/shop"}, repos)
	_, err = h.DefaultBranch(ctx, "not-a-repo")
	var pe *ports.PermanentError
	assert.ErrorAs(t, err, &pe)
	_, err = h.DefaultBranch(ctx, "acme/missing")
	assert.ErrorIs(t, err, ports.ErrNotFound)
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

func sign(secret, body string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(body))
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

func TestWebhookVerifyAndParse(t *testing.T) {
	h, _ := setup(t)
	body := `{"ref":"refs/heads/main","before":"aaa","after":"bbb","repository":{"full_name":"acme/shop"},
	  "pusher":{"name":"alice"},"sender":{"login":"alice"},
	  "commits":[{"id":"bbb","author":{"email":"alice@acme.com","username":"alice"},"committer":{"email":"noreply@github.com"},
	   "added":["a.go"],"modified":["b.go"],"removed":["c.go"]}]}`
	hdr := http.Header{"X-Hub-Signature-256": {sign("s3cret", body)}, "X-Github-Event": {"push"}, "X-Github-Delivery": {"d-1"}}
	require.NoError(t, h.VerifyWebhook(hdr, []byte(body)))
	bad := hdr.Clone()
	bad.Set("X-Hub-Signature-256", sign("wrong", body))
	assert.ErrorIs(t, h.VerifyWebhook(bad, []byte(body)), ports.ErrInvalidSignature)
	assert.ErrorIs(t, h.VerifyWebhook(http.Header{}, []byte(body)), ports.ErrInvalidSignature)

	ev, err := h.ParseWebhook(hdr, []byte(body))
	require.NoError(t, err)
	pe := ev.(ports.PushEvent)
	assert.Equal(t, "acme/shop", pe.Repo)
	assert.Equal(t, "main", pe.Branch)
	assert.Equal(t, "bbb", pe.AfterSHA)
	assert.Equal(t, "d-1", pe.Delivery)
	assert.Contains(t, pe.Authors, "alice@acme.com")
	assert.Equal(t, []string{"a.go", "b.go", "c.go"}, pe.Paths)

	tag := `{"ref":"refs/tags/v1","repository":{"full_name":"acme/shop"}}`
	ev, err = h.ParseWebhook(http.Header{"X-Github-Event": {"push"}}, []byte(tag))
	require.NoError(t, err)
	assert.Nil(t, ev, "tag pushes are ignored")

	review := `{"action":"submitted","review":{"state":"approved","user":{"login":"bob"}},"pull_request":{"number":7},"repository":{"full_name":"acme/shop"}}`
	ev, err = h.ParseWebhook(http.Header{"X-Github-Event": {"pull_request_review"}}, []byte(review))
	require.NoError(t, err)
	assert.Equal(t, ports.ReviewEvent{Repo: "acme/shop", Number: 7, Approved: true, Reviewer: "bob"}, ev)

	ev, err = h.ParseWebhook(http.Header{"X-Github-Event": {"issues"}}, []byte(`{}`))
	assert.NoError(t, err)
	assert.Nil(t, ev)
	_, err = h.ParseWebhook(http.Header{"X-Github-Event": {"push"}}, []byte(`{`))
	var ve *ports.ValidationError
	assert.ErrorAs(t, err, &ve)

	noSecret, _ := New(ports.ProviderConfig{APIKey: "t", Extra: map[string]string{"auth": "token"}}, nil)
	assert.ErrorIs(t, noSecret.VerifyWebhook(hdr, []byte(body)), ports.ErrInvalidSignature, "no secret configured never accepts payloads")
}

func TestCommitFiles_DirectProtectedAndConflict(t *testing.T) {
	h, m := setup(t)
	ctx := context.Background()
	id, err := h.BotIdentity(ctx)
	require.NoError(t, err)
	assert.Equal(t, "dth-hub[bot]", id.Login)

	sha, err := h.CommitFiles(ctx, "acme/shop", ports.CommitRequest{Branch: "main", Message: "docs: update", Author: id,
		Files: []ports.FileChange{{Path: "docs/generated/main.go.md", Content: []byte("# main\n")}, {Path: "docs/README.md", Delete: true}}})
	require.NoError(t, err)
	assert.Equal(t, sha, m.Head("acme/shop", "main"))
	got, ok := m.File("acme/shop", "main", "docs/generated/main.go.md")
	assert.True(t, ok)
	assert.Equal(t, "# main\n", got)
	_, ok = m.File("acme/shop", "main", "docs/README.md")
	assert.False(t, ok, "deletion in the same commit")
	author, _ := m.CommitMeta(sha)
	assert.Equal(t, id.Name, author)

	stale := m.Head("acme/shop", "main")
	m.Push("acme/shop", "main", map[string]*string{"x.go": sp("package x\n")}, "bob")
	_, err = h.CommitFiles(ctx, "acme/shop", ports.CommitRequest{Branch: "main", ParentSHA: stale, Message: "m",
		Files: []ports.FileChange{{Path: "docs/generated/a.md", Content: []byte("a")}}})
	assert.ErrorIs(t, err, ports.ErrConflict, "a concurrent push is never clobbered")

	m.Protect("acme/shop", "main")
	_, err = h.CommitFiles(ctx, "acme/shop", ports.CommitRequest{Branch: "main", Message: "m",
		Files: []ports.FileChange{{Path: "docs/generated/a.md", Content: []byte("a")}}})
	assert.ErrorIs(t, err, ports.ErrProtectedBranch)
}

func TestPullRequestLifecycle(t *testing.T) {
	h, m := setup(t)
	ctx := context.Background()
	m.AddCollaborator("acme/shop", "bob")
	m.AddTeam("acme/docs")
	base := m.Head("acme/shop", "main")
	require.NoError(t, h.CreateBranch(ctx, "acme/shop", "dth/docs-1", base))
	assert.ErrorIs(t, h.CreateBranch(ctx, "acme/shop", "dth/docs-1", base), ports.ErrConflict)
	sha, err := h.CommitFiles(ctx, "acme/shop", ports.CommitRequest{Branch: "dth/docs-1", Message: "docs",
		Files: []ports.FileChange{{Path: "docs/generated/main.go.md", Content: []byte("x")}}})
	require.NoError(t, err)

	pr, err := h.OpenPR(ctx, "acme/shop", ports.PRRequest{Head: "dth/docs-1", Base: "main", Title: "docs: update", Body: "b", Reviewers: []string{"bob", "acme/docs"}})
	require.NoError(t, err)
	assert.Equal(t, ports.PROpen, pr.State)
	assert.Equal(t, sha, pr.HeadSHA)
	assert.Equal(t, ports.ChecksNone, pr.Checks, "GitHub reports pending/0 when no CI is configured: that is 'no checks'")
	prs := m.PRs("acme/shop")
	assert.Equal(t, []string{"bob"}, prs[0].Reviewers)
	assert.Equal(t, []string{"docs"}, prs[0].Teams)

	m.SetChecks(sha, "success")
	m.Approve("acme/shop", pr.Number)
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
	require.NoError(t, h.DeleteBranch(ctx, "acme/shop", "dth/docs-1"))
	require.NoError(t, h.DeleteBranch(ctx, "acme/shop", "dth/docs-1"), "deleting twice is fine")

	ok, err := h.ValidReviewer(ctx, "acme/shop", "bob")
	require.NoError(t, err)
	assert.True(t, ok)
	ok, err = h.ValidReviewer(ctx, "acme/shop", "mallory")
	require.NoError(t, err)
	assert.False(t, ok, "the approver-left-the-company case")
	ok, _ = h.ValidReviewer(ctx, "acme/shop", "acme/docs")
	assert.True(t, ok)
	ok, _ = h.ValidReviewer(ctx, "acme/shop", "acme/ghosts")
	assert.False(t, ok)
}

func TestUpdateBranchCloseAndProtectedMerge(t *testing.T) {
	h, m := setup(t)
	ctx := context.Background()
	m.Protect("acme/shop", "main")
	base := m.Head("acme/shop", "main")
	require.NoError(t, h.CreateBranch(ctx, "acme/shop", "dth/docs-2", base))
	sha, _ := h.CommitFiles(ctx, "acme/shop", ports.CommitRequest{Branch: "dth/docs-2", Message: "d", Files: []ports.FileChange{{Path: "docs/generated/x.md", Content: []byte("x")}}})
	pr, err := h.OpenPR(ctx, "acme/shop", ports.PRRequest{Head: "dth/docs-2", Base: "main", Title: "t"})
	require.NoError(t, err)
	assert.ErrorIs(t, h.MergePR(ctx, "acme/shop", pr.Number, sha), ports.ErrProtectedBranch, "unapproved PR on protected base")

	require.NoError(t, h.UpdatePRBranch(ctx, "acme/shop", pr.Number))
	m.ConflictOnUpdate = true
	assert.ErrorIs(t, h.UpdatePRBranch(ctx, "acme/shop", pr.Number), ports.ErrRebaseConflict)

	require.NoError(t, h.ClosePR(ctx, "acme/shop", pr.Number, "Superseded by a newer docs PR."))
	pr, _ = h.GetPR(ctx, "acme/shop", pr.Number)
	assert.Equal(t, ports.PRClosed, pr.State)
	assert.Equal(t, []string{"Superseded by a newer docs PR."}, m.PRs("acme/shop")[0].Comments)
}

func TestRegisterWebhook_CreatesThenUpdates(t *testing.T) {
	h, m := setup(t)
	ctx := context.Background()
	require.NoError(t, h.RegisterWebhook(ctx, "acme/shop", "https://dth.example.com/hooks/github/abc", "sec"))
	hooks := m.Hooks("acme/shop")
	require.Len(t, hooks, 1)
	assert.Equal(t, []any{"push", "pull_request_review"}, hooks[0]["events"])
}

func TestAppAuth_ExchangesInstallationToken(t *testing.T) {
	m := githubmock.New()
	defer m.Close()
	m.CreateRepo("acme/app", "main", map[string]string{"a.go": "package a\n"})
	key, _ := rsa.GenerateKey(rand.Reader, 2048)
	pemKey := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})
	h, err := New(ports.ProviderConfig{BaseURL: m.APIURL(), APIKey: string(pemKey), Extra: map[string]string{"app_id": "1", "installation_id": "99"}}, nil)
	require.NoError(t, err)
	repos, err := h.ListRepos(context.Background())
	require.NoError(t, err)
	assert.Equal(t, []string{"acme/app"}, repos)
	assert.Equal(t, 1, m.AppTokenRequests)
	id, err := h.BotIdentity(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "dth-hub[bot]", id.Login)
	assert.Contains(t, id.Email, "dth-hub[bot]@users.noreply.github.com")

	_, err = New(ports.ProviderConfig{Extra: map[string]string{"app_id": "x"}}, nil)
	assert.Error(t, err)
	_, err = New(ports.ProviderConfig{APIKey: "not a pem", Extra: map[string]string{"app_id": "1", "installation_id": "2"}}, nil)
	assert.Error(t, err)
	_, err = New(ports.ProviderConfig{Extra: map[string]string{"auth": "token"}}, nil)
	assert.Error(t, err)
}

func TestClassify(t *testing.T) {
	assert.Nil(t, classify(nil))
	assert.ErrorIs(t, classify(context.Canceled), context.Canceled)
	_, transient := ports.AsTransient(classify(errors.New("dial tcp: refused")))
	assert.True(t, transient)
}
