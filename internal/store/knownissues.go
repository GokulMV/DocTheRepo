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

func validateKnownIssue(k *KnownIssue) error {
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
	if err := knownissues.Validate(k.Match, knownissues.Action(k.Action)); err != nil {
		return &ports.ValidationError{Code: "INVALID_RULE", Message: err.Error()}
	}
	k.SourceText = signals.Scrub(k.SourceText)
	return nil
}

// Create validates and stores a rule. Suggested rules are stored disabled: nothing is suppressed without a
// human.
func (x *KnownIssues) Create(ctx context.Context, k KnownIssue) (string, error) {
	if err := validateKnownIssue(&k); err != nil {
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
	return id, nil
}

// Update replaces the editable fields.
func (x *KnownIssues) Update(ctx context.Context, k KnownIssue) error {
	if err := validateKnownIssue(&k); err != nil {
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
	_ = json.Unmarshal(r.Match, &k.Match)
	return k
}
