package knownissues

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/GokulMV/DocTheRepo/internal/ports"
)

var now = time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)

func ev() *ports.SignalEvent {
	return &ports.SignalEvent{Source: "sentry", Fingerprint: "fp1", Service: "checkout", Environment: "staging", Severity: ports.SeverityError,
		Title: "TimeoutError", Message: "timeout after 30s calling payments-gw", Attrs: map[string]string{"http.status": "503", "region": "eu"}}
}

func matcher(t *testing.T, rules ...Rule) *Matcher {
	t.Helper()
	m, errs := Compile(rules)
	require.Empty(t, errs)
	return m
}

func TestMatch_EachField(t *testing.T) {
	cases := map[string]struct {
		m       Match
		mutate  func(*ports.SignalEvent)
		matches bool
	}{
		"fingerprint":          {Match{Fingerprints: []string{"fp1"}}, nil, true},
		"fingerprint miss":     {Match{Fingerprints: []string{"fp2"}}, nil, false},
		"alt fingerprint":      {Match{Fingerprints: []string{"alt"}}, func(e *ports.SignalEvent) { e.AltFingerprint = "alt" }, true},
		"regex message":        {Match{MessageRegex: `timeout .* payments-gw`}, nil, true},
		"regex title":          {Match{MessageRegex: `^TimeoutError$`}, nil, true},
		"regex miss":           {Match{MessageRegex: `refused`}, nil, false},
		"service case":         {Match{Services: []string{"Checkout"}}, nil, true},
		"service miss":         {Match{Services: []string{"cart"}}, nil, false},
		"source+service":       {Match{Sources: []string{"SENTRY"}, Services: []string{"checkout"}}, nil, true},
		"source miss":          {Match{Sources: []string{"datadog"}, Services: []string{"checkout"}}, nil, false},
		"env":                  {Match{Services: []string{"checkout"}, Environments: []string{"staging"}}, nil, true},
		"env miss":             {Match{Services: []string{"checkout"}, Environments: []string{"prod"}}, nil, false},
		"attrs":                {Match{Attrs: map[string]string{"http.status": "503"}}, nil, true},
		"attrs all":            {Match{Attrs: map[string]string{"http.status": "503", "region": "us"}}, nil, false},
		"attrs missing":        {Match{Attrs: map[string]string{"http.status": "503"}}, func(e *ports.SignalEvent) { e.Attrs = nil }, false},
		"min severity":         {Match{Services: []string{"checkout"}, MinSeverity: ports.SeverityWarning}, nil, true},
		"min severity miss":    {Match{Services: []string{"checkout"}, MinSeverity: ports.SeverityCritical}, nil, false},
		"max severity":         {Match{Services: []string{"checkout"}, MaxSeverity: ports.SeverityError}, nil, true},
		"max severity miss":    {Match{Services: []string{"checkout"}, MaxSeverity: ports.SeverityWarning}, nil, false},
		"severity only range":  {Match{MinSeverity: ports.SeverityInfo, MaxSeverity: ports.SeverityWarning}, nil, false},
		"AND of fp and regex":  {Match{Fingerprints: []string{"fp1"}, MessageRegex: "refused"}, nil, false},
		"AND of svc and attrs": {Match{Services: []string{"checkout"}, Attrs: map[string]string{"region": "eu"}}, nil, true},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			m := matcher(t, Rule{ID: "r", Match: c.m, Action: Suppress})
			e := ev()
			if c.mutate != nil {
				c.mutate(e)
			}
			res, ok := m.Match(e, now)
			assert.Equal(t, c.matches, ok)
			if ok {
				assert.Equal(t, Result{RuleID: "r", Action: Suppress}, res)
			}
		})
	}
}

func TestMatch_ExpiryAndPrecedence(t *testing.T) {
	past, future := now.Add(-time.Minute), now.Add(time.Hour)
	m := matcher(t,
		Rule{ID: "expired", Match: Match{Fingerprints: []string{"fp1"}}, Action: Suppress, ExpiresAt: &past},
		Rule{ID: "label", Match: Match{Services: []string{"checkout"}}, Action: LabelOnly},
		Rule{ID: "mute", Match: Match{MessageRegex: "timeout"}, Action: Suppress, ExpiresAt: &future},
		Rule{ID: "mute2", Match: Match{Fingerprints: []string{"fp1"}}, Action: Suppress},
	)
	res, ok := m.Match(ev(), now)
	require.True(t, ok)
	assert.Equal(t, "mute", res.RuleID, "suppress beats label-only; the earliest suppressing rule wins; expired rules never match")
	res, ok = m.Match(ev(), future)
	require.True(t, ok)
	assert.Equal(t, "mute2", res.RuleID, "the 7-day mute ended")

	only := matcher(t, Rule{ID: "label", Match: Match{Services: []string{"checkout"}}, Action: LabelOnly})
	res, ok = only.Match(ev(), now)
	require.True(t, ok)
	assert.Equal(t, LabelOnly, res.Action)

	var nilM *Matcher
	_, ok = nilM.Match(ev(), now)
	assert.False(t, ok)
}

func TestCompile_SkipsInvalidRulesAndValidates(t *testing.T) {
	m, errs := Compile([]Rule{
		{ID: "bad-regex", Match: Match{MessageRegex: "("}, Action: Suppress},
		{ID: "empty", Match: Match{}, Action: Suppress},
		{ID: "ok", Match: Match{Services: []string{"checkout"}}, Action: Suppress},
	})
	assert.Len(t, errs, 2)
	assert.Equal(t, 1, m.Len())
	_, ok := m.Match(ev(), now)
	assert.True(t, ok, "one broken rule never disables the rest")

	for name, c := range map[string]struct {
		m Match
		a Action
	}{
		"bad action":     {Match{Services: []string{"x"}}, "hide"},
		"source only":    {Match{Sources: []string{"sentry"}}, Suppress},
		"env only":       {Match{Environments: []string{"prod"}}, Suppress},
		"bad severity":   {Match{Services: []string{"x"}, MinSeverity: "loud"}, Suppress},
		"inverted range": {Match{Services: []string{"x"}, MinSeverity: ports.SeverityCritical, MaxSeverity: ports.SeverityInfo}, Suppress},
		"regex too long": {Match{MessageRegex: string(make([]byte, MaxRegex+1))}, Suppress},
	} {
		assert.Error(t, Validate(c.m, c.a), name)
	}
	assert.NoError(t, Validate(Match{Sources: []string{"sentry"}, Services: []string{"x"}}, LabelOnly))
}

func BenchmarkMatch(b *testing.B) {
	var rules []Rule
	for i := 0; i < 2000; i++ {
		rules = append(rules, Rule{ID: "r", Match: Match{Fingerprints: []string{string(rune(i + 1000))}}, Action: Suppress})
	}
	for i := 0; i < 200; i++ {
		rules = append(rules, Rule{ID: "s", Match: Match{Services: []string{"svc" + string(rune(i+'a'))}, MessageRegex: "timeout"}, Action: Suppress})
	}
	m, _ := Compile(rules)
	e := ev()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		m.Match(e, now)
	}
}
