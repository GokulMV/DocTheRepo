package llmgateway

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/GokulMV/DocTheRepo/internal/ports"
)

var q = ports.DecisionQuestion{Task: "issue_actionability", Context: "health check failed; api_key=0123456789abcdef",
	Options: []ports.DecisionOption{{ID: "actionable", Description: "fix it"}, {ID: "known_noise", Description: "ignore"}}}

func TestDecide_JSONFallbackNormalisesAndLabelsUncalibrated(t *testing.T) {
	e := newEnv(t, 1_000_000, false)
	e.routes[FeatureDecide] = Route{ProviderID: "p1", ProviderKind: "anthropic", Model: "m", MaxOutputTokens: 100}
	e.llms["p1"].steps = []step{
		ok(`{"probabilities":{"actionable":0.2}}`, 50, 5), // missing option: repaired
		ok(`{"probabilities":{"actionable":0.2,"known_noise":0.6}}`, 50, 5),
	}
	d, err := e.gw.Decide(context.Background(), CallMeta{IssueID: "i1"}, q)
	require.NoError(t, err)
	assert.Equal(t, "known_noise", d.Choice)
	assert.InDelta(t, 0.75, d.P, 1e-9, "0.6 / (0.2 + 0.6)")
	assert.InDelta(t, 0.25, d.Probabilities["actionable"], 1e-9)
	assert.False(t, d.Calibrated, "chat-model probabilities are self-reported")
	assert.Equal(t, int64(100), d.Usage.InputTokens, "both attempts counted")
	assert.NotContains(t, e.llms["p1"].reqs[0].Messages[0].Content, "0123456789abcdef", "context is scrubbed")
	assert.Contains(t, e.llms["p1"].reqs[0].Messages[0].Content, "- known_noise: ignore")
	require.Len(t, e.ledger.records, 2)
	assert.Equal(t, FeatureDecide, e.ledger.records[0].Feature)
}

type nativeLLM struct {
	fakeLLM
	got   ports.DecisionQuestion
	model string
	err   error
}

func (n *nativeLLM) Decide(_ context.Context, model string, q ports.DecisionQuestion) (ports.Decision, error) {
	n.got, n.model = q, model
	if n.err != nil {
		return ports.Decision{}, n.err
	}
	return ports.Decision{Probabilities: map[string]float64{"actionable": 0.02, "known_noise": 0.98, "bogus": 5}, Calibrated: true,
		Usage: ports.TokenUsage{InputTokens: 30, OutputTokens: 2, Reported: true}}, nil
}

type nativeProviders struct {
	*env
	n *nativeLLM
}

func (p nativeProviders) LLM(ctx context.Context, id string) (ports.LLM, error) {
	if id == "jev" {
		return p.n, nil
	}
	return p.env.LLM(ctx, id)
}

func TestDecide_NativeProviderIsCalibratedGuardedAndRecorded(t *testing.T) {
	e := newEnv(t, 1_000_000, false)
	n := &nativeLLM{}
	e.routes[FeatureDecide] = Route{ProviderID: "jev", ProviderKind: "jev", Model: "system-one"}
	e.gw.providers = nativeProviders{e, n}
	d, err := e.gw.Decide(context.Background(), CallMeta{}, q)
	require.NoError(t, err)
	assert.Equal(t, "known_noise", d.Choice)
	assert.InDelta(t, 0.98, d.P, 1e-9, "unknown options are dropped before normalising")
	assert.True(t, d.Calibrated)
	assert.Equal(t, "system-one", n.model)
	assert.NotContains(t, n.got.Context, "0123456789abcdef", "native providers get scrubbed context too")
	require.Len(t, e.ledger.records, 1)
	assert.Equal(t, int64(30), e.ledger.records[0].InputTokens)
	assert.Empty(t, e.llms["p1"].reqs, "no chat call")

	n.err = ports.Transient(errors.New("503"))
	_, err = e.gw.Decide(context.Background(), CallMeta{}, q)
	assert.Error(t, err)
	assert.Equal(t, "error", e.ledger.records[1].Outcome)
}

func TestDecide_BlockedAndInvalid(t *testing.T) {
	e := newEnv(t, 10, false) // a 10-token daily limit
	e.routes[FeatureDecide] = Route{ProviderID: "p1", ProviderKind: "anthropic", Model: "m", MaxOutputTokens: 100}
	_, err := e.gw.Decide(context.Background(), CallMeta{}, q)
	var sb *ports.SpendBlockedError
	assert.ErrorAs(t, err, &sb)
	assert.Empty(t, e.llms["p1"].reqs)

	_, err = e.gw.Decide(context.Background(), CallMeta{}, ports.DecisionQuestion{Task: "x", Options: []ports.DecisionOption{{ID: "only"}}})
	var perm *ports.PermanentError
	assert.ErrorAs(t, err, &perm)
	delete(e.routes, FeatureDecide)
	_, err = e.gw.Decide(context.Background(), CallMeta{}, q)
	assert.ErrorIs(t, err, ErrNoRoute)
}

func TestFinish_AllZeroIsUniform(t *testing.T) {
	d, _ := finish(ports.Decision{Probabilities: map[string]float64{}}, q)
	assert.Equal(t, 0.5, d.P)
	assert.Equal(t, "actionable", d.Choice, "ties go to the first option")
	assert.Equal(t, []string{"actionable", "known_noise"}, SortedOptions(d))
}
