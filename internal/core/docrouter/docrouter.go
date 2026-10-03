// Package docrouter decides, before anything is spent, how each piece of code gets its documentation:
//
//	reuse   → an earlier doc for exactly this code (no call)
//	comment → the code's own doc comment, for tiny code that already explains itself (no call)
//	fast    → a cheaper model, for short code (the docgen_fast route, when set)
//	full    → the docgen route, for everything else
//
// It is plain code, never a model, and every decision carries its reason. The mode trades thoroughness
// for cost: thorough only reuses; balanced (the default) also uses comments for tiny code and the fast
// model for short code; economy uses good comments for most documented code and the fast model widely.
package docrouter

import (
	"regexp"
	"strings"

	"github.com/GokulMV/DocTheRepo/internal/ports"
)

// Tier is how a chunk gets its doc.
type Tier string

const (
	Reuse   Tier = "reuse"
	Comment Tier = "comment"
	Fast    Tier = "fast"
	Full    Tier = "full"
)

// Mode is the cost/thoroughness setting.
type Mode string

const (
	Thorough Mode = "thorough"
	Balanced Mode = "balanced"
	Economy  Mode = "economy"
)

// ParseMode returns the mode, defaulting to balanced.
func ParseMode(s string) Mode {
	switch Mode(strings.ToLower(strings.TrimSpace(s))) {
	case Thorough:
		return Thorough
	case Economy:
		return Economy
	}
	return Balanced
}

// Key identifies a doc by the code it describes.
type Key struct {
	ContentHash string
	Symbol      string
}

// KeyOf is a chunk's cache key.
func KeyOf(c ports.Chunk) Key { return Key{ContentHash: c.ContentHash, Symbol: c.Symbol} }

// Decision is the router's choice for one chunk.
type Decision struct {
	Tier   Tier
	Reason string
	// Body is the doc for no-call tiers (reuse, comment).
	Body string
}

// limits per mode: code lines at or below which a commented chunk uses its comment, and at or below which
// the fast model writes the doc.
type limits struct{ commentLines, commentChars, fastLines int }

var modeLimits = map[Mode]limits{
	Thorough: {commentLines: -1, fastLines: -1},
	Balanced: {commentLines: 3, commentChars: 20, fastLines: 25},
	Economy:  {commentLines: 60, commentChars: 60, fastLines: 80},
}

// Decide routes one chunk. cached is an earlier doc for exactly this code, if any.
func Decide(c ports.Chunk, mode Mode, cached string, fastRouted bool) Decision {
	if strings.TrimSpace(cached) != "" {
		return Decision{Tier: Reuse, Reason: "documented before (same code)", Body: cached}
	}
	l, ok := modeLimits[mode]
	if !ok {
		l = modeLimits[Balanced]
	}
	comment, code := Split(c.Content, c.Language)
	lines := countLines(code)
	if l.commentLines >= 0 && lines <= l.commentLines && len(comment) >= l.commentChars {
		return Decision{Tier: Comment, Reason: "its doc comment covers it", Body: comment}
	}
	if fastRouted && l.fastLines >= 0 && lines <= l.fastLines {
		return Decision{Tier: Fast, Reason: "short code"}
	}
	return Decision{Tier: Full, Reason: "needs the full model"}
}

func countLines(code string) int {
	n := 0
	for _, l := range strings.Split(code, "\n") {
		if t := strings.TrimSpace(l); t != "" && t != "{" && t != "}" && t != ")" {
			n++
		}
	}
	return n
}

var (
	pyDocRE    = regexp.MustCompile(`(?s)^[^\n]*:\s*\n\s*(?:r|u)?("""|''')(.*?)("""|''')`)
	blockRE    = regexp.MustCompile(`(?s)^\s*/\*\*?(.*?)\*/`)
	lineMarker = regexp.MustCompile(`^\s*(//+|#+|--|;+)\s?`)
)

// Split separates a chunk's leading doc comment (as plain text) from its code. Python docstrings count
// as the doc comment.
func Split(content, language string) (comment, code string) {
	lines := strings.Split(content, "\n")
	i := 0
	var parts []string
	if m := blockRE.FindStringSubmatchIndex(content); m != nil && strings.TrimSpace(content[:m[0]]) == "" {
		for _, l := range strings.Split(content[m[2]:m[3]], "\n") {
			parts = append(parts, strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(l), "*")))
		}
		code = content[m[1]:]
		return clean(parts), code
	}
	for ; i < len(lines); i++ {
		t := strings.TrimSpace(lines[i])
		if t == "" && len(parts) == 0 {
			continue
		}
		if !lineMarker.MatchString(lines[i]) || strings.HasPrefix(t, "#!") || strings.HasPrefix(t, "#[") || strings.HasPrefix(t, "#include") {
			break
		}
		parts = append(parts, lineMarker.ReplaceAllString(lines[i], ""))
	}
	code = strings.Join(lines[i:], "\n")
	if len(parts) == 0 && (language == "python" || strings.Contains(code, `"""`)) {
		if m := pyDocRE.FindStringSubmatchIndex(code); m != nil {
			doc := code[m[4]:m[5]]
			return clean(strings.Split(doc, "\n")), code[:m[2]] + code[m[1]:]
		}
	}
	return clean(parts), code
}

// clean joins comment lines into paragraphs, dropping lint directives and empty edges.
func clean(lines []string) string {
	var paras []string
	var cur []string
	flush := func() {
		if len(cur) > 0 {
			paras = append(paras, strings.Join(cur, " "))
			cur = nil
		}
	}
	for _, l := range lines {
		t := strings.TrimSpace(l)
		switch {
		case t == "":
			flush()
		case strings.HasPrefix(t, "nolint") || strings.HasPrefix(t, "eslint-") || strings.HasPrefix(t, "go:") || strings.HasPrefix(t, "@ts-") || strings.HasPrefix(t, "noqa"):
		default:
			cur = append(cur, t)
		}
	}
	flush()
	return strings.TrimSpace(strings.Join(paras, "\n\n"))
}
