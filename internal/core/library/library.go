// Package library assigns docs, graph entities, knowledge pages, and known issues to topic shelves
// (the "Library", plan § 4.4, § 8.7). Assignment is rule-based data evaluated on every write — no LLM —
// so shelves stay current for free; curated shelves are only ever filled by people.
package library

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/GokulMV/DocTheRepo/internal/core/triage"
)

// Item types.
const (
	ItemDocNode        = "doc_node"
	ItemEntity         = "entity"
	ItemConfluencePage = "confluence_page"
	ItemJiraIssue      = "jira_issue"
	ItemKnownIssue     = "known_issue"
)

// Item is anything that can sit on a shelf.
type Item struct {
	Type       string   `json:"type"`
	ID         string   `json:"id"`
	Title      string   `json:"title"`
	EntityKind string   `json:"entity_kind,omitempty"`
	Path       string   `json:"path,omitempty"`
	Source     string   `json:"source,omitempty"`
	Labels     []string `json:"labels,omitempty"`
}

// Rule matches items. Every non-empty field must match (AND); within a field any value may match (OR).
type Rule struct {
	ItemTypes   []string `json:"item_types,omitempty"`
	EntityKinds []string `json:"entity_kinds,omitempty"`
	PathGlobs   []string `json:"path_globs,omitempty"`
	Labels      []string `json:"labels,omitempty"`
	Sources     []string `json:"sources,omitempty"`
	TitleRegex  string   `json:"title_regex,omitempty"`
}

// Shelf is a topic in the Library. A shelf matches an item when any of its rules does.
type Shelf struct {
	Slug        string `json:"slug"`
	Title       string `json:"title"`
	Description string `json:"description"`
	Rules       []Rule `json:"rules"`
	// Curated shelves are filled only by people; rules never add or remove their items.
	Curated bool `json:"curated"`
	Order   int  `json:"order"`
}

// DefaultShelves are seeded on first start; operators edit or add shelves in the UI.
func DefaultShelves() []Shelf {
	return []Shelf{
		{Slug: "architecture", Title: "Architecture", Order: 10, Description: "System overviews, diagrams, and how services fit together.",
			Rules: []Rule{
				{PathGlobs: []string{"**/architecture/**", "**/ARCHITECTURE*", "**/architecture*.md", "**/design/**"}},
				{Labels: []string{"architecture", "design"}},
				{ItemTypes: []string{ItemEntity}, EntityKinds: []string{"service"}},
			}},
		{Slug: "services", Title: "Services", Order: 20, Description: "Every repository and deployed service.",
			Rules: []Rule{{ItemTypes: []string{ItemEntity}, EntityKinds: []string{"service", "repo"}}}},
		{Slug: "apis", Title: "APIs", Order: 30, Description: "HTTP endpoints and API specifications.",
			Rules: []Rule{
				{ItemTypes: []string{ItemEntity}, EntityKinds: []string{"endpoint"}},
				{PathGlobs: []string{"**/openapi*", "**/swagger*", "**/*.proto", "**/api/**"}},
				{Labels: []string{"api"}},
			}},
		{Slug: "events", Title: "Events & Topics", Order: 40, Description: "Topics and queues, with their producers and consumers.",
			Rules: []Rule{{ItemTypes: []string{ItemEntity}, EntityKinds: []string{"queue_topic"}}, {Labels: []string{"events", "kafka", "messaging"}}}},
		{Slug: "data", Title: "Data & Storage", Order: 50, Description: "Databases, caches, buckets and who uses them.",
			Rules: []Rule{{ItemTypes: []string{ItemEntity}, EntityKinds: []string{"datastore"}}, {PathGlobs: []string{"**/migrations/**", "**/schema*"}}}},
		{Slug: "config", Title: "Config & Env", Order: 60, Description: "Environment variables and configuration.",
			Rules: []Rule{{ItemTypes: []string{ItemEntity}, EntityKinds: []string{"env_var"}}, {PathGlobs: []string{"**/config/**", "**/*.env.example"}}}},
		{Slug: "dependencies", Title: "Dependencies", Order: 70, Description: "Third-party libraries in use across the estate.",
			Rules: []Rule{{ItemTypes: []string{ItemEntity}, EntityKinds: []string{"dependency"}}}},
		{Slug: "runbooks", Title: "Runbooks", Order: 80, Description: "Operational procedures and playbooks.",
			Rules: []Rule{{PathGlobs: []string{"**/runbook*/**", "**/*runbook*", "**/playbook*/**", "**/oncall/**"}}, {Labels: []string{"runbook", "playbook", "oncall"}}}},
		{Slug: "known-issues", Title: "Known Issues", Order: 90, Description: "Problems the team already understands, and why they are suppressed.",
			Rules: []Rule{{ItemTypes: []string{ItemKnownIssue}}, {Labels: []string{"known-issue", "known_issue"}}}},
		{Slug: "security", Title: "Security", Order: 100, Description: "Security posture, findings, and policies.",
			Rules: []Rule{{PathGlobs: []string{"**/SECURITY*", "**/security/**"}}, {Labels: []string{"security"}}, {Sources: []string{"wiz"}}}},
		{Slug: "decisions", Title: "Decisions", Order: 110, Description: "Architecture decision records.",
			Rules: []Rule{{PathGlobs: []string{"**/adr/**", "**/adrs/**", "**/decisions/**", "**/*ADR*"}}, {Labels: []string{"adr", "decision"}}}},
		{Slug: "confluence", Title: "Confluence", Order: 120, Description: "Synced Confluence pages.",
			Rules: []Rule{{ItemTypes: []string{ItemConfluencePage}}}},
		{Slug: "jira", Title: "Jira", Order: 130, Description: "Synced Jira issues.",
			Rules: []Rule{{ItemTypes: []string{ItemJiraIssue}}}},
	}
}

type compiledRule struct {
	Rule
	globs []*regexp.Regexp
	title *regexp.Regexp
}

// Classifier evaluates shelves.
type Classifier struct {
	shelves []Shelf
	rules   map[string][]compiledRule
}

// Compile validates shelves and their rules.
func Compile(shelves []Shelf) (*Classifier, error) {
	c := &Classifier{rules: map[string][]compiledRule{}}
	seen := map[string]bool{}
	for _, s := range shelves {
		if s.Slug == "" || seen[s.Slug] {
			return nil, fmt.Errorf("shelf slug %q is empty or duplicated", s.Slug)
		}
		seen[s.Slug] = true
		for _, r := range s.Rules {
			cr := compiledRule{Rule: r}
			for _, g := range r.PathGlobs {
				re, err := triage.GlobRegexp(g)
				if err != nil {
					return nil, fmt.Errorf("shelf %s: %w", s.Slug, err)
				}
				cr.globs = append(cr.globs, re)
			}
			if r.TitleRegex != "" {
				re, err := regexp.Compile("(?i)" + r.TitleRegex)
				if err != nil {
					return nil, fmt.Errorf("shelf %s: title_regex: %w", s.Slug, err)
				}
				cr.title = re
			}
			c.rules[s.Slug] = append(c.rules[s.Slug], cr)
		}
		c.shelves = append(c.shelves, s)
	}
	sort.SliceStable(c.shelves, func(i, j int) bool { return c.shelves[i].Order < c.shelves[j].Order })
	return c, nil
}

// Assign returns the slugs of every non-curated shelf the item belongs on, in shelf order.
func (c *Classifier) Assign(it Item) []string {
	var out []string
	for _, s := range c.shelves {
		if s.Curated {
			continue
		}
		for _, r := range c.rules[s.Slug] {
			if r.match(it) {
				out = append(out, s.Slug)
				break
			}
		}
	}
	return out
}

// Shelves returns the shelves in display order.
func (c *Classifier) Shelves() []Shelf { return c.shelves }

func (r compiledRule) match(it Item) bool {
	empty := len(r.ItemTypes) == 0 && len(r.EntityKinds) == 0 && len(r.globs) == 0 && len(r.Labels) == 0 &&
		len(r.Sources) == 0 && r.title == nil
	if empty {
		return false // an empty rule would put everything on the shelf
	}
	if len(r.ItemTypes) > 0 && !in(r.ItemTypes, it.Type) {
		return false
	}
	if len(r.EntityKinds) > 0 && !in(r.EntityKinds, it.EntityKind) {
		return false
	}
	if len(r.Sources) > 0 && !in(r.Sources, it.Source) {
		return false
	}
	if len(r.globs) > 0 {
		ok := false
		for _, g := range r.globs {
			if it.Path != "" && g.MatchString(it.Path) {
				ok = true
				break
			}
		}
		if !ok {
			return false
		}
	}
	if len(r.Labels) > 0 {
		ok := false
		for _, l := range it.Labels {
			if in(r.Labels, strings.ToLower(l)) {
				ok = true
				break
			}
		}
		if !ok {
			return false
		}
	}
	if r.title != nil && !r.title.MatchString(it.Title) {
		return false
	}
	return true
}

func in(xs []string, v string) bool {
	for _, x := range xs {
		if x == v {
			return true
		}
	}
	return false
}
