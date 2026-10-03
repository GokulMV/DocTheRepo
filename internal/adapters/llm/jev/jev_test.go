package jev

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/GokulMV/DocTheRepo/internal/ports"
)

var q = ports.DecisionQuestion{Task: "issue_actionability", Question: "Does this need attention?", Context: "Title: health check failed",
	Options: []ports.DecisionOption{{ID: "actionable", Description: "fix it"}, {ID: "known_noise", Description: "ignore it"}}}

func TestDecideSpeaksSystemOne(t *testing.T) {
	var got map[string]any
	var auth, path string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth, path = r.Header.Get("Authorization"), r.URL.Path
		_ = json.NewDecoder(r.Body).Decode(&got)
		_, _ = w.Write([]byte(`{"model":"jev-1.13.0","answers":{"decision":{"type":"choice","choice":"known_noise","confidence":0.9,
			"probabilities":{"known_noise":0.94,"actionable":0.06}}},"usage":{"input_tokens":120,"output_tokens":9}}`))
	}))
	defer srv.Close()
	c, err := New(ports.ProviderConfig{APIKey: "ts-key", BaseURL: srv.URL + "/v1/"})
	require.NoError(t, err)
	d, err := c.Decide(context.Background(), "", q)
	require.NoError(t, err)
	assert.Equal(t, "Bearer ts-key", auth)
	assert.Equal(t, "/v1/systemone", path)
	assert.Equal(t, "jev-latest", got["model"], "the default model")
	assert.Equal(t, "Title: health check failed", got["state"])
	assert.Equal(t, map[string]any{"decision": map[string]any{"type": "choice", "instructions": "Does this need attention?",
		"criteria": map[string]any{"actionable": "fix it", "known_noise": "ignore it"}}}, got["questions"])
	assert.Equal(t, "known_noise", d.Choice)
	assert.Equal(t, 0.94, d.P)
	assert.True(t, d.Calibrated)
	assert.Equal(t, "jev-1.13.0", d.Model)
	assert.Equal(t, ports.TokenUsage{InputTokens: 120, OutputTokens: 9, Reported: true}, d.Usage)
}

func TestErrorsAndLimits(t *testing.T) {
	status := http.StatusUnauthorized
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(status)
		_, _ = w.Write([]byte(`{"error":"x"}`))
	}))
	defer srv.Close()
	c, err := New(ports.ProviderConfig{APIKey: "k", BaseURL: srv.URL})
	require.NoError(t, err)
	c.http.MaxRetries = 0
	var perm *ports.PermanentError
	_, err = c.Decide(context.Background(), "m", q)
	assert.ErrorAs(t, err, &perm, "401: a bad key will not fix itself")
	status = http.StatusUnprocessableEntity
	_, err = c.Decide(context.Background(), "m", q)
	assert.ErrorAs(t, err, &perm, "422: an invalid request")
	for _, s := range []int{http.StatusTooManyRequests, 529} {
		status = s
		_, err = c.Decide(context.Background(), "m", q)
		_, transient := ports.AsTransient(err)
		assert.True(t, transient, "%d is retried", s)
	}

	_, err = New(ports.ProviderConfig{})
	assert.ErrorAs(t, err, &perm)
	_, err = c.Chat(context.Background(), ports.ChatRequest{})
	assert.ErrorIs(t, err, ErrChatUnsupported)
	_, err = c.Decide(context.Background(), "m", ports.DecisionQuestion{Options: []ports.DecisionOption{{ID: "one"}}})
	assert.ErrorAs(t, err, &perm)

	ok := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"answers":{}}`))
	}))
	defer ok.Close()
	c2, _ := New(ports.ProviderConfig{APIKey: "k", BaseURL: ok.URL})
	_, err = c2.Decide(context.Background(), "m", q)
	_, transient := ports.AsTransient(err)
	assert.True(t, transient, "a response without our answer is retried")
}

func TestJudgeAsksBooleanQuestions(t *testing.T) {
	var got map[string]any
	reply := `{"model":"jev-1.13.0","answers":{"rel0":{"type":"boolean","probability":0.91},"rel1":{"type":"boolean","probability":0.04}},
		"usage":{"input_tokens":800,"output_tokens":4}}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&got)
		_, _ = w.Write([]byte(reply))
	}))
	defer srv.Close()
	c, err := New(ports.ProviderConfig{APIKey: "k", BaseURL: srv.URL})
	require.NoError(t, err)
	j, err := c.Judge(context.Background(), "", ports.JudgeRequest{Task: "ask_evidence", State: `{"question":"q"}`,
		Questions: []ports.JudgeQuestion{{ID: "rel0", Instructions: "first?"}, {ID: "rel1", Instructions: "second?"}}})
	require.NoError(t, err)
	assert.Equal(t, map[string]any{"rel0": map[string]any{"type": "boolean", "instructions": "first?"},
		"rel1": map[string]any{"type": "boolean", "instructions": "second?"}}, got["questions"])
	assert.Equal(t, map[string]float64{"rel0": 0.91, "rel1": 0.04}, j.P)
	assert.True(t, j.Calibrated)
	assert.Equal(t, int64(800), j.Usage.InputTokens)

	reply = `{"answers":{"rel0":{"type":"boolean","probability":0.5}}}`
	c.http.MaxRetries = 0
	_, err = c.Judge(context.Background(), "", ports.JudgeRequest{State: "s",
		Questions: []ports.JudgeQuestion{{ID: "rel0", Instructions: "a"}, {ID: "rel1", Instructions: "b"}}})
	assert.Error(t, err, "an unanswered question is an error, not a silent 0")
	_, err = c.Judge(context.Background(), "", ports.JudgeRequest{State: "s"})
	var perm *ports.PermanentError
	assert.ErrorAs(t, err, &perm)
}
