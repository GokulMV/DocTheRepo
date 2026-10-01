package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/GokulMV/DocTheRepo/internal/core/decide"
	"github.com/GokulMV/DocTheRepo/internal/core/llmgateway"
	"github.com/GokulMV/DocTheRepo/test/eval"
)

// TestDecisionEval runs the labelled actionability set (plan Phase 11.5) through the decide route and
// reports accuracy, calibration (ECE, reliability curve, Brier), gate coverage, defects the gate would
// have skipped, and decision tokens against the full-decode cost the gate avoids.
//
// Default: the deterministic stub, gated against test/eval/decide_baseline.json. To score a real provider
// (report only): DTH_DECIDE_EVAL_KIND (openai_compat, anthropic, …), DTH_DECIDE_EVAL_BASE_URL,
// DTH_DECIDE_EVAL_API_KEY, DTH_DECIDE_EVAL_MODEL (for TypeSafe Jev: DTH_DECIDE_EVAL_KIND=jev and the key); DTH_EVAL_REPORT=path writes the JSON report;
// DTH_EVAL_DECODE_TOKENS overrides the full-decode size (default 8000).
func TestDecisionEval(t *testing.T) {
	ctx := context.Background()
	items, err := eval.DecisionItems()
	require.NoError(t, err)
	require.Len(t, items, 100)

	env := newSignalEnv(t) // no stub routes yet: the decide route is set below
	mode, kind, base, key, model := "stub", "openai_compat", env.stub.URL+"/v1", "sk", "stub"
	if u, k := os.Getenv("DTH_DECIDE_EVAL_BASE_URL"), os.Getenv("DTH_DECIDE_EVAL_KIND"); u != "" || k != "" {
		// e.g. DTH_DECIDE_EVAL_KIND=jev DTH_DECIDE_EVAL_API_KEY=… (base URL and model default for Jev)
		mode, kind, base, key, model = "provider", k, u, os.Getenv("DTH_DECIDE_EVAL_API_KEY"), os.Getenv("DTH_DECIDE_EVAL_MODEL")
		if kind == "" {
			kind = "openai_compat"
		}
		if model == "" && kind == "jev" {
			model = "jev-latest"
		}
	}
	provID := env.addProvider(t, kind, base, key)
	env.setRoute(t, llmgateway.FeatureDecide, provID, model)

	var labelled []decide.Labelled
	failed := 0
	for _, it := range items {
		q := decide.IssueActionability(decide.IssueInput{Kind: it.Kind, Title: it.Title, Service: it.Service, Message: it.Message})
		d, err := env.a.decoder.GW.Decide(ctx, llmgateway.CallMeta{}, q)
		if err != nil {
			failed++
			t.Logf("%s: %v", it.ID, err)
			continue
		}
		labelled = append(labelled, decide.Labelled{ID: it.ID, Want: it.Label, Decision: d})
	}
	r := decide.Evaluate(decide.TaskIssueActionability, labelled, decide.DefaultThreshold, decide.KnownNoise)
	r.FailedDecisions = failed
	decodeTokens := 8000.0
	if v, err := strconv.ParseFloat(os.Getenv("DTH_EVAL_DECODE_TOKENS"), 64); err == nil && v > 0 {
		decodeTokens = v
	}
	// Per issue: every issue pays for a decision; the skipped share avoids a full decode.
	netSaved := r.NoiseSkipRate*decodeTokens - r.TokensPerItem
	t.Logf("decision eval (%s, %s): accuracy %.3f, ECE %.3f, Brier %.3f, coverage@%.2f %.2f, gated accuracy %.3f, "+
		"decode skip rate %.2f, missed defects %d, %.0f decision tokens/issue vs %.0f per full decode → net %.0f tokens/issue saved, calibrated=%v, failed %d",
		mode, model, r.Accuracy, r.ECE, r.Brier, r.Threshold, r.Coverage, r.GatedAccuracy, r.NoiseSkipRate, r.MissedDefects,
		r.TokensPerItem, decodeTokens, netSaved, r.Calibrated, r.FailedDecisions)
	for _, b := range r.Reliability {
		t.Logf("  reliability [%.1f, %.1f): n=%d mean p %.3f accuracy %.3f", b.Lo, b.Hi, b.Count, b.MeanP, b.Accuracy)
	}
	if path := os.Getenv("DTH_EVAL_REPORT"); path != "" {
		out, _ := json.MarshalIndent(map[string]any{"mode": mode, "model": model, "report": r,
			"decode_tokens": decodeTokens, "net_tokens_saved_per_issue": netSaved}, "", "  ")
		require.NoError(t, os.WriteFile(path, out, 0o644))
	}
	if mode != "stub" {
		return
	}
	require.Zero(t, failed)
	baseline, err := eval.LoadDecisionBaseline()
	require.NoError(t, err)
	assert.GreaterOrEqual(t, r.Accuracy, baseline.Accuracy-eval.MaxDrop, "accuracy regressed")
	assert.LessOrEqual(t, r.ECE, baseline.ECE+eval.MaxDrop, "calibration regressed")
	assert.LessOrEqual(t, r.MissedDefects, baseline.MissedDefects+2, fmt.Sprintf("the gate would skip %d real defects", r.MissedDefects))
	assert.False(t, r.Calibrated, "the stub is a chat provider: its probabilities are labelled self-reported")
}
