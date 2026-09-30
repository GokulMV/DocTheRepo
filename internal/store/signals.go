package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/GokulMV/DocTheRepo/internal/core/aggregate"
	"github.com/GokulMV/DocTheRepo/internal/core/knownissues"
	"github.com/GokulMV/DocTheRepo/internal/core/signals"
	"github.com/GokulMV/DocTheRepo/internal/ports"
	"github.com/GokulMV/DocTheRepo/internal/store/gen"
)

// DefaultDecodeTokens estimates a decode's size before any decode has run (the savings a suppression is
// credited with).
const DefaultDecodeTokens = 8000

// IssueRef is an issue a flush created or re-opened, for follow-up work (decode jobs).
type IssueRef struct {
	ID          string
	Fingerprint string
	Status      string // new | regressed
	Service     string
	Kind        ports.SignalKind
}

// Signals persists aggregated signal batches (aggregate.Sink) and maintains the sample partitions.
type Signals struct {
	s *Store
	// OnIssues is called after a committed flush with the issues it created (status new) or regressed.
	OnIssues func(ctx context.Context, refs []IssueRef)

	mu      sync.Mutex
	ensured map[string]bool // sample partitions known to exist (YYYY-MM-DD)
}

// NewSignals returns the signal store.
func NewSignals(s *Store) *Signals { return &Signals{s: s, ensured: map[string]bool{}} }

// Flush writes one aggregation window in one transaction: an issue upsert per fingerprint (never per
// event), minute/hour counts, the kept samples, known-issue hit counters, and savings for issues that
// became suppressed. Groups are written in fingerprint order so concurrent replicas lock rows in the same
// order (no deadlocks).
func (x *Signals) Flush(ctx context.Context, b aggregate.Batch) error {
	if len(b.Groups) == 0 {
		return nil
	}
	groups := append([]*aggregate.Group(nil), b.Groups...)
	sort.Slice(groups, func(i, j int) bool { return groups[i].Fingerprint < groups[j].Fingerprint })
	if err := x.ensureSampleDays(ctx, groups); err != nil {
		return err
	}
	var refs []IssueRef
	err := x.s.InTx(ctx, func(q *gen.Queries, tx pgx.Tx) error {
		ids, err := upsertIssues(ctx, tx, groups)
		if err != nil {
			return err
		}
		if err := writeCounts(ctx, tx, groups, ids); err != nil {
			return err
		}
		if err := writeSamples(ctx, tx, groups, ids); err != nil {
			return err
		}
		if err := countRuleHits(ctx, tx, groups); err != nil {
			return err
		}
		var suppressed []string
		for _, g := range groups {
			r := ids[g.Fingerprint]
			switch {
			case r.status == "suppressed" && r.oldStatus != "suppressed":
				suppressed = append(suppressed, r.id)
			case r.status == "new" && r.inserted, r.status == "regressed" && r.oldStatus != "regressed":
				refs = append(refs, IssueRef{ID: r.id, Fingerprint: g.Fingerprint, Status: r.status, Service: g.Service, Kind: g.Kind})
			}
		}
		if len(suppressed) > 0 {
			est, err := q.DecodeEstimate(ctx)
			if err != nil {
				return err
			}
			tokens := est.Tokens
			if tokens <= 0 {
				tokens = DefaultDecodeTokens
			}
			for _, id := range suppressed {
				if err := q.InsertSavings(ctx, gen.InsertSavingsParams{ID: ports.NewID(), Kind: gen.SavingsKindKnownIssueSuppressed,
					EstTokensAvoided: tokens, EstCostAvoidedUsd: est.CostUsd, RefID: id}); err != nil {
					return err
				}
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	if len(refs) > 0 && x.OnIssues != nil {
		x.OnIssues(ctx, refs)
	}
	return nil
}

type upserted struct {
	id, status, oldStatus string
	inserted              bool
}

const upsertIssuesSQL = `
WITH incoming AS (
    SELECT * FROM unnest($1::uuid[], $2::text[], $3::text[], $4::text[], $5::text[], $6::text[], $7::text[],
                         $8::timestamptz[], $9::timestamptz[], $10::bigint[], $11::bigint[], $12::text[], $13::text[], $14::text[])
        AS u(id, fp, alt, kind, title, service, env, first_seen, last_seen, occ, supp, sources, sev, known)
),
old AS (SELECT fingerprint, status::text AS status FROM issues WHERE fingerprint = ANY($2::text[])),
up AS (
    INSERT INTO issues AS i (id, fingerprint, alt_fingerprint, kind, title, service, environment, repo_id, first_seen, last_seen,
                             occurrences, suppressed_count, sources, status, severity_max, known_issue_id)
    SELECT u.id, u.fp, u.alt, u.kind::signal_kind, u.title, u.service, u.env,
           (SELECT sm.repo_id FROM service_map sm WHERE sm.service_name = u.service ORDER BY sm.repo_id LIMIT 1),
           u.first_seen, u.last_seen, u.occ, u.supp, string_to_array(u.sources, ','),
           (CASE WHEN u.occ = 0 AND u.supp > 0 THEN 'suppressed' ELSE 'new' END)::issue_status,
           u.sev::signal_severity,
           (SELECT k.id FROM known_issues k WHERE k.id::text = NULLIF(u.known, ''))
    FROM incoming u
    ON CONFLICT (fingerprint) DO UPDATE SET
        occurrences      = i.occurrences + EXCLUDED.occurrences,
        suppressed_count = i.suppressed_count + EXCLUDED.suppressed_count,
        first_seen       = LEAST(i.first_seen, EXCLUDED.first_seen),
        last_seen        = GREATEST(i.last_seen, EXCLUDED.last_seen),
        severity_max     = GREATEST(i.severity_max, EXCLUDED.severity_max),
        sources          = ARRAY(SELECT DISTINCT s FROM unnest(i.sources || EXCLUDED.sources) s ORDER BY s),
        alt_fingerprint  = CASE WHEN i.alt_fingerprint = '' THEN EXCLUDED.alt_fingerprint ELSE i.alt_fingerprint END,
        known_issue_id   = COALESCE(EXCLUDED.known_issue_id, i.known_issue_id),
        status = (CASE
            WHEN EXCLUDED.occurrences > 0 AND i.status = 'resolved' THEN 'regressed'
            WHEN EXCLUDED.occurrences > 0 AND i.status = 'suppressed' THEN (CASE WHEN i.decode_id IS NOT NULL THEN 'decoded' ELSE 'new' END)
            WHEN EXCLUDED.occurrences = 0 AND EXCLUDED.suppressed_count > 0 AND i.status NOT IN ('acknowledged', 'resolved') THEN 'suppressed'
            ELSE i.status::text END)::issue_status,
        resolved_at = CASE WHEN EXCLUDED.occurrences > 0 AND i.status = 'resolved' THEN NULL ELSE i.resolved_at END,
        updated_at  = now()
    RETURNING i.id::text AS id, i.fingerprint, (xmax = 0) AS inserted, i.status::text AS status
)
SELECT up.id, up.fingerprint, up.inserted, up.status, COALESCE(old.status, '') FROM up LEFT JOIN old USING (fingerprint)`

func upsertIssues(ctx context.Context, tx pgx.Tx, groups []*aggregate.Group) (map[string]upserted, error) {
	n := len(groups)
	ids, fps, alts, kinds, titles, services, envs := make([]string, n), make([]string, n), make([]string, n), make([]string, n), make([]string, n), make([]string, n), make([]string, n)
	firsts, lasts := make([]time.Time, n), make([]time.Time, n)
	occ, supp := make([]int64, n), make([]int64, n)
	sources, sevs, known := make([]string, n), make([]string, n), make([]string, n)
	for i, g := range groups {
		ids[i], fps[i], alts[i], kinds[i] = ports.NewID(), g.Fingerprint, g.AltFingerprint, string(g.Kind)
		titles[i], services[i], envs[i] = g.Title, g.Service, g.Environment
		firsts[i], lasts[i], occ[i], supp[i] = g.FirstSeen, g.LastSeen, g.Count, g.Suppressed
		var src []string
		for s := range g.Sources {
			src = append(src, strings.ReplaceAll(s, ",", "_"))
		}
		sort.Strings(src)
		sources[i], sevs[i] = strings.Join(src, ","), string(g.SeverityMax)
		known[i] = g.SuppressedBy
		if known[i] == "" {
			known[i] = g.LabeledBy
		}
	}
	rows, err := tx.Query(ctx, upsertIssuesSQL, ids, fps, alts, kinds, titles, services, envs, firsts, lasts, occ, supp, sources, sevs, known)
	if err != nil {
		return nil, fmt.Errorf("upsert issues: %w", err)
	}
	defer rows.Close()
	out := make(map[string]upserted, n)
	for rows.Next() {
		var u upserted
		var fp string
		if err := rows.Scan(&u.id, &fp, &u.inserted, &u.status, &u.oldStatus); err != nil {
			return nil, err
		}
		out[fp] = u
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("upsert issues: %w", err)
	}
	return out, nil
}

func writeCounts(ctx context.Context, tx pgx.Tx, groups []*aggregate.Group, ids map[string]upserted) error {
	type key struct {
		id string
		t  time.Time
	}
	minutes, hours := map[key]*aggregate.Minute{}, map[key]*aggregate.Minute{}
	add := func(m map[key]*aggregate.Minute, k key, c *aggregate.Minute) {
		if m[k] == nil {
			m[k] = &aggregate.Minute{}
		}
		m[k].Count += c.Count
		m[k].Suppressed += c.Suppressed
	}
	for _, g := range groups {
		id := ids[g.Fingerprint].id
		for t, c := range g.Minutes {
			add(minutes, key{id, t}, c)
			add(hours, key{id, t.Truncate(time.Hour)}, c)
		}
	}
	for table, m := range map[string]map[key]*aggregate.Minute{"issue_counts_minutely (issue_id, minute": minutes, "issue_counts_hourly (issue_id, hour": hours} {
		keys := make([]key, 0, len(m))
		for k := range m {
			keys = append(keys, k)
		}
		sort.Slice(keys, func(i, j int) bool {
			if keys[i].id != keys[j].id {
				return keys[i].id < keys[j].id
			}
			return keys[i].t.Before(keys[j].t)
		})
		ids, ts, cs, ss := make([]string, len(keys)), make([]time.Time, len(keys)), make([]int64, len(keys)), make([]int64, len(keys))
		for i, k := range keys {
			ids[i], ts[i], cs[i], ss[i] = k.id, k.t, m[k].Count, m[k].Suppressed
		}
		tbl := table[:strings.Index(table, " ")]
		if _, err := tx.Exec(ctx, `INSERT INTO `+table+`, count, suppressed_count)
			SELECT * FROM unnest($1::uuid[], $2::timestamptz[], $3::bigint[], $4::bigint[])
			ON CONFLICT ON CONSTRAINT `+tbl+`_pkey DO UPDATE SET count = `+tbl+`.count + EXCLUDED.count,
				suppressed_count = `+tbl+`.suppressed_count + EXCLUDED.suppressed_count`, ids, ts, cs, ss); err != nil {
			return fmt.Errorf("write %s: %w", tbl, err)
		}
	}
	return nil
}

const sampleCols = `issue_id, sample_day, sample_hour, sample_slot, sample_kind, connector_id, source, external_id, occurred_at, received_at,
	severity, kind, service, environment, title, message_scrubbed, exception_type, stack, attrs, fingerprint`

func writeSamples(ctx context.Context, tx pgx.Tx, groups []*aggregate.Group, ids map[string]upserted) error {
	batch := &pgx.Batch{}
	for _, g := range groups {
		id := ids[g.Fingerprint].id
		for _, smp := range g.Samples {
			e := smp.Event
			stack, _ := json.Marshal(nonNilFrames(e.Stack))
			attrs, _ := json.Marshal(nonNilMap(e.Attrs))
			hour := smp.Hour.UTC()
			args := []any{id, hour.Format("2006-01-02"), hour, smp.Slot, string(smp.Kind), strPtr(e.ConnectorID), e.Source, e.ExternalID, e.OccurredAt,
				e.ReceivedAt, string(e.Severity), string(e.Kind), e.Service, e.Environment, e.Title, e.Message, e.ExceptionType, stack, attrs, e.Fingerprint}
			if smp.Kind == aggregate.SampleFirst {
				// First samples are capped at FirstSamples per issue across replicas and restarts.
				batch.Queue(`INSERT INTO event_samples (`+sampleCols+`)
					SELECT $1::uuid, $2::date, $3, $4, $5, $6::uuid, $7, $8, $9, $10, $11::signal_severity, $12::signal_kind, $13, $14, $15, $16, $17, $18, $19, $20
					WHERE (SELECT count(*) FROM event_samples WHERE issue_id = $1::uuid AND sample_kind = 'first') < `+fmt.Sprint(aggregate.FirstSamples)+`
					ON CONFLICT DO NOTHING`, args...)
				continue
			}
			batch.Queue(`INSERT INTO event_samples (`+sampleCols+`)
				VALUES ($1, $2::date, $3, $4, $5, $6::uuid, $7, $8, $9, $10, $11::signal_severity, $12::signal_kind, $13, $14, $15, $16, $17, $18, $19, $20)
				ON CONFLICT (issue_id, sample_day, sample_hour, sample_slot) DO UPDATE SET
					sample_kind = EXCLUDED.sample_kind, connector_id = EXCLUDED.connector_id, source = EXCLUDED.source,
					external_id = EXCLUDED.external_id, occurred_at = EXCLUDED.occurred_at, received_at = EXCLUDED.received_at,
					severity = EXCLUDED.severity, kind = EXCLUDED.kind, service = EXCLUDED.service, environment = EXCLUDED.environment,
					title = EXCLUDED.title, message_scrubbed = EXCLUDED.message_scrubbed, exception_type = EXCLUDED.exception_type,
					stack = EXCLUDED.stack, attrs = EXCLUDED.attrs, fingerprint = EXCLUDED.fingerprint`, args...)
		}
	}
	if batch.Len() == 0 {
		return nil
	}
	if err := tx.SendBatch(ctx, batch).Close(); err != nil {
		return fmt.Errorf("write samples: %w", err)
	}
	return nil
}

func countRuleHits(ctx context.Context, tx pgx.Tx, groups []*aggregate.Group) error {
	hits := map[string]int64{}
	for _, g := range groups {
		if g.SuppressedBy != "" {
			hits[g.SuppressedBy] += g.Suppressed
		}
		if g.LabeledBy != "" {
			hits[g.LabeledBy] += g.Count
		}
	}
	if len(hits) == 0 {
		return nil
	}
	ids := make([]string, 0, len(hits))
	for id := range hits {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	ns := make([]int64, len(ids))
	for i, id := range ids {
		ns[i] = hits[id]
	}
	_, err := tx.Exec(ctx, `UPDATE known_issues k SET hits = k.hits + v.n, last_hit_at = now()
		FROM unnest($1::text[], $2::bigint[]) AS v(id, n) WHERE k.id::text = v.id`, ids, ns)
	if err != nil {
		return fmt.Errorf("count known-issue hits: %w", err)
	}
	return nil
}

// ensureSampleDays creates the daily sample partitions a batch needs (normally already made ahead of time
// by Maintain).
func (x *Signals) ensureSampleDays(ctx context.Context, groups []*aggregate.Group) error {
	var missing []time.Time
	x.mu.Lock()
	seen := map[string]bool{}
	for _, g := range groups {
		for _, s := range g.Samples {
			day := s.Hour.UTC().Truncate(24 * time.Hour)
			k := day.Format("2006-01-02")
			if !x.ensured[k] && !seen[k] {
				seen[k] = true
				missing = append(missing, day)
			}
		}
	}
	x.mu.Unlock()
	for _, d := range missing {
		if err := x.ensurePartitions(ctx, d, 1); err != nil {
			return err
		}
	}
	return nil
}

func (x *Signals) ensurePartitions(ctx context.Context, from time.Time, days int) error {
	if _, err := x.s.Pool.Exec(ctx, "SELECT ensure_event_sample_partitions($1::date, $2)", from.Format("2006-01-02"), days); err != nil {
		var pe *pgconn.PgError
		// Two replicas creating the same partition race on the catalog; the loser's partition exists anyway.
		if !(errors.As(err, &pe) && (pe.Code == "42P07" || pe.Code == "23505")) {
			return fmt.Errorf("create sample partitions: %w", err)
		}
	}
	x.mu.Lock()
	for i := 0; i < days; i++ {
		x.ensured[from.AddDate(0, 0, i).Format("2006-01-02")] = true
	}
	x.mu.Unlock()
	return nil
}

// Retention windows (plan § 6.4).
const (
	SampleRetention       = 30 * 24 * time.Hour
	MinuteCountsRetention = 30 * 24 * time.Hour
	HourCountsRetention   = 365 * 24 * time.Hour
)

// Maintain creates sample partitions a week ahead and drops expired data: sample partitions older than 30
// days (a cheap DROP, not a DELETE), minute counts older than 30 days, hour counts older than a year.
func (x *Signals) Maintain(ctx context.Context, now time.Time) error {
	today := now.UTC().Truncate(24 * time.Hour)
	if err := x.ensurePartitions(ctx, today.AddDate(0, 0, -1), 9); err != nil {
		return err
	}
	rows, err := x.s.Pool.Query(ctx, `SELECT c.relname::text FROM pg_inherits i JOIN pg_class c ON c.oid = i.inhrelid
		JOIN pg_class p ON p.oid = i.inhparent WHERE p.relname = 'event_samples'`)
	if err != nil {
		return err
	}
	var drop []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			rows.Close()
			return err
		}
		d, err := time.Parse("20060102", strings.TrimPrefix(name, "event_samples_"))
		if err == nil && d.Before(today.Add(-SampleRetention)) {
			drop = append(drop, name)
		}
	}
	rows.Close()
	for _, name := range drop {
		if _, err := x.s.Pool.Exec(ctx, "DROP TABLE IF EXISTS "+pgx.Identifier{name}.Sanitize()); err != nil {
			return fmt.Errorf("drop %s: %w", name, err)
		}
		x.mu.Lock()
		delete(x.ensured, strings.TrimPrefix(name, "event_samples_"))
		x.mu.Unlock()
	}
	if _, err := x.s.Q.DeleteMinuteCountsBefore(ctx, now.Add(-MinuteCountsRetention)); err != nil {
		return err
	}
	_, err = x.s.Q.DeleteHourCountsBefore(ctx, now.Add(-HourCountsRetention))
	return err
}

// ActiveRules loads the enabled, unexpired known-issue rules for the matcher.
func (x *Signals) ActiveRules(ctx context.Context) ([]knownissues.Rule, error) {
	rows, err := x.s.Q.ActiveKnownIssues(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]knownissues.Rule, 0, len(rows))
	for _, r := range rows {
		var m knownissues.Match
		if err := json.Unmarshal(r.Match, &m); err != nil {
			continue // Compile would reject it too; a corrupt row never blocks ingest
		}
		out = append(out, knownissues.Rule{ID: r.ID, Match: m, Action: knownissues.Action(r.Action), ExpiresAt: r.ExpiresAt})
	}
	return out, nil
}

// ServiceRules loads service_map entries that have attribute patterns.
func (x *Signals) ServiceRules(ctx context.Context) ([]signals.ServiceRule, error) {
	rows, err := x.s.Q.ListServiceMap(ctx)
	if err != nil {
		return nil, err
	}
	var out []signals.ServiceRule
	for _, r := range rows {
		var pats map[string]string
		if json.Unmarshal(r.SourcePatterns, &pats) != nil || len(pats) == 0 {
			continue
		}
		out = append(out, signals.ServiceRule{Service: r.ServiceName, Patterns: pats})
	}
	return out, nil
}

func nonNilFrames(f []ports.StackFrame) []ports.StackFrame {
	if f == nil {
		return []ports.StackFrame{}
	}
	return f
}

func nonNilMap(m map[string]string) map[string]string {
	if m == nil {
		return map[string]string{}
	}
	return m
}

// Resolve marks issues resolved by fingerprint (conditions that cleared, such as consumer lag); a later
// occurrence regresses them. Suppressed issues keep their status.
func (s *Signals) Resolve(ctx context.Context, fingerprints []string) (int64, error) {
	if len(fingerprints) == 0 {
		return 0, nil
	}
	n, err := s.s.Q.ResolveIssuesByFingerprint(ctx, fingerprints)
	if err != nil {
		return 0, fmt.Errorf("resolve issues: %w", err)
	}
	return n, nil
}
