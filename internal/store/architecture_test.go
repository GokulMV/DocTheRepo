package store

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func sp(s string) *string { return &s }

func TestBuildArchitectureLayers(t *testing.T) {
	const pay, ord, bill = "11111111-1111-1111-1111-111111111111", "22222222-2222-2222-2222-222222222222", "33333333-3333-3333-3333-333333333333"
	svc := archEntity{ID: "svc", Kind: "service", Key: "payments", Name: "payments"}
	ep := archEntity{ID: "ep1", Kind: "endpoint", Key: "acme/payments:POST /charges", Name: "POST /charges", RepoID: sp(pay)}
	sym := archEntity{ID: "sym", Kind: "symbol", Key: "acme/payments:charge.go#Charge", Name: "Charge", RepoID: sp(pay)}
	topic := archEntity{ID: "t1", Kind: "queue_topic", Key: "payments.settled", Name: "payments.settled"}
	inTopic := archEntity{ID: "t2", Kind: "queue_topic", Key: "orders.created", Name: "orders.created"}
	db := archEntity{ID: "db", Kind: "datastore", Key: "postgres:ledger", Name: "postgres:ledger"}
	ordSym := archEntity{ID: "osym", Kind: "symbol", Key: "acme/orders:x.go#Pay", Name: "Pay", RepoID: sp(ord)}
	billEp := archEntity{ID: "bep", Kind: "endpoint", Key: "acme/billing:POST /invoices", Name: "POST /invoices", RepoID: sp(bill)}
	stripe := archEntity{ID: "stripe", Kind: "endpoint", Key: "api.stripe.com", Name: "api.stripe.com"}
	env := archEntity{ID: "env", Kind: "env_var", Key: "STRIPE_KEY", Name: "STRIPE_KEY"}
	page := archEntity{ID: "pg", Kind: "confluence_page", Key: "123", Name: "Payments runbook"}
	notif := archEntity{ID: "nsym", Kind: "symbol", Key: "acme/notifications:h.go#OnSettled", Name: "OnSettled", RepoID: sp("44444444-4444-4444-4444-444444444444")}

	in := archInput{
		RepoID: pay, FullName: "acme/payments", RepoEntity: "repoEnt",
		Services:  []archEntity{svc},
		Endpoints: []archEntity{ep},
		Modules:   []archEntity{{ID: "mod", Kind: "module", Key: "acme/payments:internal/charge", Name: "internal/charge", RepoID: sp(pay)}},
		Degree:    map[string]int{"svc": 9, "ep1": 4},
		Edges: []archEdge{
			{Kind: "publishes", Src: sym, Dst: topic, EdgeRepo: sp(pay)},
			{Kind: "subscribes", Src: sym, Dst: inTopic, EdgeRepo: sp(pay)},
			{Kind: "uses_datastore", Src: sym, Dst: db, EdgeRepo: sp(pay)},
			{Kind: "calls", Src: ordSym, Dst: ep, EdgeRepo: sp(ord)},  // orders calls us
			{Kind: "calls", Src: sym, Dst: billEp, EdgeRepo: sp(pay)}, // we call billing (hidden from this viewer)
			{Kind: "calls", Src: sym, Dst: stripe, EdgeRepo: sp(pay)}, // an external API
			{Kind: "calls", Src: sym, Dst: stripe, EdgeRepo: sp(pay)},
			{Kind: "reads_env", Src: sym, Dst: env, EdgeRepo: sp(pay)},
			{Kind: "runbook_for", Src: page, Dst: svc},
			{Kind: "calls", Src: sym, Dst: sym, EdgeRepo: sp(pay)}, // internal: no box, no arrow
		},
		TopicPeers: []archEdge{
			{Kind: "subscribes", Src: notif, Dst: topic},
			{Kind: "publishes", Src: ordSym, Dst: inTopic},
			{Kind: "publishes", Src: sym, Dst: topic, EdgeRepo: sp(pay)}, // ourselves: ignored
		},
		RepoNames: map[string]string{ord: "acme/orders", bill: "acme/billing", "44444444-4444-4444-4444-444444444444": "acme/notifications"},
		Visible:   func(id string) bool { return id != bill },
	}
	a := buildArchitecture(in)

	layer := map[string]string{}
	for _, n := range a.Nodes {
		layer[n.ID] = n.Layer
	}
	assert.Equal(t, map[string]string{
		"svc": LayerCore, "mod": LayerCore, "api:/charges": LayerInterface,
		"t1": LayerMessaging, "t2": LayerMessaging, "db": LayerData,
		"repo:" + ord: LayerUpstream, "stripe": LayerDownstream, "repo:44444444-4444-4444-4444-444444444444": LayerDownstream,
	}, layer, "symbols lift to the service; other repositories become one box each")
	assert.Equal(t, 1, a.Restricted, "a repository the viewer cannot see is left out")
	_, hasBilling := layer["repo:"+bill]
	assert.False(t, hasBilling)

	has := func(src, kind, dst string) ArchLink {
		for _, l := range a.Links {
			if l.Src == src && l.Kind == kind && l.Dst == dst {
				return l
			}
		}
		t.Fatalf("missing link %s -%s-> %s in %+v", src, kind, dst, a.Links)
		return ArchLink{}
	}
	has("svc", "exposes", "api:/charges")
	has("svc", "contains", "mod")
	has("svc", "publishes", "t1")
	has("t2", "subscribes", "svc")
	has("svc", "uses_datastore", "db")
	has("repo:"+ord, "calls", "api:/charges")
	assert.Equal(t, 2, has("svc", "calls", "stripe").Weight)
	has("t1", "consumed by", "repo:44444444-4444-4444-4444-444444444444")
	has("repo:"+ord, "publishes", "t2")
	for _, l := range a.Links {
		assert.NotEqual(t, l.Src, l.Dst, "no self loops")
	}
	assert.Equal(t, []string{"STRIPE_KEY"}, a.Env)
	require.Len(t, a.Docs, 1)
	assert.Equal(t, "Payments runbook", a.Docs[0].Name)
	for i := 1; i < len(a.Nodes); i++ {
		assert.LessOrEqual(t, layerOrder[a.Nodes[i-1].Layer], layerOrder[a.Nodes[i].Layer], "nodes are ordered by layer")
	}
	assert.Equal(t, LayerUpstream, a.Nodes[0].Layer)
	assert.Equal(t, LayerDownstream, a.Nodes[len(a.Nodes)-1].Layer)
}

func TestBuildArchitectureWithoutServiceUsesRepoAndCaps(t *testing.T) {
	const r = "11111111-1111-1111-1111-111111111111"
	in := archInput{RepoID: r, FullName: "acme/tiny", Degree: map[string]int{}}
	for i := 0; i < 20; i++ {
		id := string(rune('a' + i))
		in.Endpoints = append(in.Endpoints, archEntity{ID: id, Kind: "endpoint", Key: id, Name: "GET /" + id, RepoID: sp(r)})
		in.Degree[id] = 20 - i
	}
	a := buildArchitecture(in)
	core := 0
	for _, n := range a.Nodes {
		if n.Layer == LayerCore {
			core++
			assert.Equal(t, "repo:"+r, n.ID)
			assert.Equal(t, "acme/tiny", n.Name)
		}
	}
	assert.Equal(t, 1, core)
	assert.Equal(t, 6, a.Hidden[LayerInterface], "20 endpoints, 14 shown")
	assert.Len(t, a.Links, 14, "links to hidden endpoints are dropped")
}

func TestArchitectureGroups(t *testing.T) {
	for in, want := range map[string]string{
		"GET /api/v1/connectors/{id}": "/connectors", "POST /v2/charges": "/charges", "GET /": "/", "GET /{id}": "/",
		"GET /api/health": "/health", "PUT /routes/${feature}": "/routes", "ANY /readyz": "/readyz",
	} {
		assert.Equal(t, want, endpointGroup(in), in)
	}
	mods := architectureModules([]archEntity{
		{ID: "1", Kind: "module", Key: "r:internal", Name: "internal"},
		{ID: "2", Kind: "module", Key: "r:internal/core", Name: "internal/core"},
		{ID: "3", Kind: "module", Key: "r:test/e2e", Name: "test/e2e"},
		{ID: "4", Kind: "module", Key: "r:web/src", Name: "web/src"},
		{ID: "5", Kind: "module", Key: "r:internal/store/storetest", Name: "internal/store/storetest"},
	})
	var names []string
	for _, m := range mods {
		names = append(names, m.Name)
	}
	assert.Equal(t, []string{"internal/core", "web/src"}, names, "containers and test folders are not boxes")

	// Endpoints and libraries are grouped, each counted once.
	repo := "11111111-1111-1111-1111-111111111111"
	ep := func(id, name string) archEntity {
		return archEntity{ID: id, Kind: "endpoint", Key: "r:" + name, Name: name, RepoID: &repo}
	}
	dep := func(id, key string) archEntity {
		return archEntity{ID: id, Kind: "dependency", Key: key, Name: key[4:]}
	}
	self := archEntity{ID: "re", Kind: "repo", Key: "r", Name: "r", RepoID: &repo}
	a := buildArchitecture(archInput{RepoID: repo, FullName: "acme/r", RepoEntity: "re",
		Endpoints: []archEntity{ep("e1", "GET /api/v1/repos"), ep("e2", "POST /api/v1/repos"), ep("e3", "GET /api/v1/users")},
		Edges: []archEdge{
			{Kind: "depends_on", Src: self, Dst: dep("d1", "npm:react"), EdgeRepo: &repo},
			{Kind: "depends_on", Src: self, Dst: dep("d2", "npm:vite"), EdgeRepo: &repo},
			{Kind: "depends_on", Src: self, Dst: dep("d3", "go:github.com/go-chi/chi/v5"), EdgeRepo: &repo},
		}})
	byID := map[string]ArchNode{}
	for _, n := range a.Nodes {
		byID[n.ID] = n
	}
	assert.Equal(t, 2, byID["api:/repos"].Count)
	assert.Equal(t, []string{"GET /api/v1/repos", "POST /api/v1/repos"}, byID["api:/repos"].Items)
	assert.Equal(t, KindEndpointGroup, byID["api:/users"].Kind)
	assert.Equal(t, "npm packages", byID["deps:npm"].Name)
	assert.Equal(t, 2, byID["deps:npm"].Count)
	assert.Equal(t, "Go modules", byID["deps:go"].Name)
	assert.Equal(t, LayerDownstream, byID["deps:go"].Layer)
}

func TestSortDiagrams(t *testing.T) {
	ds := []DiagramMeta{{Title: "Flow A — Code push"}, {Title: "Flow D — Knowledge sync"}, {Title: "Flow C — Q&A"}, {Title: "Flow B — Signals"},
		{Title: "DocTheRepo Hub — System Architecture"}, {Title: "Flow 10"}, {Title: "Flow 2"}}
	sortDiagrams(ds)
	var got []string
	for _, d := range ds {
		got = append(got, d.Title)
	}
	assert.Equal(t, []string{"DocTheRepo Hub — System Architecture", "Flow 2", "Flow 10", "Flow A — Code push", "Flow B — Signals", "Flow C — Q&A", "Flow D — Knowledge sync"}, got)
}
