// Package generic accepts JSON from any tool: one object, an array, or an object with an events / alerts /
// items / records array. Fields are found by the connector's mapping (config "field.<name>": dotted path)
// or, failing that, by common names. Top-level scalar fields become attributes (for known-issue rules and
// service mapping). Authentication is the connector secret as bearer, basic password, header, or ?token=.
package generic

import (
	"sort"
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
func (Adapter) Source() string { return "generic" }

// Verify implements ports.SignalWebhook.
func (Adapter) Verify(req ports.WebhookRequest, cc ports.ConnectorConfig) error {
	return sigutil.VerifyToken(req, cc.WebhookSecret)
}

// Fields and their default paths, tried in order.
var defaults = map[string][]string{
	"title":          {"title", "name", "summary", "subject", "alert", "error.type"},
	"message":        {"message", "msg", "description", "text", "body", "error.message", "details"},
	"severity":       {"severity", "level", "priority", "status"},
	"service":        {"service", "service_name", "app", "application", "component"},
	"environment":    {"environment", "env", "stage"},
	"occurred_at":    {"timestamp", "time", "occurred_at", "created_at", "date", "@timestamp"},
	"external_id":    {"id", "event_id", "uuid", "alert_id"},
	"kind":           {"kind", "type"},
	"group_id":       {"group_id", "issue_id"},
	"rule_id":        {"rule_id", "rule", "monitor_id", "check"},
	"exception_type": {"exception_type", "exception", "error.type", "error_class"},
	"resource_id":    {"resource_id", "resource", "host", "hostname"},
}

var kinds = map[string]ports.SignalKind{"error": ports.KindError, "exception": ports.KindError, "alert": ports.KindAlert,
	"security": ports.KindSecurityFinding, "security_finding": ports.KindSecurityFinding, "log": ports.KindLogMatch,
	"log_match": ports.KindLogMatch, "event_bus": ports.KindEventBus}

// MaxEvents bounds one delivery.
const MaxEvents = 1000

// Parse implements ports.SignalWebhook.
func (Adapter) Parse(req ports.WebhookRequest, cc ports.ConnectorConfig) ([]ports.SignalEvent, error) {
	v, err := sigutil.Decode(req.Body)
	if err != nil {
		return nil, err
	}
	var items []any
	switch x := v.(type) {
	case []any:
		items = x
	case map[string]any:
		items = []any{x}
		for _, k := range []string{"events", "alerts", "items", "records"} {
			if arr, ok := x[k].([]any); ok {
				items = arr
				break
			}
		}
	default:
		return nil, sigutil.Malformed("expected a JSON object or array")
	}
	if len(items) > MaxEvents {
		return nil, sigutil.Malformed("at most %d events per delivery", MaxEvents)
	}
	source := strings.ToLower(strings.TrimSpace(cc.Config["source_name"]))
	if source == "" {
		source = "generic"
	}
	out := make([]ports.SignalEvent, 0, len(items))
	for _, it := range items {
		if _, ok := it.(map[string]any); !ok {
			return nil, sigutil.Malformed("each event must be a JSON object")
		}
		f := func(name string) string {
			if p := cc.Config["field."+name]; p != "" {
				return sigutil.Str(it, p)
			}
			return sigutil.First(it, defaults[name]...)
		}
		kind := kinds[strings.ToLower(f("kind"))]
		if kind == "" {
			kind = ports.KindError
			if k := kinds[strings.ToLower(cc.Config["kind"])]; k != "" {
				kind = k
			}
		}
		ev := ports.SignalEvent{
			ConnectorID: cc.ID, Source: source, Kind: kind, ExternalID: f("external_id"),
			OccurredAt: sigutil.Time(f("occurred_at")), Title: f("title"), Message: f("message"),
			Severity: signals.MapSeverity(source, f("severity"), ""), Service: f("service"), Environment: f("environment"),
			GroupID: f("group_id"), RuleID: f("rule_id"), ExceptionType: f("exception_type"), ResourceID: f("resource_id"),
			Attrs: topScalars(it),
		}
		out = append(out, ev)
	}
	return out, nil
}

// topScalars keeps up to 50 top-level scalar fields as attributes.
func topScalars(v any) map[string]string {
	m := sigutil.StrMap(v, "")
	if len(m) <= 50 {
		return m
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make(map[string]string, 50)
	for _, k := range keys[:50] {
		out[k] = m[k]
	}
	return out
}
