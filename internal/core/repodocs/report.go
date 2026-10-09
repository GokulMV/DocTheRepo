package repodocs

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// Report summarises a run of document writing for review and prompt tuning: what it cost, which
// drafts needed a repair and why, which sections came out weak or off length, and what failed.
type Report struct {
	Repo      string  `json:"repo"`
	Docs      int     `json:"docs"`
	OK        int     `json:"ok"`
	Failed    int     `json:"failed"`
	Repaired  int     `json:"repaired"` // first drafts that failed the checks
	TokensIn  int64   `json:"tokens_in"`
	TokensOut int64   `json:"tokens_out"`
	CostUSD   float64 `json:"cost_usd"`
	CapUSD    float64 `json:"cap_usd,omitempty"`
	SpentUSD  float64 `json:"spent_usd,omitempty"`
	// Confidence is the average document confidence; Labels counts high / medium / low.
	Confidence float64        `json:"confidence"`
	Labels     map[string]int `json:"labels"`
	Models     []string       `json:"models"`
	// Problems are the draft problems grouped by kind, most frequent first.
	Problems []ProblemCount `json:"problems"`
	Items    []ReportItem   `json:"items"`
}

// ProblemCount is one kind of draft problem and how often it occurred.
type ProblemCount struct {
	Kind    string `json:"kind"`
	N       int    `json:"n"`
	Example string `json:"example"`
}

// ReportItem is one document in the report.
type ReportItem struct {
	Type          string          `json:"type"`
	Key           string          `json:"key"`
	Title         string          `json:"title"`
	Status        string          `json:"status"`
	Error         string          `json:"error,omitempty"`
	Model         string          `json:"model,omitempty"`
	TokensIn      int64           `json:"tokens_in"`
	TokensOut     int64           `json:"tokens_out"`
	CostUSD       float64         `json:"cost_usd"`
	Confidence    float64         `json:"confidence"`
	Label         string          `json:"label"`
	Calibrated    bool            `json:"calibrated"`
	Why           []string        `json:"why,omitempty"`
	Gaps          []string        `json:"gaps,omitempty"`
	DraftProblems []string        `json:"draft_problems,omitempty"`
	Missing       []string        `json:"missing,omitempty"` // required sections not written
	Sections      []ReportSection `json:"sections"`
	AtAGlance     string          `json:"at_a_glance,omitempty"`
}

// ReportSection is one section: its length against the target and its score.
type ReportSection struct {
	Key      string   `json:"key"`
	Title    string   `json:"title"`
	Words    int      `json:"words"`
	Target   int      `json:"target"` // 0: no target (tables, lists)
	Length   string   `json:"length"` // ok | long | short | ""
	Score    float64  `json:"score"`
	Why      []string `json:"why,omitempty"`
	Markdown string   `json:"markdown,omitempty"`
}

// SpecFor finds a document type in the catalogue (the System architecture included).
func SpecFor(typ string) (Spec, bool) {
	if typ == SystemSpec.Type {
		return SystemSpec, true
	}
	for _, s := range Catalog {
		if s.Type == typ {
			return s, true
		}
	}
	return Spec{}, false
}

// BuildReport summarises docs. With text, sections keep their Markdown (for reading the prose itself).
func BuildReport(repo string, docs []Doc, text bool) Report {
	r := Report{Repo: repo, Labels: map[string]int{"high": 0, "medium": 0, "low": 0}, Problems: []ProblemCount{}, Items: []ReportItem{}}
	models := map[string]bool{}
	kinds := map[string]*ProblemCount{}
	var conf float64
	for _, d := range docs {
		r.Docs++
		r.TokensIn += d.TokensIn
		r.TokensOut += d.TokensOut
		r.CostUSD += d.CostUSD
		if d.Model != "" {
			models[d.Model] = true
		}
		it := ReportItem{Type: d.Type, Key: d.Key, Title: d.Title, Status: d.Status, Error: d.Error, Model: d.Model, TokensIn: d.TokensIn,
			TokensOut: d.TokensOut, CostUSD: d.CostUSD, Confidence: d.Confidence, Label: Label(d.Confidence), Calibrated: d.Calibrated,
			Why: d.Why, Gaps: d.Gaps, DraftProblems: d.DraftProblems, Sections: []ReportSection{}}
		if text {
			it.AtAGlance = d.AtAGlance
		}
		if d.Status == "failed" {
			r.Failed++
		} else {
			r.OK++
			conf += d.Confidence
			r.Labels[it.Label]++
		}
		if len(d.DraftProblems) > 0 {
			r.Repaired++
		}
		for _, p := range d.DraftProblems {
			k := problemKind(p)
			if kinds[k] == nil {
				kinds[k] = &ProblemCount{Kind: k, Example: d.Type + ": " + p}
			}
			kinds[k].N++
		}
		spec, _ := SpecFor(d.Type)
		for _, sec := range spec.Sections {
			ds := d.Section(sec.Key)
			if ds == nil {
				if sec.Required {
					it.Missing = append(it.Missing, sec.Key)
				}
				continue
			}
			rs := ReportSection{Key: sec.Key, Title: sec.Title, Words: Words(ds.Markdown), Target: sec.Words, Score: ds.Score, Why: ds.Why}
			if sec.Words > 0 {
				switch {
				case float64(rs.Words) > float64(sec.Words)*1.6:
					rs.Length = "long"
				case float64(rs.Words) < float64(sec.Words)*0.4:
					rs.Length = "short"
				default:
					rs.Length = "ok"
				}
			}
			if text {
				rs.Markdown = ds.Markdown
			}
			it.Sections = append(it.Sections, rs)
		}
		r.Items = append(r.Items, it)
	}
	if r.OK > 0 {
		r.Confidence = round2(conf / float64(r.OK))
	}
	for m := range models {
		r.Models = append(r.Models, m)
	}
	sort.Strings(r.Models)
	for _, k := range kinds {
		r.Problems = append(r.Problems, *k)
	}
	sort.Slice(r.Problems, func(i, j int) bool {
		if r.Problems[i].N != r.Problems[j].N {
			return r.Problems[i].N > r.Problems[j].N
		}
		return r.Problems[i].Kind < r.Problems[j].Kind
	})
	return r
}

var (
	kindTickRE  = regexp.MustCompile("`[^`]*`")
	kindCiteRE  = regexp.MustCompile(`\[[^\]]*\]`)
	kindQuoteRE = regexp.MustCompile(`"[^"]*"`)
	kindNumRE   = regexp.MustCompile(`\d+(\.\d+)?`)
	kindPathRE  = regexp.MustCompile(`\$(\.[A-Za-z_]+|\[\d+\])+`)
)

// problemKind strips the specifics (names, paths, numbers) so the same problem groups together.
func problemKind(p string) string {
	p = kindTickRE.ReplaceAllString(p, "`…`")
	p = kindCiteRE.ReplaceAllString(p, "[…]")
	p = kindQuoteRE.ReplaceAllString(p, `"…"`)
	p = kindPathRE.ReplaceAllString(p, "$…")
	p = kindNumRE.ReplaceAllString(p, "N")
	if i := strings.Index(p, ": "); i > 0 && i < 40 && !strings.Contains(p[:i], " ") {
		p = "<section>" + p[i:] // "flows: ..." → the section key is a specific too
	}
	if len(p) > 140 {
		p = p[:140] + "…"
	}
	return p
}

// Markdown renders the report for reading or pasting into an issue.
func (r Report) Markdown() string {
	var b strings.Builder
	fmt.Fprintf(&b, "# Documents report: %s\n\n", r.Repo)
	fmt.Fprintf(&b, "| | |\n|---|---|\n")
	fmt.Fprintf(&b, "| Documents | %d (%d ok, %d failed) |\n", r.Docs, r.OK, r.Failed)
	fmt.Fprintf(&b, "| First drafts repaired | %d of %d |\n", r.Repaired, r.Docs)
	fmt.Fprintf(&b, "| Confidence (average) | %.2f — %d high, %d medium, %d low |\n", r.Confidence, r.Labels["high"], r.Labels["medium"], r.Labels["low"])
	fmt.Fprintf(&b, "| Tokens | %d in, %d out |\n", r.TokensIn, r.TokensOut)
	fmt.Fprintf(&b, "| Cost of these documents | $%.4f |\n", r.CostUSD)
	if r.CapUSD > 0 {
		fmt.Fprintf(&b, "| Spent this month / cap | $%.4f / $%.2f |\n", r.SpentUSD, r.CapUSD)
	}
	if len(r.Models) > 0 {
		fmt.Fprintf(&b, "| Models | %s |\n", strings.Join(r.Models, ", "))
	}
	b.WriteString("\nCosts above are documents only; file cards and checks are in the month's spend.\n")

	if len(r.Problems) > 0 {
		b.WriteString("\n## What first drafts got wrong\n\n| Times | Problem | Example |\n|---|---|---|\n")
		for _, p := range r.Problems {
			fmt.Fprintf(&b, "| %d | %s | %s |\n", p.N, cell(p.Kind), cell(clip(p.Example, 200)))
		}
	}

	var failed, weak, lengths []string
	for _, it := range r.Items {
		if it.Status == "failed" {
			failed = append(failed, fmt.Sprintf("| %s | %s |", cell(it.Title), cell(clip(it.Error, 300))))
		}
		for _, s := range it.Sections {
			if s.Score < 0.6 {
				weak = append(weak, fmt.Sprintf("| %s | %s | %.2f | %s |", cell(it.Title), cell(s.Title), s.Score, cell(strings.Join(s.Why, "; "))))
			}
			if s.Length == "long" || s.Length == "short" {
				lengths = append(lengths, fmt.Sprintf("| %s | %s | %d | %d | %s |", cell(it.Title), cell(s.Title), s.Words, s.Target, s.Length))
			}
		}
		if len(it.Missing) > 0 {
			weak = append(weak, fmt.Sprintf("| %s | (missing: %s) | 0 | required section not written |", cell(it.Title), strings.Join(it.Missing, ", ")))
		}
	}
	if len(failed) > 0 {
		b.WriteString("\n## Failed documents\n\n| Document | Error |\n|---|---|\n" + strings.Join(failed, "\n") + "\n")
	}
	if len(weak) > 0 {
		b.WriteString("\n## Weak sections (score below 0.6)\n\n| Document | Section | Score | Why |\n|---|---|---|---|\n" + strings.Join(weak, "\n") + "\n")
	}
	if len(lengths) > 0 {
		b.WriteString("\n## Sections off length\n\n| Document | Section | Words | Target | |\n|---|---|---|---|---|\n" + strings.Join(lengths, "\n") + "\n")
	}

	b.WriteString("\n## Every document\n\n| Document | Status | Confidence | Tokens in / out | Cost | Repaired |\n|---|---|---|---|---|---|\n")
	for _, it := range r.Items {
		rep := ""
		if len(it.DraftProblems) > 0 {
			rep = fmt.Sprintf("yes (%d)", len(it.DraftProblems))
		}
		fmt.Fprintf(&b, "| %s | %s | %.2f %s | %d / %d | $%.4f | %s |\n", cell(it.Title), it.Status, it.Confidence, it.Label, it.TokensIn, it.TokensOut, it.CostUSD, rep)
	}

	for _, it := range r.Items {
		if it.AtAGlance == "" && !hasText(it) {
			continue
		}
		fmt.Fprintf(&b, "\n---\n\n## %s (%s, confidence %.2f)\n\n", it.Title, it.Type, it.Confidence)
		if it.AtAGlance != "" {
			fmt.Fprintf(&b, "> %s\n", strings.ReplaceAll(it.AtAGlance, "\n", "\n> "))
		}
		for _, s := range it.Sections {
			fmt.Fprintf(&b, "\n### %s\n\n_%d words (target %d), score %.2f_\n\n%s\n", s.Title, s.Words, s.Target, s.Score, s.Markdown)
		}
		if len(it.Gaps) > 0 {
			b.WriteString("\n**Gaps the model reported:** " + strings.Join(it.Gaps, "; ") + "\n")
		}
	}
	return b.String()
}

func hasText(it ReportItem) bool {
	for _, s := range it.Sections {
		if s.Markdown != "" {
			return true
		}
	}
	return false
}

// cell makes text safe inside a Markdown table cell.
func cell(s string) string {
	s = strings.ReplaceAll(s, "|", `\|`)
	return strings.Join(strings.Fields(s), " ")
}
