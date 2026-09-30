// Package registry lists the signal push adapters and which connector types may receive on each
// /hooks/{source}/{connector_id} path.
package registry

import (
	"github.com/GokulMV/DocTheRepo/internal/adapters/signal/alertmanager"
	"github.com/GokulMV/DocTheRepo/internal/adapters/signal/aws"
	"github.com/GokulMV/DocTheRepo/internal/adapters/signal/datadog"
	"github.com/GokulMV/DocTheRepo/internal/adapters/signal/gcp"
	"github.com/GokulMV/DocTheRepo/internal/adapters/signal/generic"
	"github.com/GokulMV/DocTheRepo/internal/adapters/signal/opsgenie"
	"github.com/GokulMV/DocTheRepo/internal/adapters/signal/pagerduty"
	"github.com/GokulMV/DocTheRepo/internal/adapters/signal/sentry"
	"github.com/GokulMV/DocTheRepo/internal/ports"
)

// Webhooks returns every push adapter keyed by its path segment.
func Webhooks() map[string]ports.SignalWebhook {
	out := map[string]ports.SignalWebhook{}
	for _, a := range []ports.SignalWebhook{sentry.New(), pagerduty.New(), opsgenie.New(), datadog.New(), alertmanager.New(),
		alertmanager.NewGrafana(), aws.New(), gcp.New(), generic.New()} {
		out[a.Source()] = a
	}
	return out
}

// connectorTypes maps a path segment to the connector types that may receive there (the path is the
// connector type, except AWS: EventBridge API destinations belong to CloudWatch or EventBridge connectors).
var connectorTypes = map[string][]string{"aws": {"cloudwatch", "eventbridge"}}

// Accepts reports whether a connector of type connectorType may receive deliveries for source.
func Accepts(source, connectorType string) bool {
	if types, ok := connectorTypes[source]; ok {
		for _, t := range types {
			if t == connectorType {
				return true
			}
		}
		return false
	}
	return source == connectorType
}

// WebhookPath is where a connector receives pushes ("" when its type has no push adapter).
func WebhookPath(connectorType, connectorID string) string {
	switch connectorType {
	case "github", "gitlab", "firehose":
		return "/hooks/" + connectorType + "/" + connectorID
	case "cloudwatch", "eventbridge":
		return "/hooks/aws/" + connectorID
	}
	if _, ok := Webhooks()[connectorType]; ok {
		return "/hooks/" + connectorType + "/" + connectorID
	}
	return ""
}
