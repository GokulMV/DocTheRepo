package parity

import (
	"context"
	"sort"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/GokulMV/DocTheRepo/internal/ports"
	"github.com/GokulMV/DocTheRepo/test/mocks/hostfixture"
)

var seed = map[string]string{"main.go": "package main\n", "docs/README.md": "# hi\n"}

// TestCodeHostParity runs every row against GitHub and GitLab. The pipeline and push adapters only see
// ports.CodeHost, so any divergence here would surface as host-specific behavior in docs PRs.
func TestCodeHostParity(t *testing.T) {
	rows := []struct {
		name string
		run  func(t *testing.T, f *hostfixture.Fixture)
	}{
		{"compare statuses and renames", func(t *testing.T, f *hostfixture.Fixture) {
			ctx := context.Background()
			base := f.Head("main")
			head := f.Push("main", map[string]*string{"main.go": hostfixture.S("package main\n// v2\n"), "pkg/a.go": hostfixture.S("package pkg\n"),
				"docs/README.md": nil}, "alice")
			head2 := f.Push("main", map[string]*string{"pkg/a.go": nil, "internal/a.go": hostfixture.S("package pkg\n")}, "alice")
			files, err := f.Host.Compare(ctx, hostfixture.Repo, base, head)
			require.NoError(t, err)
			sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
			assert.Equal(t, []ports.ChangedFile{{Path: "docs/README.md", Status: ports.FileRemoved}, {Path: "main.go", Status: ports.FileModified},
				{Path: "pkg/a.go", Status: ports.FileAdded}}, files)
			files, err = f.Host.Compare(ctx, hostfixture.Repo, head, head2)
			require.NoError(t, err)
			assert.Equal(t, []ports.ChangedFile{{Path: "internal/a.go", PreviousPath: "pkg/a.go", Status: ports.FileRenamed, Similarity: 100}}, files)
		}},
		{"missing file is ErrNotFound", func(t *testing.T, f *hostfixture.Fixture) {
			_, err := f.Host.GetFile(context.Background(), hostfixture.Repo, "nope.go", f.Head("main"))
			assert.ErrorIs(t, err, ports.ErrNotFound)
			_, err = f.Host.BranchHead(context.Background(), hostfixture.Repo, "nope")
			assert.ErrorIs(t, err, ports.ErrNotFound)
		}},
		{"tree lists files only", func(t *testing.T, f *hostfixture.Fixture) {
			tree, err := f.Host.ListTree(context.Background(), hostfixture.Repo, "main")
			require.NoError(t, err)
			sort.Strings(tree)
			assert.Equal(t, []string{"docs/README.md", "main.go"}, tree)
		}},
		{"commits for path", func(t *testing.T, f *hostfixture.Fixture) {
			sha := f.Push("main", map[string]*string{"main.go": hostfixture.S("package main\n// x\n")}, "alice")
			cs, err := f.Host.CommitsForPath(context.Background(), hostfixture.Repo, "main.go", time.Now().Add(-time.Hour))
			require.NoError(t, err)
			require.NotEmpty(t, cs)
			assert.Equal(t, sha, cs[0].SHA)
		}},
		{"optimistic commit and protection", func(t *testing.T, f *hostfixture.Fixture) {
			ctx := context.Background()
			parent := f.Head("main")
			sha, err := f.Host.CommitFiles(ctx, hostfixture.Repo, ports.CommitRequest{Branch: "main", ParentSHA: parent, Message: "docs",
				Files: []ports.FileChange{{Path: "docs/generated/main.go.md", Content: []byte("# m\n")}, {Path: "docs/README.md", Delete: true}}})
			require.NoError(t, err)
			assert.Equal(t, sha, f.Head("main"))
			_, ok := f.File("main", "docs/README.md")
			assert.False(t, ok)
			_, err = f.Host.CommitFiles(ctx, hostfixture.Repo, ports.CommitRequest{Branch: "main", ParentSHA: parent, Message: "docs",
				Files: []ports.FileChange{{Path: "docs/generated/x.md", Content: []byte("x")}}})
			assert.ErrorIs(t, err, ports.ErrConflict)
			f.Protect("main")
			_, err = f.Host.CommitFiles(ctx, hostfixture.Repo, ports.CommitRequest{Branch: "main", Message: "docs",
				Files: []ports.FileChange{{Path: "docs/generated/x.md", Content: []byte("x")}}})
			assert.ErrorIs(t, err, ports.ErrProtectedBranch)
		}},
		{"PR open → checks → approve → merge", func(t *testing.T, f *hostfixture.Fixture) {
			ctx := context.Background()
			f.Protect("main")
			f.AddReviewer("bob")
			require.NoError(t, f.Host.CreateBranch(ctx, hostfixture.Repo, "dth/docs", f.Head("main")))
			assert.ErrorIs(t, f.Host.CreateBranch(ctx, hostfixture.Repo, "dth/docs", f.Head("main")), ports.ErrConflict)
			sha, err := f.Host.CommitFiles(ctx, hostfixture.Repo, ports.CommitRequest{Branch: "dth/docs", Message: "docs",
				Files: []ports.FileChange{{Path: "docs/generated/a.md", Content: []byte("a")}}})
			require.NoError(t, err)
			pr, err := f.Host.OpenPR(ctx, hostfixture.Repo, ports.PRRequest{Head: "dth/docs", Base: "main", Title: "docs", Reviewers: []string{"bob"}})
			require.NoError(t, err)
			assert.Equal(t, ports.PROpen, pr.State)
			assert.Equal(t, ports.ChecksNone, pr.Checks)
			assert.Equal(t, sha, pr.HeadSHA)
			assert.ErrorIs(t, f.Host.MergePR(ctx, hostfixture.Repo, pr.Number, sha), ports.ErrProtectedBranch)
			f.SetChecks(sha, "pending")
			pr, _ = f.Host.GetPR(ctx, hostfixture.Repo, pr.Number)
			assert.Equal(t, ports.ChecksPending, pr.Checks)
			f.SetChecks(sha, "failure")
			pr, _ = f.Host.GetPR(ctx, hostfixture.Repo, pr.Number)
			assert.Equal(t, ports.ChecksFailure, pr.Checks)
			f.SetChecks(sha, "success")
			f.Approve(pr.Number, "bob")
			pr, _ = f.Host.GetPR(ctx, hostfixture.Repo, pr.Number)
			assert.Equal(t, ports.ChecksSuccess, pr.Checks)
			assert.True(t, pr.Approved)
			assert.ErrorIs(t, f.Host.MergePR(ctx, hostfixture.Repo, pr.Number, "0123456789abcdef0123456789abcdef01234567"), ports.ErrConflict)
			require.NoError(t, f.Host.MergePR(ctx, hostfixture.Repo, pr.Number, sha))
			pr, _ = f.Host.GetPR(ctx, hostfixture.Repo, pr.Number)
			assert.Equal(t, ports.PRMerged, pr.State)
			got, _ := f.File("main", "docs/generated/a.md")
			assert.Equal(t, "a", got)
			require.NoError(t, f.Host.DeleteBranch(ctx, hostfixture.Repo, "dth/docs"))
		}},
		{"update branch, conflict, close with comment", func(t *testing.T, f *hostfixture.Fixture) {
			ctx := context.Background()
			require.NoError(t, f.Host.CreateBranch(ctx, hostfixture.Repo, "dth/docs", f.Head("main")))
			_, err := f.Host.CommitFiles(ctx, hostfixture.Repo, ports.CommitRequest{Branch: "dth/docs", Message: "docs",
				Files: []ports.FileChange{{Path: "docs/generated/a.md", Content: []byte("a")}}})
			require.NoError(t, err)
			pr, err := f.Host.OpenPR(ctx, hostfixture.Repo, ports.PRRequest{Head: "dth/docs", Base: "main", Title: "docs"})
			require.NoError(t, err)
			f.Push("main", map[string]*string{"z.go": hostfixture.S("package z\n")}, "carol")
			require.NoError(t, f.Host.UpdatePRBranch(ctx, hostfixture.Repo, pr.Number))
			got, ok := f.File("dth/docs", "z.go")
			assert.True(t, ok)
			assert.Equal(t, "package z\n", got)
			f.ConflictOnUpdate(true)
			assert.ErrorIs(t, f.Host.UpdatePRBranch(ctx, hostfixture.Repo, pr.Number), ports.ErrRebaseConflict)
			require.NoError(t, f.Host.ClosePR(ctx, hostfixture.Repo, pr.Number, "superseded"))
			pr, _ = f.Host.GetPR(ctx, hostfixture.Repo, pr.Number)
			assert.Equal(t, ports.PRClosed, pr.State)
			assert.Equal(t, []string{"superseded"}, f.PRComments(pr.Number))
		}},
		{"reviewer validity", func(t *testing.T, f *hostfixture.Fixture) {
			f.AddReviewer("bob")
			ok, err := f.Host.ValidReviewer(context.Background(), hostfixture.Repo, "bob")
			require.NoError(t, err)
			assert.True(t, ok)
			ok, err = f.Host.ValidReviewer(context.Background(), hostfixture.Repo, "ghost")
			require.NoError(t, err)
			assert.False(t, ok)
		}},
		{"bot identity", func(t *testing.T, f *hostfixture.Fixture) {
			id, err := f.Host.BotIdentity(context.Background())
			require.NoError(t, err)
			assert.Equal(t, f.BotLogin, id.Login)
			assert.NotEmpty(t, id.Email)
		}},
	}
	for _, row := range rows {
		for _, mk := range []func(testing.TB, map[string]string) *hostfixture.Fixture{hostfixture.GitHub, hostfixture.GitLab} {
			f := mk(t, seed)
			t.Run(f.Name+"/"+row.name, func(t *testing.T) { row.run(t, f) })
		}
	}
}
