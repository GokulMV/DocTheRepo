package store

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/GokulMV/DocTheRepo/internal/core/llmgateway"
	"github.com/GokulMV/DocTheRepo/internal/core/spendguard"
	"github.com/GokulMV/DocTheRepo/internal/ports"
	"github.com/GokulMV/DocTheRepo/internal/store/gen"
)

// Ledger implements ports.SpendLedger on usage_events.
type Ledger struct{ s *Store }

// NewLedger returns the Postgres spend ledger.
func NewLedger(s *Store) *Ledger { return &Ledger{s: s} }

// Spent sums consumption matching a limit's filter.
func (l *Ledger) Spent(ctx context.Context, f ports.SpendFilter) (ports.SpendTotals, error) {
	p := gen.SpentSinceParams{Since: f.Since, ProviderID: strPtr(f.ProviderID), RepoID: strPtr(f.RepoID)}
	if f.Feature != "" {
		ft := gen.LlmFeature(f.Feature)
		p.Feature = &ft
	}
	row, err := l.s.Q.SpentSince(ctx, p)
	if err != nil {
		return ports.SpendTotals{}, fmt.Errorf("spend ledger: %w", err)
	}
	return ports.SpendTotals{Tokens: row.Tokens, CostUSD: row.CostUsd}, nil
}

// Record inserts one usage event.
func (l *Ledger) Record(ctx context.Context, r ports.UsageRecord) error {
	outcome := r.Outcome
	if outcome == "" {
		outcome = "ok"
	}
	return l.s.Q.InsertUsageEvent(ctx, gen.InsertUsageEventParams{
		ID: ports.NewID(), At: r.At, Feature: gen.LlmFeature(r.Feature), ProviderID: strPtr(r.ProviderID),
		ProviderKind: gen.LlmProviderKind(r.ProviderKind), Model: r.Model, InputTokens: r.InputTokens,
		OutputTokens: r.OutputTokens, CostUsd: r.CostUSD, LatencyMs: int32(r.LatencyMS), Cached: r.Cached,
		Estimated: r.Estimated, RepoID: strPtr(r.RepoID), UserID: strPtr(r.UserID), JobID: strPtr(r.JobID),
		IssueID: strPtr(r.IssueID), Outcome: gen.UsageOutcome(outcome),
	})
}

// Routes implements llmgateway.Routes on model_routes.
type Routes struct{ s *Store }

// NewRoutes returns the route store.
func NewRoutes(s *Store) *Routes { return &Routes{s: s} }

// Route loads a feature's route and fallback. A disabled provider behaves as "no route".
func (r *Routes) Route(ctx context.Context, feature string) (llmgateway.Route, error) {
	row, err := r.s.Q.GetRoute(ctx, gen.LlmFeature(feature))
	if IsNoRows(err) {
		return llmgateway.Route{}, llmgateway.ErrNoRoute
	}
	if err != nil {
		return llmgateway.Route{}, fmt.Errorf("load route %s: %w", feature, err)
	}
	if !row.ProviderEnabled {
		return llmgateway.Route{}, fmt.Errorf("provider for %s is disabled: %w", feature, llmgateway.ErrNoRoute)
	}
	rt := llmgateway.Route{
		Feature: feature, ProviderID: row.ProviderID, ProviderKind: string(row.ProviderKind), Model: row.Model,
		MaxOutputTokens: int(row.MaxOutputTokens), ContextBudget: int(row.ContextTokenBudget), Effort: row.Effort,
	}
	if row.Temperature != nil {
		t := float64(*row.Temperature)
		rt.Temperature = &t
	}
	if row.FallbackProviderID != nil && row.FallbackModel != "" && row.FallbackEnabled != nil && *row.FallbackEnabled {
		rt.Fallback = &llmgateway.Route{Feature: feature, ProviderID: *row.FallbackProviderID, ProviderKind: string(*row.FallbackKind),
			Model: row.FallbackModel, MaxOutputTokens: rt.MaxOutputTokens, ContextBudget: rt.ContextBudget}
	}
	return rt, nil
}

// SetRoute creates or replaces a feature route.
func (r *Routes) SetRoute(ctx context.Context, rt llmgateway.Route) error {
	p := gen.UpsertRouteParams{Feature: gen.LlmFeature(rt.Feature), ProviderID: rt.ProviderID, Model: rt.Model,
		MaxOutputTokens: int32(rt.MaxOutputTokens), ContextTokenBudget: int32(rt.ContextBudget), Effort: rt.Effort}
	if rt.Temperature != nil {
		t := float32(*rt.Temperature)
		p.Temperature = &t
	}
	if rt.Fallback != nil {
		p.FallbackProviderID, p.FallbackModel = &rt.Fallback.ProviderID, rt.Fallback.Model
	}
	return r.s.Q.UpsertRoute(ctx, p)
}

// ProviderRecord is a stored provider with its sealed key.
type ProviderRecord struct {
	ID, Kind, Name, BaseURL string
	KeyCiphertext           []byte
	Extra                   map[string]string
	RedactPII, Enabled      bool
}

// Providers is CRUD over llm_providers.
type Providers struct{ s *Store }

// NewProviders returns the provider store.
func NewProviders(s *Store) *Providers { return &Providers{s: s} }

// Get loads one provider.
func (p *Providers) Get(ctx context.Context, id string) (ProviderRecord, error) {
	row, err := p.s.Q.GetProvider(ctx, id)
	if IsNoRows(err) {
		return ProviderRecord{}, ports.ErrNotFound
	}
	if err != nil {
		return ProviderRecord{}, fmt.Errorf("load provider: %w", err)
	}
	return toProvider(row)
}

// List loads all providers.
func (p *Providers) List(ctx context.Context) ([]ProviderRecord, error) {
	rows, err := p.s.Q.ListProviders(ctx)
	if err != nil {
		return nil, fmt.Errorf("list providers: %w", err)
	}
	out := make([]ProviderRecord, 0, len(rows))
	for _, r := range rows {
		pr, err := toProvider(r)
		if err != nil {
			return nil, err
		}
		out = append(out, pr)
	}
	return out, nil
}

// Create inserts a provider; the caller seals the key first.
func (p *Providers) Create(ctx context.Context, r ProviderRecord) (string, error) {
	if r.ID == "" {
		r.ID = ports.NewID()
	}
	extra, _ := json.Marshal(nonNil(r.Extra))
	err := p.s.Q.InsertProvider(ctx, gen.InsertProviderParams{ID: r.ID, Kind: gen.LlmProviderKind(r.Kind), Name: r.Name,
		BaseUrl: r.BaseURL, KeyCiphertext: r.KeyCiphertext, Extra: extra, RedactPii: r.RedactPII, Enabled: r.Enabled})
	if err != nil {
		return "", fmt.Errorf("create provider: %w", err)
	}
	return r.ID, nil
}

// Update changes a provider. A nil KeyCiphertext keeps the stored key.
func (p *Providers) Update(ctx context.Context, r ProviderRecord) error {
	extra, _ := json.Marshal(nonNil(r.Extra))
	n, err := p.s.Q.UpdateProvider(ctx, gen.UpdateProviderParams{ID: r.ID, Name: r.Name, BaseUrl: r.BaseURL, Extra: extra,
		RedactPii: r.RedactPII, Enabled: r.Enabled, KeyCiphertext: r.KeyCiphertext})
	if err != nil {
		return fmt.Errorf("update provider: %w", err)
	}
	if n == 0 {
		return ports.ErrNotFound
	}
	return nil
}

// Delete removes a provider (routes referencing it block deletion via the foreign key).
func (p *Providers) Delete(ctx context.Context, id string) error {
	n, err := p.s.Q.DeleteProvider(ctx, id)
	if err != nil {
		return fmt.Errorf("delete provider (is a route still using it?): %w", ports.ErrConflict)
	}
	if n == 0 {
		return ports.ErrNotFound
	}
	return nil
}

func toProvider(r gen.LlmProvider) (ProviderRecord, error) {
	extra := map[string]string{}
	if len(r.Extra) > 0 {
		if err := json.Unmarshal(r.Extra, &extra); err != nil {
			return ProviderRecord{}, fmt.Errorf("provider %s extra: %w", r.ID, err)
		}
	}
	return ProviderRecord{ID: r.ID, Kind: string(r.Kind), Name: r.Name, BaseURL: r.BaseUrl, KeyCiphertext: r.KeyCiphertext,
		Extra: extra, RedactPII: r.RedactPii, Enabled: r.Enabled}, nil
}

func nonNil(m map[string]string) map[string]string {
	if m == nil {
		return map[string]string{}
	}
	return m
}

// LoadGuard builds a spend guard from spend_limits and cost_table.
func LoadGuard(ctx context.Context, s *Store, allowUnlimited bool) (*spendguard.Guard, error) {
	rows, err := s.Q.ListSpendLimits(ctx)
	if err != nil {
		return nil, fmt.Errorf("load spend limits: %w", err)
	}
	limits := make([]spendguard.Limit, 0, len(rows))
	for _, r := range rows {
		l := spendguard.Limit{ID: r.ID, Scope: spendguard.Scope(r.Scope), ScopeKey: r.ScopeKey, Window: spendguard.Window(r.TimeWindow),
			Alert: r.OnBreach == gen.BreachActionBlockAndAlert, AlertURL: r.AlertUrl}
		if r.MaxTokens != nil {
			l.MaxTokens = *r.MaxTokens
		}
		if r.MaxCostUsd != nil {
			l.MaxCostUSD = *r.MaxCostUsd
		}
		limits = append(limits, l)
	}
	costs, err := s.Q.ListCostTable(ctx)
	if err != nil {
		return nil, fmt.Errorf("load cost table: %w", err)
	}
	prices := make(map[string]spendguard.Price, len(costs))
	for _, c := range costs {
		prices[spendguard.PriceKey(string(c.ProviderKind), c.Model)] = spendguard.Price{
			InputPerMTok: c.InputPerMtokUsd, OutputPerMTok: c.OutputPerMtokUsd, EmbedPerMTok: c.EmbedPerMtokUsd}
	}
	return spendguard.New(limits, prices, allowUnlimited)
}

// Opener decrypts sealed secrets (secrets.Box).
type Opener interface {
	Open(ctx context.Context, blob, aad []byte) ([]byte, error)
}

// ProviderSource implements the adapter pool's Source: it loads a provider and decrypts its key just in
// time. Keys never leave this path in plaintext except into the adapter that uses them.
type ProviderSource struct {
	Providers *Providers
	Box       Opener
	AAD       func(id string) []byte
}

// ProviderConfig loads and decrypts one provider.
func (ps *ProviderSource) ProviderConfig(ctx context.Context, id string) (ports.ProviderConfig, bool, error) {
	r, err := ps.Providers.Get(ctx, id)
	if err != nil {
		return ports.ProviderConfig{}, false, err
	}
	cfg := ports.ProviderConfig{ID: r.ID, Kind: r.Kind, Name: r.Name, BaseURL: r.BaseURL, Extra: r.Extra}
	if len(r.KeyCiphertext) > 0 {
		key, err := ps.Box.Open(ctx, r.KeyCiphertext, ps.AAD(r.ID))
		if err != nil {
			return cfg, r.Enabled, ports.Permanent(fmt.Errorf("decrypt key for provider %q: %w", r.Name, err))
		}
		cfg.APIKey = string(key)
	}
	return cfg, r.Enabled, nil
}
