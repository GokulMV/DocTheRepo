package sigutil

import "strings"

// Cursor is a poll position: the newest timestamp seen and the IDs already emitted at exactly that
// timestamp. Read APIs filter inclusively on time, so the next poll starts at At and skips Seen.
type Cursor struct {
	At   string
	Seen map[string]bool
}

// maxSeen bounds the IDs kept at one timestamp (a burst of more lines in the same millisecond may repeat
// a few, which deduplication absorbs).
const maxSeen = 200

// ParseCursor reads "at|id1,id2".
func ParseCursor(s string) Cursor {
	at, ids, _ := strings.Cut(s, "|")
	c := Cursor{At: at, Seen: map[string]bool{}}
	for _, id := range strings.Split(ids, ",") {
		if id != "" {
			c.Seen[id] = true
		}
	}
	return c
}

// Advance records an emitted item; less reports whether a is earlier than b.
func (c *Cursor) Advance(at, id string, less func(a, b string) bool) {
	switch {
	case c.At == "" || less(c.At, at):
		c.At, c.Seen = at, map[string]bool{id: true}
	case at == c.At && len(c.Seen) < maxSeen:
		c.Seen[id] = true
	}
}

// Skip reports whether an item was emitted by an earlier poll.
func (c Cursor) Skip(at, id string) bool { return at == c.At && c.Seen[id] }

// String encodes the cursor.
func (c Cursor) String() string {
	ids := make([]string, 0, len(c.Seen))
	for id := range c.Seen {
		ids = append(ids, strings.ReplaceAll(id, ",", ""))
	}
	return c.At + "|" + strings.Join(ids, ",")
}
