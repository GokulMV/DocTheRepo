package store_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/GokulMV/DocTheRepo/internal/adapters/secrets/localfile"
	"github.com/GokulMV/DocTheRepo/internal/core/library"
	"github.com/GokulMV/DocTheRepo/internal/core/palace"
	"github.com/GokulMV/DocTheRepo/internal/ports"
	"github.com/GokulMV/DocTheRepo/internal/secrets"
	"github.com/GokulMV/DocTheRepo/internal/store"
	"github.com/GokulMV/DocTheRepo/internal/store/storetest"
)

// fixture creates a GitHub connector and one repo.
func fixture(t *testing.T) (*store.Store, *store.Connectors, string, string) {
	t.Helper()
	st := storetest.New(t)
	kek, err := localfile.Open(filepath.Join(t.TempDir(), "k"))
	require.NoError(t, err)
	conns := store.NewConnectors(st, secrets.NewBox(kek), secrets.ConnectorCredsAAD, secrets.ConnectorWebhookAAD)
	ctx := context.Background()
	cid, err := conns.Create(ctx, store.NewConnector{Type: "github", Name: "gh", Credentials: "ghp_x", WebhookSecret: "s3cret",
		Config: map[string]string{"base_url": "https://api.github.com/"}})
	require.NoError(t, err)
	rid, err := store.NewRepos(st).Upsert(ctx, ports.RepoConfig{ConnectorID: cid, FullName: "acme/shop", DefaultBranch: "main"})
	require.NoError(t, err)
	return st, conns, cid, rid
}

func TestConnectorsAndRepos(t *testing.T) {
	st, conns, cid, rid := fixture(t)
	ctx := context.Background()
	cc, err := conns.Get(ctx, cid)
	require.NoError(t, err)
	assert.Equal(t, "ghp_x", cc.Credentials)
	assert.Equal(t, "s3cret", cc.WebhookSecret)
	assert.Equal(t, "https://api.github.com/", cc.Config["base_url"])
	assert.Equal(t, int64(60), cc.PollSeconds)
	_, err = conns.Get(ctx, ports.NewID())
	assert.ErrorIs(t, err, ports.ErrNotFound)
	list, err := conns.ListByType(ctx, "github", "gitlab")
	require.NoError(t, err)
	assert.Len(t, list, 1)
	require.NoError(t, conns.SetHealth(ctx, cid, nil))

	repos := store.NewRepos(st)
	rc, err := repos.Get(ctx, rid)
	require.NoError(t, err)
	assert.Equal(t, "github", rc.ConnectorType)
	assert.Equal(t, "docs/generated/", rc.DocsPath)
	assert.Equal(t, ports.PushConfig{Mode: ports.PushPRAutoMerge, OnReject: ports.RejectFallbackAutoMerge,
		ConflictStrategy: ports.ConflictAutoRebase, StaleAfter: 72 * time.Hour}, rc.Push)
	assert.Equal(t, "main", rc.Branch())

	again, err := repos.Upsert(ctx, ports.RepoConfig{ConnectorID: cid, FullName: "acme/shop", DefaultBranch: "main", TrackedBranch: "release",
		Push: ports.PushConfig{Mode: ports.PushPRWithApprover, Approver: "bob", StaleAfter: time.Hour}})
	require.NoError(t, err)
	assert.Equal(t, rid, again, "upsert keeps the ID")
	rc, _ = repos.ByName(ctx, cid, "acme/shop")
	assert.Equal(t, "release", rc.Branch())
	assert.Equal(t, time.Hour, rc.Push.StaleAfter)
	require.NoError(t, repos.SetLastProcessed(ctx, rid, "abc"))
	all, err := repos.ListEnabled(ctx)
	require.NoError(t, err)
	require.Len(t, all, 1)
	assert.Equal(t, "abc", all[0].LastProcessedSHA)
	_, err = repos.ByName(ctx, cid, "acme/none")
	assert.ErrorIs(t, err, ports.ErrNotFound)

	_, err = repos.Upsert(ctx, ports.RepoConfig{ConnectorID: cid, FullName: "acme/bad", Push: ports.PushConfig{OnReject: "yolo"}})
	assert.Error(t, err, "on_reject is constrained")
	require.NoError(t, store.NewSavings(st).Record(ctx, "triage_abort", 1200, 0.01, "job"))
}

func TestPRStore(t *testing.T) {
	st, _, _, rid := fixture(t)
	ctx := context.Background()
	prs := store.NewPRs(st)
	require.NoError(t, prs.SavePR(ctx, ports.PRRecord{RepoID: rid, Number: 3, URL: "u", Branch: "dth/docs-1", Base: "main",
		Mode: ports.PushPRAutoMerge, State: ports.PRStateOpen, ChunkIDs: []string{"a", "b"}, SourcePaths: []string{"main.go"}}))
	require.NoError(t, prs.SavePR(ctx, ports.PRRecord{RepoID: rid, Number: 4, URL: "u", Branch: "b", Base: "main", Mode: ports.PushPRWithApprover,
		State: ports.PRStateAwaitingReview, Approver: "bob"}))
	active, err := prs.ActivePRs(ctx, rid)
	require.NoError(t, err)
	assert.Len(t, active, 2)
	require.NoError(t, prs.SetPRState(ctx, rid, 3, ports.PRStateMerged, ""))
	all, err := prs.AllActivePRs(ctx)
	require.NoError(t, err)
	require.Len(t, all, 1)
	assert.Equal(t, 4, all[0].Number)
	got, err := prs.GetPR(ctx, rid, 3)
	require.NoError(t, err)
	assert.Equal(t, []string{"a", "b"}, got.ChunkIDs)
	assert.False(t, got.Active())
	assert.ErrorIs(t, prs.SetPRState(ctx, rid, 99, "closed", ""), ports.ErrNotFound)
	_, err = prs.GetPR(ctx, rid, 99)
	assert.ErrorIs(t, err, ports.ErrNotFound)
}

func TestChunksApply(t *testing.T) {
	st, _, _, rid := fixture(t)
	ctx := context.Background()
	cs := store.NewChunks(st)
	mk := func(id, content string) ports.Chunk {
		return ports.Chunk{ID: id, RepoID: rid, Scope: "acme/shop", Source: ports.SourceCode, Path: "main.go", Symbol: id, Language: "go",
			Content: content, ContentHash: content}
	}
	v0, _ := cs.IndexVersion(ctx)
	v, err := cs.Apply(ctx, store.ChunkWrite{Upserts: []ports.Chunk{mk("a", "1"), mk("b", "2")}})
	require.NoError(t, err)
	assert.Equal(t, v0+1, v)
	v, err = cs.Apply(ctx, store.ChunkWrite{})
	require.NoError(t, err)
	assert.Zero(t, v, "an empty write does not bump the version")

	_, err = cs.Apply(ctx, store.ChunkWrite{Remove: []string{"b"}})
	require.NoError(t, err)
	got, err := cs.ForPaths(ctx, rid, ports.SourceCode, []string{"main.go"})
	require.NoError(t, err)
	require.Len(t, got, 2, "soft-deleted chunks stay visible to the manifest diff")
	for _, c := range got {
		assert.Equal(t, c.ID == "a", c.Live())
	}
	_, err = cs.Apply(ctx, store.ChunkWrite{Revive: []string{"b"}, Drop: []string{"a"}})
	require.NoError(t, err)
	got, _ = cs.ByIDs(ctx, []string{"a", "b"})
	require.Len(t, got, 1)
	assert.True(t, got[0].Live())
	live, err := cs.LiveAfter(ctx, "", 10)
	require.NoError(t, err)
	assert.Len(t, live, 1)
	require.NoError(t, cs.RecordRename(ctx, rid, "old.go", "new.go", "sha"))
}

func TestGraphReplaceSource(t *testing.T) {
	st, _, _, rid := fixture(t)
	ctx := context.Background()
	g := store.NewGraph(st)
	var gr palace.Graph
	repo := gr.AddEntity(palace.Entity{Ref: palace.RepoRef("acme/shop"), Name: "acme/shop", Repo: "acme/shop"})
	fn := gr.AddEntity(palace.Entity{Ref: palace.SymbolRef("acme/shop", "main.go", "Hello"), Name: "Hello", Repo: "acme/shop"})
	env := gr.AddEntity(palace.Entity{Ref: palace.EnvVarRef("DB_URL"), Name: "DB_URL"})
	gr.AddEdge(repo, palace.EdgeContains, fn, palace.Evidence{Path: "main.go"})
	gr.AddEdge(fn, "reads_env", env, palace.Evidence{Path: "main.go", Line: 3})
	require.NoError(t, g.ReplaceSource(ctx, rid, "main.go", gr))
	edges, err := g.Edges(ctx, fn)
	require.NoError(t, err)
	require.Len(t, edges, 1)
	assert.Equal(t, env, edges[0].Dst)

	// Re-extraction without the function: its edges retire and the orphaned symbol goes with them.
	var gr2 palace.Graph
	gr2.AddEntity(palace.Entity{Ref: palace.RepoRef("acme/shop"), Name: "acme/shop", Repo: "acme/shop"})
	require.NoError(t, g.ReplaceSource(ctx, rid, "main.go", gr2))
	edges, err = g.Edges(ctx, repo)
	require.NoError(t, err)
	assert.Empty(t, edges)
	_, err = g.Edges(ctx, palace.Ref{Kind: "symbol", Key: "nope"})
	assert.ErrorIs(t, err, ports.ErrNotFound)
}

func TestDocsTreeAndShelves(t *testing.T) {
	st, _, _, rid := fixture(t)
	ctx := context.Background()
	cls, err := library.Compile(library.DefaultShelves())
	require.NoError(t, err)
	d := store.NewDocs(st, cls)
	require.NoError(t, d.SeedShelves(ctx))
	require.NoError(t, d.SeedShelves(ctx), "seeding is idempotent")
	require.NoError(t, d.ReplaceFile(ctx, rid, "acme/shop", "docs/adr/0001-postgres.md", "Use Postgres", []store.DocSection{{ChunkID: "c1", Title: "Decision"}}))
	require.NoError(t, d.ReplaceFile(ctx, rid, "acme/shop", "docs/adr/0001-postgres.md", "Use Postgres", []store.DocSection{{ChunkID: "c2", Title: "Context"}, {ChunkID: "c1", Title: "Decision"}}))
	roots, err := d.Children(ctx, rid, "")
	require.NoError(t, err)
	require.Len(t, roots, 1)
	dirs, _ := d.Children(ctx, rid, roots[0].ID)
	require.Len(t, dirs, 1)
	assert.Equal(t, "docs", dirs[0].Title)
	adr, _ := d.Children(ctx, rid, dirs[0].ID)
	files, _ := d.Children(ctx, rid, adr[0].ID)
	require.Len(t, files, 1)
	secs, _ := d.Children(ctx, rid, files[0].ID)
	require.Len(t, secs, 2, "sections are replaced, not appended")
	assert.Equal(t, "Context", secs[0].Title)
	items, err := d.ShelfItemIDs(ctx, "decisions")
	require.NoError(t, err)
	assert.Equal(t, []string{files[0].ID}, items)
	require.NoError(t, d.RemoveFile(ctx, rid, "docs/adr/0001-postgres.md"))
	files, _ = d.Children(ctx, rid, adr[0].ID)
	assert.Empty(t, files)
}
