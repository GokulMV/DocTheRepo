package spendguard

import (
	"context"
	"errors"
	"math/rand"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/GokulMV/DocTheRepo/internal/ports"
)

// costLedger is an in-memory ledger that sums cost per repo (empty filter: everything).
type costLedger struct {
	mu   sync.Mutex
	recs []ports.UsageRecord
}

func (l *costLedger) Spent(ctx context.Context, f ports.SpendFilter) (ports.SpendTotals, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	var t ports.SpendTotals
	for _, r := range l.recs {
		if f.RepoID == "" || r.RepoID == f.RepoID {
			t.Tokens += r.InputTokens + r.OutputTokens
			t.CostUSD += r.CostUSD
		}
	}
	return t, nil
}

func (l *costLedger) Record(ctx context.Context, r ports.UsageRecord) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.recs = append(l.recs, r)
	return nil
}

func (l *costLedger) total() float64 {
	t, _ := l.Spent(context.Background(), ports.SpendFilter{})
	return t.CostUSD
}

// docCall is a docs-sized call at $3/$15 per MTok: 20k in, 8k max out = $0.18 worst case.
var docCall = Request{Feature: "docgen", ProviderID: "prov-1", ProviderKind: "anthropic", Model: "m", RepoID: "repo-1",
	InputTokens: 20_000, OutputTokens: 8_000}

func TestBufferUSD(t *testing.T) {
	assert.InDelta(t, 0.15, BufferUSD(3, DefaultBufferPct), 1e-12, "5% of $3")
	assert.InDelta(t, 0.30, BufferUSD(3, 10), 1e-12)
	assert.InDelta(t, 0.01, BufferUSD(0.10, DefaultBufferPct), 1e-12, "at least a cent")
	assert.InDelta(t, 0.005, BufferUSD(0.01, DefaultBufferPct), 1e-12, "never more than half the cap")
	assert.Zero(t, BufferUSD(3, 0), "0 turns it off")
	assert.Zero(t, BufferUSD(0, 5), "no cap, no buffer")
}

func TestEnforcer_BufferDefaultAndOverride(t *testing.T) {
	g := guard(t, Limit{ID: "c", Scope: ScopeGlobal, Window: WindowMonth, MaxCostUSD: 3})
	l := &costLedger{recs: []ports.UsageRecord{{CostUSD: 2.70}}}
	e := NewEnforcer(g, l, nil)
	assert.Equal(t, DefaultBufferPct, e.BufferPct())

	// $2.70 + $0.18 = $2.88 is under $3 but over $3 - $0.15.
	_, err := e.Check(context.Background(), docCall)
	var sb *ports.SpendBlockedError
	require.ErrorAs(t, err, &sb)
	assert.Contains(t, sb.Reason, "less its $0.15 safety buffer")

	require.NoError(t, e.SetBufferPct(1)) // $0.03
	_, err = e.Check(context.Background(), docCall)
	require.NoError(t, err)

	assert.Error(t, e.SetBufferPct(60))
	assert.Error(t, e.SetBufferPct(-1))
	assert.Equal(t, 1.0, e.BufferPct(), "a rejected value changes nothing")
}

func TestEnforcer_ReservationsHoldCapacityUntilReleased(t *testing.T) {
	// $1 cap less $0.05 buffer: room for five $0.18 calls in flight, not six.
	g := guard(t, Limit{ID: "c", Scope: ScopeGlobal, Window: WindowMonth, MaxCostUSD: 1})
	e := NewEnforcer(g, &costLedger{}, nil)
	ctx := context.Background()
	var releases []func()
	for range 5 {
		_, release, err := e.Reserve(ctx, docCall)
		require.NoError(t, err)
		releases = append(releases, release)
	}
	assert.InDelta(t, 0.90, e.Reserved().CostUSD, 1e-9)
	_, release, err := e.Reserve(ctx, docCall)
	var sb *ports.SpendBlockedError
	require.ErrorAs(t, err, &sb, "five calls in flight hold $0.90; a sixth would pass $0.95")
	assert.Contains(t, sb.Reason, "reserved by calls in flight")
	release() // a blocked call's release is a no-op
	assert.InDelta(t, 0.90, e.Reserved().CostUSD, 1e-9)

	// A call that failed (no usage recorded) frees its whole reservation; releasing twice is harmless.
	releases[0]()
	releases[0]()
	assert.InDelta(t, 0.72, e.Reserved().CostUSD, 1e-9)
	_, release, err = e.Reserve(ctx, docCall)
	require.NoError(t, err, "the released capacity is available again")
	release()
	for _, r := range releases[1:] {
		r()
	}
	assert.Zero(t, e.Reserved().CostUSD)
}

func TestEnforcer_CheckHoldsNothing(t *testing.T) {
	e := NewEnforcer(guard(t, Limit{ID: "c", Scope: ScopeGlobal, Window: WindowMonth, MaxCostUSD: 1}), &costLedger{}, nil)
	for range 10 {
		_, err := e.Check(context.Background(), docCall)
		require.NoError(t, err)
	}
	assert.Zero(t, e.Reserved().CostUSD)
}

func TestEnforcer_ReservationsOnlyCountMatchingLimits(t *testing.T) {
	g := guard(t,
		Limit{ID: "g", Scope: ScopeGlobal, Window: WindowMonth, MaxCostUSD: 100},
		Limit{ID: "r", Scope: ScopeRepo, ScopeKey: "repo-1", Window: WindowMonth, MaxCostUSD: 0.30})
	e := NewEnforcer(g, &costLedger{}, nil)
	ctx := context.Background()
	_, rel1, err := e.Reserve(ctx, docCall)
	require.NoError(t, err)
	defer rel1()
	_, _, err = e.Reserve(ctx, docCall)
	require.Error(t, err, "repo-1's $0.30 cap (less $0.015) holds one $0.18 call in flight, not two")
	other := docCall
	other.RepoID = "repo-2"
	_, rel2, err := e.Reserve(ctx, other)
	require.NoError(t, err, "another repository's call is not held against repo-1's cap")
	rel2()
}

func TestEnforcer_ConcurrentCallsNeverExceedCap(t *testing.T) {
	// The observed failure: a $3 cap and documents written at once, each passing the check while
	// spent < cap and then all being recorded. With reservations, spent + reserved never passes cap - buffer.
	const capUSD = 3.0
	g := guard(t, Limit{ID: "c", Scope: ScopeGlobal, Window: WindowMonth, MaxCostUSD: capUSD})
	l := &costLedger{}
	e := NewEnforcer(g, l, nil)
	limit := capUSD - BufferUSD(capUSD, DefaultBufferPct)
	ctx := context.Background()
	var wg sync.WaitGroup
	var allowed, blocked atomic.Int64
	var violated atomic.Value
	for i := range 64 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			rng := rand.New(rand.NewSource(int64(i)))
			for range 4 {
				d, release, err := e.Reserve(ctx, docCall)
				if err != nil {
					var sb *ports.SpendBlockedError
					if !errors.As(err, &sb) {
						violated.Store("unexpected error: " + err.Error())
					}
					blocked.Add(1)
					continue
				}
				allowed.Add(1)
				if s := l.total() + e.Reserved().CostUSD; s > limit+1e-9 {
					violated.Store("spent + reserved passed cap - buffer")
				}
				time.Sleep(time.Duration(rng.Intn(500)) * time.Microsecond)
				// Actual cost is at most the reservation (a reply shorter than max_output_tokens).
				actual := d.EstimatedCostUSD * rng.Float64()
				assert.NoError(t, e.Record(ctx, ports.UsageRecord{Feature: "docgen", RepoID: "repo-1", CostUSD: actual}))
				release()
			}
		}()
	}
	wg.Wait()
	assert.Nil(t, violated.Load())
	assert.LessOrEqual(t, l.total(), limit+1e-9, "spent never passes the cap less its buffer")
	assert.Positive(t, allowed.Load())
	assert.Positive(t, blocked.Load(), "the cap was reached")
	assert.Zero(t, e.Reserved().CostUSD, "every reservation was released")
}

func TestEnforcer_BudgetCountsReservationsAndBlocksWithItsKey(t *testing.T) {
	g := guard(t, Limit{ID: "g", Scope: ScopeGlobal, Window: WindowMonth, MaxCostUSD: 1000})
	l := &costLedger{}
	e := NewEnforcer(g, l, nil)
	b := &Budget{Key: "repo_docs:repo-1/month", CapUSD: 3, Spent: func(ctx context.Context) (float64, error) {
		t, err := l.Spent(ctx, ports.SpendFilter{RepoID: "repo-1"})
		return t.CostUSD, err
	}}
	r := docCall
	r.Budget = b
	ctx := context.Background()
	var releases []func()
	for {
		_, release, err := e.Reserve(ctx, r)
		if err != nil {
			var sb *ports.SpendBlockedError
			require.ErrorAs(t, err, &sb)
			assert.Equal(t, "repo_docs:repo-1/month", sb.Scope)
			assert.Contains(t, sb.Reason, "reserved by calls in flight")
			break
		}
		releases = append(releases, release)
	}
	assert.Len(t, releases, 15, "$2.85 holds fifteen $0.18 calls in flight")

	// The same call without the budget is only held to the configured limits.
	_, release, err := e.Reserve(ctx, docCall)
	require.NoError(t, err)
	release()

	// An operator override lets it through with a warning.
	over := r
	over.Override = true
	d, release, err := e.Reserve(ctx, over)
	require.NoError(t, err)
	assert.Contains(t, d.Warnings[0], "budget overridden")
	release()

	// A budget whose spend cannot be read never silently allows.
	r.Budget = &Budget{Key: "k", CapUSD: 3, Spent: func(context.Context) (float64, error) { return 0, errors.New("db down") }}
	_, _, err = e.Reserve(ctx, r)
	_, transient := ports.AsTransient(err)
	assert.True(t, transient)
	for _, rel := range releases {
		rel()
	}
}
