package pipeline_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/GokulMV/DocTheRepo/internal/core/docrouter"
	"github.com/GokulMV/DocTheRepo/internal/core/llmgateway"
	"github.com/GokulMV/DocTheRepo/internal/core/pipeline"
	"github.com/GokulMV/DocTheRepo/internal/ports"
	"github.com/GokulMV/DocTheRepo/internal/store"
	"github.com/GokulMV/DocTheRepo/test/mocks/fakegw"
	"github.com/GokulMV/DocTheRepo/test/mocks/hostfixture"
)

const routed = `package shop

// Name returns the customer's display name, falling back to the email.
func (c *Customer) Name() string { return c.name }

func Discount(total int) int {
	if total > 100 {
		return total / 10
	}
	return 0
}
`

// Before anything is spent, the router decides how each piece of code gets its doc, and a full run or a
// revert reuses docs instead of paying for them again.
func TestDocRouterSpendsOnlyWhereNeeded(t *testing.T) {
	f := hostfixture.GitHub(t, map[string]string{"README.md": "# shop\n"})
	h := newHarness(t, f, fakegw.Options{})
	h.p.DocCache = store.NewDocCache(h.st)
	h.p.DocMode = docrouter.Balanced

	// Tiny commented code uses its comment; the rest goes to the model in one call.
	out, res := h.run(h.push(map[string]*string{"shop/customer.go": hostfixture.S(routed)}))
	require.Equal(t, ports.JobDone, out.Status, out.Message)
	assert.Equal(t, 1, res.Router["comment"])
	assert.Equal(t, 1, res.Router["full"])
	assert.Equal(t, 1, h.fg.Model.DocgenCalls())
	doc := h.doc("docs/generated/shop/customer.go.md")
	assert.Contains(t, doc, "Name returns the customer's display name, falling back to the email.", "the comment is the doc")
	assert.Contains(t, doc, "Documents `Discount`.")
	reqs := h.fg.Model.Reqs
	last := reqs[len(reqs)-1]
	assert.NotContains(t, last.Messages[0].Content, "Customer.Name", "the commented getter is not sent to the model")
	assert.Less(t, last.MaxOutputTokens, 2000, "output room is sized to what is asked")

	// A full run ("Generate docs now") on unchanged code costs nothing.
	out, res = h.run(pipeline.CodePushPayload{Full: true})
	require.Contains(t, []ports.JobStatus{ports.JobDone, ports.JobAborted}, out.Status, out.Message)
	assert.Equal(t, 1, h.fg.Model.DocgenCalls(), "no new calls")
	assert.Equal(t, 2, res.Router["unchanged"], "both docs are kept")

	// Removing a function and putting it back (a revert) reuses its earlier doc.
	without := strings.Replace(routed, routed[strings.Index(routed, "func Discount"):], "", 1)
	h.run(h.push(map[string]*string{"shop/customer.go": hostfixture.S(without)}))
	out, res = h.run(h.push(map[string]*string{"shop/customer.go": hostfixture.S(routed)}))
	require.Equal(t, ports.JobDone, out.Status, out.Message)
	assert.Equal(t, 1, h.fg.Model.DocgenCalls(), "the revert is documented from the cache")
	assert.Equal(t, 1, res.Router["reuse"])
	assert.Contains(t, h.doc("docs/generated/shop/customer.go.md"), "Documents `Discount`.")

	// With a fast route, short code goes to the cheaper model.
	h.fg.Routes[llmgateway.FeatureDocGenFast] = llmgateway.Route{Feature: "docgen_fast", ProviderID: "llm", ProviderKind: "anthropic", Model: "claude-haiku-4-5-20251001", MaxOutputTokens: 1000}
	out, res = h.run(h.push(map[string]*string{"shop/tax.go": hostfixture.S("package shop\n\nfunc Tax(total int) int {\n\treturn total * 2 / 10\n}\n")}))
	require.Equal(t, ports.JobDone, out.Status, out.Message)
	assert.Equal(t, 1, res.Router["fast"])
	var fast int
	for _, r := range h.fg.Ledger.Records {
		if r.Feature == llmgateway.FeatureDocGenFast {
			fast++
			assert.Equal(t, "claude-haiku-4-5-20251001", r.Model)
		}
	}
	assert.Equal(t, 1, fast, "billed to the fast route")
}
