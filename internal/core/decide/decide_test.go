package decide

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/GokulMV/DocTheRepo/internal/ports"
)

func dec(pNoise float64, calibrated bool) ports.Decision {
	d := ports.Decision{Probabilities: map[string]float64{Actionable: 1 - pNoise, KnownNoise: pNoise}, Calibrated: calibrated,
		Usage: ports.TokenUsage{InputTokens: 90, OutputTokens: 10}}
	d.Choice, d.P = Actionable, 1-pNoise
	if pNoise > 0.5 {
		d.Choice, d.P = KnownNoise, pNoise
	}
	return d
}

func TestEvaluateMetrics(t *testing.T) {
	items := []Labelled{
		{Want: KnownNoise, Decision: dec(0.95, true)}, // gated, right
		{Want: Actionable, Decision: dec(0.92, true)}, // gated, wrong: a defect the gate would skip
		{Want: Actionable, Decision: dec(0.05, true)}, // gated (actionable, p=0.95), right: not skipped
		{Want: KnownNoise, Decision: dec(0.6, true)},  // below the gate, right
	}
	r := Evaluate(TaskIssueActionability, items, 0.9, KnownNoise)
	assert.Equal(t, 4, r.N)
	assert.Equal(t, 0.75, r.Accuracy)
	assert.Equal(t, 0.75, r.Coverage)
	assert.InDelta(t, 2.0/3, r.GatedAccuracy, 1e-9)
	assert.Equal(t, 0.5, r.NoiseSkipRate)
	assert.Equal(t, 1, r.MissedDefects)
	assert.Equal(t, 100.0, r.TokensPerItem)
	assert.True(t, r.Calibrated)
	// Bins: [0.6,0.7) holds one right answer at 0.6; [0.9,1.0] holds three at mean 0.94 with 2/3 right.
	assert.Len(t, r.Reliability, 2)
	assert.InDelta(t, 0.25*0.4+0.75*(0.94-2.0/3), r.ECE, 1e-9)
	assert.Greater(t, r.Brier, 0.0)

	items[0].Decision.Calibrated = false
	assert.False(t, Evaluate(TaskIssueActionability, items, 0, KnownNoise).Calibrated, "one self-reported answer makes the run uncalibrated")
	assert.Equal(t, DefaultThreshold, Evaluate(TaskIssueActionability, items, 0, KnownNoise).Threshold)
	assert.Equal(t, Report{Task: "t", Threshold: 0.9}, Evaluate("t", nil, 0.9, KnownNoise))
}

func TestPerfectlyCalibratedHasZeroECE(t *testing.T) {
	var items []Labelled
	for i := 0; i < 10; i++ { // 80% confident, right 8 times out of 10
		want := KnownNoise
		if i >= 8 {
			want = Actionable
		}
		items = append(items, Labelled{Want: want, Decision: dec(0.8, true)})
	}
	r := Evaluate(TaskIssueActionability, items, 0.9, KnownNoise)
	assert.InDelta(t, 0, r.ECE, 1e-9)
	assert.Equal(t, 0.0, r.Coverage, "0.8 never clears a 0.9 gate")
}

func TestQuestionAndGate(t *testing.T) {
	q := IssueActionability(IssueInput{Kind: "error", Title: "t", Service: "s", Message: "m", ExceptionType: "E", Occurrences: 3})
	assert.Equal(t, TaskIssueActionability, q.Task)
	assert.Equal(t, []string{Actionable, KnownNoise}, []string{q.Options[0].ID, q.Options[1].ID})
	assert.Contains(t, q.Context, "Exception: E\nMessage: m\nOccurrences: 3")
	assert.True(t, Accept(ports.Decision{P: 0.9}, 0))
	assert.False(t, Accept(ports.Decision{P: 0.89}, 0.9))
}
