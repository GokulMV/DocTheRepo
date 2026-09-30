package parity

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/GokulMV/DocTheRepo/internal/adapters/signal/registry"
	"github.com/GokulMV/DocTheRepo/internal/core/signals"
	"github.com/GokulMV/DocTheRepo/internal/ports"
)

// signalFixture is one real-world payload (test/testdata/signals/<source>/<case>.json) and the events it
// must normalize to. Expect entries list only the fields that matter for the case.
type signalFixture struct {
	Description string            `json:"description"`
	Headers     map[string]string `json:"headers"`
	Config      map[string]string `json:"config"`
	Body        json.RawMessage   `json:"body"`
	Expect      []struct {
		Source        string            `json:"source"`
		Kind          string            `json:"kind"`
		Severity      string            `json:"severity"`
		Service       string            `json:"service"`
		Environment   string            `json:"environment"`
		Title         string            `json:"title"`
		ExceptionType string            `json:"exception_type"`
		RuleID        string            `json:"rule_id"`
		GroupID       string            `json:"group_id"`
		ExternalID    string            `json:"external_id"`
		ResourceID    string            `json:"resource_id"`
		Frames        []string          `json:"frames"`
		Attrs         map[string]string `json:"attrs"`
	} `json:"expect"`
}

const secret = "parity-secret-0123456789"

// sign authenticates a delivery the way each source does.
func sign(source string, body []byte, key string, h map[string][]string) {
	mac := hmac.New(sha256.New, []byte(key))
	mac.Write(body)
	sig := hex.EncodeToString(mac.Sum(nil))
	switch source {
	case "sentry":
		h["Sentry-Hook-Signature"] = []string{sig}
	case "pagerduty":
		h["X-Pagerduty-Signature"] = []string{"v1=" + hex.EncodeToString(make([]byte, 32)) + ",v1=" + sig} // rotation: any matching value
	default:
		h["Authorization"] = []string{"Bearer " + key}
	}
}

func fixturesDir() string {
	_, f, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(f), "..", "testdata", "signals")
}

// TestSignalWebhookParity runs the same suite over every signal push adapter (plan § 10: adapter parity
// suites are mandatory and never weakened).
func TestSignalWebhookParity(t *testing.T) {
	adapters := registry.Webhooks()
	var sources []string
	for s := range adapters {
		sources = append(sources, s)
	}
	sort.Strings(sources)
	now := time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)
	for _, source := range sources {
		a := adapters[source]
		t.Run(source, func(t *testing.T) {
			files, _ := filepath.Glob(filepath.Join(fixturesDir(), source, "*.json"))
			require.GreaterOrEqual(t, len(files), 2, "every adapter needs a problem fixture and a non-problem fixture")
			sawEmpty, sawEvents := false, false
			for _, f := range files {
				raw, err := os.ReadFile(f)
				require.NoError(t, err)
				var fx signalFixture
				require.NoError(t, json.Unmarshal(raw, &fx), f)
				t.Run(filepath.Base(f), func(t *testing.T) {
					cc := ports.ConnectorConfig{ID: "conn-1", Type: source, WebhookSecret: secret, Config: fx.Config}
					req := func(key string) ports.WebhookRequest {
						h := map[string][]string{}
						for k, v := range fx.Headers {
							h[k] = []string{v}
						}
						if key != "" {
							sign(source, fx.Body, key, h)
						}
						return ports.WebhookRequest{Header: h, Body: fx.Body}
					}
					require.NoError(t, a.Verify(req(secret), cc), "a correctly authenticated delivery verifies")
					assert.ErrorIs(t, a.Verify(req("wrong-secret-xxxxxxxxxx"), cc), ports.ErrInvalidSignature)
					assert.ErrorIs(t, a.Verify(req(""), cc), ports.ErrInvalidSignature, "unauthenticated deliveries are rejected")
					noSecret := cc
					noSecret.WebhookSecret = ""
					assert.ErrorIs(t, a.Verify(req(""), noSecret), ports.ErrInvalidSignature, "an unset secret never accepts anything")

					events, err := a.Parse(req(secret), cc)
					require.NoError(t, err)
					require.Len(t, events, len(fx.Expect), fx.Description)
					again, err := a.Parse(req(secret), cc)
					require.NoError(t, err)
					for i := range events {
						require.NoError(t, signals.Prepare(&events[i], signals.Options{Now: func() time.Time { return now }}))
						require.NoError(t, signals.Prepare(&again[i], signals.Options{Now: func() time.Time { return now }}))
						ev, want := events[i], fx.Expect[i]
						assert.Equal(t, ev.Fingerprint, again[i].Fingerprint, "parsing is deterministic")
						assert.Len(t, ev.Fingerprint, 64)
						assert.NotEmpty(t, ev.ExternalID)
						assert.NotEmpty(t, ev.Title)
						check := func(field, want, got string) {
							if want != "" {
								assert.Equal(t, want, got, field)
							}
						}
						check("source", want.Source, ev.Source)
						check("kind", want.Kind, string(ev.Kind))
						check("severity", want.Severity, string(ev.Severity))
						check("service", want.Service, ev.Service)
						check("environment", want.Environment, ev.Environment)
						check("title", want.Title, ev.Title)
						check("exception_type", want.ExceptionType, ev.ExceptionType)
						check("rule_id", want.RuleID, ev.RuleID)
						check("group_id", want.GroupID, ev.GroupID)
						check("external_id", want.ExternalID, ev.ExternalID)
						check("resource_id", want.ResourceID, ev.ResourceID)
						if want.Frames != nil {
							assert.Equal(t, want.Frames, signals.Frames(ev.Stack), "frames")
						}
						for k, v := range want.Attrs {
							assert.Equal(t, v, ev.Attrs[k], "attr %s", k)
						}
					}
					if len(events) == 0 {
						sawEmpty = true
					} else {
						sawEvents = true
					}
				})
			}
			assert.True(t, sawEvents, "at least one fixture produces events")
			assert.True(t, sawEmpty, "at least one fixture is a non-problem (resolved, acknowledged, OK, empty)")

			bad := ports.WebhookRequest{Header: map[string][]string{}, Body: []byte(`{not json`)}
			_, err := a.Parse(bad, ports.ConnectorConfig{ID: "c", Type: source, WebhookSecret: secret})
			var ve *ports.ValidationError
			assert.ErrorAs(t, err, &ve, "malformed payloads are a 400, not a crash")
		})
	}
}

func TestSignalRegistryPaths(t *testing.T) {
	assert.True(t, registry.Accepts("sentry", "sentry"))
	assert.False(t, registry.Accepts("sentry", "datadog"))
	assert.True(t, registry.Accepts("aws", "cloudwatch"))
	assert.True(t, registry.Accepts("aws", "eventbridge"))
	assert.False(t, registry.Accepts("aws", "aws"))
	assert.Equal(t, "/hooks/aws/x", registry.WebhookPath("cloudwatch", "x"))
	assert.Equal(t, "/hooks/sentry/x", registry.WebhookPath("sentry", "x"))
	assert.Equal(t, "/hooks/github/x", registry.WebhookPath("github", "x"))
	assert.Equal(t, "", registry.WebhookPath("kafka", "x"), "pull-only sources have no webhook")
}
