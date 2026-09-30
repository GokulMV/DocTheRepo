// Package alertmanager parses Prometheus Alertmanager webhooks and Grafana unified-alerting webhooks (the
// same payload family): one event per firing alert, keyed by the alert's fingerprint and start time so
// repeat notifications of the same firing are not counted twice. Neither tool signs payloads: configure the
// connector secret as the receiver's bearer token or basic-auth password.
package alertmanager

import (
	"strings"

	"github.com/GokulMV/DocTheRepo/internal/adapters/signal/sigutil"
	"github.com/GokulMV/DocTheRepo/internal/core/signals"
	"github.com/GokulMV/DocTheRepo/internal/ports"
)

// Adapter implements ports.SignalWebhook for one of the two sources.
type Adapter struct{ source string }

// New returns the Alertmanager adapter.
func New() Adapter { return Adapter{source: "alertmanager"} }

// NewGrafana returns the Grafana adapter.
func NewGrafana() Adapter { return Adapter{source: "grafana"} }

// Source implements ports.SignalWebhook.
func (a Adapter) Source() string { return a.source }

// Verify implements ports.SignalWebhook.
func (Adapter) Verify(req ports.WebhookRequest, cc ports.ConnectorConfig) error {
	return sigutil.VerifyToken(req, cc.WebhookSecret)
}

// Parse implements ports.SignalWebhook.
func (a Adapter) Parse(req ports.WebhookRequest, cc ports.ConnectorConfig) ([]ports.SignalEvent, error) {
	v, err := sigutil.Decode(req.Body)
	if err != nil {
		return nil, err
	}
	alerts := sigutil.Slice(v, "alerts")
	if alerts == nil {
		return nil, sigutil.Malformed("%s payload has no alerts array", a.source)
	}
	common := sigutil.StrMap(v, "commonLabels")
	var out []ports.SignalEvent
	for _, al := range alerts {
		if !strings.EqualFold(sigutil.Str(al, "status"), "firing") {
			continue
		}
		labels := sigutil.Merge(sigutil.StrMap(al, "labels"), common, true)
		ann := sigutil.StrMap(al, "annotations")
		attrs := map[string]string{}
		for k, val := range labels {
			if !strings.HasPrefix(k, "__") {
				attrs[k] = val
			}
		}
		for k, val := range ann {
			attrs["annotation."+k] = val
		}
		attrs["url"] = sigutil.First(al, "generatorURL", "dashboardURL", "panelURL")
		rule := firstNonEmpty(labels["__alert_rule_uid__"], labels["alertname"])
		start := sigutil.Str(al, "startsAt")
		ev := ports.SignalEvent{
			ConnectorID: cc.ID, Source: a.source, Kind: ports.KindAlert,
			ExternalID:  firstNonEmpty(sigutil.Str(al, "fingerprint"), signals.NormalizeMessage(rule)) + "@" + start,
			OccurredAt:  sigutil.Time(start),
			Severity:    signals.MapSeverity(a.source, firstNonEmpty(labels["severity"], labels["priority"]), ports.SeverityWarning),
			Title:       firstNonEmpty(ann["summary"], ann["title"], labels["alertname"]),
			Message:     firstNonEmpty(ann["description"], ann["message"]),
			RuleID:      rule,
			Service:     firstNonEmpty(labels["service"], labels["app"], labels["application"], labels["job"]),
			Environment: firstNonEmpty(labels["env"], labels["environment"], labels["stage"]),
			Attrs:       attrs,
		}
		if ev.Service == "" {
			ev.Service = labels["namespace"]
		}
		out = append(out, ev)
	}
	return out, nil
}

func firstNonEmpty(xs ...string) string {
	for _, x := range xs {
		if x != "" {
			return x
		}
	}
	return ""
}
