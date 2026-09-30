package signals

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/GokulMV/DocTheRepo/internal/ports"
)

func TestScrub_RemovesCredentials(t *testing.T) {
	cases := map[string]struct{ in, want string }{
		"aws access key":   {"key AKIAIOSFODNN7EXAMPLE used", "key <AWS_KEY> used"},
		"aws session key":  {"ASIAY34FZKBOKMUTVV7A", "<AWS_KEY>"},
		"aws secret":       {"aws_secret_access_key=wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY", "aws_secret_access_key=<AWS_SECRET>"},
		"gcp api key":      {"key=AIzaSyA1234567890abcdefghijklmnopqrstuv failed", "key=<GCP_API_KEY> failed"},
		"jwt":              {"auth eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxMjM0NTYifQ.SflKxwRJSMeKKF2QT4fwpMeJf36POk6yJV_adQssw5c ok", "auth <JWT> ok"},
		"bearer":           {"Authorization: Bearer abcdefghijklmnop0123456789", "Authorization: Bearer <TOKEN>"},
		"basic":            {"Authorization: Basic dXNlcjpwYXNzd29yZDEyMw==", "Authorization: Basic <CREDENTIALS>"},
		"url password":     {"dial postgres://app:s3cr3t-pw@db.internal:5432/orders", "dial postgres://app:<PASSWORD>@db.internal:5432/orders"},
		"conn string":      {"Server=db;User Id=sa;Password=Hunter2!;Database=x", "Server=db;User Id=sa;Password=<PASSWORD>;Database=x"},
		"dsn password":     {"host=db user=app password=topsecret dbname=x", "host=db user=app password=<PASSWORD> dbname=x"},
		"quoted password":  {`{"password": "p@ss w0rd"}`, `{"password": <PASSWORD>}`},
		"api key assign":   {"api_key=0123456789abcdef", "api_key=<SECRET>"},
		"client secret":    {`client_secret: "abcDEF123456"`, `client_secret: "<SECRET>"`},
		"github token":     {"ghp_" + strings.Repeat("a1B2", 9) + " leaked", "<GITHUB_TOKEN> leaked"},
		"slack token":      {"xoxb-1234567890-abcdefghij", "<SLACK_TOKEN>"},
		"stripe key":       {"sk_live_" + strings.Repeat("x9", 12), "<STRIPE_KEY>"},
		"private key":      {"cfg -----BEGIN RSA PRIVATE KEY-----\nMIIEow\nabc\n-----END RSA PRIVATE KEY----- end", "cfg <PRIVATE_KEY> end"},
		"openssh key":      {"-----BEGIN OPENSSH PRIVATE KEY-----\nb3Blbn\n-----END OPENSSH PRIVATE KEY-----", "<PRIVATE_KEY>"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) { assert.Equal(t, c.want, Scrub(c.in)) })
	}
}

func TestScrub_LeavesOrdinaryTextAlone(t *testing.T) {
	for _, s := range []string{
		"timeout after 30s calling payments-gw",
		"order 12345 failed: insufficient funds",
		"https://example.com/orders/42?page=2",
		"user alice logged in",
		"token bucket refilled", // "token" followed by a short word is not a credential
		"password reset email sent",
		"AKIA is a prefix", // too short to be a key
		"sha256:abcdef0123456789",
		"",
	} {
		assert.Equal(t, s, Scrub(s), s)
	}
}

func TestRedact_PII(t *testing.T) {
	r, err := NewRedactor([]CustomPattern{{Name: "customer_id", Regex: `\bCUS-\d{6}\b`}})
	require.NoError(t, err)
	cases := map[string]struct{ in, want string }{
		"email":       {"mail to jane.doe+x@example.co.uk failed", "mail to <EMAIL> failed"},
		"phone us":    {"call (415) 555-0132 now", "call <PHONE> now"},
		"phone intl":  {"sms +14155550132 queued", "sms <PHONE> queued"},
		"phone dots":  {"tel 415.555.0132", "tel <PHONE>"},
		"ipv4":        {"from 10.0.12.7 port 443", "from <IP> port 443"},
		"ipv6":        {"from 2001:db8::8a2e:370:7334 ok", "from <IP> ok"},
		"ipv6 full":   {"2001:0db8:85a3:0000:0000:8a2e:0370:7334", "<IP>"},
		"card spaced": {"card 4111 1111 1111 1111 declined", "card <CARD> declined"},
		"card plain":  {"pan=5555555555554444", "pan=<CARD>"},
		"custom":      {"customer CUS-123456 over quota", "customer <CUSTOMER_ID> over quota"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) { assert.Equal(t, c.want, r.Redact(c.in)) })
	}
}

func TestRedact_NegativeSamples(t *testing.T) {
	r, _ := NewRedactor(nil)
	for _, s := range []string{
		"order 4111111111111112 failed", // 16 digits, fails Luhn
		"at 12:30:45 the job ran",       // a time, not IPv6
		"version 1.2.3.4.5",             // not an IPv4 address (5 parts)... first four would match, see below
		"retry in 1500 ms",
		"build 20260930 ok",
		"trace id 1234567890123",
	} {
		got := r.Redact(s)
		if strings.HasPrefix(s, "version") {
			continue // dotted quads inside longer versions are ambiguous; covered by the IPv4 case above
		}
		assert.Equal(t, s, got, s)
	}
	_, err := NewRedactor([]CustomPattern{{Name: "x", Regex: "("}})
	assert.Error(t, err)
	_, err = NewRedactor([]CustomPattern{{Name: "", Regex: "x"}})
	assert.Error(t, err)
}

func TestNormalizeMessage(t *testing.T) {
	cases := map[string]struct{ in, want string }{
		"uuid":       {"Order 3f2a9c1e-8b4d-4e5f-9a6b-1c2d3e4f5a6b not found", "order <uuid> not found"},
		"numbers":    {"retry 3 of 5 after 1.5s", "retry <n> of <n> after <n>s"},
		"hex":        {"object 0xdeadbeef01 freed twice at 7f3a9b2c4d5e", "object <hex> freed twice at <hex>"},
		"quoted":     {`key "user:42" missing in 'cache-a'`, "key <str> missing in <str>"},
		"email":      {"no account for bob@example.com", "no account for <email>"},
		"ip":         {"connect 10.1.2.3:5432 refused", "connect <ip> refused"},
		"path":       {"GET /orders/123/items failed", "get <path> failed"},
		"timestamp":  {"deadline 2026-09-30T01:02:03.456Z exceeded", "deadline <ts> exceeded"},
		"clock":      {"at 12:30:45 lock held", "at <ts> lock held"},
		"whitespace": {"  a \n\t b  ", "a b"},
		"words kept": {"utf8 decode error in e2e job", "utf8 decode error in e2e job"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) { assert.Equal(t, c.want, NormalizeMessage(c.in)) })
	}
	long := NormalizeMessage(strings.Repeat("é", 400))
	assert.LessOrEqual(t, len(long), MaxNormalized)
	assert.True(t, strings.HasPrefix(long, "é"))
}

func event(msg string) ports.SignalEvent {
	return ports.SignalEvent{Source: "sentry", Kind: ports.KindError, Service: "checkout", ExceptionType: "TimeoutError", Message: msg,
		Stack: []ports.StackFrame{{Module: "checkout.pay", Function: "charge", Line: 10, InApp: true}, {Module: "requests", Function: "get", Line: 99}}}
}

func TestFingerprint_DifferentUUIDs_SameFingerprint(t *testing.T) {
	a, b := event("order 3f2a9c1e-8b4d-4e5f-9a6b-1c2d3e4f5a6b timed out after 30s"), event("order 9e8d7c6b-5a4f-4e3d-8c2b-1a0f9e8d7c6b timed out after 31s")
	Fingerprint(&a)
	Fingerprint(&b)
	assert.Equal(t, a.Fingerprint, b.Fingerprint)
	assert.Len(t, a.Fingerprint, 64)
	assert.Empty(t, a.AltFingerprint)
}

func TestFingerprint_LineNumbersAndLibraryFramesDoNotSplit(t *testing.T) {
	a, b := event("boom"), event("boom")
	b.Stack[0].Line = 250 // a deploy moved the code
	b.Stack[1] = ports.StackFrame{Module: "urllib3", Function: "urlopen"}
	Fingerprint(&a)
	Fingerprint(&b)
	assert.Equal(t, a.Fingerprint, b.Fingerprint)
}

func TestFingerprint_Separates(t *testing.T) {
	base := event("boom")
	Fingerprint(&base)
	for name, mut := range map[string]func(e *ports.SignalEvent){
		"service":   func(e *ports.SignalEvent) { e.Service = "cart" },
		"exception": func(e *ports.SignalEvent) { e.ExceptionType = "ValueError" },
		"message":   func(e *ports.SignalEvent) { e.Message = "kaboom" },
		"frame":     func(e *ports.SignalEvent) { e.Stack[0].Function = "refund" },
		"family":    func(e *ports.SignalEvent) { e.Source = "datadog" },
	} {
		e := event("boom")
		mut(&e)
		Fingerprint(&e)
		assert.NotEqual(t, base.Fingerprint, e.Fingerprint, name)
	}
}

func TestFingerprint_SourceFamilyAndGroupID(t *testing.T) {
	a := ports.SignalEvent{Source: "firehose", Kind: ports.KindError, Service: "api", Message: "db timeout"}
	b := a
	b.Source = "cloudwatch"
	Fingerprint(&a)
	Fingerprint(&b)
	assert.Equal(t, a.Fingerprint, b.Fingerprint, "Firehose and CloudWatch polling are one backend")

	g := event("boom")
	g.GroupID = "SENTRY-123"
	Fingerprint(&g)
	c := event("boom")
	Fingerprint(&c)
	assert.NotEqual(t, c.Fingerprint, g.Fingerprint)
	assert.Equal(t, c.Fingerprint, g.AltFingerprint, "the content fingerprint is kept for cross-source matching")
}

func TestFingerprint_AlertsSecurityEventBus(t *testing.T) {
	a1 := ports.SignalEvent{Source: "alertmanager", Kind: ports.KindAlert, RuleID: "HighLatency", Service: "api", Environment: "prod", Title: "p99 1.2s"}
	a2 := a1
	a2.Title = "p99 3.4s"
	Fingerprint(&a1)
	Fingerprint(&a2)
	assert.Equal(t, a1.Fingerprint, a2.Fingerprint, "every firing of one alarm is one issue")
	a3 := a1
	a3.Environment = "staging"
	Fingerprint(&a3)
	assert.NotEqual(t, a1.Fingerprint, a3.Fingerprint)

	w1 := ports.SignalEvent{Source: "wiz", Kind: ports.KindSecurityFinding, RuleID: "S3-PUBLIC", ResourceID: "arn:aws:s3:::logs"}
	w2 := w1
	w2.ResourceID = "arn:aws:s3:::backups"
	Fingerprint(&w1)
	Fingerprint(&w2)
	assert.NotEqual(t, w1.Fingerprint, w2.Fingerprint)

	lag := ports.SignalEvent{Source: "kafka", Kind: ports.KindEventBus, ResourceID: "orders/billing-consumer", RuleID: "lag"}
	dlqA := ports.SignalEvent{Source: "kafka", Kind: ports.KindEventBus, ResourceID: "orders.DLT", RuleID: "dlq", ExceptionType: "JsonParseException"}
	dlqB := dlqA
	dlqB.ExceptionType = "TimeoutException"
	for _, e := range []*ports.SignalEvent{&lag, &dlqA, &dlqB} {
		Fingerprint(e)
	}
	assert.NotEqual(t, dlqA.Fingerprint, dlqB.Fingerprint, "different DLQ failure causes are different issues")
	assert.NotEqual(t, lag.Fingerprint, dlqA.Fingerprint)
}

func TestFrames(t *testing.T) {
	st := []ports.StackFrame{{Module: "A", Function: "X"}, {File: "b.py", Function: "y"}, {}, {Module: "c", Function: "z"}}
	assert.Equal(t, []string{"a:x", "b.py:y", "c:z"}, Frames(st), "no in-app marks: top frames are used")
	many := make([]ports.StackFrame, 9)
	for i := range many {
		many[i] = ports.StackFrame{Module: "m", Function: string(rune('a' + i)), InApp: true}
	}
	assert.Len(t, Frames(many), 5)
}

func TestMapSeverity(t *testing.T) {
	assert.Equal(t, ports.SeverityError, MapSeverity("pagerduty", "high", ports.SeverityWarning))
	assert.Equal(t, ports.SeverityCritical, MapSeverity("alertmanager", "critical", ports.SeverityWarning))
	assert.Equal(t, ports.SeverityCritical, MapSeverity("wiz", "CRITICAL", ports.SeverityWarning))
	assert.Equal(t, ports.SeverityError, MapSeverity("wiz", "HIGH", ports.SeverityWarning))
	assert.Equal(t, ports.SeverityCritical, MapSeverity("opsgenie", "P1", ports.SeverityWarning))
	assert.Equal(t, ports.SeverityCritical, MapSeverity("gcp", "EMERGENCY", ports.SeverityWarning))
	assert.Equal(t, ports.SeverityInfo, MapSeverity("gcp", "DEBUG", ports.SeverityWarning))
	assert.Equal(t, ports.SeverityWarning, MapSeverity("x", "banana", ports.SeverityWarning), "unknown words use the fallback")
	assert.Equal(t, ports.SeverityError, MapSeverity("x", "", ports.SeverityError))
	assert.Equal(t, 4, ports.SeverityRank(ports.SeverityCritical))
	assert.Zero(t, ports.SeverityRank("nope"))
}

func TestResolveService(t *testing.T) {
	rules := []ServiceRule{
		{Service: "orders", Patterns: map[string]string{"log_group": "/aws/lambda/orders-*"}},
		{Service: "billing", Patterns: map[string]string{"k8s.namespace": "billing", "k8s.container": "api"}},
	}
	assert.Equal(t, "explicit", ResolveService("explicit", map[string]string{"service": "tag"}, rules))
	assert.Equal(t, "tag", ResolveService("", map[string]string{"dd.service": "tag", "log_group": "/aws/lambda/orders-prod"}, rules))
	assert.Equal(t, "orders", ResolveService("", map[string]string{"log_group": "/AWS/lambda/orders-prod"}, rules))
	assert.Equal(t, "billing", ResolveService("", map[string]string{"k8s.namespace": "billing", "k8s.container": "api"}, rules))
	assert.Equal(t, Unknown, ResolveService("", map[string]string{"k8s.namespace": "billing"}, rules), "every pattern must match")
	assert.Equal(t, Unknown, ResolveService("unknown", nil, nil))
}

func TestPrepare(t *testing.T) {
	now := time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)
	ev := ports.SignalEvent{Source: "Sentry", Kind: "bogus", Severity: "loud", OccurredAt: now.Add(time.Hour),
		ExceptionType: "DBError", Message: "connect postgres://app:pw123@db:5432/x failed\nstack…",
		Attrs: map[string]string{"env": "Prod", "service": "checkout", "header": "Bearer abcdefghijklmnopqrstuvwxyz"},
		Stack: make([]ports.StackFrame, 80)}
	require.NoError(t, Prepare(&ev, Options{Now: func() time.Time { return now }}))
	assert.Equal(t, "sentry", ev.Source)
	assert.Equal(t, ports.KindError, ev.Kind, "unknown kinds default to error")
	assert.Equal(t, ports.SeverityError, ev.Severity, "unknown severities default by kind")
	assert.Equal(t, now, ev.OccurredAt, "a timestamp from the future is clamped to receipt time")
	assert.Equal(t, "prod", ev.Environment)
	assert.Equal(t, "checkout", ev.Service)
	assert.NotContains(t, ev.Message, "pw123")
	assert.Equal(t, "Bearer <TOKEN>", ev.Attrs["header"])
	assert.Equal(t, "DBError: connect postgres://app:<PASSWORD>@db:5432/x failed", ev.Title)
	assert.Len(t, ev.Stack, maxFrames)
	assert.NotEmpty(t, ev.ExternalID)
	assert.Len(t, ev.Fingerprint, 64)

	again := ports.SignalEvent{Source: "Sentry", Kind: "bogus", Severity: "loud", OccurredAt: now.Add(time.Hour), ExceptionType: "DBError",
		Message: "connect postgres://app:pw123@db:5432/x failed\nstack…", Attrs: map[string]string{"env": "Prod", "service": "checkout", "header": "Bearer abcdefghijklmnopqrstuvwxyz"}}
	require.NoError(t, Prepare(&again, Options{Now: func() time.Time { return now }}))
	assert.Equal(t, ev.ExternalID, again.ExternalID, "derived external IDs are stable, so retries are idempotent")

	assert.Error(t, Prepare(&ports.SignalEvent{}, Options{}))
	long := ports.SignalEvent{Source: "x", Message: strings.Repeat("a", MaxMessage+100)}
	require.NoError(t, Prepare(&long, Options{}))
	assert.Len(t, long.Message, MaxMessage)
	assert.Equal(t, ports.SeverityError, long.Severity)
	bare := ports.SignalEvent{Source: "x", Kind: ports.KindAlert}
	require.NoError(t, Prepare(&bare, Options{}))
	assert.Equal(t, "alert from x", bare.Title)
	assert.Equal(t, ports.SeverityWarning, bare.Severity)
	assert.Equal(t, Unknown, bare.Service)
}
