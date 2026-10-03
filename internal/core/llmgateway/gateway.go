// Package llmgateway is the only path from the Hub to a paid model (plan § 8.13, § 8.14): it resolves the
// configured route for a feature, checks the spend guard with an estimate, calls the provider through its
// port, falls back to the route's fallback on transient failure, validates structured output with one
// repair pass, and records actual usage in the ledger.
package llmgateway

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/GokulMV/DocTheRepo/internal/core/signals"
	"github.com/GokulMV/DocTheRepo/internal/core/spendguard"
	"github.com/GokulMV/DocTheRepo/internal/ports"
	"github.com/GokulMV/DocTheRepo/pkg/contract"
)

// Features that make paid calls.
const (
	FeatureDocGen = "docgen"
	// FeatureDocGenFast is an optional cheaper model for short code (docrouter); docgen serves it when unset.
	FeatureDocGenFast = "docgen_fast"
	FeatureQA         = "qa"
	FeatureDecode     = "decode"
	FeatureTriage     = "triage"
	FeatureEmbedding  = "embedding"
	FeatureSuggest    = "suggest"
)

// Route is a feature's configured provider and model.
type Route struct {
	Feature         string
	ProviderID      string
	ProviderKind    string
	Model           string
	MaxOutputTokens int
	ContextBudget   int
	Effort          string
	Temperature     *float64
	Fallback        *Route
	// RedactPII is the provider's "redact personal data" toggle (plan § 8.8).
	RedactPII bool
}

// ErrNoRoute means the operator has not configured a provider for a feature.
var ErrNoRoute = errors.New("no LLM provider is configured for this feature")

// Routes resolves routes (backed by the model_routes table).
type Routes interface {
	Route(ctx context.Context, feature string) (Route, error)
}

// Providers resolves provider IDs to adapters (built from decrypted provider config).
type Providers interface {
	LLM(ctx context.Context, providerID string) (ports.LLM, error)
	Embedder(ctx context.Context, providerID string) (ports.Embedder, error)
	DocGenerator(ctx context.Context, providerID string) (ports.DocGenerator, error)
}

// CallMeta attributes a call for the ledger and limits.
type CallMeta struct {
	RepoID  string
	UserID  string
	JobID   string
	IssueID string
	// Override is set only for an operator retry of a spend-blocked job.
	Override bool
}

// CallRecord is emitted after every call for metrics.
type CallRecord struct {
	Feature, ProviderKind, Model, Outcome string
	Usage                                 ports.TokenUsage
	Latency                               time.Duration
	Estimated                             bool
}

// Gateway enforces the spend guard around every provider call.
type Gateway struct {
	enforcer        *spendguard.Enforcer
	routes          Routes
	providers       Providers
	allowUnreported bool
	now             func() time.Time
	// OnCall observes every completed or failed call (metrics); optional.
	OnCall func(CallRecord)
	// PII redacts personal data for providers with the toggle on; nil uses the built-in patterns.
	PII *signals.Redactor
}

var defaultRedactor, _ = signals.NewRedactor(nil)

// protect prepares text for a provider (plan § 8.8): credentials are always scrubbed; personal data is
// redacted when the route's provider asks for it.
func (g *Gateway) protect(r Route, s string) string {
	s = signals.Scrub(s)
	if r.RedactPII {
		red := g.PII
		if red == nil {
			red = defaultRedactor
		}
		s = red.Redact(s)
	}
	return s
}

// New builds a gateway. allowUnreported mirrors spend.allow_unreported_usage.
func New(enf *spendguard.Enforcer, routes Routes, providers Providers, allowUnreported bool) *Gateway {
	return &Gateway{enforcer: enf, routes: routes, providers: providers, allowUnreported: allowUnreported, now: time.Now}
}

// Route returns the configured route for a feature (callers use ContextBudget to size context).
func (g *Gateway) Route(ctx context.Context, feature string) (Route, error) {
	r, err := g.routes.Route(ctx, feature)
	if err != nil {
		return r, err
	}
	r.Feature = feature
	return r, nil
}

// Chat runs a chat call on the feature's route, falling back once on a transient failure.
func (g *Gateway) Chat(ctx context.Context, feature string, meta CallMeta, req ports.ChatRequest) (ports.ChatResponse, error) {
	route, err := g.Route(ctx, feature)
	if err != nil {
		return ports.ChatResponse{}, err
	}
	resp, err := g.chatOn(ctx, route, meta, req)
	if _, transient := ports.AsTransient(err); transient && route.Fallback != nil {
		fb := *route.Fallback
		fb.Feature = feature
		if resp2, err2 := g.chatOn(ctx, fb, meta, req); err2 == nil || !isTransient(err2) {
			return resp2, err2
		}
	}
	return resp, err
}

func isTransient(err error) bool { _, ok := ports.AsTransient(err); return ok }

func (g *Gateway) chatOn(ctx context.Context, r Route, meta CallMeta, req ports.ChatRequest) (ports.ChatResponse, error) {
	llm, err := g.providers.LLM(ctx, r.ProviderID)
	if err != nil {
		return ports.ChatResponse{}, err
	}
	req.Model = r.Model
	msgs := make([]ports.ChatMessage, len(req.Messages))
	for i, m := range req.Messages {
		m.Content = g.protect(r, m.Content)
		msgs[i] = m
	}
	req.Messages = msgs
	if req.MaxOutputTokens <= 0 {
		req.MaxOutputTokens = r.MaxOutputTokens
	}
	if req.Effort == "" {
		req.Effort = r.Effort
	}
	if req.Temperature == nil {
		req.Temperature = r.Temperature
	}
	inEst := spendguard.EstimateTokens(req.System)
	for _, m := range req.Messages {
		inEst += spendguard.EstimateTokens(m.Content)
	}
	sreq := spendguard.Request{Feature: r.Feature, ProviderID: r.ProviderID, ProviderKind: r.ProviderKind, Model: r.Model,
		RepoID: meta.RepoID, InputTokens: inEst, OutputTokens: int64(req.MaxOutputTokens), Override: meta.Override}
	if _, err := g.enforcer.Check(ctx, sreq); err != nil {
		g.observe(r, "blocked", ports.TokenUsage{}, 0, false)
		return ports.ChatResponse{}, err
	}
	start := g.now()
	resp, callErr := llm.Chat(ctx, req)
	latency := g.now().Sub(start)
	usage, estimated := resp.Usage, false
	if !usage.Reported {
		if callErr == nil || isBillable(callErr) {
			usage = ports.TokenUsage{InputTokens: inEst, OutputTokens: spendguard.EstimateTokens(resp.Text)}
			estimated = true
		}
	}
	outcome := "ok"
	if callErr != nil && !isBillable(callErr) {
		outcome = "error"
	}
	model := resp.Model
	if model == "" {
		model = r.Model
	}
	if recErr := g.record(ctx, r, model, meta, usage, latency, estimated, outcome); recErr != nil && callErr == nil {
		// The call succeeded and was paid for; failing the caller would re-run (and re-pay) it. The
		// ledger write error is surfaced to observability instead.
		g.observe(r, "ledger_error", usage, latency, estimated)
	}
	g.observe(r, outcome, usage, latency, estimated)
	if callErr != nil {
		return resp, wrapCallErr(callErr)
	}
	return resp, nil
}

// isBillable reports provider errors that still consumed (and reported) tokens: refusals and truncation.
func isBillable(err error) bool {
	var re *ports.RefusalError
	var te *ports.TruncatedError
	return errors.As(err, &re) || errors.As(err, &te)
}

// wrapCallErr makes refusals permanent for the job (retrying the same request will not help) while keeping
// the typed cause reachable with errors.As.
func wrapCallErr(err error) error {
	var re *ports.RefusalError
	if errors.As(err, &re) {
		return ports.Permanent(err)
	}
	return err
}

func (g *Gateway) record(ctx context.Context, r Route, model string, meta CallMeta, u ports.TokenUsage, latency time.Duration, estimated bool, outcome string) error {
	return g.enforcer.Record(ctx, ports.UsageRecord{
		At: g.now(), Feature: r.Feature, ProviderID: r.ProviderID, ProviderKind: r.ProviderKind, Model: model,
		InputTokens: u.InputTokens + u.CacheReadTokens + u.CacheWriteTokens, OutputTokens: u.OutputTokens,
		CacheReadTokens: u.CacheReadTokens, CacheWriteTokens: u.CacheWriteTokens,
		LatencyMS: latency.Milliseconds(), Estimated: estimated, RepoID: meta.RepoID, UserID: meta.UserID,
		JobID: meta.JobID, IssueID: meta.IssueID, Outcome: outcome,
	})
}

func (g *Gateway) observe(r Route, outcome string, u ports.TokenUsage, latency time.Duration, estimated bool) {
	if g.OnCall != nil {
		g.OnCall(CallRecord{Feature: r.Feature, ProviderKind: r.ProviderKind, Model: r.Model, Outcome: outcome, Usage: u, Latency: latency, Estimated: estimated})
	}
}

// Check validates a decoded value beyond the schema (e.g. every requested chunk was documented). It
// returns human-readable problems that are shown to the model on the repair pass.
type Check func() []string

// JSONResult reports a ChatJSON call: usage summed over every attempt (retry and repair included), the
// model that answered, and the last raw reply (kept when the output never validated).
type JSONResult struct {
	Usage ports.TokenUsage
	Model string
	Raw   string
}

func (r *JSONResult) add(resp ports.ChatResponse) {
	r.Usage.InputTokens += resp.Usage.InputTokens
	r.Usage.OutputTokens += resp.Usage.OutputTokens
	r.Usage.CacheReadTokens += resp.Usage.CacheReadTokens
	r.Usage.CacheWriteTokens += resp.Usage.CacheWriteTokens
	r.Usage.Reported = r.Usage.Reported || resp.Usage.Reported
	r.Model, r.Raw = resp.Model, resp.Text
}

// ChatJSON asks for a JSON object matching schema, decodes it into out, and runs check. When the output
// fails the schema or the check, it makes exactly one repair call that shows the model its problems; a
// second failure is a permanent *ports.SchemaError. Truncated JSON is retried once with double the
// output budget.
func (g *Gateway) ChatJSON(ctx context.Context, feature string, meta CallMeta, req ports.ChatRequest, schema contract.Schema, out any, check Check) error {
	_, err := g.ChatJSONResult(ctx, feature, meta, req, schema, out, check)
	return err
}

// ChatJSONResult is ChatJSON that also reports usage, model, and the last raw reply.
func (g *Gateway) ChatJSONResult(ctx context.Context, feature string, meta CallMeta, req ports.ChatRequest, schema contract.Schema, out any, check Check) (JSONResult, error) {
	var res JSONResult
	req.JSONSchema = schema
	resp, err := g.Chat(ctx, feature, meta, req)
	var te *ports.TruncatedError
	if errors.As(err, &te) {
		route, rerr := g.Route(ctx, feature)
		if rerr != nil {
			return res, rerr
		}
		max := te.MaxOutputTokens
		if max <= 0 {
			max = route.MaxOutputTokens
		}
		req.MaxOutputTokens = min(max*2, 64000)
		resp, err = g.Chat(ctx, feature, meta, req)
	}
	if err != nil {
		return res, err
	}
	res.add(resp)
	probs := decode(resp.Text, schema, out, check)
	if len(probs) == 0 {
		return res, nil
	}
	repair := req
	repair.Messages = append(append([]ports.ChatMessage{}, req.Messages...),
		ports.ChatMessage{Role: "assistant", Content: resp.Text},
		ports.ChatMessage{Role: "user", Content: "Your previous reply did not satisfy the required JSON format. Problems:\n- " +
			strings.Join(probs, "\n- ") + "\n\nReply with only the corrected JSON object."})
	resp, err = g.Chat(ctx, feature, meta, repair)
	if err != nil {
		return res, err
	}
	res.add(resp)
	if probs := decode(resp.Text, schema, out, check); len(probs) > 0 {
		return res, ports.Permanent(&ports.SchemaError{Problems: probs})
	}
	return res, nil
}

func decode(text string, schema contract.Schema, out any, check Check) []string {
	raw := contract.ExtractJSON(text)
	if probs := contract.Validate(schema, raw); len(probs) > 0 {
		return probs
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return []string{"$: " + err.Error()}
	}
	if check != nil {
		return check()
	}
	return nil
}

// Embed embeds texts on the embedding route, batching by the provider's limit; every batch is
// spend-guarded and recorded.
func (g *Gateway) Embed(ctx context.Context, meta CallMeta, texts []string) ([][]float32, Route, error) {
	route, err := g.Route(ctx, FeatureEmbedding)
	if err != nil {
		return nil, route, err
	}
	emb, err := g.providers.Embedder(ctx, route.ProviderID)
	if err != nil {
		return nil, route, err
	}
	batch := emb.MaxBatch()
	if batch <= 0 {
		batch = 64
	}
	out := make([][]float32, 0, len(texts))
	for start := 0; start < len(texts); start += batch {
		end := min(start+batch, len(texts))
		part := make([]string, end-start)
		for i, t := range texts[start:end] {
			part[i] = g.protect(route, t)
		}
		est := spendguard.EstimateTokens(part...)
		if _, err := g.enforcer.Check(ctx, spendguard.Request{Feature: FeatureEmbedding, ProviderID: route.ProviderID,
			ProviderKind: route.ProviderKind, Model: route.Model, RepoID: meta.RepoID, InputTokens: est, Override: meta.Override}); err != nil {
			g.observe(route, "blocked", ports.TokenUsage{}, 0, false)
			return nil, route, err
		}
		t0 := g.now()
		res, err := emb.Embed(ctx, route.Model, part)
		lat := g.now().Sub(t0)
		if err != nil {
			_ = g.record(ctx, route, route.Model, meta, ports.TokenUsage{}, lat, false, "error")
			g.observe(route, "error", ports.TokenUsage{}, lat, false)
			return nil, route, err
		}
		usage, estimated := res.Usage, false
		if !usage.Reported {
			usage, estimated = ports.TokenUsage{InputTokens: est}, true
		}
		_ = g.record(ctx, route, route.Model, meta, usage, lat, estimated, "ok")
		g.observe(route, "ok", usage, lat, estimated)
		if len(res.Vectors) != len(part) {
			return nil, route, ports.Transient(fmt.Errorf("embedder returned %d vectors for %d texts", len(res.Vectors), len(part)))
		}
		out = append(out, res.Vectors...)
	}
	return out, route, nil
}

// GenerateDocs runs an external documentation engine on the docgen route: spend-guarded, with one repair
// pass on schema errors, and usage recorded (estimated and flagged when the engine reports none).
func (g *Gateway) GenerateDocs(ctx context.Context, meta CallMeta, task contract.DocGenTask) (contract.DocGenResult, error) {
	route, err := g.Route(ctx, FeatureDocGen)
	if err != nil {
		return contract.DocGenResult{}, err
	}
	eng, err := g.providers.DocGenerator(ctx, route.ProviderID)
	if err != nil {
		return contract.DocGenResult{}, err
	}
	if !eng.ReportsUsage() && !g.allowUnreported {
		return contract.DocGenResult{}, ports.Permanent(errors.New(
			"the docgen engine does not report token usage, so the spend guard cannot measure it; set spend.allow_unreported_usage: true to run it anyway"))
	}
	perChunk := route.MaxOutputTokens
	if perChunk <= 0 {
		perChunk = 2000
	}
	task.MaxOutputTokensPerChunk = perChunk
	task.Model = route.Model
	task.Context = g.protect(route, task.Context)
	inEst := spendguard.EstimateTokens(task.Context)
	outEst := int64(perChunk * max(1, len(task.ChunksToGenerate)))
	run := func(t contract.DocGenTask) (contract.DocGenResult, error) {
		if _, err := g.enforcer.Check(ctx, spendguard.Request{Feature: FeatureDocGen, ProviderID: route.ProviderID, ProviderKind: route.ProviderKind,
			Model: route.Model, RepoID: meta.RepoID, InputTokens: inEst, OutputTokens: outEst, Override: meta.Override}); err != nil {
			g.observe(route, "blocked", ports.TokenUsage{}, 0, false)
			return contract.DocGenResult{}, err
		}
		t0 := g.now()
		res, err := eng.Generate(ctx, t)
		lat := g.now().Sub(t0)
		usage, estimated, model := ports.TokenUsage{}, false, route.Model
		switch {
		case res.Usage != nil:
			usage = ports.TokenUsage{InputTokens: res.Usage.InputTokens, OutputTokens: res.Usage.OutputTokens, Reported: true}
			if res.Usage.Model != "" {
				model = res.Usage.Model
			}
		default: // unknown actual spend: record the estimate so the ledger never under-counts
			usage, estimated = ports.TokenUsage{InputTokens: inEst, OutputTokens: outEst}, true
		}
		outcome := "ok"
		if err != nil {
			outcome = "error"
		}
		_ = g.record(ctx, route, model, meta, usage, lat, estimated, outcome)
		g.observe(route, outcome, usage, lat, estimated)
		return res, err
	}
	res, err := run(task)
	var se *ports.SchemaError
	if errors.As(err, &se) {
		task.RepairErrors = se.Problems
		res, err = run(task)
		if errors.As(err, &se) {
			return res, ports.Permanent(err)
		}
	}
	return res, err
}
