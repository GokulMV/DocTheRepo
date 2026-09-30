// Package opsgenie parses Opsgenie outgoing webhooks. Create and escalate actions are alerts; close,
// acknowledge, notes, and snoozes are not new problems. Opsgenie does not sign payloads: configure the
// connector secret as a custom header or bearer token in the integration.
package opsgenie

import (
	"github.com/GokulMV/DocTheRepo/internal/adapters/signal/sigutil"
	"github.com/GokulMV/DocTheRepo/internal/core/signals"
	"github.com/GokulMV/DocTheRepo/internal/ports"
)

// Adapter implements ports.SignalWebhook.
type Adapter struct{}

// New returns the adapter.
func New() Adapter { return Adapter{} }

// Source implements ports.SignalWebhook.
func (Adapter) Source() string { return "opsgenie" }

// Verify implements ports.SignalWebhook.
func (Adapter) Verify(req ports.WebhookRequest, cc ports.ConnectorConfig) error {
	return sigutil.VerifyToken(req, cc.WebhookSecret)
}

var problems = map[string]bool{"Create": true, "EscalateNext": true, "Escalate": true, "UnAcknowledge": true}

// Parse implements ports.SignalWebhook.
func (Adapter) Parse(req ports.WebhookRequest, cc ports.ConnectorConfig) ([]ports.SignalEvent, error) {
	v, err := sigutil.Decode(req.Body)
	if err != nil {
		return nil, err
	}
	a := sigutil.Get(v, "alert")
	if a == nil {
		return nil, sigutil.Malformed("Opsgenie payload has no alert")
	}
	action := sigutil.Str(v, "action")
	if !problems[action] {
		return nil, nil
	}
	tags := sigutil.Tags(sigutil.Get(a, "tags"))
	details := sigutil.StrMap(a, "details")
	attrs := sigutil.Merge(sigutil.Merge(nil, details, false), tags, true)
	attrs["opsgenie.alert_id"] = sigutil.Str(a, "alertId")
	attrs["opsgenie.tiny_id"] = sigutil.Str(a, "tinyId")
	attrs["priority"] = sigutil.Str(a, "priority")
	ev := ports.SignalEvent{
		ConnectorID: cc.ID, Source: "opsgenie", Kind: ports.KindAlert,
		ExternalID:  sigutil.Str(a, "alertId") + ":" + action + ":" + sigutil.Str(a, "updatedAt"),
		OccurredAt:  sigutil.Time(sigutil.First(a, "updatedAt", "createdAt")),
		Severity:    signals.MapSeverity("opsgenie", sigutil.Str(a, "priority"), ports.SeverityWarning),
		Title:       sigutil.Str(a, "message"),
		Message:     sigutil.Str(a, "description"),
		RuleID:      sigutil.First(a, "alias", "message"),
		Service:     firstNonEmpty(details["service"], tags["service"], sigutil.Str(a, "entity")),
		Environment: firstNonEmpty(details["environment"], details["env"], tags["env"], tags["environment"]),
		Attrs:       attrs,
	}
	return []ports.SignalEvent{ev}, nil
}

func firstNonEmpty(xs ...string) string {
	for _, x := range xs {
		if x != "" {
			return x
		}
	}
	return ""
}
