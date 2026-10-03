package docconv

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConvert(t *testing.T) {
	title, md, err := Convert("runbooks/db-failover.md", []byte("# Database failover\r\n\r\nPromote the replica.\r\n"))
	require.NoError(t, err)
	assert.Equal(t, "Database failover", title)
	assert.Equal(t, "# Database failover\n\nPromote the replica.", md)

	title, _, err = Convert("on_call-notes.txt", []byte("Page the SRE."))
	require.NoError(t, err)
	assert.Equal(t, "on call notes", title, "no heading: the file name")

	page := `<html><head><title>Refunds</title><style>p{}</style><script>alert(1)</script></head><body>
		<nav>menu</nav><h1>Refund flow</h1><p>Refunds go through the <a href="https://pay.example/doc">gateway</a> and are <strong>idempotent</strong>.</p>
		<ul><li>Capture</li><li>Reverse<ul><li>Partial</li></ul></li></ul><pre>curl -X POST /refunds</pre>
		<table><tr><th>Code</th><th>Meaning</th></tr><tr><td>409</td><td>Already refunded</td></tr></table></body></html>`
	title, md, err = Convert("refunds.html", []byte(page))
	require.NoError(t, err)
	assert.Equal(t, "Refunds", title)
	for _, want := range []string{"# Refund flow", "[gateway](https://pay.example/doc)", "**idempotent**", "- Capture", "  - Partial",
		"```\ncurl -X POST /refunds\n```", "| 409 | Already refunded |"} {
		assert.Contains(t, md, want)
	}
	assert.NotContains(t, md, "alert(1)")
	assert.NotContains(t, md, "menu")

	_, _, err = Convert("spec.pdf", []byte("%PDF"))
	assert.ErrorContains(t, err, "export it as Markdown")
	_, _, err = Convert("a.exe", []byte("x"))
	assert.ErrorContains(t, err, "unsupported")
	_, _, err = Convert("a.md", []byte{0xff, 0xfe})
	assert.ErrorContains(t, err, "UTF-8")
	_, _, err = Convert("big.md", []byte(strings.Repeat("a", MaxBytes+1)))
	assert.ErrorContains(t, err, "larger")
}
