package signals

import (
	"regexp"
	"strings"
	"unicode/utf8"
)

// MaxNormalized is the normalized message length used in fingerprints.
const MaxNormalized = 512

// Normalization replaces the parts of a message that vary between occurrences of the same error, so they
// group together (plan § 8.9 step 1). Order matters: specific shapes first, bare numbers last.
var normRules = []struct {
	re   *regexp.Regexp
	repl string
}{
	// ISO-8601 / RFC 3339 timestamps and common log timestamps.
	{regexp.MustCompile(`\d{4}-\d{2}-\d{2}[t ]\d{2}:\d{2}(?::\d{2}(?:[.,]\d+)?)?(?:z|[+-]\d{2}:?\d{2})?`), "<ts>"},
	{regexp.MustCompile(`\d{4}[-/]\d{2}[-/]\d{2}`), "<ts>"},
	{regexp.MustCompile(`\b\d{2}:\d{2}:\d{2}(?:[.,]\d+)?\b`), "<ts>"},
	{regexp.MustCompile(`\b[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}\b`), "<uuid>"},
	{regexp.MustCompile(`[a-z0-9._%+-]+@[a-z0-9-]+(?:\.[a-z0-9-]+)*\.[a-z]{2,}`), "<email>"},
	{regexp.MustCompile(`\b(?:\d{1,3}\.){3}\d{1,3}(?::\d+)?\b`), "<ip>"},
	{regexp.MustCompile(`(?:[0-9a-f]{1,4}:){7}[0-9a-f]{1,4}`), "<ip>"},
	{regexp.MustCompile(`"[^"]*"|'[^']*'|` + "`[^`]*`"), "<str>"},
	// Paths and URLs with a digit in them (/orders/123/items, s3://bucket/2026/09/…).
	{regexp.MustCompile(`(?:[a-z][a-z0-9+.-]*:)?(?:/[^\s/]*)*/[^\s/]*\d[^\s]*`), "<path>"},
	{regexp.MustCompile(`\b(?:0x)?[0-9a-f]*\d[0-9a-f]*[a-f][0-9a-f]*\b|\b(?:0x)?[0-9a-f]*[a-f][0-9a-f]*\d[0-9a-f]*\b`), "<hexish>"},
	// Numbers that start a token (3, 1.5s, 404); digits inside words (utf8, e2e, s3) are part of the word.
	{regexp.MustCompile(`\b\d+(?:\.\d+)?`), "<n>"},
}

var spaceRE = regexp.MustCompile(`\s+`)

// NormalizeMessage lowercases msg, replaces variable parts with placeholders, collapses whitespace, and
// truncates to MaxNormalized bytes (on a rune boundary).
func NormalizeMessage(msg string) string {
	s := strings.ToLower(msg)
	for _, r := range normRules {
		if r.repl == "<hexish>" {
			// Hex runs of 8+ chars (ids, hashes, addresses) become <hex>; shorter mixed tokens like "e2e"
			// or "utf8" are words, not identifiers.
			s = r.re.ReplaceAllStringFunc(s, func(m string) string {
				if len(strings.TrimPrefix(m, "0x")) >= 8 {
					return "<hex>"
				}
				return m
			})
			continue
		}
		s = r.re.ReplaceAllString(s, r.repl)
	}
	s = strings.TrimSpace(spaceRE.ReplaceAllString(s, " "))
	if len(s) > MaxNormalized {
		s = s[:MaxNormalized]
		for !utf8.ValidString(s) {
			s = s[:len(s)-1]
		}
	}
	return s
}
