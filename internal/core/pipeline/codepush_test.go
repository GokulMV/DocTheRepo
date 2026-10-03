package pipeline_test

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/GokulMV/DocTheRepo/internal/adapters/push"
	"github.com/GokulMV/DocTheRepo/internal/adapters/push/lifecycle"
	"github.com/GokulMV/DocTheRepo/internal/adapters/secrets/localfile"
	"github.com/GokulMV/DocTheRepo/internal/adapters/vector/pgvector"
	"github.com/GokulMV/DocTheRepo/internal/core/docgen"
	"github.com/GokulMV/DocTheRepo/internal/core/grammars"
	"github.com/GokulMV/DocTheRepo/internal/core/library"
	"github.com/GokulMV/DocTheRepo/internal/core/palace"
	"github.com/GokulMV/DocTheRepo/internal/core/pipeline"
	"github.com/GokulMV/DocTheRepo/internal/ports"
	"github.com/GokulMV/DocTheRepo/internal/queue"
	"github.com/GokulMV/DocTheRepo/internal/secrets"
	"github.com/GokulMV/DocTheRepo/internal/store"
	"github.com/GokulMV/DocTheRepo/internal/store/storetest"
	"github.com/GokulMV/DocTheRepo/test/mocks/fakegw"
	"github.com/GokulMV/DocTheRepo/test/mocks/hostfixture"
)

const mainV1 = "package main\n\nimport \"os\"\n\n// Hello greets.\nfunc Hello() string { return \"hi \" + os.Getenv(\"GREETING\") }\n"

type harness struct {
	t      *testing.T
	st     *store.Store
	f      *hostfixture.Fixture
	fg     *fakegw.Env
	p      *pipeline.Pipeline
	repoID string
	index  *pgvector.Index
}

func newHarness(t *testing.T, f *hostfixture.Fixture, o fakegw.Options) *harness {
	t.Helper()
	ctx := context.Background()
	st := storetest.New(t)
	kek, err := localfile.Open(filepath.Join(t.TempDir(), "k"))
	require.NoError(t, err)
	conns := store.NewConnectors(st, secrets.NewBox(kek), secrets.ConnectorCredsAAD, secrets.ConnectorWebhookAAD)
	cid, err := conns.Create(ctx, store.NewConnector{Type: f.Name, Name: f.Name})
	require.NoError(t, err)
	repos := store.NewRepos(st)
	rid, err := repos.Upsert(ctx, ports.RepoConfig{ConnectorID: cid, FullName: hostfixture.Repo, DefaultBranch: "main", ServiceName: "shop"})
	require.NoError(t, err)
	cls, err := library.Compile(library.DefaultShelves())
	require.NoError(t, err)
	docs := store.NewDocs(st, cls)
	require.NoError(t, docs.SeedShelves(ctx))
	fg := fakegw.New(o)
	idx := pgvector.New(st.Pool)
	prs := store.NewPRs(st)
	sweeper := &lifecycle.Sweeper{PRs: prs}
	h := &harness{t: t, st: st, f: f, fg: fg, repoID: rid, index: idx}
	h.p = &pipeline.Pipeline{Repos: repos, Chunks: store.NewChunks(st), Graph: store.NewGraph(st), Docs: docs, Savings: store.NewSavings(st),
		Hosts:  func(context.Context, string) (ports.CodeHost, error) { return f.Host, nil },
		Lander: &push.Dispatcher{PRs: prs, Lifecycle: sweeper}, GW: fg.GW, DocGen: &docgen.Generator{GW: fg.GW},
		Indexer: &pipeline.Indexer{GW: fg.GW, Index: idx}, Grammars: grammars.NewBuiltin()}
	return h
}

func (h *harness) run(pl pipeline.CodePushPayload) (ports.Outcome, pipeline.CodePushResult) {
	h.t.Helper()
	pl.RepoID = h.repoID
	job, _, err := queue.New(h.st, queue.Options{}).Enqueue(context.Background(), ports.NewJob{Type: ports.JobCodePush, RepoID: h.repoID, Payload: pl})
	require.NoError(h.t, err)
	out, err := h.p.CodePush(context.Background(), job)
	require.NoError(h.t, err)
	var res pipeline.CodePushResult
	if out.Result != nil {
		rb, _ := json.Marshal(out.Result)
		require.NoError(h.t, json.Unmarshal(rb, &res))
	}
	return out, res
}

func (h *harness) push(changes map[string]*string) pipeline.CodePushPayload {
	before := h.f.Head("main")
	after := h.f.Push("main", changes, "alice")
	return pipeline.CodePushPayload{Before: before, After: after}
}

func (h *harness) doc(p string) string {
	s, _ := h.f.File("main", p)
	return s
}

func eachHost(t *testing.T, fn func(t *testing.T, f *hostfixture.Fixture)) {
	for _, mk := range []func(testing.TB, map[string]string) *hostfixture.Fixture{hostfixture.GitHub, hostfixture.GitLab} {
		f := mk(t, map[string]string{"README.md": "# shop\n"})
		t.Run(f.Name, func(t *testing.T) { fn(t, f) })
	}
}

func TestCodePush_EndToEnd(t *testing.T) {
	eachHost(t, func(t *testing.T, f *hostfixture.Fixture) {
		ctx := context.Background()
		h := newHarness(t, f, fakegw.Options{})

		// 1. A real change: new function → documented, landed via auto-merge PR, indexed.
		first := h.push(map[string]*string{"cmd/main.go": hostfixture.S(mainV1)})
		out, res := h.run(first)
		require.Equal(t, ports.JobDone, out.Status, out.Message)
		assert.GreaterOrEqual(t, res.Documented, 1)
		doc := h.doc("docs/generated/cmd/main.go.md")
		assert.Contains(t, doc, "Documents `Hello`.")
		assert.Contains(t, doc, "<!-- dth:chunk ")
		assert.Contains(t, h.doc("docs/generated/cmd/README.md"), "[`main.go`](main.go.md) — Generated summary.")
		assert.Contains(t, h.doc("docs/generated/README.md"), "[`cmd`](cmd/README.md)", "ancestor index lists the new directory")
		require.NotNil(t, res.Landing)
		assert.Equal(t, ports.PRMerged, res.Landing.PR.State)
		assert.Greater(t, res.Embedded, 0)
		hits, err := h.index.Search(ctx, fakegw.Vector("x", fakegw.Dims), 20, ports.VectorFilter{RepoIDs: []string{h.repoID}})
		require.NoError(t, err)
		assert.NotEmpty(t, hits, "code and doc chunks are searchable")
		rc, _ := h.p.Repos.Get(ctx, h.repoID)
		assert.Equal(t, first.After, rc.LastProcessedSHA, "the pushed commit is processed; the docs merge after it is filtered next time")
		edges, err := store.NewGraph(h.st).Edges(ctx, palace.SymbolRef(hostfixture.Repo, "cmd/main.go", "Hello"))
		require.NoError(t, err)
		assert.NotEmpty(t, edges, "Palace: Hello reads GREETING")
		nodes, err := store.NewDocs(h.st, nil).Children(ctx, h.repoID, "")
		require.NoError(t, err)
		assert.Len(t, nodes, 1, "Tree root")
		calls := h.fg.Model.DocgenCalls()

		// 2. Comment-only change: triage aborts with zero spend.
		out, _ = h.run(h.push(map[string]*string{"cmd/main.go": hostfixture.S(strings.Replace(mainV1, "// Hello greets.", "// Hello greets you.", 1))}))
		assert.Equal(t, ports.JobAborted, out.Status)
		assert.Equal(t, calls, h.fg.Model.DocgenCalls())
		out, _ = h.run(pipeline.CodePushPayload{})
		assert.Equal(t, ports.JobAborted, out.Status, "nothing new since the last processed head")

		// 3. Add a function and remove nothing: only the new chunk is documented.
		v2 := mainV1 + "\n// Bye says goodbye.\nfunc Bye() string { return \"bye\" }\n"
		out, res = h.run(h.push(map[string]*string{"cmd/main.go": hostfixture.S(v2)}))
		require.Equal(t, ports.JobDone, out.Status, out.Message)
		assert.Equal(t, 1, res.Documented)
		doc = h.doc("docs/generated/cmd/main.go.md")
		assert.Contains(t, doc, "Documents `Bye`.")
		assert.Contains(t, doc, "Documents `Hello`.", "existing sections are kept")

		// 4. A human edit inside a dth:human block survives regeneration.
		human := strings.Replace(doc, "Documents `Bye`.", "Documents `Bye`.\n\n<!-- dth:human -->\nCalled at shutdown.\n<!-- dth:end -->", 1)
		h.f.Push("main", map[string]*string{"docs/generated/cmd/main.go.md": &human}, "bob")
		out, _ = h.run(h.push(map[string]*string{"cmd/main.go": hostfixture.S(strings.Replace(v2, "func Bye() string { return \"bye\" }", "func Bye(name string) string { return \"bye \" + name }", 1))}))
		require.Equal(t, ports.JobDone, out.Status, out.Message)
		assert.Contains(t, h.doc("docs/generated/cmd/main.go.md"), "Called at shutdown.")

		// 5. Remove a function: its section goes.
		out, _ = h.run(h.push(map[string]*string{"cmd/main.go": hostfixture.S(mainV1)}))
		require.Equal(t, ports.JobDone, out.Status, out.Message)
		assert.NotContains(t, h.doc("docs/generated/cmd/main.go.md"), "Documents `Bye`.")

		// 6. Pure rename: docs move, chunks are re-keyed, nothing is regenerated.
		calls = h.fg.Model.DocgenCalls()
		out, res = h.run(h.push(map[string]*string{"cmd/main.go": nil, "app/main.go": hostfixture.S(mainV1)}))
		require.Equal(t, ports.JobDone, out.Status, out.Message)
		assert.Equal(t, calls, h.fg.Model.DocgenCalls(), "a rename costs no docgen call")
		assert.Equal(t, 1, res.Renamed)
		assert.Contains(t, h.doc("docs/generated/app/main.go.md"), "Documents `Hello`.")
		_, still := h.f.File("main", "docs/generated/cmd/main.go.md")
		assert.False(t, still, "old doc removed")
		_, idx := h.f.File("main", "docs/generated/cmd/README.md")
		assert.False(t, idx, "an emptied directory index is removed")

		// 7. Delete the file: its doc goes too.
		out, _ = h.run(h.push(map[string]*string{"app/main.go": nil}))
		require.Equal(t, ports.JobDone, out.Status, out.Message)
		_, still = h.f.File("main", "docs/generated/app/main.go.md")
		assert.False(t, still)
	})
}

func TestCodePush_SpendBlockedWritesNothing(t *testing.T) {
	f := hostfixture.GitHub(t, map[string]string{"README.md": "# shop\n"})
	h := newHarness(t, f, fakegw.Options{TokenLimit: 5})
	out, _ := h.run(h.push(map[string]*string{"main.go": hostfixture.S(mainV1)}))
	assert.Equal(t, ports.JobSpendBlocked, out.Status)
	assert.Contains(t, out.Message, "ceiling")
	_, landed := f.File("main", "docs/generated/main.go.md")
	assert.False(t, landed)
	rc, _ := h.p.Repos.Get(context.Background(), h.repoID)
	assert.Empty(t, rc.LastProcessedSHA, "a blocked push is retried later, not skipped")
	// The architecture still follows the code: the graph is written before any model call.
	var n int
	require.NoError(t, h.st.Pool.QueryRow(context.Background(),
		"SELECT count(*) FROM entities WHERE repo_id = $1 AND kind = 'symbol' AND key LIKE '%main.go#Hello' AND deleted_at IS NULL", h.repoID).Scan(&n))
	assert.Equal(t, 1, n, "the pushed function is in the graph")
}

func TestCodePush_DryRunForceAndNoRoutes(t *testing.T) {
	f := hostfixture.GitLab(t, map[string]string{"README.md": "# shop\n"})
	h := newHarness(t, f, fakegw.Options{NoTriageRoute: true})
	pl := h.push(map[string]*string{"main.go": hostfixture.S(mainV1), "notes.txt": hostfixture.S("hello\n")})
	dry := pl
	dry.DryRun = true
	out, res := h.run(dry)
	assert.Equal(t, ports.JobDone, out.Status)
	assert.Contains(t, out.Message, "dry run")
	assert.Greater(t, res.EstimatedTokens, int64(0))
	assert.Zero(t, h.fg.Model.DocgenCalls(), "dry runs make no paid calls")

	out, _ = h.run(pl)
	require.Equal(t, ports.JobDone, out.Status, out.Message)
	calls := h.fg.Model.DocgenCalls()
	out, res = h.run(pipeline.CodePushPayload{ForcePaths: []string{"main.go"}, Reason: "stale"})
	require.Equal(t, ports.JobDone, out.Status, out.Message)
	assert.Equal(t, calls+1, h.fg.Model.DocgenCalls(), "forced paths regenerate even with no new commits")
	assert.GreaterOrEqual(t, res.Documented, 1)

	delete(h.fg.Routes, "docgen")
	out, res = h.run(h.push(map[string]*string{"main.go": hostfixture.S(mainV1 + "\nfunc X() {}\n")}))
	require.Equal(t, ports.JobDone, out.Status, out.Message)
	assert.Contains(t, strings.Join(res.Notes, " "), "no docgen route")
	assert.Greater(t, res.ChunksAdded, 0, "code is still indexed")
}

func TestCodePush_UntrackedRepoAborts(t *testing.T) {
	f := hostfixture.GitHub(t, map[string]string{"README.md": "# shop\n"})
	h := newHarness(t, f, fakegw.Options{})
	b, _ := json.Marshal(pipeline.CodePushPayload{RepoID: ports.NewID()})
	out, err := h.p.CodePush(context.Background(), ports.Job{ID: ports.NewID(), Payload: b})
	require.NoError(t, err)
	assert.Equal(t, ports.JobAborted, out.Status)
	_, err = h.p.CodePush(context.Background(), ports.Job{ID: ports.NewID(), Payload: []byte("{")})
	var pe *ports.PermanentError
	assert.ErrorAs(t, err, &pe)
}
