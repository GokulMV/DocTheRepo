package llmgateway

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strings"

	"github.com/GokulMV/DocTheRepo/internal/core/spendguard"
	"github.com/GokulMV/DocTheRepo/internal/ports"
	"github.com/GokulMV/DocTheRepo/pkg/contract"
)

// FeatureDecide is the decision route (typed questions → per-option probabilities).
const FeatureDecide = "decide"

// DecideSystem is the instruction for chat models answering a decision question.
const DecideSystem = `You classify. Given context and a fixed set of options, reply with one JSON object
{"probabilities": {"<option id>": <probability>, ...}} giving every option a probability between 0 and 1,
summing to 1. Put high probability on an option only when the context clearly supports it.`

// Decide answers a decision question on the decide route. Providers implementing ports.Decider answer
// natively (calibrated); any chat provider answers through a JSON contract (self-reported, labelled
// uncalibrated). Either way the call is scrubbed, spend-guarded, and recorded like every other paid call.
func (g *Gateway) Decide(ctx context.Context, meta CallMeta, q ports.DecisionQuestion) (ports.Decision, error) {
	if len(q.Options) < 2 {
		return ports.Decision{}, ports.Permanent(fmt.Errorf("decision %q needs at least two options", q.Task))
	}
	route, err := g.Route(ctx, FeatureDecide)
	if err != nil {
		return ports.Decision{}, err
	}
	llm, err := g.providers.LLM(ctx, route.ProviderID)
	if err != nil {
		return ports.Decision{}, err
	}
	if native, ok := llm.(ports.Decider); ok {
		return g.decideNative(ctx, route, meta, native, q)
	}
	return g.decideJSON(ctx, meta, q)
}

func (g *Gateway) decideNative(ctx context.Context, r Route, meta CallMeta, d ports.Decider, q ports.DecisionQuestion) (ports.Decision, error) {
	q.Context = g.protect(r, q.Context)
	inEst := spendguard.EstimateTokens(q.Context) + int64(8*len(q.Options))
	sreq := spendguard.Request{Feature: r.Feature, ProviderID: r.ProviderID, ProviderKind: r.ProviderKind, Model: r.Model,
		RepoID: meta.RepoID, InputTokens: inEst, OutputTokens: int64(4 * len(q.Options)), Override: meta.Override, Budget: meta.Budget}
	_, release, err := g.enforcer.Reserve(ctx, sreq)
	defer release()
	if err != nil {
		g.observe(r, "blocked", ports.TokenUsage{}, 0, false)
		return ports.Decision{}, err
	}
	start := g.now()
	dec, err := d.Decide(ctx, r.Model, q)
	latency := g.now().Sub(start)
	usage, estimated := dec.Usage, false
	if !usage.Reported {
		usage, estimated = ports.TokenUsage{InputTokens: inEst, OutputTokens: int64(4 * len(q.Options))}, true
	}
	outcome := "ok"
	if err != nil {
		outcome = "error"
		usage = ports.TokenUsage{}
	}
	model := dec.Model
	if model == "" {
		model = r.Model
	}
	_ = g.record(ctx, r, model, meta, usage, latency, estimated, outcome)
	g.observe(r, outcome, usage, latency, estimated)
	if err != nil {
		return ports.Decision{}, err
	}
	dec.Model, dec.Usage = model, usage
	return finish(dec, q)
}

func (g *Gateway) decideJSON(ctx context.Context, meta CallMeta, q ports.DecisionQuestion) (ports.Decision, error) {
	props := contract.Schema{}
	required := make([]any, len(q.Options))
	var list strings.Builder
	for i, o := range q.Options {
		props[o.ID] = contract.Schema{"type": "number", "minimum": 0, "maximum": 1}
		required[i] = o.ID
		fmt.Fprintf(&list, "- %s: %s\n", o.ID, o.Description)
	}
	schema := contract.Schema{"type": "object", "additionalProperties": false, "required": []any{"probabilities"},
		"properties": contract.Schema{"probabilities": contract.Schema{"type": "object", "additionalProperties": false,
			"required": required, "properties": props}}}
	var out struct {
		Probabilities map[string]float64 `json:"probabilities"`
	}
	question := q.Question
	if question == "" {
		question = q.Task
	}
	prompt := "## Question\n" + question + "\n\n## Options\n" + list.String() + "\n## Context\n" + q.Context
	res, err := g.ChatJSONResult(ctx, FeatureDecide, meta, ports.ChatRequest{System: DecideSystem,
		Messages: []ports.ChatMessage{{Role: "user", Content: prompt}}}, schema, &out, nil)
	if err != nil {
		return ports.Decision{}, err
	}
	return finish(ports.Decision{Probabilities: out.Probabilities, Calibrated: false, Model: res.Model, Usage: res.Usage}, q)
}

// finish normalises probabilities over the question's options (unknown options dropped, missing ones 0,
// all-zero → uniform) and sets the choice. Ties go to the option listed first.
func finish(d ports.Decision, q ports.DecisionQuestion) (ports.Decision, error) {
	probs := make(map[string]float64, len(q.Options))
	var sum float64
	for _, o := range q.Options {
		p := d.Probabilities[o.ID]
		if math.IsNaN(p) || p < 0 {
			p = 0
		}
		probs[o.ID] = p
		sum += p
	}
	for _, o := range q.Options {
		if sum > 0 {
			probs[o.ID] /= sum
		} else {
			probs[o.ID] = 1 / float64(len(q.Options))
		}
	}
	d.Probabilities = probs
	d.Choice, d.P = "", -1
	for _, o := range q.Options {
		if probs[o.ID] > d.P {
			d.Choice, d.P = o.ID, probs[o.ID]
		}
	}
	return d, nil
}

// SortedOptions returns a decision's options by probability, highest first (ties by ID).
func SortedOptions(d ports.Decision) []string {
	ids := make([]string, 0, len(d.Probabilities))
	for id := range d.Probabilities {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool {
		if d.Probabilities[ids[i]] != d.Probabilities[ids[j]] {
			return d.Probabilities[ids[i]] > d.Probabilities[ids[j]]
		}
		return ids[i] < ids[j]
	})
	return ids
}
