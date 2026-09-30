package eval

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestScore(t *testing.T) {
	q := Question{ID: "q", Expect: []string{"r:a.go", "r:b.md"}}
	r := Score(q, []string{"r:a.go", "r:c.go", "r:a.go"})
	assert.InDelta(t, 0.5, r.Precision, 1e-9, "1 of 2 distinct cited files expected")
	assert.InDelta(t, 0.5, r.Recall, 1e-9, "1 of 2 expected files cited")
	assert.Equal(t, []string{"r:a.go", "r:c.go"}, r.Cited)

	r = Score(q, nil)
	assert.Zero(t, r.Precision)
	assert.Zero(t, r.Recall)

	neg := Question{ID: "n"}
	assert.Equal(t, 1.0, Score(neg, nil).Precision, "a negative question is right when nothing is cited")
	assert.Equal(t, 0.0, Score(neg, []string{"r:x"}).Recall)
}

func TestSummarizeAndCheck(t *testing.T) {
	rs := []Result{
		Score(Question{ID: "1", Expect: []string{"a"}}, []string{"a"}),
		Score(Question{ID: "2", Expect: []string{"b"}}, []string{"c"}),
		Score(Question{ID: "3"}, nil),
	}
	rep := Summarize("stub", "", rs)
	assert.Equal(t, 0.667, rep.Precision)
	assert.Equal(t, 0.667, rep.Recall)
	assert.Equal(t, 0.667, rep.HitRate)
	require.NoError(t, Check(rep, Baseline{Precision: 0.7, Recall: 0.7}))
	assert.ErrorContains(t, Check(rep, Baseline{Precision: 0.8, Recall: 0.7}), "precision")
}

func TestGoldenSetIsWellFormed(t *testing.T) {
	qs, err := Golden()
	require.NoError(t, err)
	assert.Len(t, qs, 50)
	seen := map[string]bool{}
	negatives := 0
	for _, q := range qs {
		assert.False(t, seen[q.ID], "duplicate id %s", q.ID)
		seen[q.ID] = true
		assert.NotEmpty(t, q.Question)
		if len(q.Expect) == 0 {
			negatives++
		}
	}
	assert.GreaterOrEqual(t, negatives, 1, "at least one question has no answer in the sources")
}
