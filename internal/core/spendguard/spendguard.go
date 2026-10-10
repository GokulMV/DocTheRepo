// Package spendguard enforces spend ceilings before any paid call (plan § 8.13). Estimates drive the
// decision; actual usage drives the ledger, so estimation error never compounds. Every paid call in the
// Hub goes through Enforcer.Reserve (or Check) — there is no bypass flag.
//
// A ceiling is not exceeded by calls running at once: each call reserves its worst-case cost (estimated
// input plus the full max_output_tokens) until it has been recorded, and the check counts what is spent,
// what calls in flight have reserved, and this call against the ceiling less a safety buffer (BufferUSD).
// Reservations live in the process: calls made by one Hub process (a docs run, a job) never overshoot
// together; calls made at the same moment by different replicas see each other only once recorded, and
// the buffer is what absorbs that.
package spendguard

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"sync"
	"time"

	"github.com/GokulMV/DocTheRepo/internal/ports"
)

// Scope is what a limit applies to.
type Scope string

const (
	ScopeGlobal   Scope = "global"
	ScopeFeature  Scope = "feature"
	ScopeProvider Scope = "provider"
	ScopeRepo     Scope = "repo"
)

// Window is a limit's sliding time window.
type Window string

const (
	WindowDay   Window = "day"   // sliding 24 hours
	WindowMonth Window = "month" // sliding 30 days
)

// Duration returns the window length.
func (w Window) Duration() time.Duration {
	if w == WindowMonth {
		return 30 * 24 * time.Hour
	}
	return 24 * time.Hour
}

// Limit is one ceiling. A zero MaxTokens and zero MaxCostUSD means unlimited, which is only accepted
// with an explicit acknowledgement (spend.allow_unlimited) — unlimited is never reached by omission.
type Limit struct {
	ID         string
	Scope      Scope
	ScopeKey   string // feature name, provider id, or repo id; empty for global
	Window     Window
	MaxTokens  int64
	MaxCostUSD float64
	Alert      bool // on_breach = block_and_alert
	AlertURL   string
}

func (l Limit) unlimited() bool { return l.MaxTokens <= 0 && l.MaxCostUSD <= 0 }

func (l Limit) String() string {
	k := string(l.Scope)
	if l.ScopeKey != "" {
		k += ":" + l.ScopeKey
	}
	return k + "/" + string(l.Window)
}

// Price is per-million-token pricing for one provider kind + model (operator-maintained cost table).
type Price struct {
	InputPerMTok  float64
	OutputPerMTok float64
	EmbedPerMTok  float64
	// CacheReadPerMTok and CacheWritePerMTok price prompt-cache tokens (nil: as plain input).
	CacheReadPerMTok  *float64
	CacheWritePerMTok *float64
	// LongPromptThreshold > 0 with Long set: a prompt (all input, cache reads and writes included) over
	// this many tokens is priced at Long for the whole request, output and cache too. Embeddings are not.
	LongPromptThreshold int64
	Long                *Price
}

// forPrompt returns the prices that apply to a prompt of this many tokens.
func (p Price) forPrompt(promptTokens int64) Price {
	if p.Long != nil && p.LongPromptThreshold > 0 && promptTokens > p.LongPromptThreshold {
		return *p.Long
	}
	return p
}

// Request describes a paid call about to be made.
type Request struct {
	Feature      string // docgen | qa | decode | triage | embedding | suggest
	ProviderID   string
	ProviderKind string
	Model        string
	RepoID       string
	InputTokens  int64 // estimated
	OutputTokens int64 // estimated maximum (max_output_tokens)
	// Override is set only by an explicit operator retry of a spend-blocked job; it is audited upstream.
	Override bool
	// Budget is an extra dollar cap this call counts against (optional).
	Budget *Budget
}

// Budget is a dollar cap enforced on every call that carries it, on top of the configured limits: a
// repository's monthly docs budget, which is kept outside spend_limits. Calls carrying the same Key share
// reservations, so a run writing several documents at once stays under it.
type Budget struct {
	Key    string  // names the cap in a block, e.g. repo_docs:<repo id>/month
	CapUSD float64 // 0: no cap
	// Spent is what has been recorded against the cap so far.
	Spent func(ctx context.Context) (float64, error)
}

// DefaultBufferPct is the safety buffer kept under every dollar ceiling, in percent of the ceiling.
const DefaultBufferPct = 5.0

// BufferUSD is the safety buffer kept under a dollar ceiling: pct percent of it, at least $0.01 (pct > 0),
// and never more than half of it. It absorbs what the pre-call estimate cannot see (a prompt that
// tokenizes denser than chars/4, prompt-cache writes, calls by other replicas).
func BufferUSD(capUSD, pct float64) float64 {
	if capUSD <= 0 || pct <= 0 {
		return 0
	}
	return math.Min(math.Max(capUSD*pct/100, 0.01), capUSD/2)
}

// Tokens is the estimated total.
func (r Request) Tokens() int64 { return r.InputTokens + r.OutputTokens }

// Decision is the guard's verdict.
type Decision struct {
	Allow            bool
	Limit            *Limit
	Reason           string
	EstimatedTokens  int64
	EstimatedCostUSD float64
	// Utilization is (spent + estimate) / max per evaluated limit id, for the utilization gauge.
	Utilization map[string]float64
	// Warnings are non-blocking problems, e.g. a cost limit that cannot be evaluated without a price.
	Warnings []string
}

// Guard holds validated limits and prices. It performs no I/O.
type Guard struct {
	limits []Limit
	prices map[string]Price
}

// ErrUnlimitedNotAcknowledged is returned when a limit is unlimited without spend.allow_unlimited.
var ErrUnlimitedNotAcknowledged = errors.New("a spend limit of 0 means unlimited and requires spend.allow_unlimited: true")

// New validates limits. At least one limit must exist (a global ceiling is seeded by migration).
func New(limits []Limit, prices map[string]Price, allowUnlimited bool) (*Guard, error) {
	if len(limits) == 0 && !allowUnlimited {
		return nil, fmt.Errorf("no spend limits configured: %w", ErrUnlimitedNotAcknowledged)
	}
	for _, l := range limits {
		if l.MaxTokens < 0 || l.MaxCostUSD < 0 || math.IsNaN(l.MaxCostUSD) {
			return nil, fmt.Errorf("limit %s: ceilings must be >= 0", l)
		}
		if l.unlimited() && !allowUnlimited {
			return nil, fmt.Errorf("limit %s: %w", l, ErrUnlimitedNotAcknowledged)
		}
		if l.Scope != ScopeGlobal && l.ScopeKey == "" {
			return nil, fmt.Errorf("limit %s: scope %s needs a scope key", l, l.Scope)
		}
		if l.Window != WindowDay && l.Window != WindowMonth {
			return nil, fmt.Errorf("limit %s: window must be day or month", l)
		}
	}
	if prices == nil {
		prices = map[string]Price{}
	}
	return &Guard{limits: limits, prices: prices}, nil
}

// PriceKey keys the price table.
func PriceKey(providerKind, model string) string { return providerKind + "|" + model }

// Cost prices a call; ok is false when no price is known. in is the whole prompt, so a prompt over the
// model's long-prompt threshold is priced at the long rates (pre-call estimates included).
func (g *Guard) Cost(providerKind, model, feature string, in, out int64) (float64, bool) {
	p, ok := g.prices[PriceKey(providerKind, model)]
	if !ok {
		return 0, false
	}
	if feature == "embedding" {
		return float64(in+out) * p.EmbedPerMTok / 1e6, true
	}
	p = p.forPrompt(in)
	return float64(in)*p.InputPerMTok/1e6 + float64(out)*p.OutputPerMTok/1e6, true
}

// CostUsage prices actual usage, with prompt-cache reads and writes at their own prices. in is all
// input, cache tokens included, and is the prompt size the long-prompt threshold is checked against.
func (g *Guard) CostUsage(providerKind, model, feature string, in, out, cacheRead, cacheWrite int64) (float64, bool) {
	p, ok := g.prices[PriceKey(providerKind, model)]
	if !ok {
		return 0, false
	}
	if feature == "embedding" || (cacheRead == 0 && cacheWrite == 0) {
		return g.Cost(providerKind, model, feature, in, out)
	}
	p = p.forPrompt(in)
	rate := func(r *float64) float64 {
		if r == nil {
			return p.InputPerMTok
		}
		return *r
	}
	plain := in - cacheRead - cacheWrite
	if plain < 0 {
		plain = 0
	}
	return (float64(plain)*p.InputPerMTok + float64(cacheRead)*rate(p.CacheReadPerMTok) +
		float64(cacheWrite)*rate(p.CacheWritePerMTok) + float64(out)*p.OutputPerMTok) / 1e6, true
}

// counts reports whether the limit counts a request.
func (l Limit) counts(r Request) bool {
	switch l.Scope {
	case ScopeGlobal:
		return true
	case ScopeFeature:
		return l.ScopeKey == r.Feature
	case ScopeProvider:
		return l.ScopeKey == r.ProviderID
	case ScopeRepo:
		return r.RepoID != "" && l.ScopeKey == r.RepoID
	}
	return false
}

// Applicable returns the limits that count a request.
func (g *Guard) Applicable(r Request) []Limit {
	var out []Limit
	for _, l := range g.limits {
		if l.counts(r) {
			out = append(out, l)
		}
	}
	return out
}

// Filter returns the ledger filter for a limit's window ending at now.
func Filter(l Limit, now time.Time) ports.SpendFilter {
	f := ports.SpendFilter{Since: now.Add(-l.Window.Duration())}
	switch l.Scope {
	case ScopeFeature:
		f.Feature = l.ScopeKey
	case ScopeProvider:
		f.ProviderID = l.ScopeKey
	case ScopeRepo:
		f.RepoID = l.ScopeKey
	}
	return f
}

// Evaluate decides a request given what each applicable limit has already spent (keyed by limit ID),
// keeping the default buffer under dollar ceilings. Pure: no I/O, no clock.
func (g *Guard) Evaluate(r Request, spent map[string]ports.SpendTotals) Decision {
	return g.evaluate(r, spent, nil, DefaultBufferPct)
}

// evaluate is Evaluate with what calls in flight have reserved (keyed by limit ID) and the buffer in
// percent. Token ceilings are counted exactly (a reservation already takes the whole output budget);
// dollar ceilings keep BufferUSD free.
func (g *Guard) evaluate(r Request, spent, reserved map[string]ports.SpendTotals, bufferPct float64) Decision {
	est := r.Tokens()
	cost, priced := g.Cost(r.ProviderKind, r.Model, r.Feature, r.InputTokens, r.OutputTokens)
	d := Decision{Allow: true, EstimatedTokens: est, EstimatedCostUSD: cost, Utilization: map[string]float64{}}
	for _, l := range g.Applicable(r) {
		l := l
		s, res := spent[l.ID], reserved[l.ID]
		if l.MaxTokens > 0 {
			d.Utilization[l.ID] = float64(s.Tokens+res.Tokens+est) / float64(l.MaxTokens)
		}
		buf := BufferUSD(l.MaxCostUSD, bufferPct)
		var breach string
		switch {
		case l.MaxTokens > 0 && est > l.MaxTokens:
			breach = fmt.Sprintf("this call alone is estimated at %d tokens, above the %s ceiling of %d", est, l, l.MaxTokens)
		case l.MaxTokens > 0 && s.Tokens+res.Tokens+est > l.MaxTokens:
			breach = fmt.Sprintf("%d tokens spent%s + %d estimated would exceed the %s ceiling of %d", s.Tokens, reservedTokens(res.Tokens), est, l, l.MaxTokens)
		case l.MaxCostUSD > 0 && !priced:
			d.Warnings = append(d.Warnings, fmt.Sprintf("cost ceiling %s cannot be evaluated: no price for %s/%s in the cost table", l, r.ProviderKind, r.Model))
		case l.MaxCostUSD > 0 && s.CostUSD+res.CostUSD+cost > l.MaxCostUSD-buf:
			breach = costBreach(s.CostUSD, res.CostUSD, cost, l.String(), l.MaxCostUSD, buf)
		}
		if breach == "" {
			continue
		}
		if r.Override {
			d.Warnings = append(d.Warnings, "ceiling overridden by operator: "+breach)
			continue
		}
		if d.Allow { // report the first breached limit
			d.Allow, d.Limit, d.Reason = false, &l, breach
		}
	}
	return d
}

func reservedTokens(n int64) string {
	if n == 0 {
		return ""
	}
	return fmt.Sprintf(" + %d reserved by calls in flight", n)
}

func costBreach(spent, reserved, cost float64, scope string, capUSD, buf float64) string {
	msg := fmt.Sprintf("$%.4f spent", spent)
	if reserved > 0 {
		msg += fmt.Sprintf(" + $%.4f reserved by calls in flight", reserved)
	}
	msg += fmt.Sprintf(" + $%.4f estimated would exceed the %s ceiling of $%.2f", cost, scope, capUSD)
	if buf > 0 {
		msg += fmt.Sprintf(" less its $%.2f safety buffer", buf)
	}
	return msg
}

// Enforcer runs the guard against the ledger and records actual usage. It is the only way the Hub makes
// a paid call: Check before, Record after.
type Enforcer struct {
	mu        sync.RWMutex
	guard     *Guard
	bufferPct float64
	ledger    ports.SpendLedger
	alerter   ports.Alerter
	now       func() time.Time
	// OnDecision observes every decision (metrics, logs); optional.
	OnDecision func(Request, Decision)

	// checkMu makes "read spent, add reservations, decide, reserve" one step, so two calls never both
	// take the last of a ceiling. resMu guards the reservations alone: releasing never waits on the ledger.
	checkMu  sync.Mutex
	resMu    sync.Mutex
	reserved map[uint64]reservation
	nextID   uint64
}

// reservation is a call in flight: its request and worst-case tokens and cost.
type reservation struct {
	req    Request
	tokens int64
	cost   float64
}

// NewEnforcer wires a guard to the ledger with the default buffer. alerter may be nil.
func NewEnforcer(g *Guard, ledger ports.SpendLedger, alerter ports.Alerter) *Enforcer {
	return &Enforcer{guard: g, bufferPct: DefaultBufferPct, ledger: ledger, alerter: alerter, now: time.Now,
		reserved: map[uint64]reservation{}}
}

// SetBufferPct sets the safety buffer kept under every dollar ceiling, in percent (0 to 50; 0 turns it off).
func (e *Enforcer) SetBufferPct(pct float64) error {
	if pct < 0 || pct > 50 || math.IsNaN(pct) {
		return fmt.Errorf("spend buffer: want 0 to 50 percent, got %v", pct)
	}
	e.mu.Lock()
	e.bufferPct = pct
	e.mu.Unlock()
	return nil
}

// BufferPct is the safety buffer in percent.
func (e *Enforcer) BufferPct() float64 {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.bufferPct
}

// SetGuard swaps in new limits/prices (after an operator edits them) without a restart.
func (e *Enforcer) SetGuard(g *Guard) {
	e.mu.Lock()
	e.guard = g
	e.mu.Unlock()
}

// Guard returns the current guard.
func (e *Enforcer) Guard() *Guard {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.guard
}

// Check evaluates a request without holding a reservation (see Reserve). It returns
// *ports.SpendBlockedError when a ceiling would be breached; ledger failures are transient errors (never a
// silent allow).
func (e *Enforcer) Check(ctx context.Context, r Request) (Decision, error) {
	d, release, err := e.Reserve(ctx, r)
	release()
	return d, err
}

// Reserve evaluates a request and, when it is allowed, holds its worst-case tokens and cost against every
// ceiling it counts against until release is called. Call release once the actual usage is recorded, on
// every path (defer it); it is never nil and is safe to call more than once. Blocks and ledger failures
// are returned as by Check, with nothing reserved.
func (e *Enforcer) Reserve(ctx context.Context, r Request) (Decision, func(), error) {
	noop := func() {}
	g, pct := e.Guard(), e.BufferPct()
	e.checkMu.Lock()
	defer e.checkMu.Unlock()
	// Reservations are read before the ledger: a call released since was recorded before it released, so
	// its spend is in what the ledger returns (counting a call twice only errs on the safe side).
	inFlight := e.inFlight()
	now := e.now()
	spent, reserved := map[string]ports.SpendTotals{}, map[string]ports.SpendTotals{}
	for _, l := range g.Applicable(r) {
		t, err := e.ledger.Spent(ctx, Filter(l, now))
		if err != nil {
			return Decision{}, noop, ports.Transient(fmt.Errorf("read spend ledger for %s: %w", l, err))
		}
		spent[l.ID] = t
		var res ports.SpendTotals
		for _, f := range inFlight {
			if l.counts(f.req) {
				res.Tokens += f.tokens
				res.CostUSD += f.cost
			}
		}
		reserved[l.ID] = res
	}
	d := g.evaluate(r, spent, reserved, pct)
	if d.Allow {
		if err := e.checkBudget(ctx, g, r, &d, inFlight, pct); err != nil {
			return Decision{}, noop, err
		}
	}
	if e.OnDecision != nil {
		e.OnDecision(r, d)
	}
	if !d.Allow {
		if d.Limit == nil { // the request's budget
			return d, noop, &ports.SpendBlockedError{Scope: r.Budget.Key, EstimatedTokens: d.EstimatedTokens, Reason: d.Reason}
		}
		if d.Limit.Alert && e.alerter != nil {
			s := spent[d.Limit.ID]
			// Best effort: a failing alert webhook must not turn a block into an allow or a job failure.
			_ = e.alerter.Alert(ctx, ports.SpendAlert{
				LimitID: d.Limit.ID, Scope: string(d.Limit.Scope), ScopeKey: d.Limit.ScopeKey, Window: string(d.Limit.Window),
				EstimatedTokens: d.EstimatedTokens, SpentTokens: s.Tokens, MaxTokens: d.Limit.MaxTokens, Reason: d.Reason,
				At: now, URL: d.Limit.AlertURL,
			})
		}
		return d, noop, &ports.SpendBlockedError{Scope: d.Limit.String(), EstimatedTokens: d.EstimatedTokens, Reason: d.Reason}
	}
	e.resMu.Lock()
	e.nextID++
	id := e.nextID
	e.reserved[id] = reservation{req: r, tokens: d.EstimatedTokens, cost: d.EstimatedCostUSD}
	e.resMu.Unlock()
	var once sync.Once
	return d, func() {
		once.Do(func() {
			e.resMu.Lock()
			delete(e.reserved, id)
			e.resMu.Unlock()
		})
	}, nil
}

// checkBudget applies the request's budget, if any, to an allowed decision.
func (e *Enforcer) checkBudget(ctx context.Context, g *Guard, r Request, d *Decision, inFlight []reservation, pct float64) error {
	b := r.Budget
	if b == nil || b.CapUSD <= 0 || b.Spent == nil {
		return nil
	}
	if _, priced := g.Cost(r.ProviderKind, r.Model, r.Feature, 0, 0); !priced {
		d.Warnings = append(d.Warnings, fmt.Sprintf("budget %s cannot be evaluated: no price for %s/%s in the cost table", b.Key, r.ProviderKind, r.Model))
		return nil
	}
	spent, err := b.Spent(ctx)
	if err != nil {
		return ports.Transient(fmt.Errorf("read spend for %s: %w", b.Key, err))
	}
	var reserved float64
	for _, f := range inFlight {
		if f.req.Budget != nil && f.req.Budget.Key == b.Key {
			reserved += f.cost
		}
	}
	buf := BufferUSD(b.CapUSD, pct)
	if spent+reserved+d.EstimatedCostUSD <= b.CapUSD-buf {
		return nil
	}
	breach := costBreach(spent, reserved, d.EstimatedCostUSD, b.Key, b.CapUSD, buf)
	if r.Override {
		d.Warnings = append(d.Warnings, "budget overridden by operator: "+breach)
		return nil
	}
	d.Allow, d.Reason = false, breach
	return nil
}

// inFlight copies the current reservations.
func (e *Enforcer) inFlight() []reservation {
	e.resMu.Lock()
	defer e.resMu.Unlock()
	out := make([]reservation, 0, len(e.reserved))
	for _, r := range e.reserved {
		out = append(out, r)
	}
	return out
}

// Reserved is what calls in flight hold right now, in tokens and dollars (all ceilings together).
func (e *Enforcer) Reserved() ports.SpendTotals {
	var t ports.SpendTotals
	for _, r := range e.inFlight() {
		t.Tokens += r.tokens
		t.CostUSD += r.cost
	}
	return t
}

// Record writes actual usage. When the provider reported no usage, pass the estimate with Estimated set.
func (e *Enforcer) Record(ctx context.Context, u ports.UsageRecord) error {
	if u.At.IsZero() {
		u.At = e.now()
	}
	if u.CostUSD == 0 {
		if c, ok := e.Guard().CostUsage(u.ProviderKind, u.Model, u.Feature, u.InputTokens, u.OutputTokens, u.CacheReadTokens, u.CacheWriteTokens); ok {
			u.CostUSD = c
		}
	}
	if u.Outcome == "" {
		u.Outcome = "ok"
	}
	if err := e.ledger.Record(ctx, u); err != nil {
		return fmt.Errorf("record usage: %w", err)
	}
	return nil
}

// EstimateTokens approximates tokens as ceil(chars/4); deliberately simple and provider-agnostic.
func EstimateTokens(texts ...string) int64 {
	var n int
	for _, t := range texts {
		n += len(t)
	}
	return int64((n + 3) / 4)
}

// ParseScope validates a scope string from the API/DB.
func ParseScope(s string) (Scope, error) {
	switch Scope(strings.ToLower(s)) {
	case ScopeGlobal, ScopeFeature, ScopeProvider, ScopeRepo:
		return Scope(strings.ToLower(s)), nil
	}
	return "", fmt.Errorf("unknown spend scope %q", s)
}
