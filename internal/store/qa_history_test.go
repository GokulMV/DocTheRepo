package store_test

import (
	"context"
	"encoding/json"
	"testing"

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
