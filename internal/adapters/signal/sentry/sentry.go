// Package sentry parses Sentry integration-platform webhooks: error and event-alert payloads (with the
// exception, stack trace, and Sentry's own issue ID as the group), issue.created, and metric alerts.
// Deliveries are signed: Sentry-Hook-Signature is a hex HMAC-SHA256 of the body with the client secret.
package sentry

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
func (Adapter) Source() string { return "sentry" }

// Verify implements ports.SignalWebhook.
func (Adapter) Verify(req ports.WebhookRequest, cc ports.ConnectorConfig) error {
	return sigutil.VerifyHMAC(req.Body, cc.WebhookSecret, req.HeaderValue("Sentry-Hook-Signature"))
}

// Parse implements ports.SignalWebhook.
func (Adapter) Parse(req ports.WebhookRequest, cc ports.ConnectorConfig) ([]ports.SignalEvent, error) {
	v, err := sigutil.Decode(req.Body)
	if err != nil {
		return nil, err
	}
	resource := req.HeaderValue("Sentry-Hook-Resource")
	action := sigutil.Str(v, "action")
	switch {
	case resource == "error" || sigutil.Get(v, "data.error") != nil:
		return one(event(sigutil.Get(v, "data.error"), cc, ""))
	case resource == "event_alert" || sigutil.Get(v, "data.event") != nil:
		return one(event(sigutil.Get(v, "data.event"), cc, sigutil.Str(v, "data.triggered_rule")))
	case resource == "metric_alert" || sigutil.Get(v, "data.metric_alert") != nil:
		if action != "critical" && action != "warning" {
			return nil, nil // resolved
		}
		return one(metricAlert(v, action, cc))
	case resource == "issue" || sigutil.Get(v, "data.issue") != nil:
		if action != "created" && action != "unresolved" {
			return nil, nil // resolved, assigned, ignored: not new problems
		}
		return one(issue(sigutil.Get(v, "data.issue"), action, cc))
	case resource == "installation" || resource == "event" || resource == "comment":
		return nil, nil
	}
	return nil, sigutil.Malformed("unrecognised Sentry payload (resource %q)", resource)
}

func one(ev ports.SignalEvent, err error) ([]ports.SignalEvent, error) {
	if err != nil {
		return nil, err
	}
	return []ports.SignalEvent{ev}, nil
}

// tags reads Sentry tags, which arrive either as [["key","value"], …] or [{"key":…,"value":…}, …].
func tags(v any) map[string]string {
	out := map[string]string{}
	for _, t := range sigutil.Slice(v, "tags") {
		switch x := t.(type) {
		case []any:
			if len(x) == 2 {
				k, _ := x[0].(string)
				val, _ := x[1].(string)
				out[k] = val
			}
		case map[string]any:
			out[sigutil.Str(x, "key")] = sigutil.Str(x, "value")
		}
	}
	return out
}

func event(e any, cc ports.ConnectorConfig, rule string) (ports.SignalEvent, error) {
	if e == nil {
		return ports.SignalEvent{}, sigutil.Malformed("Sentry payload has no event")
	}
	t := tags(e)
	ev := ports.SignalEvent{
		ConnectorID: cc.ID, Source: "sentry", Kind: ports.KindError,
		ExternalID:  sigutil.First(e, "event_id", "id"),
		GroupID:     sigutil.First(e, "issue_id", "groupID"),
		OccurredAt:  sigutil.Time(sigutil.First(e, "timestamp", "datetime", "received")),
		Severity:    signals.MapSeverity("sentry", sigutil.First(e, "level"), ports.SeverityError),
		Title:       sigutil.Str(e, "title"),
		Environment: sigutil.First(e, "environment"),
		Attrs:       map[string]string{},
	}
	if ev.Environment == "" {
		ev.Environment = t["environment"]
	}
	// The last exception is the one that was raised; Sentry lists frames oldest first.
	exc := sigutil.Slice(e, "exception.values")
	if len(exc) > 0 {
		last := exc[len(exc)-1]
		ev.ExceptionType = sigutil.Str(last, "type")
		ev.Message = sigutil.Str(last, "value")
		frames := sigutil.Slice(last, "stacktrace.frames")
		for i := len(frames) - 1; i >= 0; i-- {
			f := frames[i]
			ev.Stack = append(ev.Stack, ports.StackFrame{Module: sigutil.First(f, "module", "package"), Function: sigutil.Str(f, "function"),
				File: sigutil.First(f, "filename", "abs_path"), Line: atoi(sigutil.Str(f, "lineno")), InApp: sigutil.Str(f, "in_app") == "true"})
		}
	}
	if ev.Message == "" {
		ev.Message = sigutil.First(e, "message", "logentry.formatted", "logentry.message", "culprit")
	}
	ev.Service = serviceOf(cc, t, sigutil.First(e, "project_slug", "project_name"))
	for k, val := range t {
		ev.Attrs[k] = val
	}
	ev.Attrs["sentry.culprit"] = sigutil.Str(e, "culprit")
	ev.Attrs["url"] = sigutil.First(e, "web_url", "url", "issue_url")
	if rule != "" {
		ev.Attrs["sentry.rule"] = rule
	}
	return ev, nil
}

func issue(is any, action string, cc ports.ConnectorConfig) (ports.SignalEvent, error) {
	if is == nil {
		return ports.SignalEvent{}, sigutil.Malformed("Sentry issue payload has no issue")
	}
	id := sigutil.Str(is, "id")
	ev := ports.SignalEvent{
		ConnectorID: cc.ID, Source: "sentry", Kind: ports.KindError, GroupID: id,
		ExternalID:    "issue:" + id + ":" + action + ":" + sigutil.Str(is, "lastSeen"),
		OccurredAt:    sigutil.Time(sigutil.First(is, "lastSeen", "firstSeen")),
		Severity:      signals.MapSeverity("sentry", sigutil.Str(is, "level"), ports.SeverityError),
		Title:         sigutil.Str(is, "title"),
		ExceptionType: sigutil.Str(is, "metadata.type"),
		Message:       sigutil.First(is, "metadata.value", "culprit"),
		Attrs:         map[string]string{"sentry.short_id": sigutil.Str(is, "shortId"), "sentry.culprit": sigutil.Str(is, "culprit"), "url": sigutil.First(is, "web_url", "permalink")},
	}
	ev.Service = serviceOf(cc, nil, sigutil.Str(is, "project.slug"))
	return ev, nil
}

func metricAlert(v any, action string, cc ports.ConnectorConfig) (ports.SignalEvent, error) {
	ma := sigutil.Get(v, "data.metric_alert")
	ruleID := sigutil.First(ma, "alert_rule.id", "id")
	sev := ports.SeverityWarning
	if action == "critical" {
		sev = ports.SeverityCritical
	}
	ev := ports.SignalEvent{
		ConnectorID: cc.ID, Source: "sentry", Kind: ports.KindAlert, RuleID: "metric_alert:" + ruleID,
		ExternalID: "metric:" + sigutil.Str(ma, "id") + ":" + action, Severity: sev,
		OccurredAt: sigutil.Time(sigutil.First(ma, "date_started", "date_detected")),
		Title:      sigutil.First(v, "data.description_title", "data.metric_alert.alert_rule.name"),
		Message:    sigutil.Str(v, "data.description_text"),
		Attrs:      map[string]string{"url": sigutil.Str(v, "data.web_url")},
	}
	ev.Environment = sigutil.Str(ma, "alert_rule.environment")
	ev.Service = serviceOf(cc, nil, "")
	return ev, nil
}

// serviceOf: an explicit service tag, then the connector's configured service, then the Sentry project.
func serviceOf(cc ports.ConnectorConfig, t map[string]string, project string) string {
	if s := t["service"]; s != "" {
		return s
	}
	if s := cc.Config["service"]; s != "" {
		return s
	}
	return strings.TrimSpace(project)
}

func atoi(s string) int {
	n := 0
	for _, c := range s {
		if c < '0' || c > '9' {
			break
		}
		n = n*10 + int(c-'0')
	}
	return n
}
