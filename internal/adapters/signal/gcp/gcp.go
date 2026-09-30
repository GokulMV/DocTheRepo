// Package gcp parses Google Cloud Monitoring webhook notification-channel payloads: an open incident is an
// alert (closed incidents are not). Cloud Monitoring supports basic auth or a token in the URL for webhook
// channels: use the connector secret for either.
package gcp

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
func (Adapter) Source() string { return "gcp" }

// Verify implements ports.SignalWebhook.
func (Adapter) Verify(req ports.WebhookRequest, cc ports.ConnectorConfig) error {
	return sigutil.VerifyToken(req, cc.WebhookSecret)
}

// Parse implements ports.SignalWebhook.
func (Adapter) Parse(req ports.WebhookRequest, cc ports.ConnectorConfig) ([]ports.SignalEvent, error) {
	v, err := sigutil.Decode(req.Body)
	if err != nil {
		return nil, err
	}
	in := sigutil.Get(v, "incident")
	if in == nil {
		return nil, sigutil.Malformed("Cloud Monitoring payload has no incident")
	}
	if sigutil.Str(in, "state") != "open" {
		return nil, nil
	}
	labels := sigutil.Merge(sigutil.StrMap(in, "policy_user_labels"), sigutil.StrMap(in, "metadata.user_labels"), true)
	resLabels := sigutil.StrMap(in, "resource.labels")
	attrs := map[string]string{"gcp.resource_type": sigutil.First(in, "resource.type", "resource_type_display_name"),
		"gcp.metric": sigutil.Str(in, "metric.type"), "gcp.project": sigutil.Str(in, "scoping_project_id"), "url": sigutil.Str(in, "url"),
		"observed_value": sigutil.Str(in, "observed_value"), "threshold_value": sigutil.Str(in, "threshold_value")}
	for k, val := range resLabels {
		attrs["gcp.resource.labels."+k] = val
	}
	for k, val := range labels {
		attrs[k] = val
	}
	ev := ports.SignalEvent{
		ConnectorID: cc.ID, Source: "gcp", Kind: ports.KindAlert, ExternalID: sigutil.Str(in, "incident_id"),
		OccurredAt: sigutil.Time(sigutil.Str(in, "started_at")),
		Severity:   signals.MapSeverity("gcp", sigutil.Str(in, "severity"), ports.SeverityWarning),
		Title:      firstNonEmpty(sigutil.Str(in, "summary"), sigutil.Str(in, "policy_name")),
		Message:    sigutil.Str(in, "documentation.content"),
		RuleID:     sigutil.Str(in, "policy_name") + "/" + sigutil.Str(in, "condition_name"),
		Service: firstNonEmpty(labels["service"], resLabels["service_name"], resLabels["service"], resLabels["module_id"],
			resLabels["function_name"], resLabels["container_name"]),
		Environment: firstNonEmpty(labels["env"], labels["environment"]),
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
