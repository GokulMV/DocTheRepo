package store_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/GokulMV/DocTheRepo/internal/core/aggregate"
	"github.com/GokulMV/DocTheRepo/internal/core/knownissues"
	"github.com/GokulMV/DocTheRepo/internal/ports"
	"github.com/GokulMV/DocTheRepo/internal/store"
	"github.com/GokulMV/DocTheRepo/internal/store/gen"
)

func group(fp string, count, suppressed int64, at time.Time, samples ...aggregate.Sample) *aggregate.Group {
	g := &aggregate.Group{Fingerprint: fp, Kind: ports.KindError, Source: "sentry", Sources: map[string]bool{"sentry": true}, Service: "checkout",
		Environment: "prod", Title: "TimeoutError: boom", SeverityMax: ports.SeverityError, FirstSeen: at, LastSeen: at, Count: count,
		Suppressed: suppressed, Minutes: map[time.Time]*aggregate.Minute{at.Truncate(time.Minute): {Count: count, Suppressed: suppressed}}, Samples: samples}
	return g
}

func sample(kind aggregate.SampleKind, slot int, at time.Time, msg string) aggregate.Sample {
	return aggregate.Sample{Kind: kind, Slot: slot, Hour: at.Truncate(time.Hour), Event: ports.SignalEvent{Source: "sentry", ExternalID: fmt.Sprint(slot, msg),
		OccurredAt: at, ReceivedAt: at, Severity: ports.SeverityError, Kind: ports.KindError, Service: "checkout", Title: "boom", Message: msg,
		Stack: []ports.StackFrame{{Module: "pay", Function: "charge", InApp: true}}, Attrs: map[string]string{"k": "v"}, Fingerprint: "fp"}}
}

func TestSignalsFlush(t *testing.T) {
	ctx := context.Background()
	st, _, _, repoID := fixture(t)
	_, err := st.Pool.Exec(ctx, `INSERT INTO service_map (service_name, repo_id) VALUES ('checkout', $1)`, repoID)
	require.NoError(t, err)
	ki := store.NewKnownIssues(st)
	ruleID, err := ki.Create(ctx, store.KnownIssue{Title: "flaky upstream", Reason: "third_party", Match: knownissues.Match{Services: []string{"cart"}}})
	require.NoError(t, err)
	sig := store.NewSignals(st)
	var refs []store.IssueRef
	sig.OnIssues = func(_ context.Context, r []store.IssueRef) { refs = append(refs, r...) }
	now := time.Now().UTC().Truncate(time.Second)

	// Window 1: a new issue with samples, and an issue that is only ever suppressed.
	a := group("fp-a", 3, 0, now, sample(aggregate.SampleFirst, -1, now, "first one"), sample(aggregate.SampleFirst, -2, now, "second"))
	b := group("fp-b", 0, 2, now)
	b.Service, b.SuppressedBy = "cart", ruleID
	require.NoError(t, sig.Flush(ctx, aggregate.Batch{Groups: []*aggregate.Group{b, a}}))
	ia, err := st.Q.GetIssueByFingerprint(ctx, "fp-a")
	require.NoError(t, err)
	assert.Equal(t, int64(3), ia.Occurrences)
	assert.Equal(t, "new", string(ia.Status))
	require.NotNil(t, ia.RepoID)
	assert.Equal(t, repoID, *ia.RepoID, "mapped to the service's repository (issue ACL)")
	ib, err := st.Q.GetIssueByFingerprint(ctx, "fp-b")
	require.NoError(t, err)
	assert.Equal(t, "suppressed", string(ib.Status))
	assert.Equal(t, int64(2), ib.SuppressedCount)
	require.NotNil(t, ib.KnownIssueID)
	assert.Equal(t, ruleID, *ib.KnownIssueID)
	require.Len(t, refs, 1, "only the actionable new issue is reported for decoding")
	assert.Equal(t, store.IssueRef{ID: ia.ID, Fingerprint: "fp-a", Status: "new", Service: "checkout", Kind: ports.KindError}, refs[0])
	rule, err := ki.Get(ctx, ruleID)
	require.NoError(t, err)
	assert.Equal(t, int64(2), rule.Hits)
	assert.NotNil(t, rule.LastHitAt)
	var saved int64
	require.NoError(t, st.Pool.QueryRow(ctx, `SELECT est_tokens_avoided FROM savings_events WHERE kind = 'known_issue_suppressed' AND ref_id = $1`, ib.ID).Scan(&saved))
	assert.Equal(t, int64(store.DefaultDecodeTokens), saved)
	samples, err := st.Q.IssueSamples(ctx, gen.IssueSamplesParams{IssueID: ia.ID, Lim: 50})
	require.NoError(t, err)
	assert.Len(t, samples, 2)
	mins, err := st.Q.IssueMinuteCounts(ctx, gen.IssueMinuteCountsParams{IssueID: ia.ID, Since: now.Add(-time.Hour)})
	require.NoError(t, err)
	require.Len(t, mins, 1)
	assert.Equal(t, int64(3), mins[0].Count)

	// Window 2: counts accumulate; severity and sources merge; first samples stay capped at five across replicas.
	a2 := group("fp-a", 2, 0, now.Add(2*time.Minute))
	a2.SeverityMax, a2.Sources = ports.SeverityCritical, map[string]bool{"datadog": true}
	for i := 1; i <= 5; i++ {
		a2.Samples = append(a2.Samples, sample(aggregate.SampleFirst, -i, now.Add(2*time.Minute), fmt.Sprint("replica2-", i)))
	}
	refs = nil
	require.NoError(t, sig.Flush(ctx, aggregate.Batch{Groups: []*aggregate.Group{a2}}))
	ia, _ = st.Q.GetIssueByFingerprint(ctx, "fp-a")
	assert.Equal(t, int64(5), ia.Occurrences)
	assert.Equal(t, "critical", string(ia.SeverityMax))
	assert.Equal(t, []string{"datadog", "sentry"}, ia.Sources)
	assert.Equal(t, now.Add(2*time.Minute), ia.LastSeen.UTC())
	assert.Equal(t, now, ia.FirstSeen.UTC())
	assert.Empty(t, refs, "an existing issue is not re-reported")
	var firsts int
	require.NoError(t, st.Pool.QueryRow(ctx, `SELECT count(*) FROM event_samples WHERE issue_id = $1 AND sample_kind = 'first'`, ia.ID).Scan(&firsts))
	assert.Equal(t, aggregate.FirstSamples, firsts)

	// Reservoir slots are overwritten, not appended.
	for _, msg := range []string{"early", "late"} {
		require.NoError(t, sig.Flush(ctx, aggregate.Batch{Groups: []*aggregate.Group{group("fp-a", 1, 0, now, sample(aggregate.SampleReservoir, 0, now, msg))}}))
	}
	var resMsg string
	var resCount int
	require.NoError(t, st.Pool.QueryRow(ctx, `SELECT count(*), max(message_scrubbed) FROM event_samples WHERE issue_id = $1 AND sample_kind = 'reservoir'`, ia.ID).Scan(&resCount, &resMsg))
	assert.Equal(t, 1, resCount)
	assert.Equal(t, "late", resMsg)

	// Resolved issues regress on a new occurrence; a suppressed-only window does not reopen them.
	_, err = st.Pool.Exec(ctx, `UPDATE issues SET status = 'resolved', resolved_at = now() WHERE id = $1`, ia.ID)
	require.NoError(t, err)
	onlySuppressed := group("fp-a", 0, 1, now)
	onlySuppressed.SuppressedBy = ruleID
	require.NoError(t, sig.Flush(ctx, aggregate.Batch{Groups: []*aggregate.Group{onlySuppressed}}))
	ia, _ = st.Q.GetIssueByFingerprint(ctx, "fp-a")
	assert.Equal(t, "resolved", string(ia.Status))
	require.NoError(t, sig.Flush(ctx, aggregate.Batch{Groups: []*aggregate.Group{group("fp-a", 1, 0, now)}}))
	ia, _ = st.Q.GetIssueByFingerprint(ctx, "fp-a")
	assert.Equal(t, "regressed", string(ia.Status))
	assert.Nil(t, ia.ResolvedAt)
	require.Len(t, refs, 1)
	assert.Equal(t, "regressed", refs[0].Status)

	// A rule deleted while events were in flight: the flush still succeeds, without the link.
	require.NoError(t, ki.Delete(ctx, ruleID))
	c := group("fp-c", 0, 1, now)
	c.SuppressedBy = ruleID
	require.NoError(t, sig.Flush(ctx, aggregate.Batch{Groups: []*aggregate.Group{c}}))
	ic, _ := st.Q.GetIssueByFingerprint(ctx, "fp-c")
	assert.Nil(t, ic.KnownIssueID)
	assert.Equal(t, "suppressed", string(ic.Status))
}

func TestSignalsMaintainAndRules(t *testing.T) {
	ctx := context.Background()
	st, _, _, repoID := fixture(t)
	sig := store.NewSignals(st)
	now := time.Now().UTC()
	old := now.AddDate(0, 0, -40)
	require.NoError(t, sig.Flush(ctx, aggregate.Batch{Groups: []*aggregate.Group{group("fp-old", 1, 0, old, sample(aggregate.SampleReservoir, 0, old, "ancient"))}}))
	require.NoError(t, sig.Maintain(ctx, now))
	var n int
	require.NoError(t, st.Pool.QueryRow(ctx, `SELECT count(*) FROM event_samples`).Scan(&n))
	assert.Zero(t, n, "the 40-day-old partition was dropped")
	require.NoError(t, st.Pool.QueryRow(ctx, `SELECT count(*) FROM issue_counts_minutely`).Scan(&n))
	assert.Zero(t, n)
	require.NoError(t, st.Pool.QueryRow(ctx, `SELECT count(*) FROM issue_counts_hourly`).Scan(&n))
	assert.Equal(t, 1, n, "hourly counts are kept for a year")
	require.NoError(t, st.Pool.QueryRow(ctx, `SELECT count(*) FROM pg_inherits i JOIN pg_class p ON p.oid = i.inhparent WHERE p.relname = 'event_samples'`).Scan(&n))
	assert.GreaterOrEqual(t, n, 9, "partitions exist a week ahead")

	ki := store.NewKnownIssues(st)
	_, err := ki.Create(ctx, store.KnownIssue{Title: "x", Reason: "nope", Match: knownissues.Match{Services: []string{"a"}}})
	assert.Error(t, err)
	_, err = ki.Create(ctx, store.KnownIssue{Title: "everything", Reason: "expected_noise"})
	var ve *ports.ValidationError
	assert.ErrorAs(t, err, &ve, "an empty match would hide everything")
	past := now.Add(-time.Hour)
	id1, err := ki.Create(ctx, store.KnownIssue{Title: "active", Reason: "known_bug", Match: knownissues.Match{MessageRegex: "timeout"}, Enabled: true,
		SourceText: "creds postgres://u:hunter2@db/x"})
	require.NoError(t, err)
	_, err = ki.Create(ctx, store.KnownIssue{Title: "expired", Reason: "known_bug", Match: knownissues.Match{Services: []string{"a"}}, Enabled: true, ExpiresAt: &past})
	require.NoError(t, err)
	_, err = ki.Create(ctx, store.KnownIssue{Title: "suggested", Reason: "expected_noise", Match: knownissues.Match{Services: []string{"b"}}, Enabled: true, Source: "suggested"})
	require.NoError(t, err)
	rules, err := sig.ActiveRules(ctx)
	require.NoError(t, err)
	require.Len(t, rules, 1, "expired and not-yet-approved suggested rules are inactive")
	assert.Equal(t, id1, rules[0].ID)
	got, err := ki.Get(ctx, id1)
	require.NoError(t, err)
	assert.NotContains(t, got.SourceText, "hunter2")
	got.Enabled = false
	require.NoError(t, ki.Update(ctx, got))
	rules, _ = sig.ActiveRules(ctx)
	assert.Empty(t, rules)
	all, err := ki.List(ctx, 10)
	require.NoError(t, err)
	assert.Len(t, all, 3)
	assert.ErrorIs(t, ki.Delete(ctx, ports.NewID()), ports.ErrNotFound)

	_, err = st.Pool.Exec(ctx, `INSERT INTO service_map (service_name, repo_id, source_patterns) VALUES ('orders', $1, '{"log_group": "/aws/lambda/orders-*"}'), ('plain', $1, '{}')`, repoID)
	require.NoError(t, err)
	sr, err := sig.ServiceRules(ctx)
	require.NoError(t, err)
	require.Len(t, sr, 1)
	assert.Equal(t, "orders", sr[0].Service)
}
