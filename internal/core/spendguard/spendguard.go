// Package spendguard enforces spend ceilings before any paid call (plan § 8.13). Estimates drive the
// decision; actual usage drives the ledger, so estimation error never compounds. Every paid call in the
// Hub goes through Enforcer.Check — there is no bypass flag.
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

// Cost prices a call; ok is false when no price is known.
func (g *Guard) Cost(providerKind, model, feature string, in, out int64) (float64, bool) {
	p, ok := g.prices[PriceKey(providerKind, model)]
	if !ok {
		return 0, false
	}
	if feature == "embedding" {
		return float64(in+out) * p.EmbedPerMTok / 1e6, true
	}
	return float64(in)*p.InputPerMTok/1e6 + float64(out)*p.OutputPerMTok/1e6, true
}

// CostUsage prices actual usage, with prompt-cache reads and writes at their own prices. in is all
// input, cache tokens included.
func (g *Guard) CostUsage(providerKind, model, feature string, in, out, cacheRead, cacheWrite int64) (float64, bool) {
	p, ok := g.prices[PriceKey(providerKind, model)]
	if !ok {
		return 0, false
	}
	if feature == "embedding" || (cacheRead == 0 && cacheWrite == 0) {
		return g.Cost(providerKind, model, feature, in, out)
	}
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

// Applicable returns the limits that count a request.
func (g *Guard) Applicable(r Request) []Limit {
	var out []Limit
	for _, l := range g.limits {
		switch l.Scope {
		case ScopeGlobal:
		case ScopeFeature:
			if l.ScopeKey != r.Feature {
				continue
			}
		case ScopeProvider:
			if l.ScopeKey != r.ProviderID {
				continue
			}
		case ScopeRepo:
			if r.RepoID == "" || l.ScopeKey != r.RepoID {
				continue
			}
		default:
			continue
		}
		out = append(out, l)
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

// Evaluate decides a request given what each applicable limit has already spent (keyed by limit ID).
// Pure: no I/O, no clock.
func (g *Guard) Evaluate(r Request, spent map[string]ports.SpendTotals) Decision {
	est := r.Tokens()
	cost, priced := g.Cost(r.ProviderKind, r.Model, r.Feature, r.InputTokens, r.OutputTokens)
	d := Decision{Allow: true, EstimatedTokens: est, EstimatedCostUSD: cost, Utilization: map[string]float64{}}
	for _, l := range g.Applicable(r) {
		l := l
		s := spent[l.ID]
		if l.MaxTokens > 0 {
			d.Utilization[l.ID] = float64(s.Tokens+est) / float64(l.MaxTokens)
		}
		var breach string
		switch {
		case l.MaxTokens > 0 && est > l.MaxTokens:
			breach = fmt.Sprintf("this call alone is estimated at %d tokens, above the %s ceiling of %d", est, l, l.MaxTokens)
		case l.MaxTokens > 0 && s.Tokens+est > l.MaxTokens:
			breach = fmt.Sprintf("%d tokens spent + %d estimated would exceed the %s ceiling of %d", s.Tokens, est, l, l.MaxTokens)
		case l.MaxCostUSD > 0 && !priced:
			d.Warnings = append(d.Warnings, fmt.Sprintf("cost ceiling %s cannot be evaluated: no price for %s/%s in the cost table", l, r.ProviderKind, r.Model))
		case l.MaxCostUSD > 0 && s.CostUSD+cost > l.MaxCostUSD:
			breach = fmt.Sprintf("$%.4f spent + $%.4f estimated would exceed the %s ceiling of $%.2f", s.CostUSD, cost, l, l.MaxCostUSD)
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

// Enforcer runs the guard against the ledger and records actual usage. It is the only way the Hub makes
// a paid call: Check before, Record after.
type Enforcer struct {
	mu      sync.RWMutex
	guard   *Guard
	ledger  ports.SpendLedger
	alerter ports.Alerter
	now     func() time.Time
	// OnDecision observes every decision (metrics, logs); optional.
	OnDecision func(Request, Decision)
}

// NewEnforcer wires a guard to the ledger. alerter may be nil.
func NewEnforcer(g *Guard, ledger ports.SpendLedger, alerter ports.Alerter) *Enforcer {
	return &Enforcer{guard: g, ledger: ledger, alerter: alerter, now: time.Now}
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

// Check evaluates a request. It returns *ports.SpendBlockedError when a ceiling would be breached; ledger
// failures are transient errors (never a silent allow).
func (e *Enforcer) Check(ctx context.Context, r Request) (Decision, error) {
	g := e.Guard()
	now := e.now()
	spent := map[string]ports.SpendTotals{}
	for _, l := range g.Applicable(r) {
		t, err := e.ledger.Spent(ctx, Filter(l, now))
		if err != nil {
			return Decision{}, ports.Transient(fmt.Errorf("read spend ledger for %s: %w", l, err))
		}
		spent[l.ID] = t
	}
	d := g.Evaluate(r, spent)
	if e.OnDecision != nil {
		e.OnDecision(r, d)
	}
	if d.Allow {
		return d, nil
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
	return d, &ports.SpendBlockedError{Scope: d.Limit.String(), EstimatedTokens: d.EstimatedTokens, Reason: d.Reason}
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
