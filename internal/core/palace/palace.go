// Package palace builds the knowledge graph ("Palace", plan § 4.4, § 8.5) deterministically from code:
// no LLM, so it is cheap on every push, reproducible, and never hallucinates an edge. Every edge carries
// evidence (file, line, commit).
package palace

import (
	"path"
	"sort"
	"strings"

	"github.com/GokulMV/DocTheRepo/internal/core/chunker"
)

// Entity kinds.
const (
	KindRepo           = "repo"
	KindModule         = "module"
	KindFile           = "file"
	KindSymbol         = "symbol"
	KindEndpoint       = "endpoint"
	KindEnvVar         = "env_var"
	KindDependency     = "dependency"
	KindDatastore      = "datastore"
	KindQueueTopic     = "queue_topic"
	KindService        = "service"
	KindCloudResource  = "cloud_resource"
	KindIssue          = "issue"
	KindKnownIssue     = "known_issue"
	KindConfluencePage = "confluence_page"
	KindJiraIssue      = "jira_issue"
	KindNotionPage     = "notion_page"
	KindDocument       = "document" // an uploaded document
	KindTeam           = "team"
	KindPerson         = "person"
)

// Edge kinds.
const (
	EdgeContains      = "contains"
	EdgeCalls         = "calls"
	EdgeExposes       = "exposes"
	EdgeReadsEnv      = "reads_env"
	EdgeDependsOn     = "depends_on"
	EdgeUsesDatastore = "uses_datastore"
	EdgePublishes     = "publishes"
	EdgeSubscribes    = "subscribes"
	EdgeDocumentedIn  = "documented_in"
	EdgeRaises        = "raises"
	EdgeOwnedBy       = "owned_by"
	EdgeRunbookFor    = "runbook_for"
	EdgeDeployedAs    = "deployed_as"
	EdgeImports       = "imports" // file → module (directory) of the same repository that it imports
)

// Ref identifies an entity by kind and key; keys are unique per kind.
type Ref struct {
	Kind string `json:"kind"`
	Key  string `json:"key"`
}

// Entity is a node.
type Entity struct {
	Ref
	Name  string            `json:"name"`
	Repo  string            `json:"repo,omitempty"`
	Attrs map[string]string `json:"attrs,omitempty"`
}

// Evidence is where an edge was observed.
type Evidence struct {
	Path   string `json:"path,omitempty"`
	Line   int    `json:"line,omitempty"`
	Commit string `json:"commit,omitempty"`
}

// Edge is a directed, typed relation.
type Edge struct {
	Src      Ref      `json:"src"`
	Dst      Ref      `json:"dst"`
	Kind     string   `json:"kind"`
	Evidence Evidence `json:"evidence"`
}

// Key returns the edge's identity (src, kind, dst).
func (e Edge) Key() string {
	return e.Src.Kind + ":" + e.Src.Key + "|" + e.Kind + "|" + e.Dst.Kind + ":" + e.Dst.Key
}

// Graph is a set of entities and edges with de-duplication.
type Graph struct {
	Entities []Entity `json:"entities"`
	Edges    []Edge   `json:"edges"`
	ent      map[Ref]int
	edg      map[string]bool
}

// AddEntity inserts or merges an entity (attributes merge; the first name wins).
func (g *Graph) AddEntity(e Entity) Ref {
	if g.ent == nil {
		g.ent = map[Ref]int{}
	}
	if i, ok := g.ent[e.Ref]; ok {
		for k, v := range e.Attrs {
			if g.Entities[i].Attrs == nil {
				g.Entities[i].Attrs = map[string]string{}
			}
			g.Entities[i].Attrs[k] = v
		}
		return e.Ref
	}
	g.ent[e.Ref] = len(g.Entities)
	g.Entities = append(g.Entities, e)
	return e.Ref
}

// AddEdge inserts an edge once (the first evidence wins).
func (g *Graph) AddEdge(src Ref, kind string, dst Ref, ev Evidence) {
	if g.edg == nil {
		g.edg = map[string]bool{}
	}
	e := Edge{Src: src, Dst: dst, Kind: kind, Evidence: ev}
	if g.edg[e.Key()] {
		return
	}
	g.edg[e.Key()] = true
	g.Edges = append(g.Edges, e)
}

// Merge adds everything from o.
func (g *Graph) Merge(o Graph) {
	for _, e := range o.Entities {
		g.AddEntity(e)
	}
	for _, e := range o.Edges {
		g.AddEdge(e.Src, e.Kind, e.Dst, e.Evidence)
	}
}

// Has reports whether an entity exists.
func (g *Graph) Has(r Ref) bool { _, ok := g.ent[r]; return ok }

// Find returns an entity.
func (g *Graph) Find(r Ref) (Entity, bool) {
	i, ok := g.ent[r]
	if !ok {
		return Entity{}, false
	}
	return g.Entities[i], true
}

// EdgesOf returns edges of a kind (all kinds when kind is "").
func (g *Graph) EdgesOf(kind string) []Edge {
	var out []Edge
	for _, e := range g.Edges {
		if kind == "" || e.Kind == kind {
			out = append(out, e)
		}
	}
	return out
}

// Delta is the change between two graphs of the same scope (e.g. one file's contribution before/after).
type Delta struct {
	AddedEntities   []Entity `json:"added_entities"`
	RemovedEntities []Ref    `json:"removed_entities"`
	AddedEdges      []Edge   `json:"added_edges"`
	RemovedEdges    []Edge   `json:"removed_edges"`
}

// Diff compares old and new graphs. Shared entities such as env vars and topics appear in many files;
// the store only deletes an entity when no live edge references it, so removal here is edge-driven.
func Diff(old, new Graph) Delta {
	var d Delta
	oldE := map[Ref]bool{}
	for _, e := range old.Entities {
		oldE[e.Ref] = true
	}
	newE := map[Ref]bool{}
	for _, e := range new.Entities {
		newE[e.Ref] = true
		if !oldE[e.Ref] {
			d.AddedEntities = append(d.AddedEntities, e)
		}
	}
	for _, e := range old.Entities {
		if !newE[e.Ref] {
			d.RemovedEntities = append(d.RemovedEntities, e.Ref)
		}
	}
	oldEd, newEd := map[string]Edge{}, map[string]Edge{}
	for _, e := range old.Edges {
		oldEd[e.Key()] = e
	}
	for _, e := range new.Edges {
		newEd[e.Key()] = e
		if _, ok := oldEd[e.Key()]; !ok {
			d.AddedEdges = append(d.AddedEdges, e)
		}
	}
	for k, e := range oldEd {
		if _, ok := newEd[k]; !ok {
			d.RemovedEdges = append(d.RemovedEdges, e)
		}
	}
	sort.Slice(d.RemovedEdges, func(i, j int) bool { return d.RemovedEdges[i].Key() < d.RemovedEdges[j].Key() })
	return d
}

// Refs for the structural hierarchy.

// RepoRef identifies a repository.
func RepoRef(repo string) Ref { return Ref{KindRepo, repo} }

// ModuleRef identifies a directory within a repository.
func ModuleRef(repo, dir string) Ref { return Ref{KindModule, repo + ":" + dir} }

// FileRef identifies a file.
func FileRef(repo, p string) Ref { return Ref{KindFile, repo + ":" + p} }

// SymbolRef identifies a definition.
func SymbolRef(repo, p, symbol string) Ref { return Ref{KindSymbol, repo + ":" + p + "#" + symbol} }

// EndpointRef identifies an HTTP endpoint exposed by a repo.
func EndpointRef(repo, method, route string) Ref {
	return Ref{KindEndpoint, repo + ":" + method + " " + route}
}

// EnvVarRef identifies an environment variable (global: shared names link services).
func EnvVarRef(name string) Ref { return Ref{KindEnvVar, name} }

// TopicRef identifies a queue/topic (global: this is what links producers to consumers across repos).
func TopicRef(name string) Ref { return Ref{KindQueueTopic, name} }

// ServiceRef identifies a deployed service.
func ServiceRef(name string) Ref { return Ref{KindService, name} }

// ExtractFile derives the graph contribution of one analysed source file. resolve, when non-nil, maps a
// callee to a definition in another file (one hop via imports); same-file calls are always resolved.
func ExtractFile(repo, filePath, commit string, a *chunker.FileAnalysis, resolve func(callee string) (string, string, bool)) Graph {
	var g Graph
	ev := func(line int) Evidence { return Evidence{Path: filePath, Line: line, Commit: commit} }
	r := g.AddEntity(Entity{Ref: RepoRef(repo), Name: repo, Repo: repo})
	parent := r
	dir := path.Dir(filePath)
	if dir != "." {
		// Every directory level is a module so the tree can be navigated from the repo root.
		parts := strings.Split(dir, "/")
		for i := range parts {
			d := strings.Join(parts[:i+1], "/")
			m := g.AddEntity(Entity{Ref: ModuleRef(repo, d), Name: d, Repo: repo})
			g.AddEdge(parent, EdgeContains, m, Evidence{Commit: commit})
			parent = m
		}
	}
	f := g.AddEntity(Entity{Ref: FileRef(repo, filePath), Name: filePath, Repo: repo, Attrs: map[string]string{"language": a.Language}})
	g.AddEdge(parent, EdgeContains, f, Evidence{Commit: commit})

	byName := map[string][]string{}
	for _, d := range a.Definitions {
		n := lastName(d.Symbol)
		byName[n] = append(byName[n], d.Symbol)
	}
	defs := append([]chunker.Definition{}, a.Definitions...)
	if a.Module != nil {
		defs = append(defs, *a.Module)
	}
	classRoutes := classRoutePrefixes(a)
	for _, d := range defs {
		var s Ref
		if d.Symbol == chunker.ModuleSymbol {
			s = f // top-level code is attributed to the file
		} else {
			s = g.AddEntity(Entity{Ref: SymbolRef(repo, filePath, d.Symbol), Name: d.Symbol, Repo: repo,
				Attrs: map[string]string{"kind": d.Kind, "path": filePath, "signature": oneLine(d.Signature)}})
			g.AddEdge(f, EdgeContains, s, ev(d.StartLine))
		}
		for _, c := range d.Calls {
			if targets, ok := byName[c.Name]; ok && (strings.HasPrefix(c.Callee, c.Name) || isSelfCall(c.Callee)) {
				for _, t := range targets {
					if t != d.Symbol {
						g.AddEdge(s, EdgeCalls, SymbolRef(repo, filePath, t), ev(c.Line))
					}
				}
			} else if resolve != nil {
				if p, sym, ok := resolve(c.Callee); ok {
					g.AddEdge(s, EdgeCalls, SymbolRef(repo, p, sym), ev(c.Line))
				}
			}
		}
		for _, e := range d.EnvReads {
			g.AddEntity(Entity{Ref: EnvVarRef(e.Name), Name: e.Name})
			g.AddEdge(s, EdgeReadsEnv, EnvVarRef(e.Name), ev(e.Line))
			if ds := datastoreFromEnv(e.Name); ds != "" {
				dr := g.AddEntity(Entity{Ref: Ref{KindDatastore, ds + ":" + e.Name}, Name: ds + " (" + e.Name + ")", Attrs: map[string]string{"type": ds, "env": e.Name}})
				g.AddEdge(s, EdgeUsesDatastore, dr, ev(e.Line))
			}
		}
		for _, ep := range endpoints(a.Language, d, classRoutes[parentOf(d.Symbol)]) {
			er := g.AddEntity(Entity{Ref: EndpointRef(repo, ep.method, ep.route), Name: ep.method + " " + ep.route, Repo: repo,
				Attrs: map[string]string{"method": ep.method, "route": ep.route}})
			g.AddEdge(s, EdgeExposes, er, ev(ep.line))
		}
		for _, tp := range topics(d) {
			tr := g.AddEntity(Entity{Ref: TopicRef(tp.name), Name: tp.name})
			g.AddEdge(s, tp.edge, tr, ev(tp.line))
		}
		for _, c := range d.Calls {
			if ds := datastoreFromCall(c); ds != "" {
				dr := g.AddEntity(Entity{Ref: Ref{KindDatastore, ds + ":" + repo}, Name: ds + " (" + repo + ")", Repo: repo, Attrs: map[string]string{"type": ds}})
				g.AddEdge(s, EdgeUsesDatastore, dr, ev(c.Line))
			}
		}
	}
	return g
}

// ExtractImports derives a file's imports of packages in the same repository, as file → module edges.
// Only Go for now: an import path under goModule (the module path from go.mod) names a directory of the
// repository exactly, and stdlib and external imports never match it. A call through a value (a method
// on a struct field) cannot be resolved without types, but the import that brings the type in always
// can, and that is what "used by" between modules needs.
func ExtractImports(repo, filePath, commit string, a *chunker.FileAnalysis, goModule string) Graph {
	var g Graph
	if a == nil || a.Language != "go" || goModule == "" {
		return g
	}
	own := path.Dir(filePath)
	var f Ref
	for _, im := range a.Imports {
		dir, ok := strings.CutPrefix(im.Path, goModule+"/")
		if !ok || dir == "" || dir == own {
			continue
		}
		if f.Key == "" {
			f = g.AddEntity(Entity{Ref: FileRef(repo, filePath), Name: filePath, Repo: repo, Attrs: map[string]string{"language": a.Language}})
		}
		m := g.AddEntity(Entity{Ref: ModuleRef(repo, dir), Name: dir, Repo: repo})
		g.AddEdge(f, EdgeImports, m, Evidence{Path: filePath, Line: im.Line, Commit: commit})
	}
	return g
}

func isSelfCall(callee string) bool {
	return strings.HasPrefix(callee, "this.") || strings.HasPrefix(callee, "self.") || strings.HasPrefix(callee, "Self::")
}

func parentOf(symbol string) string {
	if i := strings.IndexByte(symbol, '('); i > 0 {
		symbol = symbol[:i]
	}
	if i := strings.LastIndexByte(symbol, '.'); i > 0 {
		return symbol[:i]
	}
	return ""
}

func lastName(symbol string) string {
	if i := strings.IndexByte(symbol, '('); i > 0 {
		symbol = symbol[:i]
	}
	if i := strings.LastIndexByte(symbol, '.'); i >= 0 {
		return symbol[i+1:]
	}
	return symbol
}

func oneLine(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	if len(s) > 300 {
		s = s[:300]
	}
	return s
}
