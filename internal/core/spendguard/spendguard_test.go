package spendguard

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/GokulMV/DocTheRepo/internal/ports"
)

type ledgerMock struct{ mock.Mock }

func (m *ledgerMock) Spent(ctx context.Context, f ports.SpendFilter) (ports.SpendTotals, error) {
	a := m.Called(f.Feature, f.ProviderID, f.RepoID)
	return a.Get(0).(ports.SpendTotals), a.Error(1)
}

func (m *ledgerMock) Record(ctx context.Context, r ports.UsageRecord) error {
	return m.Called(r).Error(0)
}

type alerterMock struct{ mock.Mock }

func (m *alerterMock) Alert(ctx context.Context, a ports.SpendAlert) error {
	return m.Called(a).Error(0)
}

var (
	global  = Limit{ID: "g", Scope: ScopeGlobal, Window: WindowDay, MaxTokens: 1000}
	qa      = Limit{ID: "qa", Scope: ScopeFeature, ScopeKey: "qa", Window: WindowDay, MaxTokens: 300}
	prov    = Limit{ID: "p", Scope: ScopeProvider, ScopeKey: "prov-1", Window: WindowMonth, MaxCostUSD: 1.00}
	repo    = Limit{ID: "r", Scope: ScopeRepo, ScopeKey: "repo-1", Window: WindowDay, MaxTokens: 500, Alert: true, AlertURL: "https://hooks"}
	pricing = map[string]Price{PriceKey("anthropic", "m"): {InputPerMTok: 3, OutputPerMTok: 15, EmbedPerMTok: 0.1}}
)

func guard(t *testing.T, limits ...Limit) *Guard {
	t.Helper()
	g, err := New(limits, pricing, false)
	require.NoError(t, err)
	return g
}

func TestNew_Validation(t *testing.T) {
	_, err := New(nil, nil, false)
	assert.ErrorIs(t, err, ErrUnlimitedNotAcknowledged)
	_, err = New([]Limit{{ID: "x", Scope: ScopeGlobal, Window: WindowDay}}, nil, false)
	assert.ErrorIs(t, err, ErrUnlimitedNotAcknowledged, "a zero ceiling is unlimited and needs acknowledgement")
	g, err := New([]Limit{{ID: "x", Scope: ScopeGlobal, Window: WindowDay}}, nil, true)
	require.NoError(t, err)
	assert.True(t, g.Evaluate(Request{InputTokens: 1e9}, nil).Allow, "acknowledged unlimited allows anything")
	_, err = New([]Limit{{ID: "x", Scope: ScopeFeature, Window: WindowDay, MaxTokens: 1}}, nil, false)
	assert.ErrorContains(t, err, "scope key")
	_, err = New([]Limit{{ID: "x", Scope: ScopeGlobal, Window: "week", MaxTokens: 1}}, nil, false)
	assert.ErrorContains(t, err, "window")
	_, err = New([]Limit{{ID: "x", Scope: ScopeGlobal, Window: WindowDay, MaxTokens: -1}}, nil, false)
	assert.ErrorContains(t, err, ">= 0")
}

func TestApplicable_MatchesScopes(t *testing.T) {
	g := guard(t, global, qa, prov, repo)
	ids := func(ls []Limit) []string {
		var o []string
		for _, l := range ls {
			o = append(o, l.ID)
		}
		return o
	}
	assert.Equal(t, []string{"g", "qa", "p", "r"}, ids(g.Applicable(Request{Feature: "qa", ProviderID: "prov-1", RepoID: "repo-1"})))
	assert.Equal(t, []string{"g"}, ids(g.Applicable(Request{Feature: "docgen", ProviderID: "other"})))
	assert.Equal(t, []string{"g", "p"}, ids(g.Applicable(Request{Feature: "decode", ProviderID: "prov-1"})), "repo limits need a repo")
}

func TestEvaluate_Decisions(t *testing.T) {
	g := guard(t, global, qa, prov)
	cases := []struct {
		name  string
		req   Request
		spent map[string]ports.SpendTotals
		allow bool
		limit string
		why   string
	}{
		{"within all", Request{Feature: "qa", InputTokens: 100, OutputTokens: 50}, map[string]ports.SpendTotals{"g": {Tokens: 100}, "qa": {Tokens: 100}}, true, "", ""},
		{"single call above feature ceiling", Request{Feature: "qa", InputTokens: 301}, nil, false, "qa", "alone"},
		{"cumulative feature breach", Request{Feature: "qa", InputTokens: 100}, map[string]ports.SpendTotals{"qa": {Tokens: 250}}, false, "qa", "250 tokens spent"},
		{"global breach", Request{Feature: "docgen", InputTokens: 100}, map[string]ports.SpendTotals{"g": {Tokens: 950}}, false, "g", "global/day"},
		{"exact ceiling allowed", Request{Feature: "docgen", InputTokens: 50}, map[string]ports.SpendTotals{"g": {Tokens: 950}}, true, "", ""},
		{"cost breach", Request{Feature: "decode", ProviderID: "prov-1", ProviderKind: "anthropic", Model: "m", InputTokens: 100, OutputTokens: 100},
			map[string]ports.SpendTotals{"p": {CostUSD: 0.999}}, false, "p", "$"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			d := g.Evaluate(c.req, c.spent)
			assert.Equal(t, c.allow, d.Allow, d.Reason)
			if !c.allow {
				require.NotNil(t, d.Limit)
				assert.Equal(t, c.limit, d.Limit.ID)
				assert.Contains(t, d.Reason, c.why)
			}
		})
	}
}

func TestEvaluate_UtilizationCostAndWarnings(t *testing.T) {
	g := guard(t, global, prov)
	d := g.Evaluate(Request{Feature: "qa", ProviderID: "prov-1", ProviderKind: "anthropic", Model: "m", InputTokens: 1000 - 800, OutputTokens: 0},
		map[string]ports.SpendTotals{"g": {Tokens: 600}})
	assert.InDelta(t, 0.8, d.Utilization["g"], 1e-9)
	assert.InDelta(t, 200*3/1e6, d.EstimatedCostUSD, 1e-12)

	d = g.Evaluate(Request{Feature: "qa", ProviderID: "prov-1", ProviderKind: "openai", Model: "unpriced", InputTokens: 1}, nil)
	assert.True(t, d.Allow)
	require.Len(t, d.Warnings, 1)
	assert.Contains(t, d.Warnings[0], "no price")

	c, ok := g.Cost("anthropic", "m", "embedding", 1_000_000, 0)
	assert.True(t, ok)
	assert.InDelta(t, 0.1, c, 1e-12)
}

func TestEvaluate_OverrideAllowsButWarns(t *testing.T) {
	g := guard(t, global)
	d := g.Evaluate(Request{InputTokens: 5000, Override: true}, nil)
	assert.True(t, d.Allow)
	assert.Contains(t, d.Warnings[0], "overridden")
}

func TestEnforcer_CheckBlocksAndAlerts(t *testing.T) {
	g := guard(t, global, repo)
	l := &ledgerMock{}
	l.On("Spent", "", "", "").Return(ports.SpendTotals{Tokens: 10}, nil)
	l.On("Spent", "", "", "repo-1").Return(ports.SpendTotals{Tokens: 490}, nil)
	al := &alerterMock{}
	al.On("Alert", mock.MatchedBy(func(a ports.SpendAlert) bool {
		return a.LimitID == "r" && a.URL == "https://hooks" && a.SpentTokens == 490 && a.EstimatedTokens == 20
	})).Return(errors.New("webhook down")) // alert failure must not change the outcome
	e := NewEnforcer(g, l, al)
	var observed []Decision
	e.OnDecision = func(_ Request, d Decision) { observed = append(observed, d) }

	d, err := e.Check(context.Background(), Request{Feature: "docgen", RepoID: "repo-1", InputTokens: 15, OutputTokens: 5})
	var sb *ports.SpendBlockedError
	require.ErrorAs(t, err, &sb)
	assert.Equal(t, "repo:repo-1/day", sb.Scope)
	assert.Equal(t, int64(20), sb.EstimatedTokens)
	assert.False(t, d.Allow)
	assert.Len(t, observed, 1)
	al.AssertExpectations(t)
	l.AssertNotCalled(t, "Record", mock.Anything)
}

func TestEnforcer_CheckAllows_LedgerErrorIsTransient(t *testing.T) {
	l := &ledgerMock{}
	l.On("Spent", "", "", "").Return(ports.SpendTotals{}, nil).Once()
	e := NewEnforcer(guard(t, global), l, nil)
	d, err := e.Check(context.Background(), Request{InputTokens: 10})
	require.NoError(t, err)
	assert.True(t, d.Allow)

	l.On("Spent", "", "", "").Return(ports.SpendTotals{}, errors.New("db down"))
	_, err = e.Check(context.Background(), Request{InputTokens: 10})
	_, transient := ports.AsTransient(err)
	assert.True(t, transient, "an unreadable ledger never silently allows spending")
}

func TestEnforcer_RecordPricesAndDefaults(t *testing.T) {
	l := &ledgerMock{}
	now := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	l.On("Record", mock.MatchedBy(func(r ports.UsageRecord) bool {
		return r.At.Equal(now) && r.Outcome == "ok" && r.CostUSD > 0
	})).Return(nil)
	e := NewEnforcer(guard(t, global), l, nil)
	e.now = func() time.Time { return now }
	require.NoError(t, e.Record(context.Background(), ports.UsageRecord{Feature: "qa", ProviderKind: "anthropic", Model: "m", InputTokens: 1000}))
	l.AssertExpectations(t)

	l2 := &ledgerMock{}
	l2.On("Record", mock.Anything).Return(errors.New("x"))
	assert.Error(t, NewEnforcer(guard(t, global), l2, nil).Record(context.Background(), ports.UsageRecord{}))
}

func TestEnforcer_SetGuardSwapsLimits(t *testing.T) {
	l := &ledgerMock{}
	l.On("Spent", mock.Anything, mock.Anything, mock.Anything).Return(ports.SpendTotals{}, nil)
	e := NewEnforcer(guard(t, global), l, nil)
	_, err := e.Check(context.Background(), Request{InputTokens: 2000})
	require.Error(t, err)
	e.SetGuard(guard(t, Limit{ID: "big", Scope: ScopeGlobal, Window: WindowDay, MaxTokens: 1e6}))
	_, err = e.Check(context.Background(), Request{InputTokens: 2000})
	require.NoError(t, err)
}

func TestFilterAndHelpers(t *testing.T) {
	now := time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC)
	f := Filter(repo, now)
	assert.Equal(t, "repo-1", f.RepoID)
	assert.Equal(t, now.Add(-24*time.Hour), f.Since)
	assert.Equal(t, now.Add(-30*24*time.Hour), Filter(prov, now).Since)
	assert.Equal(t, "qa", Filter(qa, now).Feature)
	assert.Equal(t, "prov-1", Filter(prov, now).ProviderID)
	assert.Equal(t, int64(3), EstimateTokens("abcd", "efgh", "i"))
	s, err := ParseScope("Feature")
	require.NoError(t, err)
	assert.Equal(t, ScopeFeature, s)
	_, err = ParseScope("team")
	assert.Error(t, err)
}

func TestCostUsagePricesCacheTokens(t *testing.T) {
	rd, wr := 0.4, 5.0
	g, err := New(nil, map[string]Price{
		PriceKey("anthropic", "m"): {InputPerMTok: 4, OutputPerMTok: 20, CacheReadPerMTok: &rd, CacheWritePerMTok: &wr},
		PriceKey("openai", "m"):    {InputPerMTok: 4, OutputPerMTok: 20},
	}, true)
	require.NoError(t, err)
	// 1M input of which 600k read from cache and 100k written: 300k plain.
	c, ok := g.CostUsage("anthropic", "m", "qa", 1_000_000, 100_000, 600_000, 100_000)
	require.True(t, ok)
	assert.InDelta(t, 0.3*4+0.6*0.4+0.1*5+0.1*20, c, 1e-9)
	// No cache prices: cache tokens are plain input.
	c, _ = g.CostUsage("openai", "m", "qa", 1_000_000, 0, 600_000, 0)
	assert.InDelta(t, 4.0, c, 1e-9)
	// No cache tokens: the same as Cost.
	c1, _ := g.CostUsage("anthropic", "m", "qa", 1000, 500, 0, 0)
	c2, _ := g.Cost("anthropic", "m", "qa", 1000, 500)
	assert.Equal(t, c2, c1)
	_, ok = g.CostUsage("x", "y", "qa", 1, 1, 1, 0)
	assert.False(t, ok)
}
