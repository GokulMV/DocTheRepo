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
		"svc": LayerCore, "mod": LayerCore, "ep1": LayerInterface,
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
	has("svc", "exposes", "ep1")
	has("svc", "contains", "mod")
	has("svc", "publishes", "t1")
	has("t2", "subscribes", "svc")
	has("svc", "uses_datastore", "db")
	has("repo:"+ord, "calls", "ep1")
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
