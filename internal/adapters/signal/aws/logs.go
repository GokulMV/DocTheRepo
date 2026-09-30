package aws

import (
	"strings"
	"time"

	"github.com/GokulMV/DocTheRepo/internal/adapters/signal/loglines"
	"github.com/GokulMV/DocTheRepo/internal/core/signals"
	"github.com/GokulMV/DocTheRepo/internal/ports"
)

// LogEvent maps one CloudWatch Logs event, whether it arrived through a subscription (Firehose) or a
// FilterLogEvents poll, so both paths fingerprint identically.
func LogEvent(connectorID, logGroup, logStream, account, eventID string, timestampMs int64, message string) ports.SignalEvent {
	ev := ports.SignalEvent{ConnectorID: connectorID, Source: "cloudwatch", ExternalID: eventID,
		Attrs: map[string]string{"log_group": logGroup, "log_stream": logStream, "aws.account": account,
			signals.DerivedServiceAttr: ServiceFromLogGroup(logGroup)}}
	if timestampMs > 0 {
		ev.OccurredAt = time.UnixMilli(timestampMs).UTC()
	}
	loglines.Fill(&ev, message, "")
	return ev
}

// ServiceFromLogGroup reads the conventional names: /aws/lambda/<fn>, /ecs/<svc>, /aws/ecs/<cluster>/<svc>,
// or the last path segment.
func ServiceFromLogGroup(g string) string {
	parts := strings.FieldsFunc(g, func(r rune) bool { return r == '/' })
	if len(parts) == 0 {
		return ""
	}
	if len(parts) >= 3 && parts[0] == "aws" && parts[1] == "lambda" {
		return parts[2]
	}
	return parts[len(parts)-1]
}
