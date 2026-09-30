// Package suggest proposes known-issue rules (plan § 8.10) — never activates them:
//   - auto-suggestions: issues with >= 50 occurrences, >= 3 days old, never acknowledged, whose decode says
//     not actionable or "looks like known noise", grouped by service and environment into one proposed rule
//     per group. This needs no model call: the decode already made the judgement.
//   - from text (a pasted incident note, runbook, or ticket): a deterministic pre-pass extracts exception
//     classes, error codes, quoted messages, and known service names; recent issues matching them (and
//     issues whose decodes are semantically close) become candidates; one spend-guarded call on the suggest
//     route explains the match and proposes a rule, which must validate and may only name real candidates.
//   - Test dry-runs a rule against the last 7 days of issues.
package suggest

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/GokulMV/DocTheRepo/internal/core/knownissues"
	"github.com/GokulMV/DocTheRepo/internal/core/llmgateway"
	"github.com/GokulMV/DocTheRepo/internal/core/signals"
	"github.com/GokulMV/DocTheRepo/internal/ports"
	"github.com/GokulMV/DocTheRepo/pkg/contract"
)

// Thresholds (plan § 8.10).
const (
	AutoMinOccurrences = 50
	AutoMinAge         = 72 * time.Hour
	MaxTextChars       = 50000
	MaxCandidates      = 20
	candidateWindow    = 30 * 24 * time.Hour
	testWindow         = 7 * 24 * time.Hour
)

// Candidate is an issue a suggestion may cover.
type Candidate struct {
	IssueID     string    `json:"issue_id"`
	Fingerprint string    `json:"fingerprint"`
	Kind        string    `json:"kind"`
	Title       string    `json:"title"`
	Service     string    `json:"service"`
	Environment string    `json:"environment"`
	Status      string    `json:"status"`
	Occurrences int64     `json:"occurrences"`
	LastSeen    time.Time `json:"last_seen"`
	Summary     string    `json:"decode_summary,omitempty"`
}

// Store is what suggestions read and write.
type Store interface {
	// AutoCandidates lists issues meeting the auto-suggestion bar that no pending suggestion covers yet.
	AutoCandidates(ctx context.Context, minOccurrences int64, olderThan time.Time) ([]Candidate, error)
	SaveSuggestion(ctx context.Context, issueIDs []string, m knownissues.Match, rationale string) (string, error)
	// SearchIssues finds issues seen since a time whose title or decode mentions any term, or whose service
	// is listed.
	SearchIssues(ctx context.Context, terms, services []string, since time.Time, limit int) ([]Candidate, error)
	// IssuesForDecodeChunks maps issue_decode chunk IDs to their issues.
	IssuesForDecodeChunks(ctx context.Context, chunkIDs []string) ([]Candidate, error)
	// Services lists service names seen since a time.
	Services(ctx context.Context, since time.Time) ([]string, error)
	// LatestSamples returns each issue seen since a time with its newest sample (for dry runs).
	LatestSamples(ctx context.Context, since time.Time, limit int) ([]Sampled, error)
}

// Sampled is an issue with its newest sample.
type Sampled struct {
	Candidate
	Sample ports.SignalEvent
}

// Service runs suggestions.
type Service struct {
	Store Store
	GW    *llmgateway.Gateway
	Index ports.VectorIndex // optional: semantic candidates from decode summaries
	Now   func() time.Time
}

// AutoSuggest proposes rules for qualifying issues, one per (service, environment). It returns how many
// suggestions it saved.
func (s *Service) AutoSuggest(ctx context.Context) (int, error) {
	cands, err := s.Store.AutoCandidates(ctx, AutoMinOccurrences, s.now().Add(-AutoMinAge))
	if err != nil {
		return 0, err
	}
	type key struct{ svc, env string }
	groups := map[key][]Candidate{}
	var keys []key
	for _, c := range cands {
		k := key{c.Service, c.Environment}
		if _, ok := groups[k]; !ok {
			keys = append(keys, k)
		}
		groups[k] = append(groups[k], c)
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i].svc+"\x00"+keys[i].env < keys[j].svc+"\x00"+keys[j].env })
	n := 0
	for _, k := range keys {
		g := groups[k]
		m := knownissues.Match{}
		ids := make([]string, 0, len(g))
		var total int64
		var lines []string
		for _, c := range g {
			m.Fingerprints = append(m.Fingerprints, c.Fingerprint)
			ids = append(ids, c.IssueID)
			total += c.Occurrences
			line := fmt.Sprintf("- %s (%d occurrences)", c.Title, c.Occurrences)
			if c.Summary != "" {
				line += ": " + c.Summary
			}
			lines = append(lines, line)
		}
		if k.svc != "" && k.svc != signals.Unknown {
			m.Services = []string{k.svc}
		}
		if k.env != "" {
			m.Environments = []string{k.env}
		}
		rationale := fmt.Sprintf("%d issue(s) in %s with %d occurrences over at least 3 days were decoded as not actionable or as recurring noise, and nobody has acknowledged them:\n%s",
			len(g), orUnknown(k.svc), total, strings.Join(lines, "\n"))
		if _, err := s.Store.SaveSuggestion(ctx, ids, m, rationale); err != nil {
			return n, err
		}
		n++
	}
	return n, nil
}

// TextSuggestion is the from-text result shown before a human saves anything.
type TextSuggestion struct {
	Explanation    string            `json:"explanation"`
	Reason         string            `json:"reason"`
	ProposedMatch  knownissues.Match `json:"proposed_match"`
	Confidence     string            `json:"confidence"`
	Candidates     []Candidate       `json:"candidates"`
	Terms          Terms             `json:"extracted"`
	WouldMatch     int               `json:"matching_issues_last_7d"`
	SampleIssueIDs []string          `json:"sample_issue_ids"`
}

// Reasons are the known-issue reasons a proposal may use.
var Reasons = []any{"known_bug", "wont_fix", "third_party", "expected_noise", "cannot_action", "in_progress"}

// Schema is the suggest route's output contract.
var Schema = contract.Schema{
	"type": "object", "additionalProperties": false,
	"required": []any{"explanation", "reason", "proposed_match", "confidence"},
	"properties": contract.Schema{
		"explanation": contract.Schema{"type": "string", "minLength": 1},
		"reason":      contract.Schema{"type": "string", "enum": Reasons},
		"confidence":  contract.Schema{"type": "string", "enum": []any{"high", "medium", "low"}},
		"proposed_match": contract.Schema{"type": "object", "additionalProperties": false, "properties": contract.Schema{
			"fingerprints":  contract.Schema{"type": "array", "items": contract.Schema{"type": "string"}},
			"message_regex": contract.Schema{"type": "string"},
			"sources":       contract.Schema{"type": "array", "items": contract.Schema{"type": "string"}},
			"services":      contract.Schema{"type": "array", "items": contract.Schema{"type": "string"}},
			"environments":  contract.Schema{"type": "array", "items": contract.Schema{"type": "string"}},
			"attrs":         contract.Schema{"type": "object"},
			"min_severity":  contract.Schema{"type": "string", "enum": []any{"", "info", "warning", "error", "critical"}},
			"max_severity":  contract.Schema{"type": "string", "enum": []any{"", "info", "warning", "error", "critical"}},
		}},
	},
}

// System is the suggest route's instruction.
const System = `You propose known-issue rules for an error inbox. You are given text a team wrote about a known
problem (an incident note, runbook, or ticket) and recent issues that may be what it describes. Reply with one
JSON object: {"explanation": which issues the text describes and why, or that none match,
"reason": one of known_bug, wont_fix, third_party, expected_noise, cannot_action, in_progress,
"proposed_match": the narrowest rule that covers exactly those issues — prefer "fingerprints" copied from
the listed issues, optionally with "services", "environments", or an RE2 "message_regex",
"confidence": "high"|"medium"|"low"}. Only use fingerprints that appear in the list. If nothing matches,
return an empty proposed_match and low confidence.`

// ErrEmptyText is returned for blank input.
var ErrEmptyText = errors.New("text is required")

// FromText proposes a rule from free text. services optionally narrows the candidates.
func (s *Service) FromText(ctx context.Context, text string, services []string, userID string) (TextSuggestion, error) {
	text = strings.TrimSpace(signals.Scrub(text))
	if text == "" {
		return TextSuggestion{}, ErrEmptyText
	}
	if len(text) > MaxTextChars {
		text = text[:MaxTextChars]
	}
	known, err := s.Store.Services(ctx, s.now().Add(-candidateWindow))
	if err != nil {
		return TextSuggestion{}, err
	}
	terms := Extract(text, known)
	svcs := append(append([]string{}, services...), terms.Services...)
	cands, err := s.Store.SearchIssues(ctx, terms.All(), svcs, s.now().Add(-candidateWindow), MaxCandidates)
	if err != nil {
		return TextSuggestion{}, err
	}
	meta := llmgateway.CallMeta{UserID: userID}
	if s.Index != nil && len(cands) < MaxCandidates {
		if vs, _, err := s.GW.Embed(ctx, meta, []string{truncate(text, 8000)}); err == nil {
			hits, err := s.Index.Search(ctx, vs[0], MaxCandidates, ports.VectorFilter{Sources: []ports.ChunkSource{ports.SourceIssueDecode}})
			if err != nil && !errors.Is(err, ports.ErrNotFound) {
				return TextSuggestion{}, err
			}
			var ids []string
			for _, h := range hits {
				if h.Score >= 0.75 {
					ids = append(ids, h.ChunkID)
				}
			}
			more, err := s.Store.IssuesForDecodeChunks(ctx, ids)
			if err != nil {
				return TextSuggestion{}, err
			}
			cands = mergeCandidates(cands, more, MaxCandidates)
		} else if !errors.Is(err, llmgateway.ErrNoRoute) {
			return TextSuggestion{}, err
		}
	}
	out := TextSuggestion{Candidates: cands, Terms: terms}
	if len(cands) == 0 {
		out.Explanation = "No issue from the last 30 days matches what the text mentions."
		out.Confidence, out.Reason = "low", "known_bug"
		return out, nil
	}
	fps := map[string]bool{}
	for _, c := range cands {
		fps[c.Fingerprint] = true
	}
	var raw struct {
		Explanation   string            `json:"explanation"`
		Reason        string            `json:"reason"`
		ProposedMatch knownissues.Match `json:"proposed_match"`
		Confidence    string            `json:"confidence"`
	}
	check := func() []string {
		var probs []string
		for _, fp := range raw.ProposedMatch.Fingerprints {
			if !fps[fp] {
				probs = append(probs, fmt.Sprintf("$.proposed_match.fingerprints: %q is not one of the listed issues", fp))
			}
		}
		if !empty(raw.ProposedMatch) {
			if err := knownissues.Validate(raw.ProposedMatch, knownissues.Suppress); err != nil {
				probs = append(probs, "$.proposed_match: "+err.Error())
			}
		}
		return probs
	}
	err = s.GW.ChatJSON(ctx, llmgateway.FeatureSuggest, meta,
		ports.ChatRequest{System: System, Messages: []ports.ChatMessage{{Role: "user", Content: prompt(text, cands)}}}, Schema, &raw, check)
	if err != nil {
		return TextSuggestion{}, err
	}
	out.Explanation, out.Reason, out.ProposedMatch, out.Confidence = raw.Explanation, raw.Reason, raw.ProposedMatch, raw.Confidence
	if !empty(out.ProposedMatch) {
		res, err := s.Test(ctx, out.ProposedMatch)
		if err != nil {
			return TextSuggestion{}, err
		}
		out.WouldMatch, out.SampleIssueIDs = res.WouldMatch, res.SampleIssueIDs
	}
	return out, nil
}

// TestResult is a rule dry run.
type TestResult struct {
	WouldMatch     int      `json:"would_match_last_7d"`
	SampleIssueIDs []string `json:"sample_issue_ids"`
}

// Test dry-runs a rule against every issue seen in the last 7 days (by its newest sample).
func (s *Service) Test(ctx context.Context, m knownissues.Match) (TestResult, error) {
	if err := knownissues.Validate(m, knownissues.Suppress); err != nil {
		return TestResult{}, &ports.ValidationError{Code: "INVALID_RULE", Message: err.Error()}
	}
	matcher, errs := knownissues.Compile([]knownissues.Rule{{ID: "dry-run", Match: m, Action: knownissues.Suppress}})
	if len(errs) > 0 {
		return TestResult{}, &ports.ValidationError{Code: "INVALID_RULE", Message: errs[0].Error()}
	}
	now := s.now()
	items, err := s.Store.LatestSamples(ctx, now.Add(-testWindow), 5000)
	if err != nil {
		return TestResult{}, err
	}
	res := TestResult{SampleIssueIDs: []string{}}
	for _, it := range items {
		ev := it.Sample
		ev.Fingerprint = it.Fingerprint
		if _, ok := matcher.Match(&ev, now); ok {
			res.WouldMatch++
			if len(res.SampleIssueIDs) < 20 {
				res.SampleIssueIDs = append(res.SampleIssueIDs, it.IssueID)
			}
		}
	}
	return res, nil
}

func prompt(text string, cands []Candidate) string {
	var b strings.Builder
	b.WriteString("## Text\n")
	b.WriteString(text)
	b.WriteString("\n\n## Recent issues\n")
	for _, c := range cands {
		fmt.Fprintf(&b, "- issue %s: fingerprint %s service %s env %s kind %s | %s | %d occurrences",
			c.IssueID, c.Fingerprint, orUnknown(c.Service), orDash(c.Environment), c.Kind, c.Title, c.Occurrences)
		if c.Summary != "" {
			b.WriteString(" | decode: " + truncate(c.Summary, 300))
		}
		b.WriteString("\n")
	}
	return b.String()
}

// Terms are what the pre-pass found in the text.
type Terms struct {
	Exceptions []string `json:"exceptions,omitempty"`
	Codes      []string `json:"error_codes,omitempty"`
	Messages   []string `json:"quoted_messages,omitempty"`
	Services   []string `json:"services,omitempty"`
	Resources  []string `json:"resources,omitempty"`
}

// All returns every search term.
func (t Terms) All() []string {
	var out []string
	for _, l := range [][]string{t.Exceptions, t.Codes, t.Messages, t.Resources} {
		out = append(out, l...)
	}
	return out
}

var (
	exceptionRE = regexp.MustCompile(`\b(?:[a-z_][\w]*\.)*[A-Z][A-Za-z0-9_]*(?:Exception|Error|Fault|Panic|Timeout)\b`)
	codeRE      = regexp.MustCompile(`\b(?:[A-Z]{2,10}[-_]\d{2,6}|E\d{3,6}|HTTP[ /]?[45]\d\d|(?:status|code)[ =:]+[45]\d\d)\b`)
	quotedRE    = regexp.MustCompile("[\"'`“]([^\"'`”\n]{8,200})[\"'`”]")
	resourceRE  = regexp.MustCompile(`(?:/aws/[\w./-]+|arn:aws:[\w:/.-]+|projects/[\w-]+/(?:subscriptions|topics)/[\w.-]+)`)
)

// Extract is the deterministic pre-pass (plan § 8.10 step 2). knownServices are service names seen
// recently; only those that appear in the text are returned.
func Extract(text string, knownServices []string) Terms {
	t := Terms{
		Exceptions: uniq(exceptionRE.FindAllString(text, 20)),
		Codes:      uniq(codeRE.FindAllString(text, 20)),
		Resources:  uniq(resourceRE.FindAllString(text, 20)),
	}
	for _, m := range quotedRE.FindAllStringSubmatch(text, 20) {
		t.Messages = append(t.Messages, strings.TrimSpace(m[1]))
	}
	t.Messages = uniq(t.Messages)
	lower := strings.ToLower(text)
	for _, s := range knownServices {
		if len(s) >= 3 && s != signals.Unknown && wordIn(lower, strings.ToLower(s)) {
			t.Services = append(t.Services, s)
		}
	}
	return t
}

func wordIn(text, word string) bool {
	for i := 0; ; {
		j := strings.Index(text[i:], word)
		if j < 0 {
			return false
		}
		start, end := i+j, i+j+len(word)
		before := start == 0 || !isWord(text[start-1])
		after := end == len(text) || !isWord(text[end])
		if before && after {
			return true
		}
		i = start + 1
	}
}

func isWord(c byte) bool {
	return c == '_' || c == '-' || (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9')
}

func mergeCandidates(a, b []Candidate, limit int) []Candidate {
	seen := map[string]bool{}
	var out []Candidate
	for _, l := range [][]Candidate{a, b} {
		for _, c := range l {
			if !seen[c.IssueID] && len(out) < limit {
				seen[c.IssueID] = true
				out = append(out, c)
			}
		}
	}
	return out
}

func empty(m knownissues.Match) bool {
	return len(m.Fingerprints) == 0 && m.MessageRegex == "" && len(m.Sources) == 0 && len(m.Services) == 0 &&
		len(m.Environments) == 0 && len(m.Attrs) == 0 && m.MinSeverity == "" && m.MaxSeverity == ""
}

func uniq(in []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range in {
		if s != "" && !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

func orUnknown(s string) string {
	if s == "" {
		return signals.Unknown
	}
	return s
}

func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

func (s *Service) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}
