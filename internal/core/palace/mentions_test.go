package palace

import (
	"reflect"
	"testing"
)

func TestMentions(t *testing.T) {
	idx := NewMentionIndex([]Entity{
		{Ref: ServiceRef("checkout"), Name: "checkout"},
		{Ref: ServiceRef("api"), Name: "api"}, // too short: would link every page
		{Ref: RepoRef("acme/payments-gw"), Name: "acme/payments-gw"},
		{Ref: EndpointRef("acme/shop", "POST", "/orders"), Name: "POST /orders"},
		{Ref: EnvVarRef("DATABASE_URL"), Name: "DATABASE_URL"}, // not a mentionable kind
	})
	got := idx.Mentions("Runbook: when Checkout returns 503 on POST /orders, restart acme/payments-gw. The api is fine. DATABASE_URL unchanged. checkout again.")
	want := []Ref{ServiceRef("checkout"), EndpointRef("acme/shop", "POST", "/orders"), RepoRef("acme/payments-gw")}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	if NewMentionIndex(nil).Mentions("checkout") != nil || (*MentionIndex)(nil).Mentions("x") != nil {
		t.Fatal("empty index mentions nothing")
	}
	if len(idx.Mentions("checkouts and precheckout")) != 0 {
		t.Fatal("whole tokens only")
	}
}
