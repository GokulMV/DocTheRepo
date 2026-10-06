package store

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/GokulMV/DocTheRepo/internal/core/rag"
	"github.com/GokulMV/DocTheRepo/internal/store/gen"
)

// UsagePoint is one bucket of a usage series.
type UsagePoint struct {
	T       time.Time `json:"t"`
	Calls   int64     `json:"calls"`
	Tokens  int64     `json:"tokens"`
	CostUSD float64   `json:"cost_usd"`
	// CacheReadTokens is the part of Tokens read from the provider's prompt cache (billed far lower).
	CacheReadTokens int64 `json:"cache_read_tokens"`
	CachedCalls     int64 `json:"cached_calls"`
	Blocked         int64 `json:"blocked"`
}

// UsageSeries is one group's series.
type UsageSeries struct {
	Key    string       `json:"key"`
	Points []UsagePoint `json:"points"`
}

// UsageReport is the analytics usage response.
type UsageReport struct {
	Series []UsageSeries `json:"series"`
	Totals UsagePoint    `json:"totals"`
}

// Usage aggregates usage_events (userID limits it to one user's own calls).
func (b *Browse) Usage(ctx context.Context, groupBy, granularity string, from, to time.Time, userID string) (UsageReport, error) {
	switch groupBy {
	case "feature", "provider", "model", "repo", "user", "":
	default:
		return UsageReport{}, fmt.Errorf("group_by must be feature, provider, model, repo or user")
	}
	if granularity != "hour" && granularity != "day" {
		return UsageReport{}, fmt.Errorf("granularity must be hour or day")
	}
	rows, err := b.s.Q.UsageSeries(ctx, gen.UsageSeriesParams{Granularity: granularity, GroupBy: groupBy, FromT: from, ToT: to, UserID: strPtr(userID)})
	if err != nil {
		return UsageReport{}, err
	}
	rep := UsageReport{Series: []UsageSeries{}}
	idx := map[string]int{}
	for _, r := range rows {
		i, ok := idx[r.Key]
		if !ok {
			i = len(rep.Series)
			idx[r.Key] = i
			rep.Series = append(rep.Series, UsageSeries{Key: r.Key})
		}
		p := UsagePoint{T: r.T, Calls: r.Calls, Tokens: r.Tokens, CostUSD: r.CostUsd, CacheReadTokens: r.CacheReadTokens, CachedCalls: r.CachedCalls, Blocked: r.Blocked}
		rep.Series[i].Points = append(rep.Series[i].Points, p)
		rep.Totals.Calls += p.Calls
		rep.Totals.Tokens += p.Tokens
		rep.Totals.CostUSD += p.CostUSD
		rep.Totals.CacheReadTokens += p.CacheReadTokens
		rep.Totals.CachedCalls += p.CachedCalls
		rep.Totals.Blocked += p.Blocked
	}
	return rep, nil
}

// SavingsKind is one savings category.
type SavingsKind struct {
	Kind           string  `json:"kind"`
	Events         int64   `json:"events"`
	TokensAvoided  int64   `json:"tokens_avoided"`
	CostAvoidedUSD float64 `json:"cost_avoided_usd"`
}

// Savings summarises avoided usage.
func (b *Browse) Savings(ctx context.Context, from, to time.Time) ([]SavingsKind, SavingsKind, error) {
	rows, err := b.s.Q.SavingsByKind(ctx, gen.SavingsByKindParams{FromT: from, ToT: to})
	if err != nil {
		return nil, SavingsKind{}, err
	}
	out := make([]SavingsKind, len(rows))
	total := SavingsKind{Kind: "total"}
	for i, r := range rows {
		out[i] = SavingsKind{Kind: r.Kind, Events: r.Events, TokensAvoided: r.TokensAvoided, CostAvoidedUSD: r.CostAvoidedUsd}
		total.Events += r.Events
		total.TokensAvoided += r.TokensAvoided
		total.CostAvoidedUSD += r.CostAvoidedUsd
	}
	return out, total, nil
}

// PipelineReport is the pipeline health response.
type PipelineReport struct {
	Jobs            []map[string]any `json:"jobs"`
	TriageAbortRate float64          `json:"triage_abort_rate"`
	Freshness       []map[string]any `json:"freshness"`
}

// Pipeline reports job counts and latency, the triage abort rate, and per-repo index freshness.
func (b *Browse) Pipeline(ctx context.Context, from, to time.Time, sc rag.Scope) (PipelineReport, error) {
	rows, err := b.s.Q.PipelineStats(ctx, gen.PipelineStatsParams{FromT: from, ToT: to})
	if err != nil {
		return PipelineReport{}, err
	}
	rep := PipelineReport{Jobs: []map[string]any{}, Freshness: []map[string]any{}}
	var pushes, aborted int64
	for _, r := range rows {
		rep.Jobs = append(rep.Jobs, map[string]any{"type": r.Type, "status": r.Status, "jobs": r.Jobs, "p50_seconds": r.P50Seconds, "p95_seconds": r.P95Seconds})
		if r.Type == "code_push" && r.Status != "queued" && r.Status != "processing" {
			pushes += r.Jobs
			if r.Status == "aborted" {
				aborted += r.Jobs
			}
		}
	}
	if pushes > 0 {
		rep.TriageAbortRate = float64(aborted) / float64(pushes)
	}
	fr, err := b.s.Q.RepoFreshness(ctx)
	if err != nil {
		return rep, err
	}
	for _, r := range fr {
		if !sc.All && !contains(sc.RepoIDs, r.ID) {
			continue
		}
		rep.Freshness = append(rep.Freshness, map[string]any{"repo_id": r.ID, "repo": r.FullName, "last_processed_sha": r.LastProcessedSha, "last_success": r.LastSuccess})
	}
	return rep, nil
}

// ConnectorHealth lists connector health and activity.
func (b *Browse) ConnectorHealth(ctx context.Context) ([]map[string]any, error) {
	rows, err := b.s.Q.ConnectorStats(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]map[string]any, len(rows))
	for i, r := range rows {
		out[i] = map[string]any{"id": r.ID, "type": r.Type, "name": r.Name, "health": r.Health, "last_sync_at": r.LastSyncAt,
			"last_error": r.LastError, "jobs_last_day": r.JobsDay}
	}
	return out, nil
}

// Activity is one feed entry.
type Activity struct {
	Kind   string    `json:"kind"` // job | pr | audit
	RefID  string    `json:"ref_id"`
	Action string    `json:"action"`
	RepoID *string   `json:"repo_id,omitempty"`
	Detail string    `json:"detail,omitempty"`
	At     time.Time `json:"at"`
}

// Activity merges finished jobs, docs PR changes, and (for admins) audit entries, newest first.
func (b *Browse) Activity(ctx context.Context, kinds map[string]bool, since, before time.Time, sc rag.Scope, includeAudit bool, limit int) ([]Activity, error) {
	want := func(k string) bool { return len(kinds) == 0 || kinds[k] }
	var out []Activity
	if want("job") {
		rows, err := b.s.Q.ActivityJobs(ctx, gen.ActivityJobsParams{Since: since, Before: before, AllRepos: sc.All, RepoIds: ids(sc), Lim: int32(limit)})
		if err != nil {
			return nil, err
		}
		for _, r := range rows {
			out = append(out, Activity{Kind: "job", RefID: r.ID, Action: r.Type + ":" + r.Status, RepoID: r.RepoID, Detail: r.Error, At: r.UpdatedAt})
		}
	}
	if want("pr") {
		rows, err := b.s.Q.ActivityPRs(ctx, gen.ActivityPRsParams{Since: since, Before: before, AllRepos: sc.All, RepoIds: ids(sc), Lim: int32(limit)})
		if err != nil {
			return nil, err
		}
		for _, r := range rows {
			rid := r.RepoID
			out = append(out, Activity{Kind: "pr", RefID: fmt.Sprintf("%s#%d", r.RepoID, r.Number), Action: "docs_pr:" + r.State, RepoID: &rid, Detail: r.Url, At: r.UpdatedAt})
		}
	}
	if want("audit") && includeAudit {
		rows, err := b.s.Q.ActivityAudit(ctx, gen.ActivityAuditParams{Since: since, Before: before, Lim: int32(limit)})
		if err != nil {
			return nil, err
		}
		for _, r := range rows {
			out = append(out, Activity{Kind: "audit", RefID: r.ID, Action: r.Action, Detail: r.TargetType + ":" + r.TargetID, At: r.At})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].At.After(out[j].At) })
	if len(out) > limit {
		out = out[:limit]
	}
	if out == nil {
		out = []Activity{}
	}
	return out, nil
}

// SiftReport is what Ask's source picker did over a period (Analytics → Ask source picking).
type SiftReport struct {
	Answers     int64     `json:"answers"`      // Ask answers in the period
	Picked      int64     `json:"picked"`       // answers whose sources the picker judged
	Trimmed     int64     `json:"trimmed"`      // answers that read fewer sources than retrieved
	Candidates  int64     `json:"candidates"`   // sources retrieved, over picked answers
	Kept        int64     `json:"kept"`         // sources sent to the answering model
	TokensSaved int64     `json:"tokens_saved"` // answering-model input tokens not sent
	JudgeTokens int64     `json:"judge_tokens"`
	JudgeCost   float64   `json:"judge_cost_usd"`
	SavedUSD    float64   `json:"saved_usd"` // net: answering-model cost avoided minus the judge's cost
	Explored    int64     `json:"explored"`  // files read while exploring the index
	Daily       []SiftDay `json:"daily"`
}

// SiftDay is one day of the source picker's savings.
type SiftDay struct {
	Day         time.Time `json:"day"`
	Picked      int64     `json:"picked"`
	TokensSaved int64     `json:"tokens_saved"`
	SavedUSD    float64   `json:"saved_usd"`
}

// Sift summarises the source picker from the summaries stored with Ask answers.
func (b *Browse) Sift(ctx context.Context, from, to time.Time) (SiftReport, error) {
	t, err := b.s.Q.SiftTotals(ctx, gen.SiftTotalsParams{FromT: from, ToT: to})
	if err != nil {
		return SiftReport{}, err
	}
	rep := SiftReport{Answers: t.Answers, Picked: t.Picked, Trimmed: t.Trimmed, Candidates: t.Candidates, Kept: t.Kept,
		TokensSaved: t.TokensSaved, JudgeTokens: t.JudgeTokens, JudgeCost: t.JudgeCostUsd, SavedUSD: t.SavedUsd, Explored: t.Explored}
	rows, err := b.s.Q.SiftDaily(ctx, gen.SiftDailyParams{FromT: from, ToT: to})
	if err != nil {
		return SiftReport{}, err
	}
	rep.Daily = make([]SiftDay, len(rows))
	for i, r := range rows {
		rep.Daily[i] = SiftDay{Day: r.Day, Picked: r.Picked, TokensSaved: r.TokensSaved, SavedUSD: r.SavedUsd}
	}
	return rep, nil
}

// notSignalTypes are the connector types that do not feed the Issues pages: code and team documents.
const notSignalTypes = `'github', 'gitlab', 'confluence', 'jira', 'notion', 'upload'`

// HasIssues reports whether the Issues pages apply: an alert source is connected, or an issue exists.
func (b *Browse) HasIssues(ctx context.Context) bool {
	var ok bool
	err := b.s.Pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM connectors WHERE type::text NOT IN (`+notSignalTypes+`))
		OR EXISTS (SELECT 1 FROM issues)`).Scan(&ok)
	return err != nil || ok // when unsure, show them
}
