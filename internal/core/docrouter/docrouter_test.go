package docrouter

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/GokulMV/DocTheRepo/internal/ports"
)

func TestSplit(t *testing.T) {
	c, code := Split("// Add returns the sum.\n// It never overflows.\nfunc Add(a, b int) int { return a + b }", "go")
	assert.Equal(t, "Add returns the sum. It never overflows.", c)
	assert.Equal(t, "func Add(a, b int) int { return a + b }", code)
	c, _ = Split("/**\n * Loads the user.\n *\n * Throws when missing.\n */\nfunction load() {}", "javascript")
	assert.Equal(t, "Loads the user.\n\nThrows when missing.", c)
	c, code = Split("def total(xs):\n    \"\"\"Sum of xs, ignoring None.\"\"\"\n    return sum(x for x in xs if x)", "python")
	assert.Equal(t, "Sum of xs, ignoring None.", c)
	assert.NotContains(t, code, "Sum of xs")
	c, _ = Split("//nolint:gocyclo\nfunc F() {}", "go")
	assert.Empty(t, c, "lint directives are not docs")
	c, _ = Split("#!/bin/sh\necho hi", "bash")
	assert.Empty(t, c)
}

func TestDecide(t *testing.T) {
	tiny := ports.Chunk{Symbol: "User.Name", Language: "go", Content: "// Name returns the user's display name.\nfunc (u *User) Name() string { return u.name }"}
	bare := ports.Chunk{Symbol: "f", Language: "go", Content: "func f() int {\n\treturn 1\n}"}
	big := ports.Chunk{Symbol: "g", Language: "go", Content: "// g does a lot.\nfunc g() {\n" + repeat("\tx++\n", 40) + "}"}

	assert.Equal(t, Reuse, Decide(big, Balanced, "cached doc", true).Tier, "cached docs always win")
	d := Decide(tiny, Balanced, "", true)
	assert.Equal(t, Comment, d.Tier)
	assert.Equal(t, "Name returns the user's display name.", d.Body)
	assert.Equal(t, Fast, Decide(bare, Balanced, "", true).Tier, "short, uncommented: the fast model")
	assert.Equal(t, Full, Decide(bare, Balanced, "", false).Tier, "no fast route: the full model")
	assert.Equal(t, Full, Decide(big, Balanced, "", true).Tier)
	assert.Equal(t, Full, Decide(tiny, Thorough, "", true).Tier, "thorough never skips the model")
	assert.Equal(t, Comment, Decide(ports.Chunk{Language: "go", Content: "// g walks the order book, matching bids and asks by price then time.\nfunc g() {\n" + repeat("\tx++\n", 30) + "}"}, Economy, "", true).Tier)
	assert.Equal(t, Balanced, ParseMode(""))
	assert.Equal(t, Economy, ParseMode("Economy"))
}

func repeat(s string, n int) string {
	out := ""
	for range n {
		out += s
	}
	return out
}
