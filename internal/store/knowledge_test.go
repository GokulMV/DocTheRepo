package store_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/GokulMV/DocTheRepo/internal/core/knownissues"
	"github.com/GokulMV/DocTheRepo/internal/core/library"
	"github.com/GokulMV/DocTheRepo/internal/core/palace"
	"github.com/GokulMV/DocTheRepo/internal/core/rag"
	"github.com/GokulMV/DocTheRepo/internal/ports"
	"github.com/GokulMV/DocTheRepo/internal/store"
	"github.com/GokulMV/DocTheRepo/internal/store/gen"
)

func TestKnowledgeApplyAndRemove(t *testing.T) {
	ctx := context.Background()
	st, conns, _, repoID := fixture(t)
	cid, err := conns.Create(ctx, store.NewConnector{Type: "confluence", Name: "wiki", Credentials: "tok",
		Config: map[string]string{"base_url": "https://acme.atlassian.net/wiki", "spaces": "ENG"}})
	require.NoError(t, err)
	shelves, err := library.Compile(library.DefaultShelves())
	require.NoError(t, err)
	docs := store.NewDocs(st, shelves)
	require.NoError(t, docs.SeedShelves(ctx))
	// The Palace already knows a service from code.
	require.NoError(t, store.NewGraph(st).ReplaceSource(ctx, repoID, "deploy/service.yaml", func() palace.Graph {
		var g palace.Graph
		g.AddEdge(palace.ServiceRef("checkout"), palace.EdgeDeployedAs, palace.RepoRef("acme/shop"), palace.Evidence{Path: "deploy/service.yaml"})
		return g
	}()))

	k := store.NewKnowledge(st, shelves)
	idx, err := k.MentionIndex(ctx)
	require.NoError(t, err)
	page := ports.KnowledgeDoc{Source: ports.SourceConfluence, ExternalID: "123", Space: "ENG", Title: "Checkout runbook",
		URL: "https://acme.atlassian.net/wiki/spaces/ENG/pages/123", Labels: []string{"runbook"}, Status: "current",
		Markdown: "When **checkout** returns 503, drain the pool.\n\n## Steps\n\n1. Scale up.\n2. Page the owner of acme/shop."}
	res, err := k.Apply(ctx, cid, []ports.KnowledgeDoc{page}, idx)
	require.NoError(t, err)
	assert.Equal(t, 1, res.Docs)
	assert.Greater(t, res.Added, 0)
	assert.Len(t, res.Embed, res.Added)
	assert.Equal(t, 2, res.Links, "checkout and acme/shop are linked")

	// Chunks are repo-less and visible to a reader scoped to an unrelated repo.
	chunks, err := st.Q.SharedChunksForPath(ctx, gen.SharedChunksForPathParams{Source: gen.ChunkSourceConfluence, Path: "confluence/ENG/123"})
	require.NoError(t, err)
	require.NotEmpty(t, chunks)
	assert.Nil(t, chunks[0].RepoID)
	qa := store.NewQA(st)
	scoped := rag.Scope{RepoIDs: []string{"00000000-0000-0000-0000-000000000001"}}
	hits, err := qa.FullText(ctx, "drain the pool", scoped, 5)
	require.NoError(t, err)
	assert.NotEmpty(t, hits, "shared sources are readable by every viewer")

	// Palace: checkout documented_in the page; the runbook is runbook_for checkout.
	edges, err := store.NewGraph(st).Edges(ctx, palace.ServiceRef("checkout"))
	require.NoError(t, err)
	assert.Contains(t, edges, store.EdgeView{Kind: palace.EdgeDocumentedIn, Dst: palace.Ref{Kind: palace.KindConfluencePage, Key: "123"}})
	edges, err = store.NewGraph(st).Edges(ctx, palace.Ref{Kind: palace.KindConfluencePage, Key: "123"})
	require.NoError(t, err)
	assert.Contains(t, edges, store.EdgeView{Kind: palace.EdgeRunbookFor, Dst: palace.ServiceRef("checkout")})

	// Library: the Confluence and Runbooks shelves, rendered with title and URL.
	sh, err := store.NewBrowse(st).Shelf(ctx, "runbooks", scoped)
	require.NoError(t, err)
	require.Len(t, sh.Entries, 1)
	assert.Equal(t, "confluence_page", sh.Entries[0].Type)
	assert.Equal(t, "Checkout runbook", sh.Entries[0].Title)
	assert.Equal(t, page.URL, sh.Entries[0].Path)
	ids, err := docs.ShelfItemIDs(ctx, "confluence")
	require.NoError(t, err)
	assert.Len(t, ids, 1)

	// Re-applying unchanged content embeds nothing; a renamed page keeps its chunks' identity.
	res, err = k.Apply(ctx, cid, []ports.KnowledgeDoc{page}, idx)
	require.NoError(t, err)
	assert.Empty(t, res.Embed)
	assert.Equal(t, 1, res.Unchanged)

	// The page disappears upstream: chunks soft-deleted, entity and placements gone. An empty listing is ignored.
	n, err := k.RemoveMissing(ctx, cid, "ENG", nil)
	require.NoError(t, err)
	assert.Zero(t, n)
	n, err = k.RemoveMissing(ctx, cid, "ENG", []string{"999"})
	require.NoError(t, err)
	assert.Equal(t, 1, n)
	hits, err = qa.FullText(ctx, "drain the pool", rag.Scope{All: true}, 5)
	require.NoError(t, err)
	assert.Empty(t, hits)
	ids, err = docs.ShelfItemIDs(ctx, "confluence")
	require.NoError(t, err)
	assert.Empty(t, ids)
	_, err = store.NewGraph(st).Edges(ctx, palace.Ref{Kind: palace.KindConfluencePage, Key: "123"})
	require.NoError(t, err) // retired entities still resolve; their edges are gone
}

func TestKnownIssuesUpstream(t *testing.T) {
	ctx := context.Background()
	st, _, _, _ := fixture(t)
	ki := store.NewKnownIssues(st)

	// A label import with no proposal yet: stored as a disabled draft without a match.
	id, err := ki.CreateImported(ctx, ports.ImportedRule{Source: "jira", Ref: "ENG-1", Title: "ENG-1: pool exhausted", Reason: "known_bug",
		Action: "suppress", UpstreamStatus: "In Progress", TicketURL: "https://acme.atlassian.net/browse/ENG-1"})
	require.NoError(t, err)
	require.NotEmpty(t, id)
	again, err := ki.CreateImported(ctx, ports.ImportedRule{Source: "jira", Ref: "ENG-1", Title: "dup", Reason: "known_bug", Action: "suppress"})
	require.NoError(t, err)
	assert.Empty(t, again, "a second import of the same issue is a no-op")
	k, err := ki.Get(ctx, id)
	require.NoError(t, err)
	assert.False(t, k.Enabled)
	assert.True(t, k.LabelManaged)
	assert.Equal(t, "ENG-1", k.JiraKey)
	assert.Equal(t, "In Progress", k.UpstreamStatus)

	// Enabling the draft needs a match.
	k.Enabled = true
	var v *ports.ValidationError
	require.ErrorAs(t, ki.Update(ctx, k), &v)
	k.Match = knownissues.Match{Services: []string{"checkout"}}
	require.NoError(t, ki.Update(ctx, k))

	// An invalid proposed match is dropped rather than failing the import.
	bad, _ := json.Marshal(knownissues.Match{MessageRegex: "("})
	cid, err := ki.CreateImported(ctx, ports.ImportedRule{Source: "confluence", Ref: "77", Title: "Known: cache misses", Reason: "expected_noise",
		Action: "suppress", Match: bad})
	require.NoError(t, err)
	c, err := ki.Get(ctx, cid)
	require.NoError(t, err)
	assert.Equal(t, "77", c.ConfluencePageID)
	assert.Empty(t, c.Match.MessageRegex)

	rules, err := ki.UpstreamRules(ctx, "jira", []string{"ENG-1", "ENG-2"}, false)
	require.NoError(t, err)
	require.Contains(t, rules, "ENG-1")
	assert.Equal(t, "suppress", rules["ENG-1"].Action)
	assert.True(t, rules["ENG-1"].Enabled)

	require.NoError(t, ki.SetUpstream(ctx, id, "Done", "label_only", "fixed upstream — verify"))
	k, err = ki.Get(ctx, id)
	require.NoError(t, err)
	assert.Equal(t, "label_only", k.Action)
	assert.Equal(t, "Done", k.UpstreamStatus)
	assert.Equal(t, "fixed upstream — verify", k.UpstreamNote)
	require.NoError(t, ki.SetUpstream(ctx, id, "Reopened", "", ""))
	k, _ = ki.Get(ctx, id)
	assert.Equal(t, "label_only", k.Action, "a status-only update never changes the action")
	assert.Equal(t, "fixed upstream — verify", k.UpstreamNote)

	managed, err := ki.UpstreamRules(ctx, "confluence", nil, true)
	require.NoError(t, err)
	assert.Len(t, managed, 1)
}
