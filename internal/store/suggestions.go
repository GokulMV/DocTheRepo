package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/GokulMV/DocTheRepo/internal/core/knownissues"
	"github.com/GokulMV/DocTheRepo/internal/core/suggest"
	"github.com/GokulMV/DocTheRepo/internal/ports"
)

// Suggestions implements suggest.Store and the suggestion review queue.
type Suggestions struct {
	s  *Store
	ki *KnownIssues
}

// NewSuggestions returns the suggestion store.
func NewSuggestions(s *Store, ki *KnownIssues) *Suggestions { return &Suggestions{s: s, ki: ki} }

const candidateCols = `i.id, i.fingerprint, i.kind::text, i.title, i.service, i.environment, i.status::text, i.occurrences, i.last_seen,
	COALESCE(d.summary, '')`

func scanCandidates(rows pgx.Rows) ([]suggest.Candidate, error) {
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (suggest.Candidate, error) {
		var c suggest.Candidate
		err := r.Scan(&c.IssueID, &c.Fingerprint, &c.Kind, &c.Title, &c.Service, &c.Environment, &c.Status, &c.Occurrences, &c.LastSeen, &c.Summary)
		return c, err
	})
}

// AutoCandidates implements suggest.Store: open issues past the bar whose decode says not actionable or
// known noise, not covered by a pending or rejected suggestion.
func (x *Suggestions) AutoCandidates(ctx context.Context, minOccurrences int64, olderThan time.Time) ([]suggest.Candidate, error) {
	rows, err := x.s.Pool.Query(ctx, `SELECT `+candidateCols+` FROM issues i JOIN decodes d ON d.id = i.decode_id
		WHERE i.occurrences >= $1 AND i.first_seen <= $2 AND i.status IN ('new', 'decoded', 'regressed')
		  AND (NOT d.is_actionable OR d.suggest_known_issue)
		  AND NOT EXISTS (SELECT 1 FROM known_issue_suggestions s WHERE i.id = ANY(s.issue_ids) AND s.status IN ('pending', 'rejected'))
		ORDER BY i.service, i.environment, i.occurrences DESC LIMIT 500`, minOccurrences, olderThan)
	if err != nil {
		return nil, fmt.Errorf("auto candidates: %w", err)
	}
	return scanCandidates(rows)
}

// SaveSuggestion implements suggest.Store.
func (x *Suggestions) SaveSuggestion(ctx context.Context, issueIDs []string, m knownissues.Match, rationale string) (string, error) {
	id := ports.NewID()
	b, _ := json.Marshal(m)
	_, err := x.s.Pool.Exec(ctx, `INSERT INTO known_issue_suggestions (id, issue_ids, proposed_match, rationale) VALUES ($1, $2::uuid[], $3, $4)`,
		id, issueIDs, b, rationale)
	if err != nil {
		return "", fmt.Errorf("save suggestion: %w", err)
	}
	return id, nil
}

// SearchIssues implements suggest.Store (substring match, so no LIKE escaping is needed).
func (x *Suggestions) SearchIssues(ctx context.Context, terms, services []string, since time.Time, limit int) ([]suggest.Candidate, error) {
	lt := make([]string, 0, len(terms))
	for _, t := range terms {
		if t = strings.ToLower(strings.TrimSpace(t)); len(t) >= 3 {
			lt = append(lt, t)
		}
	}
	ls := make([]string, 0, len(services))
	for _, s := range services {
		ls = append(ls, strings.ToLower(s))
	}
	if len(lt) == 0 && len(ls) == 0 {
		return nil, nil
	}
	rows, err := x.s.Pool.Query(ctx, `SELECT `+candidateCols+` FROM issues i LEFT JOIN decodes d ON d.id = i.decode_id
		WHERE i.last_seen >= $1 AND NOT `+noLLMExists+` AND (
		  EXISTS (SELECT 1 FROM unnest($2::text[]) t WHERE strpos(lower(i.title), t) > 0 OR strpos(lower(COALESCE(d.summary, '')), t) > 0)
		  OR lower(i.service) = ANY($3::text[]))
		ORDER BY (EXISTS (SELECT 1 FROM unnest($2::text[]) t WHERE strpos(lower(i.title), t) > 0)) DESC, i.occurrences DESC
		LIMIT $4`, since, lt, ls, limit)
	if err != nil {
		return nil, fmt.Errorf("search issues: %w", err)
	}
	return scanCandidates(rows)
}

// IssuesForDecodeChunks implements suggest.Store.
func (x *Suggestions) IssuesForDecodeChunks(ctx context.Context, chunkIDs []string) ([]suggest.Candidate, error) {
	if len(chunkIDs) == 0 {
		return nil, nil
	}
	rows, err := x.s.Pool.Query(ctx, `SELECT `+candidateCols+` FROM chunks c
		JOIN issues i ON c.path = 'issues/' || i.id::text LEFT JOIN decodes d ON d.id = i.decode_id
		WHERE c.chunk_id = ANY($1) AND c.source = 'issue_decode' AND c.deleted_at IS NULL`, chunkIDs)
	if err != nil {
		return nil, fmt.Errorf("issues for decodes: %w", err)
	}
	return scanCandidates(rows)
}

// Services implements suggest.Store.
func (x *Suggestions) Services(ctx context.Context, since time.Time) ([]string, error) {
	rows, err := x.s.Pool.Query(ctx, `SELECT DISTINCT service FROM issues WHERE last_seen >= $1 AND service <> '' ORDER BY 1 LIMIT 2000`, since)
	if err != nil {
		return nil, fmt.Errorf("services: %w", err)
	}
	return pgx.CollectRows(rows, pgx.RowTo[string])
}

// LatestSamples implements suggest.Store.
func (x *Suggestions) LatestSamples(ctx context.Context, since time.Time, limit int) ([]suggest.Sampled, error) {
	rows, err := x.s.Pool.Query(ctx, `SELECT i.id, i.fingerprint, i.kind::text, i.title, i.service, i.environment, i.status::text,
		i.occurrences, i.last_seen, s.source, s.severity::text, s.kind::text, s.title, s.message_scrubbed, s.exception_type, s.attrs
		FROM issues i JOIN LATERAL (SELECT * FROM event_samples es WHERE es.issue_id = i.id ORDER BY es.occurred_at DESC LIMIT 1) s ON true
		WHERE i.last_seen >= $1 ORDER BY i.last_seen DESC LIMIT $2`, since, limit)
	if err != nil {
		return nil, fmt.Errorf("latest samples: %w", err)
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (suggest.Sampled, error) {
		var it suggest.Sampled
		var sev, kind string
		var attrs []byte
		err := r.Scan(&it.IssueID, &it.Fingerprint, &it.Kind, &it.Title, &it.Service, &it.Environment, &it.Status, &it.Occurrences,
			&it.LastSeen, &it.Sample.Source, &sev, &kind, &it.Sample.Title, &it.Sample.Message, &it.Sample.ExceptionType, &attrs)
		it.Sample.Severity, it.Sample.Kind = ports.Severity(sev), ports.SignalKind(kind)
		it.Sample.Service, it.Sample.Environment = it.Service, it.Environment
		_ = json.Unmarshal(attrs, &it.Sample.Attrs)
		return it, err
	})
}

// Suggestion is a proposed rule awaiting review.
type Suggestion struct {
	ID            string            `json:"id"`
	IssueIDs      []string          `json:"issue_ids"`
	ProposedMatch knownissues.Match `json:"proposed_match"`
	Rationale     string            `json:"rationale"`
	Status        string            `json:"status"`
	DecidedBy     string            `json:"decided_by,omitempty"`
	DecidedAt     *time.Time        `json:"decided_at,omitempty"`
	CreatedAt     time.Time         `json:"created_at"`
	KnownIssueID  string            `json:"known_issue_id,omitempty"`
}

// ListSuggestions returns suggestions by status (empty: pending), newest first.
func (x *Suggestions) ListSuggestions(ctx context.Context, status string, limit int) ([]Suggestion, error) {
	if status == "" {
		status = "pending"
	}
	rows, err := x.s.Pool.Query(ctx, `SELECT id, issue_ids::text[], proposed_match, rationale, status::text, decided_by, decided_at, created_at
		FROM known_issue_suggestions WHERE status = $1::suggestion_status ORDER BY created_at DESC LIMIT $2`, status, limit)
	if err != nil {
		return nil, fmt.Errorf("list suggestions: %w", err)
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (Suggestion, error) {
		var s Suggestion
		var m []byte
		var by *string
		err := r.Scan(&s.ID, &s.IssueIDs, &m, &s.Rationale, &s.Status, &by, &s.DecidedAt, &s.CreatedAt)
		_ = json.Unmarshal(m, &s.ProposedMatch)
		if by != nil {
			s.DecidedBy = *by
		}
		return s, err
	})
}

// ErrAlreadyDecided is returned when a suggestion was accepted or rejected before.
var ErrAlreadyDecided = errors.New("suggestion already decided")

// DecideSuggestion accepts (creating an enabled known issue owned by the reviewer) or rejects a pending
// suggestion. Accepting is the human approval the plan requires before anything is suppressed.
func (x *Suggestions) DecideSuggestion(ctx context.Context, id, userID string, accept bool, title, reason string) (Suggestion, error) {
	var s Suggestion
	var m []byte
	err := x.s.Pool.QueryRow(ctx, `SELECT id, issue_ids::text[], proposed_match, rationale, status::text FROM known_issue_suggestions WHERE id = $1`, id).
		Scan(&s.ID, &s.IssueIDs, &m, &s.Rationale, &s.Status)
	if errors.Is(err, pgx.ErrNoRows) {
		return s, ports.ErrNotFound
	}
	if err != nil {
		return s, fmt.Errorf("load suggestion: %w", err)
	}
	if s.Status != "pending" {
		return s, ErrAlreadyDecided
	}
	_ = json.Unmarshal(m, &s.ProposedMatch)
	status := "rejected"
	if accept {
		status = "accepted"
		if title == "" {
			title = "Accepted suggestion"
		}
		if reason == "" {
			reason = "expected_noise"
		}
		kiID, err := x.ki.Create(ctx, KnownIssue{Title: title, Explanation: s.Rationale, Reason: reason, Match: s.ProposedMatch,
			Action: "suppress", Source: "suggested", OwnerUserID: userID})
		if err != nil {
			return s, err
		}
		// Suggested rules are created disabled; the reviewer's acceptance is what enables this one.
		if _, err := x.s.Pool.Exec(ctx, `UPDATE known_issues SET enabled = true, updated_at = now() WHERE id = $1`, kiID); err != nil {
			return s, fmt.Errorf("enable accepted rule: %w", err)
		}
		s.KnownIssueID = kiID
	}
	tag, err := x.s.Pool.Exec(ctx, `UPDATE known_issue_suggestions SET status = $2::suggestion_status, decided_by = $3, decided_at = now()
		WHERE id = $1 AND status = 'pending'`, id, status, nilIfEmpty(userID))
	if err != nil {
		return s, fmt.Errorf("decide suggestion: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return s, ErrAlreadyDecided
	}
	s.Status = status
	return s, nil
}

func nilIfEmpty(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
