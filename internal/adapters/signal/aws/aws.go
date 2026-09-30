// Package aws parses Amazon EventBridge events delivered by an API destination (one event per request, or
// an array): CloudWatch alarm state changes (only transitions into ALARM), GuardDuty findings (security),
// AWS Health events, and any other event as a generic alert. EventBridge API destinations send a
// configured header: use the connector secret as an API-key header, bearer token, or basic-auth password.
package aws

import (
	"encoding/json"
	"strconv"

	"github.com/GokulMV/DocTheRepo/internal/adapters/signal/sigutil"
	"github.com/GokulMV/DocTheRepo/internal/ports"
)

// Adapter implements ports.SignalWebhook.
type Adapter struct{}

// New returns the adapter.
func New() Adapter { return Adapter{} }

// Source implements ports.SignalWebhook.
func (Adapter) Source() string { return "aws" }

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
	items, ok := v.([]any)
	if !ok {
		items = []any{v}
	}
	var out []ports.SignalEvent
	for _, it := range items {
		if sigutil.Str(it, "detail-type") == "" {
			return nil, sigutil.Malformed("not an EventBridge event (no detail-type)")
		}
		if ev, ok := Event(it, cc.ID); ok {
			out = append(out, ev)
		}
	}
	return out, nil
}

// Event maps one EventBridge event (also used for events arriving through Firehose).
func Event(e any, connectorID string) (ports.SignalEvent, bool) {
	dt := sigutil.Str(e, "detail-type")
	d := sigutil.Get(e, "detail")
	base := ports.SignalEvent{ConnectorID: connectorID, Source: "cloudwatch", ExternalID: sigutil.Str(e, "id"),
		OccurredAt: sigutil.Time(sigutil.Str(e, "time")),
		Attrs:      map[string]string{"aws.region": sigutil.Str(e, "region"), "aws.account": sigutil.Str(e, "account"), "aws.source": sigutil.Str(e, "source")}}
	switch dt {
	case "CloudWatch Alarm State Change":
		if sigutil.Str(d, "state.value") != "ALARM" {
			return ports.SignalEvent{}, false // OK / INSUFFICIENT_DATA are not problems
		}
		base.Kind, base.Severity = ports.KindAlert, ports.SeverityError
		base.RuleID = sigutil.First(e, "resources.0", "detail.alarmName")
		base.Title = sigutil.Str(d, "alarmName")
		base.Message = sigutil.Str(d, "state.reason")
		if t := sigutil.Time(sigutil.Str(d, "state.timestamp")); !t.IsZero() {
			base.OccurredAt = t
		}
		base.Attrs["alarm_name"] = sigutil.Str(d, "alarmName")
		base.Attrs["alarm_description"] = sigutil.Str(d, "configuration.description")
		base.Attrs["metric_namespace"] = sigutil.First(d, "configuration.metrics.0.metricStat.metric.namespace")
		base.Attrs["metric_name"] = sigutil.First(d, "configuration.metrics.0.metricStat.metric.name")
		for k, val := range sigutil.StrMap(d, "configuration.metrics.0.metricStat.metric.dimensions") {
			base.Attrs["dimension."+k] = val
		}
	case "GuardDuty Finding":
		base.Source, base.Kind = "guardduty", ports.KindSecurityFinding
		base.RuleID = sigutil.Str(d, "type")
		base.ResourceID = sigutil.First(d, "resource.instanceDetails.instanceId", "resource.accessKeyDetails.accessKeyId",
			"resource.s3BucketDetails.0.name", "resource.resourceType")
		base.Title = sigutil.Str(d, "title")
		base.Message = sigutil.Str(d, "description")
		base.Severity = guardDutySeverity(sigutil.Str(d, "severity"))
		base.ExternalID = sigutil.First(d, "id", "arn") + ":" + sigutil.Str(d, "updatedAt")
	case "AWS Health Event":
		base.Kind, base.Severity = ports.KindAlert, ports.SeverityWarning
		base.RuleID = sigutil.Str(d, "eventTypeCode")
		base.Title = sigutil.First(d, "eventTypeCode", "service")
		base.Message = sigutil.Str(d, "eventDescription.0.latestDescription")
		base.Service = sigutil.Str(d, "service")
	default:
		base.Kind, base.Severity = ports.KindAlert, ports.SeverityWarning
		base.RuleID = sigutil.Str(e, "source") + ":" + dt
		base.Title = dt
		if raw, err := json.Marshal(d); err == nil {
			base.Message = string(raw)
		}
	}
	return base, true
}

// guardDutySeverity maps GuardDuty's 0–10 score (low < 4, medium < 7, high < 9, critical ≥ 9).
func guardDutySeverity(s string) ports.Severity {
	f, _ := strconv.ParseFloat(s, 64)
	switch {
	case f >= 9:
		return ports.SeverityCritical
	case f >= 7:
		return ports.SeverityError
	case f >= 4:
		return ports.SeverityWarning
	}
	return ports.SeverityInfo
}
