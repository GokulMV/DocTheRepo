// Package decode explains an issue (plan § 8.11): it assembles a bounded context — recent redacted
// samples, the code the stack frames point at (exact path + symbol, then vector search in the service's
// repositories), that code's docs, commits in the last 7 days touching it, similar decoded issues, and
// runbooks — and asks the decode route for the decode contract. A decode whose affected code is unchanged
// is reused instead of paying again; every decode is indexed so later issues can find it as "similar".
package decode

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/GokulMV/DocTheRepo/internal/core/chunker"
	"github.com/GokulMV/DocTheRepo/internal/core/decide"
	"github.com/GokulMV/DocTheRepo/internal/core/llmgateway"
	"github.com/GokulMV/DocTheRepo/internal/core/signals"
	"github.com/GokulMV/DocTheRepo/internal/ports"
	"github.com/GokulMV/DocTheRepo/pkg/contract"
)

// Defaults (plan § 8.11).
const (
	DefaultBudget    = 12000 // context tokens
	SimilarThreshold = 0.85
	RunbookThreshold = 0.75
	samplesUsed      = 3
	maxCodeChunks    = 8
	maxCommitPaths   = 5
	commitWindow     = 7 * 24 * time.Hour
)

// Issue is what the decoder reads about an issue.
type Issue struct {
	ID, Fingerprint, Kind, Title, Service, Environment, Status, RepoID, DecodeID string
	Occurrences                                                                  int64
	Sources                                                                      []string
	// NoLLM is set when the issue's events come from a connector that must never be sent to a model.
	NoLLM bool
}

// NoLLMReason explains a decode skipped by connector policy.
const NoLLMReason = "not sent to a model: the connector is set to never send its data to an LLM"

// Affected is one code chunk a decode blames, with the content hash it had (staleness check).
type Affected struct {
	ChunkID     string `json:"chunk_id"`
	RepoID      string `json:"repo_id,omitempty"`
	Path        string `json:"path"`
	Symbol      string `json:"symbol,omitempty"`
	StartLine   int    `json:"start_line,omitempty"`
	EndLine     int    `json:"end_line,omitempty"`
	ContentHash string `json:"content_hash"`
	Reason      string `json:"reason"`
}

// DocRef is a doc section supplied to the decode.
type DocRef struct {
	ChunkID string `json:"chunk_id"`
	Path    string `json:"path"`
	Symbol  string `json:"symbol,omitempty"`
	Source  string `json:"source"`
}

// Stored is the latest persisted decode of an issue.
type Stored struct {
	ID                 string
	FingerprintVersion int
	Affected           []Affected
	Tokens             int64
	CostUSD            float64
}

// Record is a decode to persist.
type Record struct {
	IssueID        string
	Result         contract.DecodeResult
	Affected       []Affected
	RelatedCommits []ports.Commit
	RelatedDocs    []DocRef
	SimilarIssues  []string
	Provider       string
	Model          string
	Tokens         int64
	CostUSD        float64
}

// Store is the persistence the decoder needs.
type Store interface {
	Issue(ctx context.Context, id string) (Issue, error)
	// Samples returns the most recent redacted samples, newest first.
	Samples(ctx context.Context, issueID string, n int) ([]ports.SignalEvent, error)
	// ServiceRepos lists the repositories service_map ties to a service.
	ServiceRepos(ctx context.Context, service string) ([]string, error)
	// FrameChunks finds live code chunks by file path suffix and symbol, one per frame, in order.
	FrameChunks(ctx context.Context, repoIDs []string, frames []ports.StackFrame) ([]ports.Chunk, error)
	// Docs returns generated doc sections for these code paths.
	Docs(ctx context.Context, repoID string, paths []string) ([]ports.Chunk, error)
	// Chunks loads live chunks by ID.
	Chunks(ctx context.Context, ids []string) ([]ports.Chunk, error)
	LatestDecode(ctx context.Context, issueID string) (Stored, bool, error)
	// SaveDecode stores a decode, links it as the issue's current decode, and moves new → decoded.
	SaveDecode(ctx context.Context, r Record) (string, error)
	// PutChunk upserts a chunk (the decode's searchable summary).
	PutChunk(ctx context.Context, c ports.Chunk) error
	// IssueForChunks maps issue_decode chunk IDs back to their issues.
	IssueForChunks(ctx context.Context, chunkIDs []string) (map[string]string, error)
}

// Savings records avoided spend.
type Savings interface {
	Record(ctx context.Context, kind string, tokens int64, costUSD float64, ref string) error
}

// Decoder runs decodes.
type Decoder struct {
	Store   Store
	GW      *llmgateway.Gateway
	Index   ports.VectorIndex // nil: no vector search (full context from frames only)
	Savings Savings
	// Embed indexes chunks (pipeline.Indexer.Embed); nil skips indexing the decode summary.
	Embed func(ctx context.Context, meta llmgateway.CallMeta, chunks []ports.Chunk) error
	// Commits lists commits touching a path since a time (the code host); nil skips commits.
	Commits func(ctx context.Context, repoID, path string, since time.Time) ([]ports.Commit, error)
	// Cost prices a call; optional.
	Cost   func(providerKind, model, feature string, in, out int64) (float64, bool)
	Budget int
	Now    func() time.Time

	// Decide, when set, asks the decision route whether a new issue is recurring noise before paying for a
	// full decode (plan Phase 11.5). Only "known_noise" at p >= GateThreshold short-circuits; anything else,
	// or no decide route, runs the full decode.
	Decide        func(ctx context.Context, meta llmgateway.CallMeta, q ports.DecisionQuestion) (ports.Decision, error)
	GateThreshold float64
	// Estimate is the average cost of a full decode (the saving a gated decode is credited with).
	Estimate func(ctx context.Context) (tokens int64, costUSD float64)
}

// Outcome of one decode request.
type Outcome struct {
	Status   string `json:"status"` // decoded | reused | gated | skipped
	DecodeID string `json:"decode_id,omitempty"`
	Reason   string `json:"reason,omitempty"`
}

// Decode explains an issue. force re-decodes even when the current decode is still valid (a user asked).
func (d *Decoder) Decode(ctx context.Context, issueID string, force bool, jobID string) (Outcome, error) {
	is, err := d.Store.Issue(ctx, issueID)
	if err != nil {
		return Outcome{}, err
	}
	if is.NoLLM { // policy, not cost: even a forced decode stays local
		return Outcome{Status: "skipped", Reason: NoLLMReason}, nil
	}
	if is.Status == "suppressed" && !force {
		return Outcome{Status: "skipped", Reason: "suppressed by a known-issue rule"}, nil
	}
	meta := llmgateway.CallMeta{JobID: jobID, IssueID: is.ID, RepoID: is.RepoID}
	if !force {
		if prev, ok, err := d.Store.LatestDecode(ctx, is.ID); err != nil {
			return Outcome{}, err
		} else if ok {
			fresh, err := d.fresh(ctx, prev)
			if err != nil {
				return Outcome{}, err
			}
			if fresh {
				if d.Savings != nil {
					_ = d.Savings.Record(ctx, "decode_reused", prev.Tokens, prev.CostUSD, is.ID)
				}
				return Outcome{Status: "reused", DecodeID: prev.ID}, nil
			}
		}
	}

	if !force {
		if out, ok, err := d.gate(ctx, meta, is); err != nil {
			return Outcome{}, err
		} else if ok {
			return out, nil
		}
	}

	c, err := d.gather(ctx, meta, is)
	if err != nil {
		return Outcome{}, err
	}
	prompt, supplied := c.prompt(d.budget())
	var res contract.DecodeResult
	jr, err := d.GW.ChatJSONResult(ctx, llmgateway.FeatureDecode, meta,
		ports.ChatRequest{System: System, Messages: []ports.ChatMessage{{Role: "user", Content: prompt}}},
		contract.DecodeSchema, &res, nil)
	var se *ports.SchemaError
	switch {
	case errors.As(err, &se):
		// The contract: one repair, then store what the model said at low confidence.
		res = contract.DecodeResult{Summary: truncate(strings.TrimSpace(jr.Raw), 2000), Confidence: "low", IsActionable: true,
			ProbableCause: "The decode did not match the expected format: " + strings.Join(se.Problems, "; ")}
		if res.Summary == "" {
			res.Summary = "The model returned no usable decode."
		}
	case err != nil:
		return Outcome{}, err
	}
	rec := Record{IssueID: is.ID, Result: res, RelatedCommits: c.commits, SimilarIssues: c.similar, Model: jr.Model,
		Tokens: jr.Usage.InputTokens + jr.Usage.OutputTokens}
	if rt, rerr := d.GW.Route(ctx, llmgateway.FeatureDecode); rerr == nil {
		rec.Provider = rt.ProviderKind
		if d.Cost != nil {
			rec.CostUSD, _ = d.Cost(rt.ProviderKind, jr.Model, llmgateway.FeatureDecode, jr.Usage.InputTokens, jr.Usage.OutputTokens)
		}
	}
	for _, a := range res.AffectedCode { // only chunks the model was shown: no invented locations
		if ch, ok := supplied[a.ChunkID]; ok {
			rec.Affected = append(rec.Affected, Affected{ChunkID: ch.ID, RepoID: ch.RepoID, Path: ch.Path, Symbol: ch.Symbol,
				StartLine: ch.StartLine, EndLine: ch.EndLine, ContentHash: ch.ContentHash, Reason: a.Reason})
		}
	}
	for _, ch := range c.docs {
		if _, ok := supplied[ch.ID]; ok {
			rec.RelatedDocs = append(rec.RelatedDocs, DocRef{ChunkID: ch.ID, Path: ch.Path, Symbol: ch.Symbol, Source: string(ch.Source)})
		}
	}
	id, err := d.Store.SaveDecode(ctx, rec)
	if err != nil {
		return Outcome{}, err
	}
	d.index(ctx, meta, is, res)
	return Outcome{Status: "decoded", DecodeID: id}, nil
}

// gate asks the decision route whether the issue is recurring noise; a confident "yes" stores a short
// decode that says so (not actionable, suggest as known issue) instead of the full one. Errors other than
// a spend block fall through to the full decode: the gate may only save, never block, an explanation.
func (d *Decoder) gate(ctx context.Context, meta llmgateway.CallMeta, is Issue) (Outcome, bool, error) {
	if d.Decide == nil {
		return Outcome{}, false, nil
	}
	samples, err := d.Store.Samples(ctx, is.ID, 1)
	if err != nil {
		return Outcome{}, false, err
	}
	in := decide.IssueInput{Kind: is.Kind, Title: is.Title, Service: is.Service, Environment: is.Environment,
		Occurrences: is.Occurrences, Sources: is.Sources}
	if len(samples) > 0 {
		in.Message, in.ExceptionType = samples[0].Message, samples[0].ExceptionType
	}
	dec, err := d.Decide(ctx, meta, decide.IssueActionability(in))
	var sb *ports.SpendBlockedError
	switch {
	case errors.As(err, &sb):
		return Outcome{}, false, err
	case err != nil:
		return Outcome{}, false, nil
	}
	if dec.Choice != decide.KnownNoise || !decide.Accept(dec, d.GateThreshold) {
		return Outcome{}, false, nil
	}
	source := "self-reported"
	if dec.Calibrated {
		source = "calibrated"
	}
	conf := "medium"
	if dec.P >= 0.97 {
		conf = "high"
	}
	res := contract.DecodeResult{
		Summary:       "Recurring noise: " + is.Title,
		ProbableCause: fmt.Sprintf("The decision model classified this as known noise (p=%.2f, %s probability), so no full decode was run. Ask for a full decode if that is wrong.", dec.P, source),
		Impact:        "None expected.", Confidence: conf, IsActionable: false, SuggestKnownIssue: true, NextSteps: []string{"Mark as known if this is expected"},
	}
	rec := Record{IssueID: is.ID, Result: res, Model: dec.Model, Tokens: dec.Usage.InputTokens + dec.Usage.OutputTokens}
	if rt, rerr := d.GW.Route(ctx, llmgateway.FeatureDecide); rerr == nil {
		rec.Provider = "decide:" + rt.ProviderKind
		if d.Cost != nil {
			rec.CostUSD, _ = d.Cost(rt.ProviderKind, dec.Model, llmgateway.FeatureDecide, dec.Usage.InputTokens, dec.Usage.OutputTokens)
		}
	}
	id, err := d.Store.SaveDecode(ctx, rec)
	if err != nil {
		return Outcome{}, false, err
	}
	if d.Savings != nil && d.Estimate != nil {
		tokens, cost := d.Estimate(ctx)
		_ = d.Savings.Record(ctx, "decision_gate", max(tokens-rec.Tokens, 0), max(cost-rec.CostUSD, 0), is.ID)
	}
	return Outcome{Status: "gated", DecodeID: id, Reason: fmt.Sprintf("known noise at p=%.2f", dec.P)}, true, nil
}

// fresh reports whether a stored decode still stands: same fingerprint recipe and every blamed chunk
// unchanged (plan: re-decode when the affected code changed, lazily on the next occurrence).
func (d *Decoder) fresh(ctx context.Context, prev Stored) (bool, error) {
	if prev.FingerprintVersion != signals.FingerprintVersion {
		return false, nil
	}
	if len(prev.Affected) == 0 {
		return true, nil
	}
	ids := make([]string, len(prev.Affected))
	for i, a := range prev.Affected {
		ids[i] = a.ChunkID
	}
	live, err := d.Store.Chunks(ctx, ids)
	if err != nil {
		return false, err
	}
	hash := map[string]string{}
	for _, c := range live {
		if c.Live() {
			hash[c.ID] = c.ContentHash
		}
	}
	for _, a := range prev.Affected {
		if hash[a.ChunkID] != a.ContentHash {
			return false, nil
		}
	}
	return true, nil
}

// DecodeChunkID is the chunk holding an issue's decode summary.
func DecodeChunkID(issueID string) string { return chunker.ID("issue", "issues/"+issueID, "decode") }

// index stores the decode as an issue_decode chunk and embeds it (best effort: a failure only means the
// issue is not found as "similar" until its next decode).
func (d *Decoder) index(ctx context.Context, meta llmgateway.CallMeta, is Issue, r contract.DecodeResult) {
	content := fmt.Sprintf("Issue: %s\nService: %s\nSummary: %s\nProbable cause: %s\nImpact: %s", is.Title, is.Service,
		r.Summary, r.ProbableCause, r.Impact)
	ch := ports.Chunk{ID: DecodeChunkID(is.ID), RepoID: is.RepoID, Scope: "issue", Source: ports.SourceIssueDecode,
		Path: "issues/" + is.ID, Symbol: "decode", Language: "markdown", Content: content, ContentHash: chunker.Hash(content)}
	if err := d.Store.PutChunk(ctx, ch); err != nil {
		return
	}
	if d.Embed != nil {
		_ = d.Embed(ctx, meta, []ports.Chunk{ch})
	}
}

func (d *Decoder) budget() int {
	if d.Budget > 0 {
		return d.Budget
	}
	return DefaultBudget
}

func (d *Decoder) now() time.Time {
	if d.Now != nil {
		return d.Now()
	}
	return time.Now()
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

// uniq keeps the first occurrence of each string.
func uniq(in []string) []string {
	seen := map[string]bool{}
	out := in[:0:0]
	for _, s := range in {
		if s != "" && !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}
