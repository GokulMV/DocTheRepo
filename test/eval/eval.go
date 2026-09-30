// Package eval scores answers against the golden question set (plan § 10 quality evaluations): for each
// question, the files a correct answer should cite. Precision is the share of cited files that were
// expected, recall the share of expected files that were cited; a question expecting nothing is correct
// only when the answer cites nothing. The build fails when either macro average drops more than
// MaxDrop below the committed baseline.
package eval

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
)

// MaxDrop is the allowed regression against the baseline (10 points).
const MaxDrop = 0.10

// Question is one golden question. Expect holds "owner/repo:path" entries.
type Question struct {
	ID       string   `json:"id"`
	Question string   `json:"question"`
	Expect   []string `json:"expect"`
}

// Result is one scored answer.
type Result struct {
	ID        string   `json:"id"`
	Question  string   `json:"question"`
	Expected  []string `json:"expected"`
	Cited     []string `json:"cited"`
	Precision float64  `json:"precision"`
	Recall    float64  `json:"recall"`
}

// Report aggregates a run.
type Report struct {
	Mode      string   `json:"mode"` // stub | provider
	Model     string   `json:"model,omitempty"`
	Questions int      `json:"questions"`
	Precision float64  `json:"precision"`
	Recall    float64  `json:"recall"`
	HitRate   float64  `json:"hit_rate"` // questions where at least one expected file was cited
	Results   []Result `json:"results"`
}

// Baseline is the committed reference for the stub run.
type Baseline struct {
	Precision float64 `json:"precision"`
	Recall    float64 `json:"recall"`
}

// Dir is this package's directory (golden.json and baseline.json live here).
func Dir() string {
	_, f, _, _ := runtime.Caller(0)
	return filepath.Dir(f)
}

// Golden loads golden.json.
func Golden() ([]Question, error) {
	var qs []Question
	b, err := os.ReadFile(filepath.Join(Dir(), "golden.json"))
	if err != nil {
		return nil, err
	}
	return qs, json.Unmarshal(b, &qs)
}

// LoadBaseline reads baseline.json.
func LoadBaseline() (Baseline, error) {
	var b Baseline
	raw, err := os.ReadFile(filepath.Join(Dir(), "baseline.json"))
	if err != nil {
		return b, err
	}
	return b, json.Unmarshal(raw, &b)
}

// Score compares the cited files (duplicates ignored) with the expected ones.
func Score(q Question, cited []string) Result {
	set := map[string]bool{}
	var uniq []string
	for _, c := range cited {
		if !set[c] {
			set[c] = true
			uniq = append(uniq, c)
		}
	}
	sort.Strings(uniq)
	r := Result{ID: q.ID, Question: q.Question, Expected: q.Expect, Cited: uniq}
	if len(q.Expect) == 0 {
		if len(uniq) == 0 {
			r.Precision, r.Recall = 1, 1
		}
		return r
	}
	hits := 0
	for _, e := range q.Expect {
		if set[e] {
			hits++
		}
	}
	if len(uniq) > 0 {
		r.Precision = float64(hits) / float64(len(uniq))
	}
	r.Recall = float64(hits) / float64(len(q.Expect))
	return r
}

// Summarize macro-averages results.
func Summarize(mode, model string, rs []Result) Report {
	rep := Report{Mode: mode, Model: model, Questions: len(rs), Results: rs}
	if len(rs) == 0 {
		return rep
	}
	hits := 0
	for _, r := range rs {
		rep.Precision += r.Precision
		rep.Recall += r.Recall
		if r.Recall > 0 || (len(r.Expected) == 0 && len(r.Cited) == 0) {
			hits++
		}
	}
	n := float64(len(rs))
	rep.Precision, rep.Recall, rep.HitRate = round3(rep.Precision/n), round3(rep.Recall/n), round3(float64(hits)/n)
	return rep
}

// Check fails when precision or recall regressed by more than MaxDrop.
func Check(rep Report, b Baseline) error {
	var problems []string
	if rep.Precision < b.Precision-MaxDrop {
		problems = append(problems, fmt.Sprintf("citation precision %.3f is more than %.0f points below the baseline %.3f", rep.Precision, MaxDrop*100, b.Precision))
	}
	if rep.Recall < b.Recall-MaxDrop {
		problems = append(problems, fmt.Sprintf("citation recall %.3f is more than %.0f points below the baseline %.3f", rep.Recall, MaxDrop*100, b.Recall))
	}
	if len(problems) > 0 {
		return fmt.Errorf("retrieval quality regressed: %v", problems)
	}
	return nil
}

func round3(x float64) float64 { return float64(int(x*1000+0.5)) / 1000 }
