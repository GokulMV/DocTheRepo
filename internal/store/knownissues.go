package store

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/GokulMV/DocTheRepo/internal/core/knownissues"
	"github.com/GokulMV/DocTheRepo/internal/core/signals"
	"github.com/GokulMV/DocTheRepo/internal/ports"
	"github.com/GokulMV/DocTheRepo/internal/store/gen"
)

// KnownIssue is a known-issue rule with its bookkeeping.
type KnownIssue struct {
	ID          string            `json:"id"`
	Title       string            `json:"title"`
	Description string            `json:"description"`
	Explanation string            `json:"explanation"`
	SourceText  string            `json:"source_text,omitempty"`
	JiraKey     string            `json:"jira_key,omitempty"`
	// ConfluencePageID links a rule to the page it was imported or explained from.
	ConfluencePageID string `json:"confluence_page_id,omitempty"`
	// UpstreamStatus is the linked Jira issue's status (or Confluence page status) at the last sync;
	// UpstreamNote explains an automatic change ("fixed upstream — verify").
	UpstreamStatus string `json:"upstream_status,omitempty"`
	UpstreamNote   string `json:"upstream_note,omitempty"`
	// LabelManaged rules were imported from a labelled issue or page and follow the label.
	LabelManaged bool `json:"label_managed"`
	Reason      string            `json:"reason"`
	Match       knownissues.Match `json:"match"`
	Action      string            `json:"action"`
	Enabled     bool              `json:"enabled"`
	ExpiresAt   *time.Time        `json:"expires_at,omitempty"`
	Source      string            `json:"source"`
	OwnerUserID string            `json:"owner_user_id,omitempty"`
	TicketURL   string            `json:"ticket_url,omitempty"`
	Hits        int64             `json:"hits"`
	LastHitAt   *time.Time        `json:"last_hit_at,omitempty"`
	CreatedAt   time.Time         `json:"created_at"`
	UpdatedAt   time.Time         `json:"updated_at"`
}

// KnownIssues stores known-issue rules.
type KnownIssues struct{ s *Store }

// NewKnownIssues returns the known-issue store.
func NewKnownIssues(s *Store) *KnownIssues { return &KnownIssues{s: s} }

var reasons = map[string]bool{"known_bug": true, "wont_fix": true, "third_party": true, "expected_noise": true, "cannot_action": true, "in_progress": true}
var kiSources = map[string]bool{"manual": true, "suggested": true, "confluence": true, "jira": true, "pasted": true}

// validateKnownIssue checks a rule. allowDraft accepts a disabled rule without a match yet (a label import,
// or editing such a draft); new manual rules always need one.
func validateKnownIssue(k *KnownIssue, allowDraft bool) error {
	if k.Title == "" {
		return &ports.ValidationError{Code: "VALIDATION_FAILED", Message: "title is required"}
	}
	if !reasons[k.Reason] {
		return &ports.ValidationError{Code: "VALIDATION_FAILED", Message: "reason must be known_bug, wont_fix, third_party, expected_noise, cannot_action, or in_progress"}
	}
	if k.Action == "" {
		k.Action = string(knownissues.Suppress)
	}
	if k.Source == "" {
		k.Source = "manual"
	}
	if !kiSources[k.Source] {
		return &ports.ValidationError{Code: "VALIDATION_FAILED", Message: "unknown source"}
	}
	if err := knownissues.Validate(k.Match, knownissues.Action(k.Action)); err != nil && (!allowDraft || k.Enabled || !emptyMatch(k.Match)) {
		return &ports.ValidationError{Code: "INVALID_RULE", Message: err.Error()}
	}
	if k.Action != string(knownissues.Suppress) && k.Action != string(knownissues.LabelOnly) {
		return &ports.ValidationError{Code: "INVALID_RULE", Message: "action must be suppress or label_only"}
	}
	k.SourceText = signals.Scrub(k.SourceText)
	return nil
}

// Create validates and stores a rule. Suggested rules are stored disabled: nothing is suppressed without a
// human.
func (x *KnownIssues) Create(ctx context.Context, k KnownIssue) (string, error) {
	if err := validateKnownIssue(&k, false); err != nil {
		return "", err
	}
	if k.Source == "suggested" {
		k.Enabled = false
	}
	m, _ := json.Marshal(k.Match)
	id := ports.NewID()
	err := x.s.Q.CreateKnownIssue(ctx, gen.CreateKnownIssueParams{ID: id, Title: k.Title, Description: k.Description, Explanation: k.Explanation,
		SourceText: k.SourceText, JiraKey: strPtr(k.JiraKey), Reason: gen.KnownIssueReason(k.Reason), Match: m, Action: gen.KnownIssueAction(k.Action),
		Enabled: k.Enabled, ExpiresAt: k.ExpiresAt, Source: gen.KnownIssueSource(k.Source), OwnerUserID: strPtr(k.OwnerUserID), TicketUrl: k.TicketURL})
	if err != nil {
		return "", fmt.Errorf("create known issue: %w", err)
	}
	if k.ConfluencePageID != "" {
		if _, err := x.s.Pool.Exec(ctx, `UPDATE known_issues SET confluence_page_id = $2 WHERE id = $1`, id, k.ConfluencePageID); err != nil {
			return "", err
		}
	}
	return id, nil
}

func emptyMatch(m knownissues.Match) bool {
	return len(m.Fingerprints)+len(m.Sources)+len(m.Services)+len(m.Environments)+len(m.Attrs) == 0 && m.MessageRegex == "" &&
		m.MinSeverity == "" && m.MaxSeverity == ""
}

// upstreamCol is the column holding a source's upstream reference.
func upstreamCol(source string) (string, error) {
	switch source {
	case "jira":
		return "jira_key", nil
	case "confluence":
		return "confluence_page_id", nil
	}
	return "", fmt.Errorf("unknown upstream source %q", source)
}

// UpstreamRules returns the rules tied to the given Jira keys or Confluence page IDs, keyed by reference.
// With labelManaged set and no refs, it returns every label-managed rule of the source.
func (x *KnownIssues) UpstreamRules(ctx context.Context, source string, refs []string, labelManaged bool) (map[string]ports.UpstreamRule, error) {
	col, err := upstreamCol(source)
	if err != nil {
		return nil, err
	}
	q := `SELECT id, ` + col + `, action::text, enabled, label_managed, upstream_note FROM known_issues WHERE source = $1::known_issue_source AND ` + col + ` IS NOT NULL`
	args := []any{source}
	if len(refs) > 0 {
		q += ` AND ` + col + ` = ANY($2::text[])`
		args = append(args, refs)
	}
	if labelManaged {
		q += ` AND label_managed`
	}
	rows, err := x.s.Pool.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]ports.UpstreamRule{}
	for rows.Next() {
		r := ports.UpstreamRule{Source: source}
		if err := rows.Scan(&r.ID, &r.Ref, &r.Action, &r.Enabled, &r.LabelManaged, &r.UpstreamNote); err != nil {
			return nil, err
		}
		out[r.Ref] = r
	}
	return out, rows.Err()
}

// CreateImported stores a label-imported draft rule (disabled, label-managed). A concurrent import of the
// same reference is a no-op.
func (x *KnownIssues) CreateImported(ctx context.Context, r ports.ImportedRule) (string, error) {
	col, err := upstreamCol(r.Source)
	if err != nil {
		return "", err
	}
	var m knownissues.Match
	if len(r.Match) > 0 {
		if err := json.Unmarshal(r.Match, &m); err != nil {
			return "", err
		}
	}
	k := KnownIssue{Title: r.Title, Description: r.Description, Explanation: r.Explanation, SourceText: r.SourceText, Reason: r.Reason,
		Match: m, Action: r.Action, Source: r.Source, TicketURL: r.TicketURL}
	if err := validateKnownIssue(&k, true); err != nil {
		if !emptyMatch(m) { // an invalid proposal: keep the draft, drop the match
			k.Match = knownissues.Match{}
			if err := validateKnownIssue(&k, true); err != nil {
				return "", err
			}
		} else {
			return "", err
		}
	}
	mb, _ := json.Marshal(k.Match)
	id := ports.NewID()
	tag, err := x.s.Pool.Exec(ctx, `INSERT INTO known_issues (id, title, description, explanation, source_text, `+col+`, reason, match, action,
			enabled, source, ticket_url, upstream_status, upstream_note, label_managed)
		VALUES ($1, $2, $3, $4, $5, $6, $7::known_issue_reason, $8, $9::known_issue_action, false, $10::known_issue_source, $11, $12, $13, true)
		ON CONFLICT DO NOTHING`,
		id, k.Title, k.Description, k.Explanation, k.SourceText, r.Ref, k.Reason, mb, k.Action, k.Source, k.TicketURL, r.UpstreamStatus, r.UpstreamNote)
	if err != nil {
		return "", fmt.Errorf("import known issue: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return "", nil
	}
	return id, nil
}

// SetUpstream records the upstream status and, when action is non-empty, changes the rule's action with
// a note explaining why.
func (x *KnownIssues) SetUpstream(ctx context.Context, id, status, action, note string) error {
	_, err := x.s.Pool.Exec(ctx, `UPDATE known_issues SET upstream_status = $2,
			action = CASE WHEN $3 = '' THEN action ELSE $3::known_issue_action END,
			upstream_note = CASE WHEN $3 = '' THEN upstream_note ELSE $4 END,
			updated_at = CASE WHEN $3 = '' AND upstream_status = $2 THEN updated_at ELSE now() END
		WHERE id = $1`, id, status, action, note)
	return err
}

// Update replaces the editable fields.
func (x *KnownIssues) Update(ctx context.Context, k KnownIssue) error {
	if err := validateKnownIssue(&k, true); err != nil {
		return err
	}
	m, _ := json.Marshal(k.Match)
	n, err := x.s.Q.UpdateKnownIssue(ctx, gen.UpdateKnownIssueParams{ID: k.ID, Title: k.Title, Description: k.Description, Reason: gen.KnownIssueReason(k.Reason),
		Match: m, Action: gen.KnownIssueAction(k.Action), Enabled: k.Enabled, ExpiresAt: k.ExpiresAt, TicketUrl: k.TicketURL})
	if err != nil {
		return err
	}
	if n == 0 {
		return ports.ErrNotFound
	}
	return nil
}

// Get loads one rule.
func (x *KnownIssues) Get(ctx context.Context, id string) (KnownIssue, error) {
	r, err := x.s.Q.GetKnownIssue(ctx, id)
	if IsNoRows(err) {
		return KnownIssue{}, ports.ErrNotFound
	}
	if err != nil {
		return KnownIssue{}, err
	}
	return toKnownIssue(r), nil
}

// List returns rules, newest first.
func (x *KnownIssues) List(ctx context.Context, limit int32) ([]KnownIssue, error) {
	rows, err := x.s.Q.ListKnownIssues(ctx, limit)
	if err != nil {
		return nil, err
	}
	out := make([]KnownIssue, 0, len(rows))
	for _, r := range rows {
		out = append(out, toKnownIssue(r))
	}
	return out, nil
}

// Delete removes a rule; issues it suppressed keep their history and resume normal handling.
func (x *KnownIssues) Delete(ctx context.Context, id string) error {
	n, err := x.s.Q.DeleteKnownIssue(ctx, id)
	if err != nil {
		return err
	}
	if n == 0 {
		return ports.ErrNotFound
	}
	return nil
}

func toKnownIssue(r gen.KnownIssue) KnownIssue {
	k := KnownIssue{ID: r.ID, Title: r.Title, Description: r.Description, Explanation: r.Explanation, SourceText: r.SourceText,
		Reason: string(r.Reason), Action: string(r.Action), Enabled: r.Enabled, ExpiresAt: r.ExpiresAt, Source: string(r.Source),
		TicketURL: r.TicketUrl, Hits: r.Hits, LastHitAt: r.LastHitAt, CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt}
	if r.JiraKey != nil {
		k.JiraKey = *r.JiraKey
	}
	if r.OwnerUserID != nil {
		k.OwnerUserID = *r.OwnerUserID
	}
	if r.ConfluencePageID != nil {
		k.ConfluencePageID = *r.ConfluencePageID
	}
	k.UpstreamStatus, k.UpstreamNote, k.LabelManaged = r.UpstreamStatus, r.UpstreamNote, r.LabelManaged
	_ = json.Unmarshal(r.Match, &k.Match)
	return k
}
