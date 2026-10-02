package palace

import (
	"strings"
	"unicode"
)

// MentionIndex finds Palace entities named in prose (Confluence pages, Jira issues) by exact name, without
// an LLM: service and repository names as whole tokens (case-insensitive), endpoints as "METHOD /path".
type MentionIndex struct {
	tokens    map[string][]Ref // lower-cased single-token names
	endpoints map[string][]Ref // "GET /orders"
}

// MinMentionLen keeps short, common words ("api", "db") from linking every page to a service.
const MinMentionLen = 4

// NewMentionIndex indexes entities (kinds service, repo, endpoint; others are ignored).
func NewMentionIndex(es []Entity) *MentionIndex {
	m := &MentionIndex{tokens: map[string][]Ref{}, endpoints: map[string][]Ref{}}
	for _, e := range es {
		switch e.Kind {
		case KindService, KindRepo:
			name := strings.ToLower(strings.TrimSpace(e.Key))
			if len(name) >= MinMentionLen && !strings.ContainsFunc(name, unicode.IsSpace) {
				m.tokens[name] = append(m.tokens[name], e.Ref)
			}
		case KindEndpoint:
			if method, route, ok := strings.Cut(strings.TrimSpace(e.Name), " "); ok && strings.HasPrefix(route, "/") && len(route) > 1 {
				m.endpoints[strings.ToUpper(method)+" "+route] = append(m.endpoints[strings.ToUpper(method)+" "+route], e.Ref)
			}
		}
	}
	return m
}

// Empty reports whether nothing can be mentioned.
func (m *MentionIndex) Empty() bool { return m == nil || len(m.tokens)+len(m.endpoints) == 0 }

func isTokenRune(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsDigit(r) || r == '-' || r == '_' || r == '/' || r == '.'
}

// Mentions returns the entities a text names, each once, in first-mention order.
func (m *MentionIndex) Mentions(text string) []Ref {
	if m.Empty() {
		return nil
	}
	var out []Ref
	seen := map[Ref]bool{}
	add := func(rs []Ref) {
		for _, r := range rs {
			if !seen[r] {
				seen[r] = true
				out = append(out, r)
			}
		}
	}
	words := strings.FieldsFunc(text, func(r rune) bool { return !isTokenRune(r) })
	for i, w := range words {
		w = strings.Trim(w, "./")
		if w == "" {
			continue
		}
		add(m.tokens[strings.ToLower(w)])
		if i+1 < len(words) {
			switch up := strings.ToUpper(w); up {
			case "GET", "POST", "PUT", "PATCH", "DELETE", "HEAD", "OPTIONS":
				add(m.endpoints[up+" "+strings.TrimRight(words[i+1], ".")])
			}
		}
	}
	return out
}
