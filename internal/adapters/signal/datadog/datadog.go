// Package datadog parses Datadog webhook-integration payloads. The body is operator-templated; the Hub's
// setup guide uses Datadog's standard variables ($ID, $EVENT_TITLE, $TEXT_ONLY_MSG, $ALERT_TYPE,
// $ALERT_TRANSITION, $PRIORITY, $DATE, $TAGS, $ALERT_ID, $AGGREG_KEY, $HOSTNAME, $LINK), and common
// alternative names are accepted. Triggered and warning transitions are alerts; recoveries are not.
// Datadog does not sign payloads: set the connector secret as a custom header or in the URL.
package datadog

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
func (Adapter) Source() string { return "datadog" }

// Verify implements ports.SignalWebhook.
func (Adapter) Verify(req ports.WebhookRequest, cc ports.ConnectorConfig) error {
	return sigutil.VerifyToken(req, cc.WebhookSecret)
}

// Parse implements ports.SignalWebhook. A body may hold one event or an array of them.
func (Adapter) Parse(req ports.WebhookRequest, cc ports.ConnectorConfig) ([]ports.SignalEvent, error) {
	v, err := sigutil.Decode(req.Body)
	if err != nil {
		return nil, err
	}
	items, ok := v.([]any)
	if !ok {
		items = []any{v}
	}
	var out []ports.SignalEvent
	for _, it := range items {
		if _, isObj := it.(map[string]any); !isObj {
			return nil, sigutil.Malformed("Datadog payload must be an object or an array of objects")
		}
		if ev, ok := parseOne(it, cc); ok {
			out = append(out, ev)
		}
	}
	return out, nil
}

func parseOne(v any, cc ports.ConnectorConfig) (ports.SignalEvent, bool) {
	transition := strings.ToLower(sigutil.First(v, "transition", "alert_transition"))
	alertType := strings.ToLower(sigutil.Str(v, "alert_type"))
	switch {
	case strings.Contains(transition, "recover"), alertType == "success" && transition == "":
		return ports.SignalEvent{}, false
	case transition == "" && alertType == "info":
		return ports.SignalEvent{}, false
	}
	tags := sigutil.Tags(sigutil.Get(v, "tags"))
	sev := signals.MapSeverity("datadog", alertType, ports.SeverityWarning)
	if strings.Contains(transition, "warn") {
		sev = ports.SeverityWarning
	}
	if p := strings.ToUpper(sigutil.Str(v, "priority")); p == "P1" {
		sev = ports.SeverityCritical
	}
	attrs := sigutil.Merge(nil, tags, false)
	attrs["host"] = sigutil.Str(v, "hostname")
	attrs["url"] = sigutil.First(v, "url", "link")
	attrs["transition"] = sigutil.First(v, "transition", "alert_transition")
	ev := ports.SignalEvent{
		ConnectorID: cc.ID, Source: "datadog", Kind: ports.KindAlert,
		ExternalID:  sigutil.First(v, "id", "event_id"),
		OccurredAt:  sigutil.Time(sigutil.First(v, "date", "last_updated")),
		Severity:    sev,
		Title:       sigutil.First(v, "title", "event_title"),
		Message:     sigutil.First(v, "msg", "text", "body", "event_msg"),
		RuleID:      sigutil.First(v, "alert_id", "monitor_id", "aggreg_key"),
		Service:     tags["service"],
		Environment: firstNonEmpty(tags["env"], tags["environment"]),
		Attrs:       attrs,
	}
	return ev, true
}

func firstNonEmpty(xs ...string) string {
	for _, x := range xs {
		if x != "" {
			return x
		}
	}
	return ""
}
