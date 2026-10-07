package rag

import (
	"fmt"
	"math"
	"regexp"
	"strings"

	"github.com/GokulMV/DocTheRepo/internal/ports"
)

// Confidence is how far an answer can be trusted.
type Confidence struct {
	Score float64  `json:"score"`
	Label string   `json:"label"` // high | medium | low
	Why   []string `json:"why,omitempty"`
}

var (
	sentenceRE = regexp.MustCompile(`[^.!?\n]+[.!?]?`)
	citedRE    = regexp.MustCompile(`\[\d{1,3}\]`)
	docConfRE  = regexp.MustCompile(`confidence (high|medium|low)\)`)
)

// sourceTrust is how much a cited source is worth: code and live tool results are the ground truth,
// written pages a little less, generated documents by their own confidence.
func sourceTrust(c ports.Chunk) (float64, string) {
	switch c.Source {
	case ports.SourceCode:
		return 0.95, ""
	case ports.SourceTool:
		return 0.9, ""
	case ports.SourceGeneratedDoc:
		if m := docConfRE.FindStringSubmatch(c.Content); m != nil {
			switch m[1] {
			case "high":
				return 0.9, ""
			case "medium":
				return 0.7, "it cites a document of medium confidence"
			default:
				return 0.45, "it cites a document of low confidence"
			}
		}
		return 0.8, ""
	}
	return 0.8, ""
}

// Score rates an answer from what it cites and how much of it is cited: the share of its sentences that
// carry a citation, and the trust of the sources behind them (code above documents; a generated document
// counts by its own confidence). A weak source pick or a single source lowers it.
func Score(a Answer, packed []ports.Chunk, weakPick bool) *Confidence {
	if len(a.Citations) == 0 || a.Text == NotFoundAnswer {
		return nil
	}
	var why []string
	total, cited := 0, 0
	for _, s := range sentenceRE.FindAllString(stripCode(a.Text), -1) {
		if len(strings.Fields(s)) < 4 {
			continue // headings, list markers, fragments
		}
		total++
		if citedRE.MatchString(s) {
			cited++
		}
	}
	cover := 1.0
	if total > 0 {
		cover = float64(cited) / float64(total)
	}
	if cover < 0.6 {
		why = append(why, fmt.Sprintf("only %d of %d statements cite a source", cited, total))
	}
	byID := map[string]ports.Chunk{}
	for _, c := range packed {
		byID[c.ID] = c
	}
	sum, lowest := 0.0, 1.0
	seen := map[string]bool{}
	for _, c := range a.Citations {
		ch, ok := byID[c.ChunkID]
		if !ok {
			continue
		}
		t, reason := sourceTrust(ch)
		sum += t
		lowest = math.Min(lowest, t)
		if reason != "" && !seen[reason] {
			seen[reason] = true
			why = append(why, reason)
		}
	}
	trust := 0.8
	if n := len(a.Citations); n > 0 && sum > 0 {
		trust = 0.7*(sum/float64(n)) + 0.3*lowest
	}
	score := 0.45*cover + 0.55*trust
	if len(a.Citations) == 1 {
		score -= 0.05
		why = append(why, "it rests on a single source")
	}
	if weakPick {
		score -= 0.1
		why = append(why, "the source picker found nothing clearly relevant")
	}
	score = math.Max(0, math.Min(1, score))
	label := "low"
	switch {
	case score >= 0.8:
		label = "high"
	case score >= 0.6:
		label = "medium"
	}
	return &Confidence{Score: math.Round(score*100) / 100, Label: label, Why: why}
}

var fenceStripRE = regexp.MustCompile("(?s)```.*?```")

func stripCode(s string) string { return fenceStripRE.ReplaceAllString(s, " ") }
