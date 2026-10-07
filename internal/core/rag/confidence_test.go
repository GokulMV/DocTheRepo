package rag_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/GokulMV/DocTheRepo/internal/core/rag"
	"github.com/GokulMV/DocTheRepo/internal/ports"
)

func TestScore_CodeBackedAnswersRankAboveWeakDocs(t *testing.T) {
	code := ports.Chunk{ID: "c", Source: ports.SourceCode, Content: "func Refund() {}"}
	code2 := ports.Chunk{ID: "c2", Source: ports.SourceCode, Content: "func Retry() {}"}
	lowDoc := ports.Chunk{ID: "d", Source: ports.SourceGeneratedDoc, Content: "acme › Overview › What (written from commit abc, confidence low)\n\nIt refunds."}
	cits := func(ids ...string) []rag.Citation {
		var out []rag.Citation
		for i, id := range ids {
			out = append(out, rag.Citation{N: i + 1, ChunkID: id})
		}
		return out
	}
	strong := rag.Score(rag.Answer{Text: "Refunds are reversed by the refund handler [1]. Failed ones are retried with backoff [2].", Citations: cits("c", "c2")}, []ports.Chunk{code, code2}, false)
	require.NotNil(t, strong)
	assert.Equal(t, "high", strong.Label)
	assert.Empty(t, strong.Why)

	weak := rag.Score(rag.Answer{Text: "Refunds are reversed by the refund handler [1]. They are approved by finance every Friday afternoon. Nobody knows the limit.", Citations: cits("d")}, []ports.Chunk{lowDoc}, true)
	require.NotNil(t, weak)
	assert.Equal(t, "low", weak.Label)
	assert.Contains(t, weak.Why, "it cites a document of low confidence")
	assert.Contains(t, weak.Why, "only 1 of 3 statements cite a source")
	assert.Contains(t, weak.Why, "it rests on a single source")
	assert.Less(t, weak.Score, strong.Score)

	assert.Nil(t, rag.Score(rag.Answer{Text: rag.NotFoundAnswer}, nil, false), "no confidence for not found")
}
