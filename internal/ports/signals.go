package ports

import (
	"context"
	"time"
)

// Severity is a normalized signal severity (plan § 6.4). Order matters: see SeverityRank.
type Severity string

const (
	SeverityInfo     Severity = "info"
	SeverityWarning  Severity = "warning"
	SeverityError    Severity = "error"
	SeverityCritical Severity = "critical"
)

// SeverityRank orders severities (info < warning < error < critical); unknown values rank 0.
func SeverityRank(s Severity) int {
	switch s {
	case SeverityInfo:
		return 1
	case SeverityWarning:
		return 2
	case SeverityError:
		return 3
	case SeverityCritical:
		return 4
	}
	return 0
}

// SignalKind is what a signal describes.
type SignalKind string

const (
	KindError           SignalKind = "error"
	KindAlert           SignalKind = "alert"
	KindSecurityFinding SignalKind = "security_finding"
	KindLogMatch        SignalKind = "log_match"
	KindEventBus        SignalKind = "event_bus"
)

// StackFrame is one frame of a stack trace. InApp marks the application's own code (not libraries).
type StackFrame struct {
	Module   string `json:"module,omitempty"`
	Function string `json:"function,omitempty"`
	File     string `json:"file,omitempty"`
	Line     int    `json:"line,omitempty"`
	InApp    bool   `json:"in_app,omitempty"`
}

// SignalEvent is what every signal adapter produces (plan § 8.8): an error, alert, security finding, log
// match, or event-bus condition. Adapters fill what the source provides; normalization fills the rest.
type SignalEvent struct {
	ConnectorID   string            `json:"connector_id"`
	Source        string            `json:"source"` // adapter name: sentry, cloudwatch, alertmanager, sqs, …
	ExternalID    string            `json:"external_id"`
	OccurredAt    time.Time         `json:"occurred_at"`
	ReceivedAt    time.Time         `json:"received_at"`
	Severity      Severity          `json:"severity"`
	Kind          SignalKind        `json:"kind"`
	Service       string            `json:"service"`
	Environment   string            `json:"environment"`
	Title         string            `json:"title"`
	Message       string            `json:"message"`
	ExceptionType string            `json:"exception_type,omitempty"`
	Stack         []StackFrame      `json:"stack,omitempty"`
	Attrs         map[string]string `json:"attrs,omitempty"`
	// GroupID is the source's own grouping (Sentry issue, GCP Error Reporting group, Datadog error issue).
	GroupID string `json:"group_id,omitempty"`
	// RuleID identifies the alert rule / monitor / alarm (alerts) or control (security findings).
	RuleID string `json:"rule_id,omitempty"`
	// ResourceID is the affected resource (security findings, event-bus queues and consumer groups).
	ResourceID string `json:"resource_id,omitempty"`
	// Fingerprint and AltFingerprint are set by normalization.
	Fingerprint    string `json:"fingerprint,omitempty"`
	AltFingerprint string `json:"alt_fingerprint,omitempty"`
}

// SignalSink receives normalized events (the ingest pipeline).
type SignalSink interface {
	Ingest(ctx context.Context, events []SignalEvent) error
}
