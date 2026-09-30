package suggest

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/GokulMV/DocTheRepo/internal/core/knownissues"
	"github.com/GokulMV/DocTheRepo/internal/core/llmgateway"
	"github.com/GokulMV/DocTheRepo/internal/core/spendguard"
	"github.com/GokulMV/DocTheRepo/internal/ports"
)

type ledger struct{}

func (ledger) Spent(context.Context, ports.SpendFilter) (ports.SpendTotals, error) {
	return ports.SpendTotals{}, nil
}
func (ledger) Record(context.Context, ports.UsageRecord) error { return nil }

type llm struct {
	replies []string
	prompts []string
}

func (f *llm) Kind() string { return "anthropic" }
func (f *llm) Chat(_ context.Context, r ports.ChatRequest) (ports.ChatResponse, error) {
	f.prompts = append(f.prompts, r.Messages[len(r.Messages)-1].Content)
	if len(f.replies) == 0 {
		return ports.ChatResponse{}, errors.New("no reply")
	}
	t := f.replies[0]
	f.replies = f.replies[1:]
	return ports.ChatResponse{Text: t, Usage: ports.TokenUsage{InputTokens: 10, OutputTokens: 10, Reported: true}}, nil
}
func (f *llm) Ping(context.Context, string) error { return nil }

type providers struct{ l *llm }

func (p providers) Route(_ context.Context, f string) (llmgateway.Route, error) {
	if f != llmgateway.FeatureSuggest {
		return llmgateway.Route{}, llmgateway.ErrNoRoute
	}
	return llmgateway.Route{ProviderID: "p", ProviderKind: "anthropic", Model: "m", MaxOutputTokens: 500}, nil
}
func (p providers) LLM(context.Context, string) (ports.LLM, error) { return p.l, nil }
func (p providers) Embedder(context.Context, string) (ports.Embedder, error) {
	return nil, errors.New("unused")
}
func (p providers) DocGenerator(context.Context, string) (ports.DocGenerator, error) {
	return nil, errors.New("unused")
}

type mem struct {
	auto      []Candidate
	saved     []knownissues.Match
	rationale []string
	search    []Candidate
	terms     []string
	services  []string
	samples   []Sampled
}

func (m *mem) AutoCandidates(context.Context, int64, time.Time) ([]Candidate, error) {
	return m.auto, nil
}
func (m *mem) SaveSuggestion(_ context.Context, _ []string, mt knownissues.Match, r string) (string, error) {
	m.saved = append(m.saved, mt)
	m.rationale = append(m.rationale, r)
	return "s", nil
}
func (m *mem) SearchIssues(_ context.Context, terms, services []string, _ time.Time, _ int) ([]Candidate, error) {
	m.terms, m.services = terms, services
	return m.search, nil
}
func (m *mem) IssuesForDecodeChunks(context.Context, []string) ([]Candidate, error) { return nil, nil }
func (m *mem) Services(context.Context, time.Time) ([]string, error) {
	return []string{"checkout", "payments-gw", "api"}, nil
}
func (m *mem) LatestSamples(context.Context, time.Time, int) ([]Sampled, error) {
	return m.samples, nil
}

func service(t *testing.T, m *mem, replies ...string) (*Service, *llm) {
	l := &llm{replies: replies}
	g, err := spendguard.New(nil, nil, true)
	require.NoError(t, err)
	gw := llmgateway.New(spendguard.NewEnforcer(g, ledger{}, nil), providers{l}, providers{l}, true)
	return &Service{Store: m, GW: gw, Now: func() time.Time { return time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC) }}, l
}

func TestExtract(t *testing.T) {
	text := `Known issue: checkout times out calling payments-gw during the nightly batch.
Symptoms: "upstream request timeout after 30000ms", java.net.SocketTimeoutException and ERR_PAYMENT_503 / HTTP 503.
Logs are in /aws/lambda/checkout-batch. The apiary service is unrelated.`
	got := Extract(text, []string{"checkout", "payments-gw", "api", "unknown"})
	assert.Equal(t, []string{"java.net.SocketTimeoutException"}, got.Exceptions)
	assert.Contains(t, got.Codes, "HTTP 503")
	assert.Equal(t, []string{"upstream request timeout after 30000ms"}, got.Messages)
	assert.Equal(t, []string{"/aws/lambda/checkout-batch."}, got.Resources[:1])
	assert.Equal(t, []string{"checkout", "payments-gw"}, got.Services, "whole words only: 'apiary' is not 'api'")
}

func TestAutoSuggestGroupsByServiceAndEnvironment(t *testing.T) {
	m := &mem{auto: []Candidate{
		{IssueID: "i1", Fingerprint: "fp1", Title: "health check failed", Service: "lb", Environment: "prod", Occurrences: 900, Summary: "expected during deploys"},
		{IssueID: "i2", Fingerprint: "fp2", Title: "probe timeout", Service: "lb", Environment: "prod", Occurrences: 120},
		{IssueID: "i3", Fingerprint: "fp3", Title: "noise", Service: "unknown", Occurrences: 60},
	}}
	s, l := service(t, m)
	n, err := s.AutoSuggest(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 2, n)
	assert.Empty(t, l.prompts, "auto-suggestions need no model call")
	assert.Equal(t, knownissues.Match{Fingerprints: []string{"fp1", "fp2"}, Services: []string{"lb"}, Environments: []string{"prod"}}, m.saved[0])
	assert.Contains(t, m.rationale[0], "2 issue(s) in lb with 1020 occurrences")
	assert.Contains(t, m.rationale[0], "expected during deploys")
	assert.Equal(t, knownissues.Match{Fingerprints: []string{"fp3"}}, m.saved[1], "no service scope for unknown")
}

func TestFromTextRepairsInventedFingerprints(t *testing.T) {
	m := &mem{search: []Candidate{{IssueID: "i1", Fingerprint: "fp-timeout", Title: "SocketTimeoutException calling payments-gw", Service: "checkout", Kind: "error", Occurrences: 40}},
		samples: []Sampled{
			{Candidate: Candidate{IssueID: "i1", Fingerprint: "fp-timeout"}, Sample: ports.SignalEvent{Service: "checkout", Severity: ports.SeverityError}},
			{Candidate: Candidate{IssueID: "i9", Fingerprint: "fp-other"}, Sample: ports.SignalEvent{Service: "checkout", Severity: ports.SeverityError}},
		}}
	s, l := service(t, m,
		`{"explanation":"x","reason":"known_bug","proposed_match":{"fingerprints":["fp-made-up"]},"confidence":"high"}`,
		`{"explanation":"The note describes the checkout timeouts to payments-gw.","reason":"third_party","proposed_match":{"fingerprints":["fp-timeout"]},"confidence":"high"}`)
	got, err := s.FromText(context.Background(), `checkout: java.net.SocketTimeoutException to payments-gw. token=sk-live-abcdef0123456789abcdef`, nil, "u1")
	require.NoError(t, err)
	assert.Equal(t, "third_party", got.Reason)
	assert.Equal(t, []string{"fp-timeout"}, got.ProposedMatch.Fingerprints)
	assert.Equal(t, 1, got.WouldMatch)
	assert.Equal(t, []string{"i1"}, got.SampleIssueIDs)
	require.Len(t, l.prompts, 2)
	assert.Contains(t, l.prompts[1], `"fp-made-up" is not one of the listed issues`)
	assert.Contains(t, l.prompts[0], "- issue i1: fingerprint fp-timeout service checkout")
	assert.NotContains(t, l.prompts[0], "sk-live-abcdef0123456789abcdef", "pasted text is scrubbed before any model sees it")
	assert.Contains(t, m.terms, "java.net.SocketTimeoutException")
	assert.Equal(t, []string{"checkout", "payments-gw"}, m.services)
}

func TestFromTextWithoutCandidatesMakesNoCall(t *testing.T) {
	s, l := service(t, &mem{})
	got, err := s.FromText(context.Background(), "something vague", nil, "")
	require.NoError(t, err)
	assert.Equal(t, "low", got.Confidence)
	assert.Empty(t, l.prompts)
	_, err = s.FromText(context.Background(), "   ", nil, "")
	assert.ErrorIs(t, err, ErrEmptyText)
	long := strings.Repeat("a", MaxTextChars+10)
	_, err = s.FromText(context.Background(), long, nil, "")
	assert.NoError(t, err)
}

func TestDryRun(t *testing.T) {
	m := &mem{samples: []Sampled{
		{Candidate: Candidate{IssueID: "a", Fingerprint: "f1"}, Sample: ports.SignalEvent{Service: "api", Message: "timeout calling db", Severity: ports.SeverityError}},
		{Candidate: Candidate{IssueID: "b", Fingerprint: "f2"}, Sample: ports.SignalEvent{Service: "api", Message: "ok", Severity: ports.SeverityInfo}},
		{Candidate: Candidate{IssueID: "c", Fingerprint: "f3"}, Sample: ports.SignalEvent{Service: "web", Message: "timeout", Severity: ports.SeverityError}},
	}}
	s, _ := service(t, m)
	res, err := s.Test(context.Background(), knownissues.Match{Services: []string{"api"}, MessageRegex: "timeout"})
	require.NoError(t, err)
	assert.Equal(t, TestResult{WouldMatch: 1, SampleIssueIDs: []string{"a"}}, res)
	var ve *ports.ValidationError
	_, err = s.Test(context.Background(), knownissues.Match{})
	assert.ErrorAs(t, err, &ve, "an empty rule would match everything")
	_, err = s.Test(context.Background(), knownissues.Match{Services: []string{"api"}, MessageRegex: "("})
	assert.ErrorAs(t, err, &ve)
}
