package repodocs

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestReportGroupsDraftProblemsAndFlagsSections(t *testing.T) {
	long := strings.Repeat("word ", 300) // "what" aims for 120 words
	docs := []Doc{
		{Type: "overview", Key: "overview", Title: "Overview", Status: "ok", Model: "m1", TokensIn: 1000, TokensOut: 200, CostUSD: 0.01, Confidence: 0.85,
			DraftProblems: []string{"concepts: cites [internal/x.go:12] which is not in the material", "at_a_glance is empty: write 3-5 plain sentences"},
			Sections:      []DocSection{{Key: "what", Title: "What it does", Markdown: long, Score: 0.9}, {Key: "concepts", Title: "Key concepts", Markdown: "Two words.", Score: 0.4, Why: []string{"few citations"}}}},
		{Type: "architecture", Key: "architecture", Title: "Architecture", Status: "ok", Model: "m1", TokensIn: 3000, TokensOut: 900, CostUSD: 0.03, Confidence: 0.5,
			DraftProblems: []string{"flows: cites [cmd/main.go:99] which is not in the material"}},
		{Type: "glossary", Key: "glossary", Title: "Glossary", Status: "failed", Error: "schema: $.sections: missing"},
	}
	r := BuildReport("acme/shop", docs, false)
	assert.Equal(t, 3, r.Docs)
	assert.Equal(t, 2, r.OK)
	assert.Equal(t, 1, r.Failed)
	assert.Equal(t, 2, r.Repaired)
	assert.Equal(t, int64(4000), r.TokensIn)
	assert.InDelta(t, 0.04, r.CostUSD, 1e-9)
	assert.Equal(t, 0.68, r.Confidence)
	assert.Equal(t, map[string]int{"high": 1, "medium": 0, "low": 1}, r.Labels)
	require.NotEmpty(t, r.Problems)
	assert.Equal(t, 2, r.Problems[0].N, "the same problem in two sections and documents groups together: %+v", r.Problems)
	assert.Equal(t, "<section>: cites […] which is not in the material", r.Problems[0].Kind)

	ov := r.Items[0]
	assert.Equal(t, "long", ov.Sections[0].Length)
	assert.Equal(t, "short", ov.Sections[1].Length)
	assert.Contains(t, ov.Missing, "map", "required sections not written are listed")
	assert.Empty(t, ov.Sections[0].Markdown, "no prose without text")

	md := r.Markdown()
	assert.Contains(t, md, "| First drafts repaired | 2 of 3 |")
	assert.Contains(t, md, "## Failed documents")
	assert.Contains(t, md, "| Overview | Key concepts | 0.40 | few citations |")
	assert.NotContains(t, md, "word word", "no prose without text")

	withText := BuildReport("acme/shop", docs, true).Markdown()
	assert.Contains(t, withText, "word word")
}
