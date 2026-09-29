package store_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/GokulMV/DocTheRepo/internal/adapters/llm"
	"github.com/GokulMV/DocTheRepo/internal/adapters/secrets/localfile"
	"github.com/GokulMV/DocTheRepo/internal/core/llmgateway"
	"github.com/GokulMV/DocTheRepo/internal/core/spendguard"
	"github.com/GokulMV/DocTheRepo/internal/ports"
	"github.com/GokulMV/DocTheRepo/internal/secrets"
	"github.com/GokulMV/DocTheRepo/internal/store"
	"github.com/GokulMV/DocTheRepo/internal/store/storetest"
	"github.com/GokulMV/DocTheRepo/test/mocks/llmmock"
)

func TestLedger_RecordAndSpentFilters(t *testing.T) {
	st := storetest.New(t)
	ctx := context.Background()
	l := store.NewLedger(st)
	provA, provB, repo := ports.NewID(), ports.NewID(), ""
	now := time.Now()
	for _, r := range []ports.UsageRecord{
		{At: now, Feature: "qa", ProviderID: provA, ProviderKind: "anthropic", Model: "m", InputTokens: 100, OutputTokens: 10, CostUSD: 0.5},
		{At: now, Feature: "docgen", ProviderID: provB, ProviderKind: "openai", Model: "m", InputTokens: 1000, OutputTokens: 100, CostUSD: 1},
		{At: now.Add(-48 * time.Hour), Feature: "qa", ProviderID: provA, ProviderKind: "anthropic", Model: "m", InputTokens: 9999},
	} {
		require.NoError(t, l.Record(ctx, r))
	}
	_ = repo
	day := now.Add(-24 * time.Hour)
	all, err := l.Spent(ctx, ports.SpendFilter{Since: day})
	require.NoError(t, err)
	assert.Equal(t, ports.SpendTotals{Tokens: 1210, CostUSD: 1.5}, all, "the window excludes the 2-day-old row")
	qa, _ := l.Spent(ctx, ports.SpendFilter{Since: day, Feature: "qa"})
	assert.Equal(t, int64(110), qa.Tokens)
	b, _ := l.Spent(ctx, ports.SpendFilter{Since: day, ProviderID: provB})
	assert.Equal(t, int64(1100), b.Tokens)
}

func TestLoadGuard_SeededDefaults(t *testing.T) {
	st := storetest.New(t)
	g, err := store.LoadGuard(context.Background(), st, false)
	require.NoError(t, err)
	d := g.Evaluate(spendguard.Request{InputTokens: 2_000_001}, nil)
	assert.False(t, d.Allow, "the seeded 2M/day global ceiling applies")
	c, ok := g.Cost("anthropic", "claude-opus-5-5", "qa", 1_000_000, 1_000_000)
	assert.True(t, ok)
	assert.InDelta(t, 24.0, c, 1e-9, "seeded list price $4 in + $20 out per MTok")
}

// TestProviderToGatewayEndToEnd stores a provider with an encrypted key, routes QA to it with a fallback,
// and runs a real gateway call through the adapter pool against the mock LLM server.
func TestProviderToGatewayEndToEnd(t *testing.T) {
	st := storetest.New(t)
	ctx := context.Background()
	kek, err := localfile.Open(filepath.Join(t.TempDir(), "k"))
	require.NoError(t, err)
	box := secrets.NewBox(kek)
	srv := llmmock.New()
	defer srv.Close()

	provs := store.NewProviders(st)
	primaryID, fallbackID := ports.NewID(), ports.NewID()
	sealed, err := box.Seal(ctx, []byte("sk-primary"), secrets.ProviderKeyAAD(primaryID))
	require.NoError(t, err)
	_, err = provs.Create(ctx, store.ProviderRecord{ID: primaryID, Kind: "openai", Name: "OpenAI prod", BaseURL: srv.URL + "/v1",
		KeyCiphertext: sealed, Extra: map[string]string{"organization": "org"}, Enabled: true})
	require.NoError(t, err)
	fsealed, _ := box.Seal(ctx, []byte("sk-fallback"), secrets.ProviderKeyAAD(fallbackID))
	_, err = provs.Create(ctx, store.ProviderRecord{ID: fallbackID, Kind: "anthropic", Name: "Claude", BaseURL: srv.URL, KeyCiphertext: fsealed, Enabled: true})
	require.NoError(t, err)

	routes := store.NewRoutes(st)
	require.NoError(t, routes.SetRoute(ctx, llmgateway.Route{Feature: "qa", ProviderID: primaryID, Model: llmmock.RateLimited,
		MaxOutputTokens: 500, ContextBudget: 16000, Effort: "medium",
		Fallback: &llmgateway.Route{ProviderID: fallbackID, Model: llmmock.OK}}))
	rt, err := routes.Route(ctx, "qa")
	require.NoError(t, err)
	assert.Equal(t, "openai", rt.ProviderKind)
	require.NotNil(t, rt.Fallback)
	assert.Equal(t, "anthropic", rt.Fallback.ProviderKind)
	_, err = routes.Route(ctx, "decode")
	assert.ErrorIs(t, err, llmgateway.ErrNoRoute)

	guard, err := store.LoadGuard(ctx, st, false)
	require.NoError(t, err)
	pool := llm.NewPool(&store.ProviderSource{Providers: provs, Box: box, AAD: secrets.ProviderKeyAAD}, time.Minute)
	gw := llmgateway.New(spendguard.NewEnforcer(guard, store.NewLedger(st), nil), routes, pool, false)

	resp, err := gw.Chat(ctx, "qa", llmgateway.CallMeta{}, ports.ChatRequest{Messages: []ports.ChatMessage{{Role: "user", Content: "q"}}})
	require.NoError(t, err, "primary is rate limited; the fallback route answers")
	assert.Equal(t, llmmock.Text, resp.Text)
	var sawPrimaryKey, sawFallbackKey bool
	for _, r := range srv.Requests() {
		if r.Header.Get("Authorization") == "Bearer sk-primary" {
			sawPrimaryKey = true
		}
		if r.Header.Get("X-Api-Key") == "sk-fallback" {
			sawFallbackKey = true
		}
	}
	assert.True(t, sawPrimaryKey && sawFallbackKey, "each provider's decrypted key reached its own API")

	spent, err := store.NewLedger(st).Spent(ctx, ports.SpendFilter{Since: time.Now().Add(-time.Hour), ProviderID: fallbackID})
	require.NoError(t, err)
	assert.Equal(t, int64(llmmock.InputTokens+llmmock.OutputTokens), spent.Tokens, "usage is attributed to the provider that served")

	// Disabling a provider takes effect after invalidation.
	rec, _ := provs.Get(ctx, fallbackID)
	rec.Enabled, rec.KeyCiphertext = false, nil
	require.NoError(t, provs.Update(ctx, rec))
	pool.Invalidate(fallbackID)
	_, err = pool.LLM(ctx, fallbackID)
	assert.ErrorContains(t, err, "disabled")
	after, _ := provs.Get(ctx, fallbackID)
	assert.Equal(t, fsealed, after.KeyCiphertext, "updating without a new key keeps the stored key")

	// A ciphertext copied to another provider row does not decrypt.
	other := ports.NewID()
	_, err = provs.Create(ctx, store.ProviderRecord{ID: other, Kind: "anthropic", Name: "stolen", KeyCiphertext: sealed, Enabled: true})
	require.NoError(t, err)
	_, err = pool.LLM(ctx, other)
	assert.ErrorContains(t, err, "decrypt")

	assert.ErrorIs(t, provs.Delete(ctx, primaryID), ports.ErrConflict, "a provider in use by a route cannot be deleted")
	assert.ErrorIs(t, provs.Delete(ctx, ports.NewID()), ports.ErrNotFound)
	list, err := provs.List(ctx)
	require.NoError(t, err)
	assert.Len(t, list, 3)
}
