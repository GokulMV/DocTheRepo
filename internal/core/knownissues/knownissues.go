// Package knownissues matches signal events against known-issue rules (plan § 8.10) before any LLM call
// and before an issue is queued for decoding: a suppressed event costs a counter increment, not a decode.
package knownissues

import (
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/GokulMV/DocTheRepo/internal/ports"
)

// Action is what a matching rule does.
type Action string

const (
	// Suppress hides the event: the issue is marked suppressed and never decoded.
	Suppress Action = "suppress"
	// LabelOnly tags the issue with the rule but keeps it in the Inbox (e.g. "fixed upstream — verify").
	LabelOnly Action = "label_only"
)

// Match is a rule's condition. Every present field must match (AND); within a list any value matches (OR).
type Match struct {
	Fingerprints []string          `json:"fingerprints,omitempty"`
	MessageRegex string            `json:"message_regex,omitempty"`
	Sources      []string          `json:"sources,omitempty"`
	Services     []string          `json:"services,omitempty"`
	Environments []string          `json:"environments,omitempty"`
	Attrs        map[string]string `json:"attrs,omitempty"`
	MinSeverity  ports.Severity    `json:"min_severity,omitempty"`
	MaxSeverity  ports.Severity    `json:"max_severity,omitempty"`
}

// Rule is an active known issue.
type Rule struct {
	ID        string
	Match     Match
	Action    Action
	ExpiresAt *time.Time
}

// MaxRegex bounds a rule's regex source.
const MaxRegex = 2000

// Validate reports why a match cannot be saved: it must constrain something, and its regex must compile.
func Validate(m Match, a Action) error {
	if a != Suppress && a != LabelOnly {
		return fmt.Errorf("action must be suppress or label_only")
	}
	if len(m.Fingerprints)+len(m.Sources)+len(m.Services)+len(m.Environments)+len(m.Attrs) == 0 && m.MessageRegex == "" &&
		m.MinSeverity == "" && m.MaxSeverity == "" {
		return fmt.Errorf("a rule must match on at least one field (it would hide everything)")
	}
	if len(m.Fingerprints)+len(m.Services)+len(m.Attrs) == 0 && m.MessageRegex == "" && len(m.Sources)+len(m.Environments) > 0 &&
		m.MinSeverity == "" && m.MaxSeverity == "" {
		// Source or environment alone would hide a whole tool or environment.
		return fmt.Errorf("a rule on sources or environments alone is too broad: add a service, message pattern, fingerprint, or attribute")
	}
	if len(m.MessageRegex) > MaxRegex {
		return fmt.Errorf("message_regex is longer than %d characters", MaxRegex)
	}
	if m.MessageRegex != "" {
		if _, err := regexp.Compile(m.MessageRegex); err != nil {
			return fmt.Errorf("message_regex: %w", err)
		}
	}
	for _, s := range []ports.Severity{m.MinSeverity, m.MaxSeverity} {
		if s != "" && ports.SeverityRank(s) == 0 {
			return fmt.Errorf("unknown severity %q (use info, warning, error, critical)", s)
		}
	}
	if m.MinSeverity != "" && m.MaxSeverity != "" && ports.SeverityRank(m.MinSeverity) > ports.SeverityRank(m.MaxSeverity) {
		return fmt.Errorf("min_severity is above max_severity")
	}
	return nil
}

type compiled struct {
	rule     Rule
	order    int
	fps      map[string]bool
	re       *regexp.Regexp
	sources  map[string]bool
	services map[string]bool
	envs     map[string]bool
}

// Matcher is an immutable, compiled rule set; rebuild it when rules change. Safe for concurrent use.
type Matcher struct {
	byFP      map[string][]*compiled // rules that name fingerprints, by fingerprint (O(1) lookup)
	byService map[string][]*compiled // rules that name services (and no fingerprints), by service
	rest      []*compiled            // everything else
	size      int
}

// Compile builds a matcher. Invalid rules are skipped and reported, so one bad rule never disables the rest.
func Compile(rules []Rule) (*Matcher, []error) {
	m := &Matcher{byFP: map[string][]*compiled{}, byService: map[string][]*compiled{}}
	var errs []error
	for i, r := range rules {
		if err := Validate(r.Match, r.Action); err != nil {
			errs = append(errs, fmt.Errorf("known issue %s: %w", r.ID, err))
			continue
		}
		c := &compiled{rule: r, order: i, fps: set(r.Match.Fingerprints, false), sources: set(r.Match.Sources, true),
			services: set(r.Match.Services, true), envs: set(r.Match.Environments, true)}
		if r.Match.MessageRegex != "" {
			c.re = regexp.MustCompile(r.Match.MessageRegex)
		}
		switch {
		case len(c.fps) > 0:
			for fp := range c.fps {
				m.byFP[fp] = append(m.byFP[fp], c)
			}
		case len(c.services) > 0:
			for s := range c.services {
				m.byService[s] = append(m.byService[s], c)
			}
		default:
			m.rest = append(m.rest, c)
		}
		m.size++
	}
	return m, errs
}

// Len is the number of compiled rules.
func (m *Matcher) Len() int { return m.size }

// Result is a match.
type Result struct {
	RuleID string
	Action Action
}

// Match returns the rule that applies to ev at now. A suppressing rule wins over a label-only one; among
// equals, the earliest rule wins. Expired rules never match.
func (m *Matcher) Match(ev *ports.SignalEvent, now time.Time) (Result, bool) {
	if m == nil || m.size == 0 {
		return Result{}, false
	}
	var best *compiled
	consider := func(cs []*compiled) {
		for _, c := range cs {
			if best != nil && !better(c, best) {
				continue
			}
			if c.matches(ev, now) {
				best = c
			}
		}
	}
	consider(m.byFP[ev.Fingerprint])
	if ev.AltFingerprint != "" {
		consider(m.byFP[ev.AltFingerprint])
	}
	consider(m.byService[strings.ToLower(ev.Service)])
	consider(m.rest)
	if best == nil {
		return Result{}, false
	}
	return Result{RuleID: best.rule.ID, Action: best.rule.Action}, true
}

func better(a, b *compiled) bool {
	if (a.rule.Action == Suppress) != (b.rule.Action == Suppress) {
		return a.rule.Action == Suppress
	}
	return a.order < b.order
}

func (c *compiled) matches(ev *ports.SignalEvent, now time.Time) bool {
	r := c.rule
	if r.ExpiresAt != nil && !now.Before(*r.ExpiresAt) {
		return false
	}
	if len(c.fps) > 0 && !c.fps[ev.Fingerprint] && !(ev.AltFingerprint != "" && c.fps[ev.AltFingerprint]) {
		return false
	}
	if len(c.sources) > 0 && !c.sources[strings.ToLower(ev.Source)] {
		return false
	}
	if len(c.services) > 0 && !c.services[strings.ToLower(ev.Service)] {
		return false
	}
	if len(c.envs) > 0 && !c.envs[strings.ToLower(ev.Environment)] {
		return false
	}
	for k, v := range r.Match.Attrs {
		if ev.Attrs[k] != v {
			return false
		}
	}
	rank := ports.SeverityRank(ev.Severity)
	if r.Match.MinSeverity != "" && rank < ports.SeverityRank(r.Match.MinSeverity) {
		return false
	}
	if r.Match.MaxSeverity != "" && rank > ports.SeverityRank(r.Match.MaxSeverity) {
		return false
	}
	if c.re != nil && !c.re.MatchString(ev.Message) && !c.re.MatchString(ev.Title) {
		return false
	}
	return true
}

func set(xs []string, fold bool) map[string]bool {
	if len(xs) == 0 {
		return nil
	}
	m := make(map[string]bool, len(xs))
	for _, x := range xs {
		if fold {
			x = strings.ToLower(x)
		}
		m[x] = true
	}
	return m
}
