package store_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/GokulMV/DocTheRepo/internal/core/rag"
	"github.com/GokulMV/DocTheRepo/internal/ports"
	"github.com/GokulMV/DocTheRepo/internal/store"
	"github.com/GokulMV/DocTheRepo/internal/store/gen"
	"github.com/GokulMV/DocTheRepo/internal/store/storetest"
)

// Thread history keeps whether an answer was found by looking further, and how its sources were picked.
func TestThreadHistoryKeepsInvestigated(t *testing.T) {
	st := storetest.New(t)
	ctx := context.Background()
	u, err := st.Q.CreateUser(ctx, gen.CreateUserParams{ID: ports.NewID(), Email: "ann@acme.com", Role: gen.UserRoleViewer})
	require.NoError(t, err)
	qa := store.NewQA(st)
	tid, err := qa.CreateThread(ctx, u.ID, "Where are retries?", map[string]any{})
	require.NoError(t, err)
	_, err = qa.AddExchange(ctx, tid, "Where are retries?", rag.Answer{Text: "In pay.go [1].", Citations: []rag.Citation{}, Investigated: true,
		Sift: &rag.SiftSummary{Candidates: 20, Kept: 3, TokensSaved: 9400, SavedUSD: 0.046, Model: "jev-latest"}})
	require.NoError(t, err)
	_, err = qa.AddExchange(ctx, tid, "And backoff?", rag.Answer{Text: "Same file.", Citations: []rag.Citation{}})
	require.NoError(t, err)
	th, err := qa.GetThread(ctx, u.ID, tid, true)
	require.NoError(t, err)
	require.Len(t, th.Messages, 4)
	assert.True(t, th.Messages[1].Investigated)
	assert.False(t, th.Messages[3].Investigated)
	var sift rag.SiftSummary
	require.NoError(t, json.Unmarshal(th.Messages[1].Sift, &sift), "how sources were picked survives a reload")
	assert.Equal(t, 9400, sift.TokensSaved)
	assert.Equal(t, 3, sift.Kept)
	assert.Empty(t, th.Messages[3].Sift, "no picker, no summary")
}

// Analytics adds up what the source picker did across answers.
func TestSiftAnalytics(t *testing.T) {
	st := storetest.New(t)
	ctx := context.Background()
	u, err := st.Q.CreateUser(ctx, gen.CreateUserParams{ID: ports.NewID(), Email: "bo@acme.com", Role: gen.UserRoleViewer})
	require.NoError(t, err)
	qa := store.NewQA(st)
	tid, err := qa.CreateThread(ctx, u.ID, "q", map[string]any{})
	require.NoError(t, err)
	add := func(s *rag.SiftSummary) {
		_, err := qa.AddExchange(ctx, tid, "q", rag.Answer{Text: "a", Citations: []rag.Citation{}, Sift: s})
		require.NoError(t, err)
	}
	add(&rag.SiftSummary{Candidates: 20, Kept: 3, TokensSaved: 9000, JudgeTokens: 2000, CostUSD: 0.002, SavedUSD: 0.04})
	add(&rag.SiftSummary{Candidates: 12, Kept: 12, JudgeTokens: 1500, CostUSD: 0.001, SavedUSD: -0.001})
	add(&rag.SiftSummary{Kept: 2, Explored: 2, JudgeTokens: 800, CostUSD: 0.0005, SavedUSD: -0.0005})
	add(nil)

	rep, err := store.NewBrowse(st).Sift(ctx, time.Now().Add(-time.Hour), time.Now().Add(time.Hour))
	require.NoError(t, err)
	assert.Equal(t, int64(4), rep.Answers)
	assert.Equal(t, int64(3), rep.Picked)
	assert.Equal(t, int64(1), rep.Trimmed, "only the first read fewer sources than retrieved")
	assert.Equal(t, int64(32), rep.Candidates)
	assert.Equal(t, int64(17), rep.Kept)
	assert.Equal(t, int64(9000), rep.TokensSaved)
	assert.Equal(t, int64(4300), rep.JudgeTokens)
	assert.InDelta(t, 0.0385, rep.SavedUSD, 1e-9)
	assert.Equal(t, int64(2), rep.Explored)
	require.Len(t, rep.Daily, 1)
	assert.Equal(t, int64(3), rep.Daily[0].Picked)
}
