package store

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/GokulMV/DocTheRepo/internal/core/rag"
	"github.com/GokulMV/DocTheRepo/internal/ports"
)

// Architecture layers, left to right in the diagram.
const (
	LayerUpstream   = "upstream"   // other repositories that call this one or produce the topics it reads
	LayerInterface  = "interface"  // endpoints this repository exposes
	LayerCore       = "core"       // its service(s) and main modules
	LayerMessaging  = "messaging"  // topics and queues it publishes or subscribes to
	LayerData       = "data"       // datastores it uses
	LayerDownstream = "downstream" // repositories and external APIs it calls, and consumers of its topics
)

// Per-layer caps keep the diagram readable; the rest is counted in Hidden.
var archCaps = map[string]int{LayerUpstream: 10, LayerInterface: 14, LayerCore: 9, LayerMessaging: 12, LayerData: 10, LayerDownstream: 12}

// ArchNode is one box. EntityID links it to the Palace (empty for synthetic nodes).
type ArchNode struct {
	ID       string `json:"id"`
	Kind     string `json:"kind"`
	Name     string `json:"name"`
	Key      string `json:"key,omitempty"`
	Layer    string `json:"layer"`
	EntityID string `json:"entity_id,omitempty"`
	RepoID   string `json:"repo_id,omitempty"` // for nodes that stand for a repository
	Degree   int    `json:"degree"`
}

// ArchLink is one arrow; Weight counts the code-level edges it stands for.
type ArchLink struct {
	Src    string `json:"src"`
	Dst    string `json:"dst"`
	Kind   string `json:"kind"`
	Weight int    `json:"weight"`
}

// ArchDoc is a page that documents the repository or its services.
type ArchDoc struct {
	EntityID string `json:"entity_id"`
	Kind     string `json:"kind"`
	Name     string `json:"name"`
	Key      string `json:"key"`
	Relation string `json:"relation"`
}

// Architecture is one repository's generated architecture, plus the diagrams authored in it.
type Architecture struct {
	Repo struct {
		ID          string `json:"id"`
		FullName    string `json:"full_name"`
		ServiceName string `json:"service_name,omitempty"`
	} `json:"repo"`
	Nodes []ArchNode `json:"nodes"`
	Links []ArchLink `json:"links"`
	// Hidden counts nodes left out per layer (over the cap); Restricted counts links to repositories the
	// caller cannot see, which are left out entirely.
	Hidden     map[string]int `json:"hidden"`
	Restricted int            `json:"restricted"`
	Env        []string       `json:"env"`
	Docs       []ArchDoc      `json:"docs"`
	Owners     []string       `json:"owners"`
	Diagrams   []DiagramMeta  `json:"diagrams"`
	UpdatedAt  *time.Time     `json:"updated_at,omitempty"`
}

// DiagramMeta describes an authored diagram (the HTML is fetched separately).
type DiagramMeta struct {
	ID        string    `json:"id"`
	RepoID    string    `json:"repo_id"`
	Path      string    `json:"path"`
	Title     string    `json:"title"`
	Generator string    `json:"generator,omitempty"`
	CommitSHA string    `json:"commit_sha,omitempty"`
	SizeBytes int       `json:"size_bytes"`
	UpdatedAt time.Time `json:"updated_at"`
}

// ArchitectureStore reads generated architectures and stores authored diagrams.
type ArchitectureStore struct{ s *Store }

// NewArchitecture returns the architecture store.
func NewArchitecture(s *Store) *ArchitectureStore { return &ArchitectureStore{s: s} }

// archEntity and archEdge are the raw rows the builder classifies.
type archEntity struct {
	ID, Kind, Key, Name string
	RepoID              *string
}

type archEdge struct {
	Kind     string
	Src, Dst archEntity
	EdgeRepo *string
}

// archInput is everything the builder needs; it is filled from the database (or a test).
type archInput struct {
	RepoID, FullName string
	RepoEntity       string         // the repo entity's id, if any
	Services         []archEntity   // services deployed as this repository
	Endpoints        []archEntity   // endpoints with repo_id = this repository
	Modules          []archEntity   // modules with repo_id = this repository
	Degree           map[string]int // entity id → edge count
	Edges            []archEdge     // edges touching this repository
	TopicPeers       []archEdge     // publish/subscribe edges from elsewhere onto this repository's topics
	RepoNames        map[string]string
	Visible          func(repoID string) bool
}

func isLocalKind(kind string) bool {
	switch kind {
	case "symbol", "file", "module", "repo", "service":
		return true
	}
	return false
}

// buildArchitecture classifies the graph around one repository into diagram layers. Code-level entities
// (symbols, files) are lifted: local ones to the repository's service, remote ones to their repository.
func buildArchitecture(in archInput) Architecture {
	var out Architecture
	out.Repo.ID, out.Repo.FullName = in.RepoID, in.FullName
	out.Nodes, out.Links, out.Env, out.Docs, out.Owners = []ArchNode{}, []ArchLink{}, []string{}, []ArchDoc{}, []string{}
	out.Hidden = map[string]int{}

	nodes := map[string]*ArchNode{}
	add := func(n ArchNode) string {
		if cur, ok := nodes[n.ID]; ok {
			if cur.Degree < n.Degree {
				cur.Degree = n.Degree
			}
			return n.ID
		}
		nodes[n.ID] = &n
		return n.ID
	}
	entityNode := func(e archEntity, layer string) string {
		return add(ArchNode{ID: e.ID, Kind: e.Kind, Name: e.Name, Key: e.Key, Layer: layer, EntityID: e.ID, Degree: in.Degree[e.ID]})
	}

	// The core anchor: the repository's service(s), or the repository itself.
	core := map[string]bool{}
	anchor := ""
	for _, s := range in.Services {
		id := entityNode(s, LayerCore)
		core[s.ID] = true
		if anchor == "" {
			anchor = id
		}
	}
	if anchor == "" {
		n := ArchNode{ID: "repo:" + in.RepoID, Kind: "repo", Name: in.FullName, Layer: LayerCore, RepoID: in.RepoID, EntityID: in.RepoEntity}
		if in.RepoEntity != "" {
			n.Degree = in.Degree[in.RepoEntity]
		}
		anchor = add(n)
	}
	for _, m := range in.Modules {
		link(&out, anchor, entityNode(m, LayerCore), "contains")
	}
	for _, e := range in.Endpoints {
		link(&out, anchor, entityNode(e, LayerInterface), "exposes")
	}

	isLocal := func(e archEntity, edgeRepo *string) bool {
		if e.RepoID != nil {
			return *e.RepoID == in.RepoID
		}
		if e.ID == in.RepoEntity || core[e.ID] {
			return true
		}
		// A shared entity (no repo) produced by this repository's code, like the service it runs as.
		return edgeRepo != nil && *edgeRepo == in.RepoID && isLocalKind(e.Kind)
	}
	// localNode maps a local entity to its box: endpoints and services stay, everything else lifts to the anchor.
	localNode := func(e archEntity) string {
		switch {
		case e.Kind == "endpoint":
			return entityNode(e, LayerInterface)
		case core[e.ID]:
			return e.ID
		}
		return anchor
	}
	// remoteNode maps an entity owned by another repository to that repository's box ("" if hidden).
	remoteNode := func(e archEntity, layer string) string {
		if e.RepoID == nil {
			return entityNode(e, layer) // shared or external: an external API, a third-party service
		}
		if in.Visible != nil && !in.Visible(*e.RepoID) {
			out.Restricted++
			return ""
		}
		name := in.RepoNames[*e.RepoID]
		if name == "" {
			name = "repository " + (*e.RepoID)[:8]
		}
		return add(ArchNode{ID: "repo:" + *e.RepoID, Kind: "repo", Name: name, Layer: layer, RepoID: *e.RepoID})
	}

	envSeen, docSeen, ownerSeen := map[string]bool{}, map[string]bool{}, map[string]bool{}
	for _, x := range in.Edges {
		sl, dl := isLocal(x.Src, x.EdgeRepo), isLocal(x.Dst, x.EdgeRepo)
		switch {
		case x.Kind == "reads_env":
			if (sl || dl) && !envSeen[x.Dst.Key] {
				envSeen[x.Dst.Key] = true
				out.Env = append(out.Env, x.Dst.Name)
			}
		case x.Kind == "documented_in" || x.Kind == "runbook_for":
			doc, rel := x.Dst, x.Kind
			if x.Kind == "runbook_for" { // page runbook_for service
				doc = x.Src
			}
			if !docSeen[doc.ID] {
				docSeen[doc.ID] = true
				out.Docs = append(out.Docs, ArchDoc{EntityID: doc.ID, Kind: doc.Kind, Name: doc.Name, Key: doc.Key, Relation: rel})
			}
		case x.Kind == "owned_by":
			if sl && !ownerSeen[x.Dst.Name] {
				ownerSeen[x.Dst.Name] = true
				out.Owners = append(out.Owners, x.Dst.Name)
			}
		case x.Kind == "deployed_as" || x.Kind == "contains" || x.Kind == "raises":
			// structure already shown, or not architecture
		case sl && dl:
			if x.Kind == "exposes" || x.Kind == "calls" {
				s, d := localNode(x.Src), localNode(x.Dst)
				if s != d {
					link(&out, s, d, x.Kind)
				}
			}
		case sl && x.Dst.Kind == "queue_topic":
			t := entityNode(x.Dst, LayerMessaging)
			if x.Kind == "subscribes" {
				link(&out, t, localNode(x.Src), "subscribes")
			} else {
				link(&out, localNode(x.Src), t, x.Kind)
			}
		case sl && x.Dst.Kind == "datastore":
			link(&out, localNode(x.Src), entityNode(x.Dst, LayerData), x.Kind)
		case sl:
			if d := remoteNode(x.Dst, LayerDownstream); d != "" {
				link(&out, localNode(x.Src), d, x.Kind)
			}
		case dl:
			if s := remoteNode(x.Src, LayerUpstream); s != "" {
				link(&out, s, localNode(x.Dst), x.Kind)
			}
		}
	}
	// Who else is on this repository's topics: producers upstream, consumers downstream.
	for _, x := range in.TopicPeers {
		if _, ok := nodes[x.Dst.ID]; !ok || isLocal(x.Src, x.EdgeRepo) {
			continue
		}
		if x.Kind == "subscribes" {
			if c := remoteNode(x.Src, LayerDownstream); c != "" {
				link(&out, x.Dst.ID, c, "consumed by")
			}
		} else if p := remoteNode(x.Src, LayerUpstream); p != "" {
			link(&out, p, x.Dst.ID, x.Kind)
		}
	}

	// Cap each layer by degree (the anchor always stays), and drop links to removed nodes.
	byLayer := map[string][]*ArchNode{}
	for _, n := range nodes {
		byLayer[n.Layer] = append(byLayer[n.Layer], n)
	}
	keep := map[string]bool{}
	for layer, ns := range byLayer {
		sort.Slice(ns, func(i, j int) bool {
			if (ns[i].ID == anchor) != (ns[j].ID == anchor) {
				return ns[i].ID == anchor
			}
			if ns[i].Degree != ns[j].Degree {
				return ns[i].Degree > ns[j].Degree
			}
			return ns[i].Name < ns[j].Name
		})
		limit := archCaps[layer]
		for i, n := range ns {
			if i < limit {
				keep[n.ID] = true
				out.Nodes = append(out.Nodes, *n)
			} else {
				out.Hidden[layer]++
			}
		}
	}
	links := out.Links[:0]
	for _, l := range out.Links {
		if keep[l.Src] && keep[l.Dst] {
			links = append(links, l)
		}
	}
	out.Links = links
	sort.Slice(out.Nodes, func(i, j int) bool {
		a, b := out.Nodes[i], out.Nodes[j]
		if a.Layer != b.Layer {
			return layerOrder[a.Layer] < layerOrder[b.Layer]
		}
		if (a.ID == anchor) != (b.ID == anchor) {
			return a.ID == anchor
		}
		if a.Degree != b.Degree {
			return a.Degree > b.Degree
		}
		return a.Name < b.Name
	})
	sort.Slice(out.Links, func(i, j int) bool {
		a, b := out.Links[i], out.Links[j]
		return a.Src+a.Kind+a.Dst < b.Src+b.Kind+b.Dst
	})
	sort.Strings(out.Env)
	sort.Strings(out.Owners)
	return out
}

var layerOrder = map[string]int{LayerUpstream: 0, LayerInterface: 1, LayerCore: 2, LayerMessaging: 3, LayerData: 4, LayerDownstream: 5}

// link adds or strengthens an arrow.
func link(a *Architecture, src, dst, kind string) {
	for i := range a.Links {
		if l := &a.Links[i]; l.Src == src && l.Dst == dst && l.Kind == kind {
			l.Weight++
			return
		}
	}
	a.Links = append(a.Links, ArchLink{Src: src, Dst: dst, Kind: kind, Weight: 1})
}

// maxArchEdges bounds the edges read for one repository.
const maxArchEdges = 20000

// Repo returns a repository's generated architecture. The caller has checked access to repoID; sc hides
// other repositories the caller cannot see.
func (a *ArchitectureStore) Repo(ctx context.Context, repoID string, sc rag.Scope) (Architecture, error) {
	if !uuidRE.MatchString(repoID) {
		return Architecture{}, ports.ErrNotFound
	}
	in := archInput{RepoID: repoID, Degree: map[string]int{}, RepoNames: map[string]string{}}
	var serviceName string
	err := a.s.Pool.QueryRow(ctx, `SELECT full_name, coalesce(service_name, '') FROM repos WHERE id = $1`, repoID).Scan(&in.FullName, &serviceName)
	if errors.Is(err, pgx.ErrNoRows) {
		return Architecture{}, ports.ErrNotFound
	}
	if err != nil {
		return Architecture{}, err
	}
	allowed := map[string]bool{}
	for _, id := range sc.RepoIDs {
		allowed[id] = true
	}
	in.Visible = func(id string) bool { return sc.All || allowed[id] }

	_ = a.s.Pool.QueryRow(ctx, `SELECT id FROM entities WHERE kind = 'repo' AND key = $1 AND deleted_at IS NULL`, in.FullName).Scan(&in.RepoEntity)
	degree := `(SELECT count(*) FROM edges x WHERE x.deleted_at IS NULL AND (x.src_id = e.id OR x.dst_id = e.id))::int`
	scanEntities := func(q string, args ...any) ([]archEntity, error) {
		rows, err := a.s.Pool.Query(ctx, q, args...)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		var out []archEntity
		for rows.Next() {
			var e archEntity
			var d int
			if err := rows.Scan(&e.ID, &e.Kind, &e.Key, &e.Name, &e.RepoID, &d); err != nil {
				return nil, err
			}
			in.Degree[e.ID] = d
			out = append(out, e)
		}
		return out, rows.Err()
	}
	if in.Services, err = scanEntities(`SELECT DISTINCT e.id, e.kind, e.key, e.name, e.repo_id, `+degree+` FROM entities e
		LEFT JOIN edges x ON x.src_id = e.id AND x.kind = 'deployed_as' AND x.deleted_at IS NULL
		WHERE e.kind = 'service' AND e.deleted_at IS NULL AND ((x.dst_id IS NOT NULL AND x.dst_id::text = $1) OR (e.key = $2 AND $2 <> ''))
		ORDER BY 6 DESC`, in.RepoEntity, serviceName); err != nil {
		return Architecture{}, fmt.Errorf("services: %w", err)
	}
	if in.Endpoints, err = scanEntities(`SELECT e.id, e.kind, e.key, e.name, e.repo_id, `+degree+` FROM entities e
		WHERE e.repo_id = $1 AND e.kind = 'endpoint' AND e.deleted_at IS NULL ORDER BY 6 DESC, e.key LIMIT 200`, repoID); err != nil {
		return Architecture{}, fmt.Errorf("endpoints: %w", err)
	}
	if in.Modules, err = scanEntities(`SELECT e.id, e.kind, e.key, e.name, e.repo_id, `+degree+` FROM entities e
		WHERE e.repo_id = $1 AND e.kind = 'module' AND e.deleted_at IS NULL ORDER BY 6 DESC, e.key LIMIT 50`, repoID); err != nil {
		return Architecture{}, fmt.Errorf("modules: %w", err)
	}
	coreIDs := []string{}
	for _, s := range in.Services {
		coreIDs = append(coreIDs, s.ID)
	}
	if in.RepoEntity != "" {
		coreIDs = append(coreIDs, in.RepoEntity)
	}

	scanEdges := func(q string, args ...any) ([]archEdge, error) {
		rows, err := a.s.Pool.Query(ctx, q, args...)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		var out []archEdge
		for rows.Next() {
			var x archEdge
			if err := rows.Scan(&x.Kind, &x.Src.ID, &x.Src.Kind, &x.Src.Key, &x.Src.Name, &x.Src.RepoID,
				&x.Dst.ID, &x.Dst.Kind, &x.Dst.Key, &x.Dst.Name, &x.Dst.RepoID, &x.EdgeRepo); err != nil {
				return nil, err
			}
			out = append(out, x)
		}
		return out, rows.Err()
	}
	edgeCols := `x.kind, s.id, s.kind, s.key, s.name, s.repo_id, d.id, d.kind, d.key, d.name, d.repo_id, x.repo_id
		FROM edges x JOIN entities s ON s.id = x.src_id JOIN entities d ON d.id = x.dst_id
		WHERE x.deleted_at IS NULL AND s.deleted_at IS NULL AND d.deleted_at IS NULL`
	if in.Edges, err = scanEdges(`SELECT `+edgeCols+` AND x.kind <> 'contains'
		AND (x.repo_id = $1 OR s.repo_id = $1 OR d.repo_id = $1 OR s.id = ANY($2::uuid[]) OR d.id = ANY($2::uuid[])) LIMIT $3`,
		repoID, coreIDs, maxArchEdges); err != nil {
		return Architecture{}, fmt.Errorf("edges: %w", err)
	}
	topics := map[string]bool{}
	for _, x := range in.Edges {
		if x.Dst.Kind == "queue_topic" {
			topics[x.Dst.ID] = true
		}
	}
	if len(topics) > 0 {
		ids := make([]string, 0, len(topics))
		for id := range topics {
			ids = append(ids, id)
		}
		if in.TopicPeers, err = scanEdges(`SELECT `+edgeCols+` AND x.kind IN ('publishes', 'subscribes') AND d.id = ANY($1::uuid[]) LIMIT $2`,
			ids, maxArchEdges); err != nil {
			return Architecture{}, fmt.Errorf("topic peers: %w", err)
		}
	}
	rows, err := a.s.Pool.Query(ctx, `SELECT id, full_name FROM repos`)
	if err != nil {
		return Architecture{}, err
	}
	for rows.Next() {
		var id, name string
		if err := rows.Scan(&id, &name); err != nil {
			rows.Close()
			return Architecture{}, err
		}
		in.RepoNames[id] = name
	}
	rows.Close()

	out := buildArchitecture(in)
	out.Repo.ServiceName = serviceName
	if out.Diagrams, err = a.Diagrams(ctx, repoID); err != nil {
		return Architecture{}, err
	}
	var updated *time.Time
	_ = a.s.Pool.QueryRow(ctx, `SELECT max(last_seen) FROM entities WHERE repo_id = $1`, repoID).Scan(&updated)
	out.UpdatedAt = updated
	return out, nil
}

// ArchSummary is one row of the Architecture tab's repository list.
type ArchSummary struct {
	RepoID      string `json:"repo_id"`
	FullName    string `json:"full_name"`
	ServiceName string `json:"service_name,omitempty"`
	Endpoints   int    `json:"endpoints"`
	Modules     int    `json:"modules"`
	Topics      int    `json:"topics"`
	Datastores  int    `json:"datastores"`
	Diagrams    int    `json:"diagrams"`
}

// Summaries lists the repositories the caller can see with what their architecture contains.
func (a *ArchitectureStore) Summaries(ctx context.Context, sc rag.Scope) ([]ArchSummary, error) {
	rows, err := a.s.Pool.Query(ctx, `
		SELECT r.id, r.full_name, coalesce(r.service_name, ''),
		  (SELECT count(*) FROM entities e WHERE e.repo_id = r.id AND e.kind = 'endpoint' AND e.deleted_at IS NULL)::int,
		  (SELECT count(*) FROM entities e WHERE e.repo_id = r.id AND e.kind = 'module' AND e.deleted_at IS NULL)::int,
		  (SELECT count(DISTINCT x.dst_id) FROM edges x JOIN entities d ON d.id = x.dst_id
		     WHERE x.repo_id = r.id AND x.deleted_at IS NULL AND d.kind = 'queue_topic')::int,
		  (SELECT count(DISTINCT x.dst_id) FROM edges x JOIN entities d ON d.id = x.dst_id
		     WHERE x.repo_id = r.id AND x.deleted_at IS NULL AND d.kind = 'datastore')::int,
		  (SELECT count(*) FROM architecture_diagrams g WHERE g.repo_id = r.id)::int
		FROM repos r WHERE ($1::boolean OR r.id = ANY($2::uuid[])) ORDER BY r.full_name`, sc.All, ids(sc))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ArchSummary{}
	for rows.Next() {
		var s ArchSummary
		if err := rows.Scan(&s.RepoID, &s.FullName, &s.ServiceName, &s.Endpoints, &s.Modules, &s.Topics, &s.Datastores, &s.Diagrams); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// Diagrams lists a repository's authored diagrams.
func (a *ArchitectureStore) Diagrams(ctx context.Context, repoID string) ([]DiagramMeta, error) {
	rows, err := a.s.Pool.Query(ctx, `SELECT id, repo_id, path, title, generator, commit_sha, size_bytes, updated_at
		FROM architecture_diagrams WHERE repo_id = $1 ORDER BY path`, repoID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []DiagramMeta{}
	for rows.Next() {
		var d DiagramMeta
		if err := rows.Scan(&d.ID, &d.RepoID, &d.Path, &d.Title, &d.Generator, &d.CommitSHA, &d.SizeBytes, &d.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// DiagramHTML returns a diagram's HTML and its repository (for the access check).
func (a *ArchitectureStore) DiagramHTML(ctx context.Context, id string) (repoID, html string, err error) {
	if !uuidRE.MatchString(id) {
		return "", "", ports.ErrNotFound
	}
	err = a.s.Pool.QueryRow(ctx, `SELECT repo_id, html FROM architecture_diagrams WHERE id = $1`, id).Scan(&repoID, &html)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", "", ports.ErrNotFound
	}
	return repoID, html, err
}

// UpsertDiagram stores or replaces the diagram at path.
func (a *ArchitectureStore) UpsertDiagram(ctx context.Context, repoID, path, title, generator, html, sha string) error {
	_, err := a.s.Pool.Exec(ctx, `INSERT INTO architecture_diagrams (id, repo_id, path, title, generator, html, commit_sha, size_bytes)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		ON CONFLICT (repo_id, path) DO UPDATE SET title = EXCLUDED.title, generator = EXCLUDED.generator, html = EXCLUDED.html,
		  commit_sha = EXCLUDED.commit_sha, size_bytes = EXCLUDED.size_bytes, updated_at = now()`,
		ports.NewID(), repoID, path, title, generator, html, sha, len(html))
	return err
}

// DeleteDiagrams removes the diagrams at paths.
func (a *ArchitectureStore) DeleteDiagrams(ctx context.Context, repoID string, paths []string) error {
	_, err := a.s.Pool.Exec(ctx, `DELETE FROM architecture_diagrams WHERE repo_id = $1 AND path = ANY($2::text[])`, repoID, paths)
	return err
}

// KeepDiagrams removes every diagram of the repository not at one of paths (after a full scan).
func (a *ArchitectureStore) KeepDiagrams(ctx context.Context, repoID string, paths []string) (int64, error) {
	tag, err := a.s.Pool.Exec(ctx, `DELETE FROM architecture_diagrams WHERE repo_id = $1 AND NOT (path = ANY($2::text[]))`, repoID, paths)
	return tag.RowsAffected(), err
}
