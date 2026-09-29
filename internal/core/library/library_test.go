package library

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDefaultShelves_Assign(t *testing.T) {
	c, err := Compile(DefaultShelves())
	require.NoError(t, err)
	cases := []struct {
		item Item
		want []string
	}{
		{Item{Type: ItemEntity, EntityKind: "endpoint", Title: "GET /orders"}, []string{"apis"}},
		{Item{Type: ItemEntity, EntityKind: "service", Title: "checkout"}, []string{"architecture", "services"}},
		{Item{Type: ItemEntity, EntityKind: "queue_topic"}, []string{"events"}},
		{Item{Type: ItemEntity, EntityKind: "datastore"}, []string{"data"}},
		{Item{Type: ItemEntity, EntityKind: "env_var"}, []string{"config"}},
		{Item{Type: ItemEntity, EntityKind: "dependency"}, []string{"dependencies"}},
		{Item{Type: ItemDocNode, Path: "docs/adr/0003-use-postgres.md"}, []string{"decisions"}},
		{Item{Type: ItemDocNode, Path: "ops/runbooks/db-failover.md"}, []string{"runbooks"}},
		{Item{Type: ItemConfluencePage, Labels: []string{"Runbook"}, Source: "confluence"}, []string{"runbooks", "confluence"}},
		{Item{Type: ItemKnownIssue, Title: "Timeout from payments gateway"}, []string{"known-issues"}},
		{Item{Type: ItemJiraIssue, Labels: []string{"known-issue"}}, []string{"known-issues", "jira"}},
		{Item{Type: ItemEntity, EntityKind: "issue", Source: "wiz"}, []string{"security"}},
		{Item{Type: ItemDocNode, Path: "docs/generated/internal/core/triage.go.md"}, nil},
	}
	for _, tc := range cases {
		assert.Equal(t, tc.want, c.Assign(tc.item), "%+v", tc.item)
	}
	assert.Equal(t, "architecture", c.Shelves()[0].Slug)
}

func TestCuratedShelvesAreNeverRuleFilled(t *testing.T) {
	c, err := Compile([]Shelf{{Slug: "onboarding", Curated: true, Rules: []Rule{{ItemTypes: []string{ItemDocNode}}}}})
	require.NoError(t, err)
	assert.Empty(t, c.Assign(Item{Type: ItemDocNode, Path: "x.md"}))
}

func TestRuleSemantics(t *testing.T) {
	c, err := Compile([]Shelf{
		{Slug: "and", Rules: []Rule{{ItemTypes: []string{ItemDocNode}, PathGlobs: []string{"**/payments/**"}, TitleRegex: "refund"}}},
		{Slug: "empty", Rules: []Rule{{}}},
	})
	require.NoError(t, err)
	assert.Equal(t, []string{"and"}, c.Assign(Item{Type: ItemDocNode, Path: "svc/payments/x.md", Title: "Refund flow"}))
	assert.Empty(t, c.Assign(Item{Type: ItemDocNode, Path: "svc/payments/x.md", Title: "Charge"}), "all conditions must hold")
	assert.Empty(t, c.Assign(Item{Type: ItemEntity, Path: "svc/payments/x.md", Title: "refund"}))
	assert.Empty(t, c.Assign(Item{Type: ItemDocNode, Title: "refund"}), "a glob rule needs a path")
}

func TestCompile_Errors(t *testing.T) {
	_, err := Compile([]Shelf{{Slug: "a"}, {Slug: "a"}})
	assert.Error(t, err)
	_, err = Compile([]Shelf{{Slug: ""}})
	assert.Error(t, err)
	_, err = Compile([]Shelf{{Slug: "x", Rules: []Rule{{TitleRegex: "("}}}})
	assert.ErrorContains(t, err, "title_regex")
}
