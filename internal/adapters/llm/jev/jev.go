// Package jev is the adapter for TypeSafe's System One API and its decision model, Jev (plan Phase 11.5).
// Jev does not write text: it reads a state and answers typed questions with calibrated probabilities, so
// this adapter implements ports.Decider and refuses chat.
//
// Wire format (POST {base}/systemone, Authorization: Bearer <key>):
//
//	request  {"model": "jev-latest", "state": "<text>", "questions": {"decision": {"type": "choice",
//	          "instructions": "<question>", "criteria": {"<option id>": "<description>", ...}}}}
//	response {"model": "jev-…", "answers": {"decision": {"type": "choice", "choice": "<option id>",
//	          "confidence": 0.82, "probabilities": {"<option id>": 0.84, ...}}},
//	          "usage": {"input_tokens": 318, "output_tokens": 34}}
//
// Choice questions take up to 255 options. 401 (bad key) and 422 (invalid request) are permanent; 429
// (rate limit) and 529 (overloaded) are retried with backoff by httpx. Only input tokens are billed.
package jev

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/GokulMV/DocTheRepo/internal/adapters/httpx"
	"github.com/GokulMV/DocTheRepo/internal/ports"
)

// Defaults.
const (
	DefaultBaseURL = "https://api.typesafe.ai/v1"
	DefaultModel   = "jev-latest"
	MaxOptions     = 255
	questionID     = "decision"
)

// ErrChatUnsupported is returned by Chat: Jev answers decisions, it does not generate text.
var ErrChatUnsupported = errors.New("jev answers typed decisions only; route chat features to a chat provider")

// Client implements ports.LLM (for the provider pool) and ports.Decider.
type Client struct {
	base string
	key  string
	http *httpx.Client
}

// New builds a client from provider config (BaseURL optional, APIKey required).
func New(cfg ports.ProviderConfig) (*Client, error) {
	if strings.TrimSpace(cfg.APIKey) == "" {
		return nil, ports.Permanent(errors.New("jev: api_key is required"))
	}
	base := strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/")
	if base == "" {
		base = DefaultBaseURL
	}
	return &Client{base: base, key: cfg.APIKey, http: httpx.New("typesafe")}, nil
}

// Kind implements ports.LLM.
func (*Client) Kind() string { return "jev" }

// Chat implements ports.LLM by refusing: decision routes call Decide.
func (*Client) Chat(context.Context, ports.ChatRequest) (ports.ChatResponse, error) {
	return ports.ChatResponse{}, ports.Permanent(ErrChatUnsupported)
}

type question struct {
	Type         string            `json:"type"`
	Instructions string            `json:"instructions"`
	Criteria     map[string]string `json:"criteria,omitempty"`
}

type request struct {
	Model     string              `json:"model"`
	State     string              `json:"state"`
	Questions map[string]question `json:"questions"`
}

type answer struct {
	Type          string             `json:"type"`
	Choice        string             `json:"choice"`
	Confidence    float64            `json:"confidence"`
	Probabilities map[string]float64 `json:"probabilities"`
	Probability   *float64           `json:"probability"` // noul
}

type response struct {
	Model   string            `json:"model"`
	Answers map[string]answer `json:"answers"`
	Usage   struct {
		InputTokens  int64 `json:"input_tokens"`
		OutputTokens int64 `json:"output_tokens"`
	} `json:"usage"`
}

// Decide implements ports.Decider with one choice question.
func (c *Client) Decide(ctx context.Context, model string, q ports.DecisionQuestion) (ports.Decision, error) {
	if len(q.Options) < 2 || len(q.Options) > MaxOptions {
		return ports.Decision{}, ports.Permanent(fmt.Errorf("jev: a choice needs 2 to %d options, got %d", MaxOptions, len(q.Options)))
	}
	if model == "" {
		model = DefaultModel
	}
	instructions := q.Question
	if instructions == "" {
		instructions = q.Task
	}
	criteria := make(map[string]string, len(q.Options))
	for _, o := range q.Options {
		criteria[o.ID] = o.Description
	}
	var out response
	err := c.http.JSON(ctx, http.MethodPost, c.base+"/systemone", map[string]string{"Authorization": "Bearer " + c.key},
		request{Model: model, State: q.Context, Questions: map[string]question{questionID: {Type: "choice", Instructions: instructions, Criteria: criteria}}}, &out)
	if err != nil {
		return ports.Decision{}, err
	}
	a, ok := out.Answers[questionID]
	if !ok || len(a.Probabilities) == 0 {
		return ports.Decision{}, ports.Transient(fmt.Errorf("jev: response has no answer for %q", questionID))
	}
	return ports.Decision{Probabilities: a.Probabilities, Choice: a.Choice, P: a.Probabilities[a.Choice], Calibrated: true, Model: out.Model,
		Usage: ports.TokenUsage{InputTokens: out.Usage.InputTokens, OutputTokens: out.Usage.OutputTokens, Reported: true}}, nil
}

// Ping implements ports.LLM with the smallest possible decision (a two-option choice on a one-word state).
func (c *Client) Ping(ctx context.Context, model string) error {
	_, err := c.Decide(ctx, model, ports.DecisionQuestion{Task: "ping", Question: "Is this a connectivity check?", Context: "ping",
		Options: []ports.DecisionOption{{ID: "yes", Description: "yes"}, {ID: "no", Description: "no"}}})
	return err
}
