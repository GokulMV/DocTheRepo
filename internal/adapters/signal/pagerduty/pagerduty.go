// Package pagerduty parses PagerDuty v3 webhooks. Incident triggered/reopened/escalated events are alerts;
// acknowledgements, resolutions, and annotations are not new problems. Deliveries are signed:
// X-PagerDuty-Signature lists one or more "v1=<hex HMAC-SHA256>" values (several during secret rotation).
package pagerduty

import (
	"strings"

	"github.com/GokulMV/DocTheRepo/internal/adapters/signal/sigutil"
	"github.com/GokulMV/DocTheRepo/internal/core/signals"
	"github.com/GokulMV/DocTheRepo/internal/ports"
)

// Adapter implements ports.SignalWebhook.
type Adapter struct{}

// New returns the adapter.
func New() Adapter { return Adapter{} }

// Source implements ports.SignalWebhook.
func (Adapter) Source() string { return "pagerduty" }

// Verify implements ports.SignalWebhook.
func (Adapter) Verify(req ports.WebhookRequest, cc ports.ConnectorConfig) error {
	for _, part := range strings.Split(req.HeaderValue("X-PagerDuty-Signature"), ",") {
		if sig, ok := strings.CutPrefix(strings.TrimSpace(part), "v1="); ok && sigutil.VerifyHMAC(req.Body, cc.WebhookSecret, sig) == nil {
			return nil
		}
	}
	return ports.ErrInvalidSignature
}

var problems = map[string]bool{"incident.triggered": true, "incident.reopened": true, "incident.escalated": true}

// Parse implements ports.SignalWebhook.
func (Adapter) Parse(req ports.WebhookRequest, cc ports.ConnectorConfig) ([]ports.SignalEvent, error) {
	v, err := sigutil.Decode(req.Body)
	if err != nil {
		return nil, err
	}
	e := sigutil.Get(v, "event")
	if e == nil {
		return nil, sigutil.Malformed("PagerDuty payload has no event (v3 webhooks are supported)")
	}
	if !problems[sigutil.Str(e, "event_type")] {
		return nil, nil
	}
	d := sigutil.Get(e, "data")
	sev := signals.MapSeverity("pagerduty", sigutil.Str(d, "urgency"), ports.SeverityError)
	if p := strings.ToUpper(sigutil.Str(d, "priority.summary")); p == "P1" || p == "SEV-1" || p == "SEV1" {
		sev = ports.SeverityCritical
	}
	ev := ports.SignalEvent{
		ConnectorID: cc.ID, Source: "pagerduty", Kind: ports.KindAlert, ExternalID: sigutil.Str(e, "id"),
		OccurredAt: sigutil.Time(sigutil.Str(e, "occurred_at")), Severity: sev,
		Title:   sigutil.Str(d, "title"),
		Service: sigutil.First(d, "service.summary", "service.name"),
		// Incidents from the same PagerDuty service with the same (normalized) title are one issue.
		RuleID: sigutil.Str(d, "service.id"),
		Attrs: map[string]string{"pagerduty.incident_id": sigutil.Str(d, "id"), "pagerduty.number": sigutil.Str(d, "number"),
			"priority": sigutil.Str(d, "priority.summary"), "urgency": sigutil.Str(d, "urgency"), "url": sigutil.Str(d, "html_url"),
			"event_type": sigutil.Str(e, "event_type")},
	}
	if ev.RuleID != "" {
		ev.RuleID += ":" + signals.NormalizeMessage(ev.Title)
	}
	return []ports.SignalEvent{ev}, nil
}
