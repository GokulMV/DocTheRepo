package sigutil

import (
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestCursorRoundTrip(t *testing.T) {
	less := func(a, b string) bool { x, _ := strconv.Atoi(a); y, _ := strconv.Atoi(b); return x < y }
	c := ParseCursor("")
	c.Advance("100", "a", less)
	c.Advance("200", "b", less)
	c.Advance("200", "c", less)
	c.Advance("150", "late", less)
	back := ParseCursor(c.String())
	assert.Equal(t, "200", back.At)
	assert.True(t, back.Skip("200", "b"))
	assert.True(t, back.Skip("200", "c"))
	assert.False(t, back.Skip("200", "d"), "a new event at the boundary timestamp is not skipped")
	assert.False(t, back.Skip("300", "b"))
	assert.False(t, back.Skip("100", "a"), "only the boundary is tracked; earlier items are excluded by time")
}
