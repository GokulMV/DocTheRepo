package store

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"sort"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/GokulMV/DocTheRepo/internal/core/library"
	"github.com/GokulMV/DocTheRepo/internal/core/rag"
	"github.com/GokulMV/DocTheRepo/internal/ports"
	"github.com/GokulMV/DocTheRepo/internal/store/gen"
)

// Browse serves the read APIs: the docs Tree, Palace, Library, and analytics. Every repository-owned row
// is filtered by the caller's scope in SQL.
type Browse struct{ s *Store }

// NewBrowse returns the browse store.
func NewBrowse(s *Store) *Browse { return &Browse{s: s} }

func ids(sc rag.Scope) []string {
	if sc.RepoIDs == nil {
		return []string{}
	}
	return sc.RepoIDs
}

// TreeNode is one docs Tree node.
type TreeNode struct {
	ID          string    `json:"id"`
	Kind        string    `json:"kind"`
	Title       string    `json:"title"`
	Path        string    `json:"path"`
	Summary     string    `json:"summary,omitempty"`
	RepoID      string    `json:"repo_id,omitempty"`
	HasChildren bool      `json:"has_children"`
	UpdatedAt   time.Time `json:"updated_at,omitempty"`
}

// Roots lists the readable repositories' Tree roots.
func (b *Browse) Roots(ctx context.Context, sc rag.Scope) ([]TreeNode, error) {
	rows, err := b.s.Q.DocRoots(ctx, gen.DocRootsParams{AllRepos: sc.All, RepoIds: ids(sc)})
	if err != nil {
		return nil, err
	}
	out := make([]TreeNode, len(rows))
	for i, r := range rows {
		out[i] = TreeNode{ID: r.ID, Kind: "repo", Title: r.Title, RepoID: r.RepoID, HasChildren: r.HasChildren}
	}
	return out, nil
}

// Children lists a node's children.
func (b *Browse) Children(ctx context.Context, repoID, parentID string) ([]TreeNode, error) {
	rows, err := b.s.Q.DocChildrenWithCounts(ctx, gen.DocChildrenWithCountsParams{RepoID: repoID, ParentID: strPtr(parentID)})
	if err != nil {
		return nil, err
	}
	out := make([]TreeNode, len(rows))
	for i, r := range rows {
		out[i] = TreeNode{ID: r.ID, Kind: string(r.Kind), Title: r.Title, Path: r.Path, Summary: r.Summary, RepoID: repoID,
			HasChildren: r.HasChildren, UpdatedAt: r.UpdatedAt}
	}
	return out, nil
}

// NodeDetail is a Tree node with its content.
type NodeDetail struct {
	ID        string           `json:"id"`
	Kind      string           `json:"kind"`
	Title     string           `json:"title"`
	Path      string           `json:"path"`
	Repo      string           `json:"repo"`
	RepoID    string           `json:"repo_id"`
	Summary   string           `json:"summary"`
	Markdown  string           `json:"markdown"`
	CommitSHA string           `json:"commit_sha,omitempty"`
	UpdatedAt time.Time        `json:"updated_at"`
	Chunks    []map[string]any `json:"chunks"`
	ParentID  string           `json:"parent_id,omitempty"`
}

// Node loads a Tree node; section nodes show their parent file's content. Callers check the repo ACL.
func (b *Browse) Node(ctx context.Context, id string) (NodeDetail, error) {
	n, err := b.s.Q.GetDocNode(ctx, id)
	if IsNoRows(err) {
		return NodeDetail{}, ports.ErrNotFound
	}
	if err != nil {
		return NodeDetail{}, err
	}
	d := NodeDetail{ID: n.ID, Kind: string(n.Kind), Title: n.Title, Path: n.Path, Repo: n.RepoName, RepoID: n.RepoID, Summary: n.Summary,
		Markdown: n.Content, CommitSHA: n.CommitSha, UpdatedAt: n.UpdatedAt, Chunks: []map[string]any{}}
	if n.ParentID != nil {
		d.ParentID = *n.ParentID
	}
	fileID := n.ID
	if n.Kind == gen.DocNodeKindSection && n.ParentID != nil {
		fileID = *n.ParentID
		if f, err := b.s.Q.GetDocNode(ctx, fileID); err == nil {
			d.Markdown, d.CommitSHA = f.Content, f.CommitSha
		}
	}
	rows, err := b.s.Q.DocSectionChunks(ctx, &fileID)
	if err != nil {
		return d, err
	}
	for _, r := range rows {
		d.Chunks = append(d.Chunks, map[string]any{"chunk_id": r.ChunkID, "path": r.Path, "symbol": r.Symbol, "language": r.Language,
			"signature": r.Signature, "commit_sha": r.CommitSha})
	}
	return d, nil
}

// Entity is a Palace node.
type Entity struct {
	ID       string            `json:"id"`
	Kind     string            `json:"kind"`
	Key      string            `json:"key"`
	Name     string            `json:"name"`
	RepoID   *string           `json:"repo_id,omitempty"`
	Attrs    map[string]string `json:"attrs,omitempty"`
	LastSeen time.Time         `json:"last_seen"`
}

// EntityCursor pages entities by (kind, key).
type EntityCursor struct {
	Kind string `json:"k"`
	Key  string `json:"y"`
}

// Entities lists Palace entities.
func (b *Browse) Entities(ctx context.Context, kind, q, repoID string, sc rag.Scope, after EntityCursor, limit int) ([]Entity, error) {
	rows, err := b.s.Q.ListEntities(ctx, gen.ListEntitiesParams{Kind: kind, Q: q, RepoID: strPtr(repoID), AllRepos: sc.All, RepoIds: ids(sc),
		AfterKind: after.Kind, AfterKey: after.Key, Lim: int32(limit)})
	if err != nil {
		return nil, err
	}
	out := make([]Entity, len(rows))
	for i, r := range rows {
		out[i] = toEntity(r.ID, r.Kind, r.Key, r.Name, r.RepoID, r.Attrs, r.LastSeen)
	}
	return out, nil
}

func toEntity(id, kind, key, name string, repo *string, attrs json.RawMessage, seen time.Time) Entity {
	e := Entity{ID: id, Kind: kind, Key: key, Name: name, RepoID: repo, LastSeen: seen}
	_ = json.Unmarshal(attrs, &e.Attrs)
	return e
}

// GraphEdge is one edge in a graph response.
type GraphEdge struct {
	Src      string          `json:"src"`
	Dst      string          `json:"dst"`
	Kind     string          `json:"kind"`
	Evidence json.RawMessage `json:"evidence,omitempty"`
}

// Neighbours is a graph neighbourhood.
type Neighbours struct {
	Nodes []Entity    `json:"nodes"`
	Edges []GraphEdge `json:"edges"`
}

// Neighbourhood walks up to depth hops from an entity (breadth-first, max 500 nodes), ACL-filtered.
func (b *Browse) Neighbourhood(ctx context.Context, id string, depth int, kinds []string, sc rag.Scope) (Neighbours, error) {
	if !uuidRE.MatchString(id) {
		return Neighbours{}, ports.ErrNotFound
	}
	root, err := b.s.Q.GetEntity(ctx, id)
	if IsNoRows(err) {
		return Neighbours{}, ports.ErrNotFound
	}
	if err != nil {
		return Neighbours{}, err
	}
	if root.RepoID != nil && !sc.All && !contains(sc.RepoIDs, *root.RepoID) {
		return Neighbours{}, ports.ErrNotFound
	}
	if kinds == nil {
		kinds = []string{}
	}
	nodes := map[string]Entity{root.ID: toEntity(root.ID, root.Kind, root.Key, root.Name, root.RepoID, root.Attrs, root.LastSeen)}
	edges := map[string]GraphEdge{}
	frontier := []string{root.ID}
	for d := 0; d < depth && len(frontier) > 0 && len(nodes) < 500; d++ {
		rows, err := b.s.Q.EntityEdges(ctx, gen.EntityEdgesParams{Ids: frontier, Kinds: kinds, AllRepos: sc.All, RepoIds: ids(sc)})
		if err != nil {
			return Neighbours{}, err
		}
		var next []string
		for _, r := range rows {
			edges[r.SrcID+r.Kind+r.DstID] = GraphEdge{Src: r.SrcID, Dst: r.DstID, Kind: r.Kind, Evidence: r.Evidence}
			for _, n := range []Entity{{ID: r.SrcID, Kind: r.SrcKind, Key: r.SrcKey, Name: r.SrcName, RepoID: r.SrcRepo},
				{ID: r.DstID, Kind: r.DstKind, Key: r.DstKey, Name: r.DstName, RepoID: r.DstRepo}} {
				if _, ok := nodes[n.ID]; !ok && len(nodes) < 500 {
					nodes[n.ID] = n
					next = append(next, n.ID)
				}
			}
		}
		frontier = next
	}
	g := Neighbours{Nodes: make([]Entity, 0, len(nodes)), Edges: make([]GraphEdge, 0, len(edges))}
	for _, n := range nodes {
		g.Nodes = append(g.Nodes, n)
	}
	for _, e := range edges {
		if _, ok := nodes[e.Src]; ok {
			if _, ok := nodes[e.Dst]; ok {
				g.Edges = append(g.Edges, e)
			}
		}
	}
	sort.Slice(g.Nodes, func(i, j int) bool { return g.Nodes[i].Key < g.Nodes[j].Key })
	sort.Slice(g.Edges, func(i, j int) bool {
		return g.Edges[i].Src+g.Edges[i].Kind+g.Edges[i].Dst < g.Edges[j].Src+g.Edges[j].Kind+g.Edges[j].Dst
	})
	return g, nil
}

func contains(xs []string, x string) bool {
	for _, v := range xs {
		if v == x {
			return true
		}
	}
	return false
}

// Shelf is a Library shelf.
type Shelf struct {
	ID          string          `json:"id"`
	Slug        string          `json:"slug"`
	Title       string          `json:"title"`
	Description string          `json:"description"`
	Rules       json.RawMessage `json:"rules"`
	Curated     bool            `json:"curated"`
	Order       int             `json:"order"`
	Items       int64           `json:"item_count"`
	Entries     []ShelfEntry    `json:"items,omitempty"`
}

// ShelfEntry is one item on a shelf.
type ShelfEntry struct {
	Type    string  `json:"type"`
	ID      string  `json:"id"`
	Title   string  `json:"title"`
	Path    string  `json:"path"`
	Summary string  `json:"summary,omitempty"`
	RepoID  *string `json:"repo_id,omitempty"`
	Pinned  bool    `json:"pinned"`
	Note    string  `json:"note,omitempty"`
}

// Shelves lists shelves with item counts (counts are not ACL-filtered; items are).
func (b *Browse) Shelves(ctx context.Context) ([]Shelf, error) {
	rows, err := b.s.Q.ShelvesWithCounts(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]Shelf, len(rows))
	for i, r := range rows {
		out[i] = Shelf{ID: r.ID, Slug: r.Slug, Title: r.Title, Description: r.Description, Rules: r.Rule, Curated: r.Curated, Order: int(r.OrderKey), Items: r.Items}
	}
	return out, nil
}

// Shelf loads a shelf with the caller's readable items.
func (b *Browse) Shelf(ctx context.Context, slug string, sc rag.Scope) (Shelf, error) {
	r, err := b.s.Q.GetShelfBySlug(ctx, slug)
	if IsNoRows(err) {
		return Shelf{}, ports.ErrNotFound
	}
	if err != nil {
		return Shelf{}, err
	}
	sh := Shelf{ID: r.ID, Slug: r.Slug, Title: r.Title, Description: r.Description, Rules: r.Rule, Curated: r.Curated, Order: int(r.OrderKey), Entries: []ShelfEntry{}}
	docs, err := b.s.Q.ShelfDocItems(ctx, gen.ShelfDocItemsParams{ShelfID: r.ID, AllRepos: sc.All, RepoIds: ids(sc)})
	if err != nil {
		return sh, err
	}
	for _, d := range docs {
		rid := d.RepoID
		sh.Entries = append(sh.Entries, ShelfEntry{Type: string(d.ItemType), ID: d.ItemID, Title: d.Title, Path: d.Path, Summary: d.Summary, RepoID: &rid, Pinned: d.Pinned, Note: d.Note})
	}
	ents, err := b.s.Q.ShelfEntityItems(ctx, gen.ShelfEntityItemsParams{ShelfID: r.ID, AllRepos: sc.All, RepoIds: ids(sc)})
	if err != nil {
		return sh, err
	}
	for _, e := range ents {
		sh.Entries = append(sh.Entries, ShelfEntry{Type: string(e.ItemType), ID: e.ItemID, Title: e.Title, Path: e.Path, Summary: e.Summary, RepoID: e.RepoID, Pinned: e.Pinned, Note: e.Note})
	}
	// Confluence pages and Jira issues are shared sources: every viewer may see them.
	rows, err := b.s.Pool.Query(ctx, `SELECT si.item_type::text, si.item_id, si.pinned, si.note, k.title, k.url, k.summary
		FROM shelf_items si JOIN knowledge_docs k ON k.id::text = si.item_id
		WHERE si.shelf_id = $1 AND si.item_type IN ('confluence_page', 'jira_issue')
		ORDER BY si.pinned DESC, k.title`, r.ID)
	if err != nil {
		return sh, err
	}
	defer rows.Close()
	for rows.Next() {
		var e ShelfEntry
		if err := rows.Scan(&e.Type, &e.ID, &e.Pinned, &e.Note, &e.Title, &e.Path, &e.Summary); err != nil {
			return sh, err
		}
		sh.Entries = append(sh.Entries, e)
	}
	if err := rows.Err(); err != nil {
		return sh, err
	}
	sh.Items = int64(len(sh.Entries))
	return sh, nil
}

// CreateShelf adds a shelf (curated shelves are filled only by people).
func (b *Browse) CreateShelf(ctx context.Context, sh library.Shelf) (string, error) {
	if _, err := library.Compile([]library.Shelf{sh}); err != nil {
		return "", &ports.ValidationError{Code: "VALIDATION_FAILED", Message: err.Error()}
	}
	id := ports.NewID()
	rules, _ := json.Marshal(sh.Rules)
	if sh.Rules == nil {
		rules = []byte("[]")
	}
	err := b.s.Q.CreateShelf(ctx, gen.CreateShelfParams{ID: id, Slug: sh.Slug, Title: sh.Title, Description: sh.Description, Rule: rules, Curated: sh.Curated, OrderKey: int32(sh.Order)})
	if err != nil {
		return "", fmt.Errorf("create shelf (is the slug taken?): %w", ports.ErrConflict)
	}
	return id, nil
}

// UpdateShelf edits a shelf's title, description, or order.
func (b *Browse) UpdateShelf(ctx context.Context, id string, title, description *string, order *int) error {
	if !uuidRE.MatchString(id) {
		return ports.ErrNotFound
	}
	var o *int32
	if order != nil {
		v := int32(*order)
		o = &v
	}
	n, err := b.s.Q.UpdateShelf(ctx, gen.UpdateShelfParams{ID: id, Title: title, Description: description, OrderKey: o})
	if err == nil && n == 0 {
		return ports.ErrNotFound
	}
	return err
}

// DeleteShelf removes a shelf.
func (b *Browse) DeleteShelf(ctx context.Context, id string) error {
	if !uuidRE.MatchString(id) {
		return ports.ErrNotFound
	}
	n, err := b.s.Q.DeleteShelf(ctx, id)
	if err == nil && n == 0 {
		return ports.ErrNotFound
	}
	return err
}

// PinItem places an item on a shelf by hand (pinned items survive rule re-evaluation).
func (b *Browse) PinItem(ctx context.Context, shelfID, itemType, itemID, note string) error {
	if !uuidRE.MatchString(shelfID) {
		return ports.ErrNotFound
	}
	switch itemType {
	case "doc_node", "entity", "confluence_page", "jira_issue", "known_issue":
	default:
		return &ports.ValidationError{Code: "VALIDATION_FAILED", Message: "item_type must be doc_node, entity, confluence_page, jira_issue or known_issue"}
	}
	_, err := b.s.Q.PinShelfItem(ctx, gen.PinShelfItemParams{ShelfID: shelfID, ItemType: gen.ShelfItemType(itemType), ItemID: itemID, Note: note})
	if err != nil {
		return fmt.Errorf("pin item: %w", ports.ErrNotFound)
	}
	return nil
}

// RepoPatch is a partial repository settings update.
type RepoPatch struct {
	TrackedBranch      *string   `json:"tracked_branch"`
	DocsPath           *string   `json:"docs_path"`
	PushMode           *string   `json:"push_mode"`
	OnReject           *string   `json:"on_reject"`
	Approver           *string   `json:"approver"`
	ServiceName        *string   `json:"service_name"`
	Owners             *[]string `json:"owners"`
	Enabled            *bool     `json:"enabled"`
	PRConflictStrategy *string   `json:"pr_conflict_strategy"`
}

// UpdateRepo applies a patch.
func (r *Repos) UpdateRepo(ctx context.Context, id string, p RepoPatch) error {
	if !uuidRE.MatchString(id) {
		return ports.ErrNotFound
	}
	var owners []string
	if p.Owners != nil {
		owners = nonNilSlice(*p.Owners)
	}
	n, err := r.s.Q.UpdateRepoSettings(ctx, gen.UpdateRepoSettingsParams{ID: id, TrackedBranch: p.TrackedBranch, DocsPath: p.DocsPath,
		PushMode: (*gen.PushMode)(p.PushMode), OnReject: p.OnReject, Approver: p.Approver, ServiceName: p.ServiceName, Owners: owners, Enabled: p.Enabled,
		PrConflictStrategy: p.PRConflictStrategy})
	if err != nil {
		return &ports.ValidationError{Code: "VALIDATION_FAILED", Message: "invalid repository settings: " + err.Error()}
	}
	if n == 0 {
		return ports.ErrNotFound
	}
	return nil
}

// ConnectorView is a connector as the API shows it (never credentials).
type ConnectorView struct {
	ID               string            `json:"id"`
	Type             string            `json:"type"`
	Name             string            `json:"name"`
	Config           map[string]string `json:"config"`
	Mode             string            `json:"mode"`
	PollSeconds      int64             `json:"poll_seconds"`
	Enabled          bool              `json:"enabled"`
	Health           string            `json:"health"`
	LastError        string            `json:"last_error,omitempty"`
	LastSyncAt       *time.Time        `json:"last_sync_at,omitempty"`
	HasCredentials   bool              `json:"has_credentials"`
	HasWebhookSecret bool              `json:"has_webhook_secret"`
	CreatedAt        time.Time         `json:"created_at"`
}

// ListPublic lists connectors without secrets.
func (c *Connectors) ListPublic(ctx context.Context) ([]ConnectorView, error) {
	rows, err := c.s.Q.ListConnectorsPublic(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]ConnectorView, len(rows))
	for i, r := range rows {
		v := ConnectorView{ID: r.ID, Type: string(r.Type), Name: r.Name, Config: map[string]string{}, Mode: string(r.Mode), PollSeconds: r.PollSeconds,
			Enabled: r.Enabled, Health: string(r.Health), LastError: r.LastError, LastSyncAt: r.LastSyncAt, HasCredentials: r.HasCredentials == true,
			HasWebhookSecret: r.HasWebhookSecret == true, CreatedAt: r.CreatedAt}
		_ = json.Unmarshal(r.Config, &v.Config)
		out[i] = v
	}
	return out, nil
}

// ConnectorPatch is a partial connector update; secrets are write-only.
type ConnectorPatch struct {
	Name          *string            `json:"name"`
	Config        *map[string]string `json:"config"`
	Credentials   *string            `json:"credentials"`
	WebhookSecret *string            `json:"webhook_secret"`
	Mode          *string            `json:"mode"`
	PollSeconds   *int64             `json:"poll_seconds"`
	Enabled       *bool              `json:"enabled"`
}

// Update applies a patch, sealing new secrets.
func (c *Connectors) Update(ctx context.Context, id string, p ConnectorPatch) error {
	if !uuidRE.MatchString(id) {
		return ports.ErrNotFound
	}
	params := gen.UpdateConnectorParams{ID: id, Name: p.Name, Enabled: p.Enabled}
	if p.Config != nil {
		b, _ := json.Marshal(orEmpty(*p.Config))
		params.Config = b
	}
	if p.Credentials != nil {
		ct, err := c.box.Seal(ctx, []byte(*p.Credentials), c.CredsAAD(id))
		if err != nil {
			return err
		}
		params.CredsCiphertext = ct
	}
	if p.WebhookSecret != nil {
		ct, err := c.box.Seal(ctx, []byte(*p.WebhookSecret), c.WebhookAAD(id))
		if err != nil {
			return err
		}
		h := sha256.Sum256([]byte(*p.WebhookSecret))
		params.WebhookSecretCt, params.WebhookSecretHash = ct, h[:]
	}
	if p.Mode != nil {
		params.Mode = (*gen.ConnectorMode)(p.Mode)
	}
	if p.PollSeconds != nil {
		f := float64(*p.PollSeconds)
		params.PollSeconds = &f
	}
	n, err := c.s.Q.UpdateConnector(ctx, params)
	if err != nil {
		return &ports.ValidationError{Code: "VALIDATION_FAILED", Message: "invalid connector settings: " + err.Error()}
	}
	if n == 0 {
		return ports.ErrNotFound
	}
	return nil
}

// Delete removes a connector and (by cascade) its repositories.
func (c *Connectors) Delete(ctx context.Context, id string) error {
	if !uuidRE.MatchString(id) {
		return ports.ErrNotFound
	}
	n, err := c.s.Q.DeleteConnector(ctx, id)
	if err == nil && n == 0 {
		return ports.ErrNotFound
	}
	return err
}

// SpendLimit is a spend ceiling as the API edits it.
type SpendLimit struct {
	Scope      string   `json:"scope"`
	ScopeKey   string   `json:"scope_key"`
	Window     string   `json:"window"`
	MaxTokens  *int64   `json:"max_tokens"`
	MaxCostUSD *float64 `json:"max_cost_usd"`
	OnBreach   string   `json:"on_breach"`
	AlertURL   string   `json:"alert_url,omitempty"`
}

// SpendLimits lists the ceilings.
func (b *Browse) SpendLimits(ctx context.Context) ([]SpendLimit, error) {
	rows, err := b.s.Q.ListSpendLimits(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]SpendLimit, len(rows))
	for i, r := range rows {
		out[i] = SpendLimit{Scope: string(r.Scope), ScopeKey: r.ScopeKey, Window: string(r.TimeWindow), MaxTokens: r.MaxTokens, MaxCostUSD: r.MaxCostUsd,
			OnBreach: string(r.OnBreach), AlertURL: r.AlertUrl}
	}
	return out, nil
}

// ReplaceSpendLimits swaps the whole set atomically.
func (b *Browse) ReplaceSpendLimits(ctx context.Context, ls []SpendLimit) error {
	return b.s.InTx(ctx, func(q *gen.Queries, _ pgx.Tx) error {
		if err := q.ReplaceSpendLimitsDelete(ctx); err != nil {
			return err
		}
		for _, l := range ls {
			if l.OnBreach == "" {
				l.OnBreach = "block"
			}
			if l.Window == "" {
				l.Window = "day"
			}
			if err := q.InsertSpendLimit(ctx, gen.InsertSpendLimitParams{ID: ports.NewID(), Scope: gen.SpendScope(l.Scope), ScopeKey: l.ScopeKey,
				TimeWindow: gen.SpendWindow(l.Window), MaxTokens: l.MaxTokens, MaxCostUsd: l.MaxCostUSD, OnBreach: gen.BreachAction(l.OnBreach), AlertUrl: l.AlertURL}); err != nil {
				return &ports.ValidationError{Code: "VALIDATION_FAILED", Message: fmt.Sprintf("invalid limit %s/%s/%s: %v", l.Scope, l.ScopeKey, l.Window, err)}
			}
		}
		return nil
	})
}

// ReindexEstimate counts live chunks and their approximate tokens.
func (b *Browse) ReindexEstimate(ctx context.Context) (int64, int64, error) {
	n, err := b.s.Q.CountLiveChunks(ctx)
	if err != nil {
		return 0, 0, err
	}
	t, err := b.s.Q.LiveChunkTokens(ctx)
	return n, int64(t), err
}
