// Package decide holds the typed decision questions the Hub asks behind confidence gates (plan Phase 11.5),
// and the evaluation that measures whether a decision provider's probabilities can be trusted: accuracy,
// calibration (reliability curve and expected calibration error), and how many paid calls a gate saves.
// Decisions only ever choose; anything written for people still comes from the full model path.
package decide

import (
	"fmt"
	"math"
	"sort"
	"strings"

	"github.com/GokulMV/DocTheRepo/internal/ports"
)

// Tasks and their options.
const (
	TaskIssueActionability = "issue_actionability"
	Actionable             = "actionable"
	KnownNoise             = "known_noise"
)

// DefaultThreshold is the gate: a decision is used only at p >= 0.9; otherwise the full path runs.
const DefaultThreshold = 0.9

// IssueInput is what the actionability question reads.
type IssueInput struct {
	Kind, Title, Service, Environment, Message, ExceptionType string
	Occurrences                                               int64
	Sources                                                   []string
}

// IssueActionability asks whether an issue needs engineering attention or is recurring noise (a health
// probe, an expected retry, a third-party blip). Only "known_noise" at high confidence skips the decode.
func IssueActionability(in IssueInput) ports.DecisionQuestion {
	var b strings.Builder
	fmt.Fprintf(&b, "Kind: %s\nTitle: %s\nService: %s\n", in.Kind, in.Title, in.Service)
	if in.Environment != "" {
		fmt.Fprintf(&b, "Environment: %s\n", in.Environment)
	}
	if in.ExceptionType != "" {
		fmt.Fprintf(&b, "Exception: %s\n", in.ExceptionType)
	}
	if in.Message != "" && in.Message != in.Title {
		msg := in.Message
		if len(msg) > 1500 {
			msg = msg[:1500]
		}
		fmt.Fprintf(&b, "Message: %s\n", msg)
	}
	if in.Occurrences > 0 {
		fmt.Fprintf(&b, "Occurrences: %d\n", in.Occurrences)
	}
	if len(in.Sources) > 0 {
		fmt.Fprintf(&b, "Sources: %s\n", strings.Join(in.Sources, ", "))
	}
	return ports.DecisionQuestion{Task: TaskIssueActionability,
		Question: "Does this production issue need engineering attention, or is it recurring noise nobody needs to act on?",
		Context:  b.String(), Options: []ports.DecisionOption{
			{ID: Actionable, Description: "a defect or outage the owning team should investigate and fix"},
			{ID: KnownNoise, Description: "expected or recurring noise nobody needs to act on: health probes, deploy churn, expected retries, client aborts, third-party blips, test traffic"},
		}}
}

// Accept reports whether a decision clears the gate.
func Accept(d ports.Decision, threshold float64) bool {
	if threshold <= 0 {
		threshold = DefaultThreshold
	}
	return d.P >= threshold
}

// Labelled is one evaluation item's outcome: the decision and the correct option.
type Labelled struct {
	ID       string
	Want     string
	Decision ports.Decision
}

// Bin is one reliability-curve bucket.
type Bin struct {
	Lo       float64 `json:"lo"`
	Hi       float64 `json:"hi"`
	Count    int     `json:"count"`
	MeanP    float64 `json:"mean_p"`   // mean confidence (probability of the chosen option)
	Accuracy float64 `json:"accuracy"` // fraction correct
}

// Report summarises an evaluation.
type Report struct {
	Task            string  `json:"task"`
	N               int     `json:"n"`
	Accuracy        float64 `json:"accuracy"`
	ECE             float64 `json:"ece"`   // expected calibration error over 10 equal-width bins
	Brier           float64 `json:"brier"` // multi-class Brier score (lower is better)
	Reliability     []Bin   `json:"reliability"`
	Threshold       float64 `json:"threshold"`
	Coverage        float64 `json:"coverage"`         // fraction decided at p >= threshold
	GatedAccuracy   float64 `json:"gated_accuracy"`   // accuracy of those decisions
	NoiseSkipRate   float64 `json:"noise_skip_rate"`  // fraction whose decode the gate would skip
	MissedDefects   int     `json:"missed_defects"`   // actionable items the gate would have skipped
	TokensPerItem   float64 `json:"tokens_per_item"`  // decision tokens
	Calibrated      bool    `json:"calibrated"`       // provider reports calibrated probabilities
	FailedDecisions int     `json:"failed_decisions"` // items the provider could not answer
}

// Evaluate scores decisions against labels. skipOption is the option whose gated acceptance skips the
// expensive path (KnownNoise for actionability).
func Evaluate(task string, items []Labelled, threshold float64, skipOption string) Report {
	if threshold <= 0 {
		threshold = DefaultThreshold
	}
	r := Report{Task: task, N: len(items), Threshold: threshold, Calibrated: len(items) > 0}
	if len(items) == 0 {
		return r
	}
	bins := make([]Bin, 10)
	for i := range bins {
		bins[i].Lo, bins[i].Hi = float64(i)/10, float64(i+1)/10
	}
	var correct, gated, gatedCorrect, skipped int
	var tokens int64
	for _, it := range items {
		d := it.Decision
		r.Calibrated = r.Calibrated && d.Calibrated
		tokens += d.Usage.InputTokens + d.Usage.OutputTokens
		ok := d.Choice == it.Want
		if ok {
			correct++
		}
		for opt, p := range d.Probabilities {
			y := 0.0
			if opt == it.Want {
				y = 1
			}
			r.Brier += (p - y) * (p - y)
		}
		if _, has := d.Probabilities[it.Want]; !has {
			r.Brier++ // the right answer got no probability at all
		}
		idx := min(int(d.P*10), 9)
		if idx < 0 {
			idx = 0
		}
		b := &bins[idx]
		b.Count++
		b.MeanP += d.P
		if ok {
			b.Accuracy++
		}
		if Accept(d, threshold) {
			gated++
			if ok {
				gatedCorrect++
			}
			if d.Choice == skipOption {
				skipped++
				if !ok {
					r.MissedDefects++
				}
			}
		}
	}
	n := float64(len(items))
	r.Accuracy = float64(correct) / n
	r.Brier /= n
	r.Coverage = float64(gated) / n
	r.NoiseSkipRate = float64(skipped) / n
	if gated > 0 {
		r.GatedAccuracy = float64(gatedCorrect) / float64(gated)
	}
	r.TokensPerItem = float64(tokens) / n
	for i := range bins {
		b := &bins[i]
		if b.Count > 0 {
			b.MeanP /= float64(b.Count)
			b.Accuracy /= float64(b.Count)
			r.ECE += float64(b.Count) / n * math.Abs(b.Accuracy-b.MeanP)
			r.Reliability = append(r.Reliability, *b)
		}
	}
	sort.Slice(r.Reliability, func(i, j int) bool { return r.Reliability[i].Lo < r.Reliability[j].Lo })
	return r
}
