package pipeline_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/GokulMV/DocTheRepo/internal/core/pipeline"
	"github.com/GokulMV/DocTheRepo/internal/ports"
	"github.com/GokulMV/DocTheRepo/internal/queue"
	"github.com/GokulMV/DocTheRepo/internal/store"
	"github.com/GokulMV/DocTheRepo/test/mocks/fakegw"
	"github.com/GokulMV/DocTheRepo/test/mocks/hostfixture"
)

func (h *harness) job(t ports.JobType, payload any, fn func(context.Context, ports.Job) (ports.Outcome, error)) (ports.Outcome, json.RawMessage) {
	h.t.Helper()
	job, _, err := queue.New(h.st, queue.Options{}).Enqueue(context.Background(), ports.NewJob{Type: t, RepoID: h.repoID, Payload: payload})
	require.NoError(h.t, err)
	out, err := fn(context.Background(), job)
	require.NoError(h.t, err)
	b, _ := json.Marshal(out.Result)
	return out, b
}

func TestImportDocs(t *testing.T) {
	f := hostfixture.GitHub(t, map[string]string{
		"README.md":                 "# shop\n\nThe shop service sells things.\n\n## Setup\n\nRun make.\n",
		"docs/adr/0001-postgres.md": "# Use Postgres\n\n## Decision\n\nPostgres.\n",
		"docs/generated/x.go.md":    "# generated\n",
		"main.go":                   "package main\n",
	})
	h := newHarness(t, f, fakegw.Options{})
	out, raw := h.job(ports.JobImportDocs, pipeline.ImportPayload{RepoID: h.repoID}, h.p.ImportDocs)
	require.Equal(t, ports.JobDone, out.Status)
	var res pipeline.ImportResult
	require.NoError(t, json.Unmarshal(raw, &res))
	assert.Equal(t, 2, res.Files, "generated docs are not imported")
	assert.Greater(t, res.Added, 2)
	assert.Equal(t, res.Added, res.Embedded)
	items, err := store.NewDocs(h.st, nil).ShelfItemIDs(context.Background(), "decisions")
	require.NoError(t, err)
	assert.Len(t, items, 1, "the ADR lands on the Decisions shelf")

	_, raw = h.job(ports.JobImportDocs, pipeline.ImportPayload{RepoID: h.repoID}, h.p.ImportDocs)
	require.NoError(t, json.Unmarshal(raw, &res))
	assert.Zero(t, res.Added+res.Changed+res.Embedded, "re-import writes nothing when unchanged")

	_, raw = h.job(ports.JobImportDocs, pipeline.ImportPayload{RepoID: h.repoID, Prefixes: []string{"docs/adr"}}, h.p.ImportDocs)
	require.NoError(t, json.Unmarshal(raw, &res))
	assert.Equal(t, 1, res.Files)

	out, _ = h.job(ports.JobImportDocs, pipeline.ImportPayload{RepoID: ports.NewID()}, h.p.ImportDocs)
	assert.Equal(t, ports.JobAborted, out.Status)
}

func TestReindexSwapsModelWithoutDowntime(t *testing.T) {
	f := hostfixture.GitLab(t, map[string]string{"README.md": "# shop\n"})
	h := newHarness(t, f, fakegw.Options{})
	ctx := context.Background()

	out, _ := h.job(ports.JobReindex, pipeline.ReindexPayload{}, h.p.Reindex)
	assert.Equal(t, ports.JobDone, out.Status, "first run initialises the index")
	out, _ = h.job(ports.JobReindex, pipeline.ReindexPayload{}, h.p.Reindex)
	assert.Equal(t, ports.JobAborted, out.Status, "same model: nothing to do")

	o, res := h.run(h.push(map[string]*string{"main.go": hostfixture.S(mainV1)}))
	require.Equal(t, ports.JobDone, o.Status, o.Message)
	require.Greater(t, res.Embedded, 0)

	h.fg.SetEmbeddingModel("fake-embed-2", 6)
	o, res = h.run(h.push(map[string]*string{"main.go": hostfixture.S(mainV1 + "\nfunc Extra() int { return 1 }\n")}))
	require.Equal(t, ports.JobDone, o.Status, o.Message)
	assert.Contains(t, strings.Join(res.Notes, " "), "embedding model changed", "a push between the model change and the reindex is not failed")
	hits, err := h.index.Search(ctx, fakegw.Vector("q", fakegw.Dims), 5, ports.VectorFilter{})
	require.NoError(t, err)
	assert.NotEmpty(t, hits, "reads keep working on the old model")

	out, raw := h.job(ports.JobReindex, pipeline.ReindexPayload{PageSize: 2}, h.p.Reindex)
	require.Equal(t, ports.JobDone, out.Status, out.Message)
	var rr pipeline.ReindexResult
	require.NoError(t, json.Unmarshal(raw, &rr))
	assert.Equal(t, "fake-embed", rr.From.Model)
	assert.Equal(t, ports.EmbeddingSpec{ProviderKind: "openai", Model: "fake-embed-2", Dimensions: 6}, rr.To)
	assert.Greater(t, rr.Embedded, res.ChunksAdded, "every live chunk, including the ones the push could not embed")
	st, err := h.index.State(ctx)
	require.NoError(t, err)
	assert.Equal(t, "fake-embed-2", st.Live.Model)
	hits, err = h.index.Search(ctx, fakegw.Vector("q", 6), 50, ports.VectorFilter{})
	require.NoError(t, err)
	assert.Len(t, hits, rr.Embedded)
}
