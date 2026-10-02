// Package wiz reads Wiz security issues into the Inbox as security findings (plan § 8.8, § 8.9, § 8.16):
// pushed by a Wiz webhook integration (issue created/reopened) or polled from the Wiz GraphQL API. One
// issue per control/rule and resource: fp = sha256("security" + "wiz" + rule + resource).
package wiz

import (
	"strings"

	"github.com/GokulMV/DocTheRepo/internal/adapters/signal/sigutil"
	"github.com/GokulMV/DocTheRepo/internal/core/signals"
	"github.com/GokulMV/DocTheRepo/internal/ports"
)

// Adapter implements ports.SignalWebhook for Wiz webhook integrations. Wiz sends the connector secret as
// a bearer token, basic-auth password, or custom header (configured on the integration).
type Adapter struct{}

// New returns the webhook adapter.
func New() Adapter { return Adapter{} }

// Source implements ports.SignalWebhook.
func (Adapter) Source() string { return "wiz" }

// Verify implements ports.SignalWebhook.
func (Adapter) Verify(req ports.WebhookRequest, cc ports.ConnectorConfig) error {
	return sigutil.VerifyToken(req, cc.WebhookSecret)
}

// closed issue states and trigger types that are not new problems.
var closed = map[string]bool{"RESOLVED": true, "REJECTED": true, "CLOSED": true}

// Parse implements ports.SignalWebhook. It reads the default Wiz issue payload ({trigger, issue, resource,
// control}) and the GraphQL issue shape ({id, sourceRule, entitySnapshot, ...}) alike.
func (Adapter) Parse(req ports.WebhookRequest, cc ports.ConnectorConfig) ([]ports.SignalEvent, error) {
	v, err := sigutil.Decode(req.Body)
	if err != nil {
		return nil, err
	}
	if t := strings.ToLower(sigutil.Str(v, "trigger.type")); t == "resolved" || t == "rejected" {
		return nil, nil
	}
	issue := sigutil.Get(v, "issue")
	if issue == nil {
		issue = v
	}
	if closed[strings.ToUpper(sigutil.Str(issue, "status"))] {
		return nil, nil
	}
	ev, ok := Event(v, issue, cc)
	if !ok {
		return nil, sigutil.Malformed("Wiz payload has no issue id")
	}
	return []ports.SignalEvent{ev}, nil
}

// Event maps a Wiz issue (root holds webhook siblings such as control and resource; issue is the issue
// object) to a security finding.
func Event(root, issue any, cc ports.ConnectorConfig) (ports.SignalEvent, bool) {
	id := sigutil.First(issue, "id")
	if id == "" {
		return ports.SignalEvent{}, false
	}
	rule := sigutil.First(root, "control.id", "trigger.ruleId")
	if rule == "" {
		rule = sigutil.First(issue, "sourceRule.id", "control.id")
	}
	ruleName := sigutil.First(root, "control.name", "trigger.ruleName")
	if ruleName == "" {
		ruleName = sigutil.First(issue, "sourceRule.name", "control.name")
	}
	res := sigutil.Get(root, "resource")
	if res == nil {
		res = sigutil.Get(issue, "entitySnapshot")
	}
	resID := sigutil.First(res, "providerId", "externalId", "id")
	resName := sigutil.First(res, "name")
	sev := sigutil.First(issue, "severity")
	if sev == "" {
		sev = sigutil.First(root, "control.severity", "severity")
	}
	title := ruleName
	if title == "" {
		title = "Wiz issue " + id
	}
	if resName != "" {
		title += " on " + resName
	}
	tags := sigutil.StrMap(res, "tags")
	serviceTag := cc.Config["service_tag"]
	if serviceTag == "" {
		serviceTag = "service"
	}
	ev := ports.SignalEvent{
		ConnectorID: cc.ID, Source: "wiz", Kind: ports.KindSecurityFinding, ExternalID: id,
		OccurredAt: sigutil.Time(sigutil.First(issue, "updatedAt", "statusChangedAt", "createdAt", "created")),
		Severity:   signals.MapSeverity("wiz", sev, ports.SeverityWarning), Title: title,
		Message: sigutil.First(root, "control.description"), RuleID: rule, ResourceID: resID,
		Service:     firstNonEmpty(tags[serviceTag], tags["app"], tags["application"]),
		Environment: firstNonEmpty(tags["env"], tags["environment"], sigutil.First(res, "subscriptionName")),
		Attrs: map[string]string{"wiz.issue_id": id, "wiz.status": sigutil.Str(issue, "status"), "severity": sev,
			"cloud": sigutil.First(res, "cloudPlatform"), "region": sigutil.First(res, "region"),
			"resource_type": sigutil.First(res, "nativeType", "type"), "resource_name": resName,
			"subscription": sigutil.First(res, "subscriptionName", "subscriptionExternalId", "subscriptionId"),
			"url":          firstNonEmpty(sigutil.First(issue, "url", "issueUrl"), "https://app.wiz.io/issues#~(issue~'"+id+")")},
	}
	if ev.Message == "" {
		ev.Message = sigutil.First(issue, "description", "sourceRule.description")
	}
	var projects []string
	for _, p := range sigutil.Slice(issue, "projects") {
		if n := sigutil.Str(p, "name"); n != "" {
			projects = append(projects, n)
		}
	}
	if len(projects) > 0 {
		ev.Attrs["wiz.projects"] = strings.Join(projects, ",")
	}
	for k, v := range ev.Attrs {
		if v == "" {
			delete(ev.Attrs, k)
		}
	}
	return ev, true
}

func firstNonEmpty(vs ...string) string {
	for _, v := range vs {
		if v != "" {
			return v
		}
	}
	return ""
}
