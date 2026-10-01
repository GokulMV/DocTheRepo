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
	"github.com/GokulMV/DocTheRepo/internal/ports"
	"github.com/GokulMV/DocTheRepo/internal/store/gen"
)

// Issues serves the Inbox (plan § 7.5).
type Issues struct {
	s  *Store
	ki *KnownIssues
}

// NewIssues returns the Inbox store.
func NewIssues(s *Store, ki *KnownIssues) *Issues { return &Issues{s: s, ki: ki} }

// IssueFilter narrows the Inbox. Empty fields match everything; RepoIDs applies unless AllRepos.
type IssueFilter struct {
	Status, Source, Service, Environment, Severity, Kind, Q string
	Since                                                   time.Time
	AllRepos                                                bool
	RepoIDs                                                 []string
	// Keyset cursor: issues strictly older than (BeforeSeen, BeforeID) in (last_seen, id) order.
	BeforeSeen *time.Time
	BeforeID   string
	Limit      int
}

// IssueRow is one Inbox line.
type IssueRow struct {
	ID              string     `json:"id"`
	Fingerprint     string     `json:"fingerprint"`
	Kind            string     `json:"kind"`
	Title           string     `json:"title"`
	Service         string     `json:"service"`
	Environment     string     `json:"environment"`
	Status          string     `json:"status"`
	Severity        string     `json:"severity"`
	Occurrences     int64      `json:"occurrences"`
	SuppressedCount int64      `json:"suppressed_count"`
	Sources         []string   `json:"sources"`
	FirstSeen       time.Time  `json:"first_seen"`
	LastSeen        time.Time  `json:"last_seen"`
	RepoID          string     `json:"repo_id,omitempty"`
	KnownIssueID    string     `json:"known_issue_id,omitempty"`
	AssigneeUserID  string     `json:"assignee_user_id,omitempty"`
	ResolvedAt      *time.Time `json:"resolved_at,omitempty"`
	Summary         string     `json:"decode_summary,omitempty"`
	Confidence      string     `json:"decode_confidence,omitempty"`
	Actionable      *bool      `json:"is_actionable,omitempty"`
	// Sparkline is occurrences per hour for the last 24 hours, oldest first.
	Sparkline []int64 `json:"sparkline"`
}

const issueCols = `i.id, i.fingerprint, i.kind::text, i.title, i.service, i.environment, i.status::text, i.severity_max::text,
	i.occurrences, i.suppressed_count, i.sources, i.first_seen, i.last_seen, i.repo_id, i.known_issue_id, i.assignee_user_id,
	i.resolved_at, COALESCE(d.summary, ''), COALESCE(d.confidence::text, ''), d.is_actionable`

func scanIssue(r pgx.Row) (IssueRow, error) {
	var it IssueRow
	var repo, ki, assignee *string
	err := r.Scan(&it.ID, &it.Fingerprint, &it.Kind, &it.Title, &it.Service, &it.Environment, &it.Status, &it.Severity,
		&it.Occurrences, &it.SuppressedCount, &it.Sources, &it.FirstSeen, &it.LastSeen, &repo, &ki, &assignee, &it.ResolvedAt,
		&it.Summary, &it.Confidence, &it.Actionable)
	it.RepoID, it.KnownIssueID, it.AssigneeUserID = deref(repo), deref(ki), deref(assignee)
	if it.Sources == nil {
		it.Sources = []string{}
	}
	return it, err
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// visible is the ACL predicate: issues not tied to a repository are visible to every viewer; others follow
// the repository's access ($1 all, $2 ids).
const visible = `($1::boolean OR i.repo_id IS NULL OR i.repo_id = ANY($2::uuid[]))`

// List returns Inbox rows by last_seen desc.
func (x *Issues) List(ctx context.Context, f IssueFilter) ([]IssueRow, error) {
	args := []any{f.AllRepos, orEmptySlice(f.RepoIDs)}
	where := []string{visible}
	add := func(cond string, v any) {
		args = append(args, v)
		where = append(where, strings.ReplaceAll(cond, "?", fmt.Sprintf("$%d", len(args))))
	}
	if f.Status != "" {
		add("i.status::text = ?", f.Status)
	} else {
		where = append(where, "i.status <> 'suppressed'") // the Inbox hides known issues unless asked
	}
	if f.Source != "" {
		add("? = ANY(i.sources)", f.Source)
	}
	if f.Service != "" {
		add("i.service = ?", f.Service)
	}
	if f.Environment != "" {
		add("i.environment = ?", f.Environment)
	}
	if f.Severity != "" {
		add("i.severity_max >= ?::signal_severity", f.Severity)
	}
	if f.Kind != "" {
		add("i.kind::text = ?", f.Kind)
	}
	if q := strings.TrimSpace(f.Q); q != "" {
		add("(strpos(lower(i.title), lower(?)) > 0 OR strpos(lower(COALESCE(d.summary, '')), lower(?)) > 0)", q)
	}
	if !f.Since.IsZero() {
		add("i.last_seen >= ?", f.Since)
	}
	if f.BeforeSeen != nil {
		args = append(args, *f.BeforeSeen, f.BeforeID)
		where = append(where, fmt.Sprintf("(i.last_seen, i.id) < ($%d, $%d::uuid)", len(args)-1, len(args)))
	}
	limit := f.Limit
	if limit <= 0 {
		limit = 50
	}
	args = append(args, limit)
	rows, err := x.s.Pool.Query(ctx, `SELECT `+issueCols+` FROM issues i LEFT JOIN decodes d ON d.id = i.decode_id
		WHERE `+strings.Join(where, " AND ")+fmt.Sprintf(` ORDER BY i.last_seen DESC, i.id DESC LIMIT $%d`, len(args)), args...)
	if err != nil {
		return nil, fmt.Errorf("list issues: %w", err)
	}
	items, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (IssueRow, error) { return scanIssue(r) })
	if err != nil {
		return nil, err
	}
	return items, x.sparklines(ctx, items)
}

// sparklines fills the last 24 hourly counts for each row.
func (x *Issues) sparklines(ctx context.Context, items []IssueRow) error {
	if len(items) == 0 {
		return nil
	}
	ids := make([]string, len(items))
	idx := map[string]int{}
	for i, it := range items {
		ids[i] = it.ID
		idx[it.ID] = i
		items[i].Sparkline = make([]int64, 24)
	}
	start := time.Now().UTC().Truncate(time.Hour).Add(-23 * time.Hour)
	rows, err := x.s.Pool.Query(ctx, `SELECT issue_id::text, hour, count FROM issue_counts_hourly WHERE issue_id = ANY($1::uuid[]) AND hour >= $2`, ids, start)
	if err != nil {
		return fmt.Errorf("sparklines: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		var hour time.Time
		var n int64
		if err := rows.Scan(&id, &hour, &n); err != nil {
			return err
		}
		if b := int(hour.UTC().Sub(start) / time.Hour); b >= 0 && b < 24 {
			items[idx[id]].Sparkline[b] += n
		}
	}
	return rows.Err()
}

// IssueDetail is one issue with its decode, recent events, and counts.
type IssueDetail struct {
	IssueRow
	Decode     *DecodeView         `json:"decode,omitempty"`
	Events     []ports.SignalEvent `json:"events"`
	Hourly     []CountPoint        `json:"hourly"`
	KnownIssue *KnownIssue         `json:"known_issue,omitempty"`
	Similar    []IssueRef2         `json:"similar_issues"`
}

// DecodeView is a stored decode as the UI shows it.
type DecodeView struct {
	ID                string          `json:"id"`
	Summary           string          `json:"summary"`
	ProbableCause     string          `json:"probable_cause"`
	Impact            string          `json:"impact"`
	AffectedCode      json.RawMessage `json:"affected_code"`
	RelatedCommits    json.RawMessage `json:"related_commits"`
	RelatedDocs       json.RawMessage `json:"related_docs"`
	NextSteps         []string        `json:"next_steps"`
	Confidence        string          `json:"confidence"`
	IsActionable      bool            `json:"is_actionable"`
	SuggestKnownIssue bool            `json:"suggest_known_issue"`
	Provider          string          `json:"provider"`
	Model             string          `json:"model"`
	Tokens            int64           `json:"tokens"`
	CostUSD           float64         `json:"cost_usd"`
	CreatedAt         time.Time       `json:"created_at"`
}

// CountPoint is one bucket of a count series.
type CountPoint struct {
	T          time.Time `json:"t"`
	Count      int64     `json:"count"`
	Suppressed int64     `json:"suppressed"`
}

// IssueRef2 is a linked issue.
type IssueRef2 struct {
	ID     string `json:"id"`
	Title  string `json:"title"`
	Status string `json:"status"`
}

// Get returns an issue's detail, or ErrNotFound (also when the caller may not read its repository).
func (x *Issues) Get(ctx context.Context, id string, all bool, repoIDs []string) (IssueDetail, error) {
	row, err := scanIssue(x.s.Pool.QueryRow(ctx, `SELECT `+issueCols+` FROM issues i LEFT JOIN decodes d ON d.id = i.decode_id
		WHERE `+visible+` AND i.id = $3`, all, orEmptySlice(repoIDs), id))
	if errors.Is(err, pgx.ErrNoRows) {
		return IssueDetail{}, ports.ErrNotFound
	}
	if err != nil {
		return IssueDetail{}, fmt.Errorf("load issue: %w", err)
	}
	det := IssueDetail{IssueRow: row, Events: []ports.SignalEvent{}, Hourly: []CountPoint{}, Similar: []IssueRef2{}}
	items := []IssueRow{det.IssueRow}
	if err := x.sparklines(ctx, items); err != nil {
		return det, err
	}
	det.Sparkline = items[0].Sparkline
	var dv DecodeView
	var similar []string
	err = x.s.Pool.QueryRow(ctx, `SELECT d.id, d.summary, d.probable_cause, d.impact, d.affected_code, d.related_commits, d.related_docs,
		d.next_steps, d.confidence::text, d.is_actionable, d.suggest_known_issue, d.provider, d.model, d.tokens, d.cost_usd::float8,
		d.created_at, d.similar_issue_ids::text[] FROM issues i JOIN decodes d ON d.id = i.decode_id WHERE i.id = $1`, id).
		Scan(&dv.ID, &dv.Summary, &dv.ProbableCause, &dv.Impact, &dv.AffectedCode, &dv.RelatedCommits, &dv.RelatedDocs, &dv.NextSteps,
			&dv.Confidence, &dv.IsActionable, &dv.SuggestKnownIssue, &dv.Provider, &dv.Model, &dv.Tokens, &dv.CostUSD, &dv.CreatedAt, &similar)
	switch {
	case err == nil:
		det.Decode = &dv
	case !errors.Is(err, pgx.ErrNoRows):
		return det, fmt.Errorf("load decode: %w", err)
	}
	samples, err := x.s.Q.IssueSamples(ctx, gen.IssueSamplesParams{IssueID: id, Lim: 50})
	if err != nil {
		return det, fmt.Errorf("load events: %w", err)
	}
	for _, r := range samples {
		ev := ports.SignalEvent{Source: r.Source, ExternalID: r.ExternalID, OccurredAt: r.OccurredAt, ReceivedAt: r.ReceivedAt,
			Severity: ports.Severity(r.Severity), Kind: ports.SignalKind(r.Kind), Service: r.Service, Environment: r.Environment,
			Title: r.Title, Message: r.MessageScrubbed, ExceptionType: r.ExceptionType}
		if r.ConnectorID != nil {
			ev.ConnectorID = *r.ConnectorID
		}
		_ = json.Unmarshal(r.Stack, &ev.Stack)
		_ = json.Unmarshal(r.Attrs, &ev.Attrs)
		det.Events = append(det.Events, ev)
	}
	rows, err := x.s.Pool.Query(ctx, `SELECT hour, count, suppressed_count FROM issue_counts_hourly WHERE issue_id = $1 AND hour >= $2 ORDER BY hour`,
		id, time.Now().Add(-7*24*time.Hour))
	if err != nil {
		return det, fmt.Errorf("load counts: %w", err)
	}
	pts, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (CountPoint, error) {
		var p CountPoint
		return p, r.Scan(&p.T, &p.Count, &p.Suppressed)
	})
	if err != nil {
		return det, err
	}
	if pts != nil {
		det.Hourly = pts
	}
	if row.KnownIssueID != "" {
		if k, err := x.ki.Get(ctx, row.KnownIssueID); err == nil {
			det.KnownIssue = &k
		}
	}
	if len(similar) > 0 {
		rows, err := x.s.Pool.Query(ctx, `SELECT i.id::text, i.title, i.status::text FROM issues i WHERE i.id = ANY($3::uuid[]) AND `+visible,
			all, orEmptySlice(repoIDs), similar)
		if err == nil {
			if refs, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (IssueRef2, error) {
				var s IssueRef2
				return s, r.Scan(&s.ID, &s.Title, &s.Status)
			}); err == nil && refs != nil {
				det.Similar = refs
			}
		}
	}
	return det, nil
}

// IssueStatuses a person may set (new reopens; suppressed comes only from a known-issue rule).
var IssueStatuses = map[string]bool{"new": true, "acknowledged": true, "resolved": true}

// Update changes status and/or assignee. assignee "" leaves it; "-" clears it.
func (x *Issues) Update(ctx context.Context, id, status, assignee string) error {
	if status != "" && !IssueStatuses[status] {
		return &ports.ValidationError{Code: "VALIDATION_FAILED", Message: "status must be new, acknowledged, or resolved"}
	}
	var assignArg any
	switch assignee {
	case "":
	case "-":
		assignArg = "-"
	default:
		assignArg = assignee
	}
	tag, err := x.s.Pool.Exec(ctx, `UPDATE issues SET
		status = COALESCE(NULLIF($2, '')::issue_status, status),
		resolved_at = CASE WHEN $2 = 'resolved' THEN now() WHEN $2 IN ('new', 'acknowledged') THEN NULL ELSE resolved_at END,
		assignee_user_id = CASE WHEN $3::text IS NULL THEN assignee_user_id WHEN $3::text = '-' THEN NULL ELSE $3::uuid END,
		updated_at = now() WHERE id = $1`, id, status, assignArg)
	if err != nil {
		return fmt.Errorf("update issue: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ports.ErrNotFound
	}
	return nil
}

// MarkKnown creates an enabled known-issue rule from an issue's fingerprint (plus its service and
// environment unless override replaces the match) and marks the issue suppressed by it.
func (x *Issues) MarkKnown(ctx context.Context, id string, k KnownIssue, override *knownissues.Match) (string, error) {
	var fp, service, env string
	err := x.s.Pool.QueryRow(ctx, `SELECT fingerprint, service, environment FROM issues WHERE id = $1`, id).Scan(&fp, &service, &env)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ports.ErrNotFound
	}
	if err != nil {
		return "", fmt.Errorf("load issue: %w", err)
	}
	if override != nil {
		k.Match = *override
	} else {
		k.Match = knownissues.Match{Fingerprints: []string{fp}}
		if service != "" && service != "unknown" {
			k.Match.Services = []string{service}
		}
		if env != "" {
			k.Match.Environments = []string{env}
		}
	}
	if k.Action == "" {
		k.Action = "suppress"
	}
	k.Source, k.Enabled = "manual", true
	kiID, err := x.ki.Create(ctx, k)
	if err != nil {
		return "", err
	}
	if _, err := x.s.Pool.Exec(ctx, `UPDATE issues SET known_issue_id = $2, updated_at = now(),
		status = CASE WHEN $3 = 'suppress' THEN 'suppressed'::issue_status ELSE status END WHERE id = $1`, id, kiID, k.Action); err != nil {
		return "", fmt.Errorf("link known issue: %w", err)
	}
	return kiID, nil
}
