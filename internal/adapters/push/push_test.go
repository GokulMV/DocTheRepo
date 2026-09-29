package push_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/GokulMV/DocTheRepo/internal/adapters/push"
	"github.com/GokulMV/DocTheRepo/internal/adapters/push/lifecycle"
	"github.com/GokulMV/DocTheRepo/internal/ports"
	"github.com/GokulMV/DocTheRepo/test/mocks/hostfixture"
	"github.com/GokulMV/DocTheRepo/test/mocks/memprs"
)

const repoID = "repo-1"

type env struct {
	f        *hostfixture.Fixture
	prs      *memprs.Store
	sweeper  *lifecycle.Sweeper
	d        *push.Dispatcher
	cfg      ports.PushConfig
	mu       sync.Mutex
	requeued []string
}

func newEnv(t *testing.T, f *hostfixture.Fixture, cfg ports.PushConfig) *env {
	e := &env{f: f, prs: memprs.New(), cfg: cfg}
	e.sweeper = &lifecycle.Sweeper{PRs: e.prs,
		Hosts: func(context.Context, string) (ports.CodeHost, ports.RepoConfig, error) {
			return f.Host, ports.RepoConfig{ID: repoID, FullName: hostfixture.Repo, DefaultBranch: "main", Push: e.cfg}, nil
		},
		Requeue: func(_ context.Context, rec ports.PRRecord, reason string) error {
			e.mu.Lock()
			defer e.mu.Unlock()
			e.requeued = append(e.requeued, reason)
			return nil
		}}
	e.d = &push.Dispatcher{PRs: e.prs, Lifecycle: e.sweeper}
	return e
}

func landing(job string, chunks ...string) ports.DocsLanding {
	return ports.DocsLanding{RepoID: repoID, Repo: hostfixture.Repo, Branch: "main", JobID: job, Message: "docs: update " + job,
		Title: "docs: update", Body: "generated", ChunkIDs: chunks, SourcePaths: []string{"main.go"},
		Files: []ports.FileChange{{Path: "docs/generated/main.go.md", Content: []byte("# main " + job + "\n")}}}
}

func each(t *testing.T, fn func(t *testing.T, f *hostfixture.Fixture)) {
	for _, mk := range []func(testing.TB, map[string]string) *hostfixture.Fixture{hostfixture.GitHub, hostfixture.GitLab} {
		f := mk(t, map[string]string{"main.go": "package main\n"})
		t.Run(f.Name, func(t *testing.T) { fn(t, f) })
	}
}

func TestAutoMerge_MergesWhenUnprotected(t *testing.T) {
	each(t, func(t *testing.T, f *hostfixture.Fixture) {
		e := newEnv(t, f, ports.PushConfig{Mode: ports.PushPRAutoMerge})
		res, err := e.d.Land(context.Background(), f.Host, e.cfg, landing("job1", "a"))
		require.NoError(t, err)
		assert.Equal(t, ports.PushPRAutoMerge, res.Mode)
		assert.Equal(t, ports.JobDone, res.Status)
		require.NotNil(t, res.PR)
		assert.Equal(t, ports.PRMerged, res.PR.State, "green and unprotected: merged on the first step")
		got, _ := f.File("main", "docs/generated/main.go.md")
		assert.Equal(t, "# main job1\n", got)
		assert.Empty(t, f.Head("dth/docs-job1"), "docs branch deleted")
		assert.Equal(t, ports.PRStateMerged, e.prs.All()[0].State)

		res, err = e.d.Land(context.Background(), f.Host, e.cfg, ports.DocsLanding{RepoID: repoID, Repo: hostfixture.Repo, Branch: "main", JobID: "j0"})
		require.NoError(t, err)
		assert.Nil(t, res.PR, "nothing to land")
	})
}

func TestAutoMerge_RetriedJobReusesItsPR(t *testing.T) {
	each(t, func(t *testing.T, f *hostfixture.Fixture) {
		f.Protect("main")
		e := newEnv(t, f, ports.PushConfig{Mode: ports.PushPRAutoMerge})
		a, err := e.d.Land(context.Background(), f.Host, e.cfg, landing("job1", "a"))
		require.NoError(t, err)
		b, err := e.d.Land(context.Background(), f.Host, e.cfg, landing("job1", "a"))
		require.NoError(t, err)
		assert.Equal(t, a.PR.Number, b.PR.Number)
		assert.Len(t, e.prs.All(), 1)
	})
}

func TestDirect_CommitsAndFallsBack(t *testing.T) {
	each(t, func(t *testing.T, f *hostfixture.Fixture) {
		ctx := context.Background()
		e := newEnv(t, f, ports.PushConfig{Mode: ports.PushDirect, OnReject: ports.RejectFallbackAutoMerge})
		res, err := e.d.Land(ctx, f.Host, e.cfg, landing("job1", "a"))
		require.NoError(t, err)
		assert.Equal(t, ports.PushDirect, res.Mode)
		assert.Equal(t, f.Head("main"), res.CommitSHA)

		f.Protect("main")
		res, err = e.d.Land(ctx, f.Host, e.cfg, landing("job2", "a"))
		require.NoError(t, err)
		assert.Equal(t, ports.PushPRAutoMerge, res.Mode)
		assert.Contains(t, res.Note, "fell back")
		require.NotNil(t, res.PR)
		assert.Equal(t, ports.PROpen, res.PR.State, "protection needs a review: the PR waits")

		e.cfg.OnReject = ports.RejectFailJob
		_, err = e.d.Land(ctx, f.Host, e.cfg, landing("job3", "a"))
		var pe *ports.PermanentError
		assert.ErrorAs(t, err, &pe)

		e.cfg.OnReject, e.cfg.Approver = ports.RejectFallbackApprover, "bob"
		f.AddReviewer("bob")
		res, err = e.d.Land(ctx, f.Host, e.cfg, landing("job4", "b"))
		require.NoError(t, err)
		assert.Equal(t, ports.PushPRWithApprover, res.Mode)
		assert.Equal(t, ports.JobPendingApproval, res.Status)

		_, err = e.d.Land(ctx, f.Host, ports.PushConfig{Mode: "yolo"}, landing("job5", "a"))
		assert.ErrorAs(t, err, &pe)
	})
}

func TestApprover_ReviewThenMerge(t *testing.T) {
	each(t, func(t *testing.T, f *hostfixture.Fixture) {
		ctx := context.Background()
		f.Protect("main")
		f.AddReviewer("bob")
		e := newEnv(t, f, ports.PushConfig{Mode: ports.PushPRWithApprover, Approver: "bob"})
		res, err := e.d.Land(ctx, f.Host, e.cfg, landing("job1", "a"))
		require.NoError(t, err)
		assert.Equal(t, ports.JobPendingApproval, res.Status)
		n := res.PR.Number
		rec, _ := e.prs.GetPR(ctx, repoID, n)
		assert.Equal(t, ports.PRStateAwaitingReview, rec.State)
		assert.Equal(t, "bob", rec.Approver)

		require.NoError(t, e.sweeper.Sweep(ctx))
		rec, _ = e.prs.GetPR(ctx, repoID, n)
		assert.Equal(t, ports.PRStateAwaitingReview, rec.State, "no review yet")

		f.Approve(n, "bob")
		state, err := e.sweeper.OnReview(ctx, repoID, n)
		require.NoError(t, err)
		assert.Equal(t, ports.PRStateMerged, state)
		got, _ := f.File("main", "docs/generated/main.go.md")
		assert.Equal(t, "# main job1\n", got)

		state, err = e.sweeper.OnReview(ctx, repoID, 999)
		require.NoError(t, err)
		assert.Empty(t, state, "reviews on non-docs PRs are ignored")
		_, err = (&push.Dispatcher{PRs: e.prs}).Land(ctx, f.Host, ports.PushConfig{Mode: ports.PushPRWithApprover}, landing("j9", "z"))
		assert.Error(t, err, "approver mode needs an approver")
	})
}

func TestApprover_InvalidFallsBack(t *testing.T) {
	each(t, func(t *testing.T, f *hostfixture.Fixture) {
		ctx := context.Background()
		e := newEnv(t, f, ports.PushConfig{Mode: ports.PushPRWithApprover, Approver: "ghost"})
		res, err := e.d.Land(ctx, f.Host, e.cfg, landing("job1", "a"))
		require.NoError(t, err)
		assert.Equal(t, ports.PushPRAutoMerge, res.Mode)
		assert.Contains(t, res.Note, "no longer exists")
		assert.Equal(t, ports.PRMerged, res.PR.State)
	})
}

func TestApprover_LeavesDuringReview_NeedsHuman(t *testing.T) {
	each(t, func(t *testing.T, f *hostfixture.Fixture) {
		ctx := context.Background()
		f.Protect("main")
		f.AddReviewer("bob")
		e := newEnv(t, f, ports.PushConfig{Mode: ports.PushPRWithApprover, Approver: "bob"})
		res, err := e.d.Land(ctx, f.Host, e.cfg, landing("job1", "a"))
		require.NoError(t, err)
		// bob leaves: the record's approver no longer validates.
		rec, _ := e.prs.GetPR(ctx, repoID, res.PR.Number)
		rec.Approver = "bob-left"
		require.NoError(t, e.prs.SavePR(ctx, rec))
		e.prs.Age(repoID, res.PR.Number, 2*time.Hour)
		require.NoError(t, e.sweeper.Sweep(ctx))
		rec, _ = e.prs.GetPR(ctx, repoID, res.PR.Number)
		assert.Equal(t, ports.PRStateNeedsHuman, rec.State, "fell back to auto-merge, which protection refuses")
		assert.Equal(t, ports.PushPRAutoMerge, rec.Mode)
	})
}

func TestConflictStrategies(t *testing.T) {
	for _, tc := range []struct {
		strategy, want string
		requeued   bool
	}{
		{ports.ConflictAutoRebase, ports.PRStateRequeued, true},
		{ports.ConflictRequeue, ports.PRStateRequeued, true},
		{ports.ConflictLeaveOpen, ports.PRStateNeedsHuman, false},
	} {
		t.Run(tc.strategy, func(t *testing.T) {
			each(t, func(t *testing.T, f *hostfixture.Fixture) {
				ctx := context.Background()
				f.Protect("main")
				e := newEnv(t, f, ports.PushConfig{Mode: ports.PushPRAutoMerge, ConflictStrategy: tc.strategy})
				res, err := e.d.Land(ctx, f.Host, e.cfg, landing("job1", "a"))
				require.NoError(t, err)
				f.Conflict(res.PR.Number)
				require.NoError(t, e.sweeper.Sweep(ctx))
				rec, _ := e.prs.GetPR(ctx, repoID, res.PR.Number)
				assert.Equal(t, tc.want, rec.State)
				assert.Equal(t, tc.requeued, len(e.requeued) == 1)
				if tc.requeued {
					pr, _ := f.Host.GetPR(ctx, hostfixture.Repo, res.PR.Number)
					assert.Equal(t, ports.PRClosed, pr.State)
				}
			})
		})
	}
}

func TestStaleChecksAndExternalStates(t *testing.T) {
	each(t, func(t *testing.T, f *hostfixture.Fixture) {
		ctx := context.Background()
		f.Protect("main")
		e := newEnv(t, f, ports.PushConfig{Mode: ports.PushPRAutoMerge, StaleAfter: time.Hour})
		stale, _ := e.d.Land(ctx, f.Host, e.cfg, landing("job1", "a"))
		failing, _ := e.d.Land(ctx, f.Host, e.cfg, landing("job2", "b"))
		pending, _ := e.d.Land(ctx, f.Host, e.cfg, landing("job3", "c"))
		gone, _ := e.d.Land(ctx, f.Host, e.cfg, landing("job4", "d"))
		e.prs.Age(repoID, stale.PR.Number, 2*time.Hour)
		f.SetChecks(f.Head("dth/docs-job2"), "failure")
		f.SetChecks(f.Head("dth/docs-job3"), "pending")
		require.NoError(t, f.Host.ClosePR(ctx, hostfixture.Repo, gone.PR.Number, ""))

		require.NoError(t, e.sweeper.Sweep(ctx))
		state := func(n int) string { r, _ := e.prs.GetPR(ctx, repoID, n); return r.State }
		assert.Equal(t, ports.PRStateStale, state(stale.PR.Number))
		assert.Equal(t, []string{ports.PRStateStale}, e.requeued)
		assert.Contains(t, f.PRComments(stale.PR.Number)[0], "open longer than 1h0m0s")
		assert.Equal(t, ports.PRStateNeedsHuman, state(failing.PR.Number))
		assert.Equal(t, ports.PRStateOpen, state(pending.PR.Number))
		assert.Equal(t, ports.PRStateClosed, state(gone.PR.Number))
	})
}

func TestSupersedesFullyCoveredPRs(t *testing.T) {
	each(t, func(t *testing.T, f *hostfixture.Fixture) {
		ctx := context.Background()
		f.Protect("main")
		e := newEnv(t, f, ports.PushConfig{Mode: ports.PushPRAutoMerge})
		old, _ := e.d.Land(ctx, f.Host, e.cfg, landing("job1", "a"))
		res, err := e.d.Land(ctx, f.Host, e.cfg, landing("job2", "a", "z"))
		require.NoError(t, err)
		assert.Equal(t, []int{old.PR.Number}, res.Superseded)
		partial := res
		res, err = e.d.Land(ctx, f.Host, e.cfg, landing("job3", "a", "b"))
		require.NoError(t, err)
		assert.Empty(t, res.Superseded)
		r, _ := e.prs.GetPR(ctx, repoID, old.PR.Number)
		assert.Equal(t, ports.PRStateSuperseded, r.State)
		pr, _ := f.Host.GetPR(ctx, hostfixture.Repo, old.PR.Number)
		assert.Equal(t, ports.PRClosed, pr.State)
		assert.Contains(t, f.PRComments(old.PR.Number)[0], "Superseded by")
		r, _ = e.prs.GetPR(ctx, repoID, partial.PR.Number)
		assert.Equal(t, ports.PRStateOpen, r.State, "partial overlap is not superseded")
	})
}

func TestSweepSurvivesResolverErrors(t *testing.T) {
	prs := memprs.New()
	require.NoError(t, prs.SavePR(context.Background(), ports.PRRecord{RepoID: "x", Number: 1, State: ports.PRStateOpen}))
	s := &lifecycle.Sweeper{PRs: prs, Hosts: func(context.Context, string) (ports.CodeHost, ports.RepoConfig, error) {
		return nil, ports.RepoConfig{}, errors.New("connector gone")
	}}
	assert.Error(t, s.Sweep(context.Background()))
}
