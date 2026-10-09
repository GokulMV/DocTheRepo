package repodocs

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/GokulMV/DocTheRepo/internal/core/llmgateway"
	"github.com/GokulMV/DocTheRepo/internal/ports"
)

// SystemRepo is one repository as the System architecture sees it: its summary and architecture.
type SystemRepo struct {
	ID           string
	Name         string
	Overview     string // the Overview's "At a glance" and "What it does"
	Architecture string // style, components and flow, clipped
	// Hash identifies what was read (the documents' inputs).
	Hash string
}

// SystemInput is what the System architecture is written from.
type SystemInput struct {
	Repos []SystemRepo
	Links []SystemLink
	Head  string // a fingerprint of the repositories' commits, for the record
}

// SystemSpec is the document across repositories (shown as its own page).
var SystemSpec = Spec{
	Type: "system", Title: "System architecture", Group: "System", Order: 0, Applies: always,
	Audience: "everyone working across services, including non-engineers for the map", Purpose: "How the repositories work together as one system.",
	Sections: []Section{
		s("map", "The system at a glance", 0, true, false, "A table: Repository | Its role in the system (one line). Then two or three sentences on how the whole fits together."),
		s("interactions", "How the parts talk", 0, true, true, "Explain the diagram that is provided (do not redraw it), then a table: From | To | How (API, event, library, call, image, pipeline) | What is exchanged | Where [repo/path:line]. Links found in configuration (a host one calls that another serves) and in pipelines (CI that uses, checks out or triggers another repository; an image one runs that another publishes) count as interactions too."),
		s("flows", "End-to-end flows across services", 500, true, true, "The 2-4 most important journeys that cross repositories, step by step, each with a mermaid sequenceDiagram whose participants are repositories."),
		s("contracts", "Shared contracts", 0, false, true, "For each shared API, event or package: producer, consumers, what is in it, and what would break consumers if it changed."),
		s("coupling", "Coupling and risks", 200, true, false, "Where the parts are tightly coupled, cycles, single points of failure, and any mismatch the material shows (a consumer expecting what a producer does not send)."),
		s("dos", "Rules for changes across services", 150, true, false, "Do's and don'ts when changing something other repositories depend on."),
	},
}

// SystemHash covers the System architecture's inputs: the links and what each repository's documents say.
func SystemHash(in SystemInput) string {
	parts := []string{SpecVersion, "system"}
	for _, r := range in.Repos {
		parts = append(parts, r.ID, r.Hash)
	}
	for _, l := range in.Links {
		parts = append(parts, l.FromRepo, l.ToRepo, l.Kind, l.Via)
	}
	return Hash(parts...)
}

// SystemDiagram draws the repositories and how they talk, from the links (never by the model).
func SystemDiagram(in SystemInput) string {
	var b strings.Builder
	b.WriteString("flowchart LR\n")
	id := map[string]string{}
	for i, r := range in.Repos {
		id[r.ID] = fmt.Sprintf("r%d", i)
		fmt.Fprintf(&b, "  r%d[\"%s\"]\n", i, strings.ReplaceAll(r.Name, `"`, "'"))
	}
	type edge struct{ a, b, label string }
	seen := map[edge]bool{}
	var edges []edge
	for _, l := range in.Links {
		label := l.Kind
		if l.Kind == "event" || l.Kind == "api" || l.Kind == "library" || l.Kind == "image" || l.Kind == "pipeline" {
			label = l.Kind + ": " + clipLabel(l.Via)
		}
		e := edge{id[l.FromRepo], id[l.ToRepo], label}
		if e.a == "" || e.b == "" || seen[e] {
			continue
		}
		seen[e] = true
		edges = append(edges, e)
	}
	sort.Slice(edges, func(i, j int) bool {
		return edges[i].a+edges[i].b+edges[i].label < edges[j].a+edges[j].b+edges[j].label
	})
	for i, e := range edges {
		if i == 60 {
			break
		}
		fmt.Fprintf(&b, "  %s -->|\"%s\"| %s\n", e.a, strings.ReplaceAll(e.label, `"`, "'"), e.b)
	}
	return b.String()
}

func clipLabel(s string) string {
	if len(s) > 32 {
		return s[:31] + "…"
	}
	return s
}

// systemFacts is a stand-in Facts so the checks work across repositories: citations are
// repository-qualified paths (acme/shop/internal/x.go:12) and the known names are the links'.
func systemFacts(in SystemInput) *Facts {
	f := &Facts{Repo: "system"}
	for _, r := range in.Repos {
		f.AllPaths = append(f.AllPaths, r.Name)
		f.Facts = append(f.Facts, Fact{Kind: "service", Name: r.Name})
	}
	for _, l := range in.Links {
		if l.Path != "" {
			f.AllPaths = append(f.AllPaths, l.FromName+"/"+l.Path)
		}
		f.Facts = append(f.Facts, Fact{Kind: l.Kind, Name: l.Via})
	}
	return f
}

// WriteSystem writes the System architecture.
func (g *Generator) WriteSystem(ctx context.Context, in SystemInput, prev *Doc, o RunOptions) (Doc, error) {
	route, err := g.GW.Route(ctx, llmgateway.FeatureDocGen)
	if err != nil {
		return Doc{}, err
	}
	spec := SystemSpec
	f := systemFacts(in)
	d := Doc{Type: "system", Key: "system", Title: spec.Title, Group: spec.Group, InputsHash: SystemHash(in), SourceSHA: in.Head, Status: "failed", UpdatedAt: g.now()}
	if prev != nil {
		d.ID = prev.ID
	}
	var mat strings.Builder
	mat.WriteString("Citations here are repository-qualified: cite as [owner/repo/path/file.go:12], exactly as shown.\n\n## Repositories\n")
	for _, r := range in.Repos {
		fmt.Fprintf(&mat, "### %s\n%s\n\n%s\n\n", r.Name, clip(r.Overview, 3000), clip(r.Architecture, 6000))
	}
	mat.WriteString("## How they talk (from the code)\n")
	for _, l := range in.Links {
		where := ""
		if l.Path != "" {
			where = fmt.Sprintf(" [%s/%s:%d]", l.FromName, l.Path, max(l.Line, 1))
		}
		fmt.Fprintf(&mat, "- %s → %s: %s `%s` (%d places)%s\n", l.FromName, l.ToName, l.Kind, l.Via, l.N, where)
	}
	diagram := SystemDiagram(in)
	fmt.Fprintf(&mat, "\n## Diagram (already drawn; explain it, do not redraw)\n```mermaid\n%s```\n", diagram)
	user := prompt(&env{facts: &Facts{Repo: "all tracked repositories"}}, job{spec: spec}, mat.String())
	known := f.Known()
	var w written
	var res checkResult
	chk := func() []string {
		res = check(spec, &w, f, known, mat.String())
		return res.hard
	}
	r, err := g.GW.ChatJSONResult(ctx, llmgateway.FeatureDocGen, o.Meta, ports.ChatRequest{System: system,
		Messages: []ports.ChatMessage{{Role: "user", Content: user}}, MaxOutputTokens: min(max(route.MaxOutputTokens, 24000), 32000)}, docSchema, &w, chk)
	d.TokensIn, d.TokensOut, d.Model, d.DraftProblems = inputTokens(r.Usage), r.Usage.OutputTokens, r.Model, r.Repaired
	d.CostUSD = g.usageCost(route.ProviderKind, r.Model, llmgateway.FeatureDocGen, r.Usage)
	if err != nil {
		d.Error = err.Error()
		return d, err
	}
	d.AtAGlance, d.Gaps = strings.TrimSpace(w.AtAGlance), w.Gaps
	got := map[string]string{}
	for _, sec := range w.Sections {
		got[sec.Key] = strings.TrimSpace(sec.Markdown)
	}
	for _, sec := range spec.Sections {
		md := got[sec.Key]
		if md == "" {
			continue
		}
		if sec.Key == "interactions" {
			md = "```mermaid\n" + diagram + "```\n\n" + md
		}
		md = DropDeadRefs(md, f)
		d.Sections = append(d.Sections, DocSection{Key: sec.Key, Title: sec.Title, Markdown: md})
	}
	g.score(ctx, &env{facts: f, files: map[string]*File{}}, job{spec: spec}, &d, res, RunOptions{})
	d.Status = "ok"
	return d, nil
}
