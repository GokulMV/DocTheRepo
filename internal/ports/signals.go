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

// SignalWebhook is a push adapter (Sentry, PagerDuty, Alertmanager, …): it verifies and parses one
// delivery. Adapters never do I/O; the ingress layer loads the connector and hands events to the sink.
type SignalWebhook interface {
	// Source is the adapter name, also the path segment of /hooks/{source}/{connector_id}.
	Source() string
	// Verify authenticates a delivery against the connector's secret (ErrInvalidSignature on failure).
	Verify(req WebhookRequest, cc ConnectorConfig) error
	// Parse maps a delivery to events; state changes that are not problems (resolved, acknowledged) map to
	// none. Malformed payloads return a *ValidationError.
	Parse(req WebhookRequest, cc ConnectorConfig) ([]SignalEvent, error)
}

// WebhookRequest is what a push adapter sees of an HTTP delivery.
type WebhookRequest struct {
	Header map[string][]string
	Query  map[string][]string
	Body   []byte
}

// HeaderValue returns the first value of a header, case-insensitively.
func (r WebhookRequest) HeaderValue(name string) string {
	for k, v := range r.Header {
		if len(v) > 0 && equalFold(k, name) {
			return v[0]
		}
	}
	return ""
}

func equalFold(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := 0; i < len(a); i++ {
		ca, cb := a[i], b[i]
		if 'A' <= ca && ca <= 'Z' {
			ca += 'a' - 'A'
		}
		if 'A' <= cb && cb <= 'Z' {
			cb += 'a' - 'A'
		}
		if ca != cb {
			return false
		}
	}
	return true
}

// SignalPoller pulls events from a low-volume read API (CloudWatch Logs FilterLogEvents, alarm history,
// GCP Logging entries.list) with a cursor per stream (a log group, a project). Poll hands each page to
// emit with the cursor to store once that page's events are persisted; an emit error stops the poll and
// the page is fetched again next time (at-least-once, deduplicated by external ID).
type SignalPoller interface {
	// Type is the connector type polled.
	Type() string
	Poll(ctx context.Context, cc ConnectorConfig, cursors map[string]string, emit func(stream, cursor string, events []SignalEvent) error) error
}

// BusCondition is what an event-platform reading measures (plan § 8.20).
type BusCondition string

const (
	// BusLag is a consumer's backlog: consumer-group lag, queue depth, undelivered messages, iterator age.
	BusLag BusCondition = "lag"
	// BusDLQ is a dead-letter queue's depth; growth opens an issue per error class.
	BusDLQ BusCondition = "dlq"
	// BusFailures counts delivery failures in the last interval (SNS, EventBridge targets).
	BusFailures BusCondition = "failures"
)

// BusSample is one dead-lettered message read without consuming it (or from a Hub-owned subscription).
type BusSample struct {
	ErrorClass string            `json:"error_class,omitempty"`
	Reason     string            `json:"reason,omitempty"`
	Body       string            `json:"body,omitempty"`
	Attrs      map[string]string `json:"attrs,omitempty"`
}

// BusReading is one inspector measurement of one resource (a consumer group, queue, subscription, rule).
type BusReading struct {
	Resource  string       `json:"resource"`
	Condition BusCondition `json:"condition"`
	// Backlog is messages waiting (lag) or DLQ depth; HasBacklog is false when the platform cannot say.
	Backlog    int64 `json:"backlog"`
	HasBacklog bool  `json:"has_backlog"`
	// OldestAge is the oldest waiting message's age (0 when unknown).
	OldestAge time.Duration `json:"oldest_age"`
	// Failures is the failure count in the last interval (BusFailures).
	Failures int64 `json:"failures"`
	// Samples are ordered oldest first. SamplesNewest says they are the newest messages (Kafka reads the
	// partition tails), so on DLQ growth of n only the last n are new; queue peeks read the head instead.
	Samples       []BusSample `json:"samples,omitempty"`
	SamplesNewest bool        `json:"samples_newest,omitempty"`
	// Attrs help map the resource to a service (queue, topic, consumer_group, subscription, rule).
	Attrs map[string]string `json:"attrs,omitempty"`
}

// BusInspector reads an event platform's lag, backlog, and dead-letter state. Inspectors are strictly
// read-only on application resources: they never commit offsets, delete, or acknowledge application
// messages.
type BusInspector interface {
	// Type is the connector type inspected (kafka, sqs, sns, eventbridge, kinesis, pubsub_bus, rabbitmq).
	Type() string
	Inspect(ctx context.Context, cc ConnectorConfig) ([]BusReading, error)
}

// BusCommitter is implemented by inspectors that consume from Hub-owned resources (a Pub/Sub dead-letter
// subscription created for the Hub): Commit acknowledges what the last Inspect read, and is called only
// after that inspection's events are persisted.
type BusCommitter interface {
	Commit(ctx context.Context, cc ConnectorConfig) error
}
