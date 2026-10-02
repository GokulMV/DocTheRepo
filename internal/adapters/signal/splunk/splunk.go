// Package splunk reads Splunk into the Inbox two ways (plan § 8.16): alert actions push the webhook
// payload ({result, sid, results_link, search_name, app, owner}) — one alert per firing of a saved
// search — and a poller runs configured saved searches or SPL queries over the window since the cursor,
// turning each result row into a log event that groups like any other error.
package splunk

import (
	"fmt"
	"sort"
	"strings"

	"github.com/GokulMV/DocTheRepo/internal/adapters/signal/loglines"
	"github.com/GokulMV/DocTheRepo/internal/adapters/signal/sigutil"
	"github.com/GokulMV/DocTheRepo/internal/core/signals"
	"github.com/GokulMV/DocTheRepo/internal/ports"
)

// Adapter implements ports.SignalWebhook for Splunk webhook alert actions. Splunk's webhook action cannot
// add headers, so the connector secret goes in the URL as ?token= (or a proxy adds a bearer token).
type Adapter struct{}

// New returns the webhook adapter.
func New() Adapter { return Adapter{} }

// Source implements ports.SignalWebhook.
func (Adapter) Source() string { return "splunk" }

// Verify implements ports.SignalWebhook.
func (Adapter) Verify(req ports.WebhookRequest, cc ports.ConnectorConfig) error {
	return sigutil.VerifyToken(req, cc.WebhookSecret)
}

// Field defaults, tried in order; config "field.<name>" overrides (dotted path into the result row).
var fieldDefaults = map[string][]string{
	"service":     {"service", "service_name", "app_name", "application", "kubernetes.labels.app"},
	"environment": {"environment", "env", "stage"},
	"severity":    {"severity", "level", "log_level", "loglevel", "urgency"},
	"message":     {"message", "msg", "error", "error_message", "_raw"},
}

func field(row any, cfg map[string]string, name string) string {
	if p := cfg["field."+name]; p != "" {
		return sigutil.Str(row, p)
	}
	return sigutil.First(row, fieldDefaults[name]...)
}

// Parse implements ports.SignalWebhook. A firing with an empty result row (an alert set to trigger even
// with no results) is not a problem.
func (Adapter) Parse(req ports.WebhookRequest, cc ports.ConnectorConfig) ([]ports.SignalEvent, error) {
	v, err := sigutil.Decode(req.Body)
	if err != nil {
		return nil, err
	}
	name := sigutil.Str(v, "search_name")
	sid := sigutil.Str(v, "sid")
	if name == "" && sid == "" {
		return nil, sigutil.Malformed("Splunk payload has no search_name or sid (webhook alert actions are supported)")
	}
	row := sigutil.Get(v, "result")
	m, _ := row.(map[string]any)
	if len(m) == 0 {
		return nil, nil
	}
	app := sigutil.Str(v, "app")
	msg := loglines.FirstLine(field(row, cc.Config, "message"))
	title := name
	if msg != "" && len(msg) < 200 {
		title = name + ": " + msg
	}
	ev := ports.SignalEvent{
		ConnectorID: cc.ID, Source: "splunk", Kind: ports.KindAlert, ExternalID: firstNonEmpty(sid, name),
		OccurredAt: sigutil.Time(sigutil.Str(row, "_time")), Title: title, Message: field(row, cc.Config, "message"),
		Severity:    signals.MapSeverity("splunk", field(row, cc.Config, "severity"), ports.SeverityError),
		Service:     field(row, cc.Config, "service"),
		Environment: field(row, cc.Config, "environment"),
		// Each saved search is one alert rule: its firings group into one issue per service.
		RuleID: strings.Trim(app+"/"+name, "/"),
		Attrs:  map[string]string{"splunk.sid": sid, "splunk.search": name, "splunk.app": app, "url": sigutil.Str(v, "results_link")},
	}
	addRowAttrs(ev.Attrs, m)
	return []ports.SignalEvent{ev}, nil
}

// addRowAttrs copies up to 10 scalar result fields (internal _fields and the raw event excluded).
func addRowAttrs(attrs map[string]string, row map[string]any) {
	keys := make([]string, 0, len(row))
	for k := range row {
		if !strings.HasPrefix(k, "_") {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	n := 0
	for _, k := range keys {
		if n >= 10 {
			break
		}
		switch x := row[k].(type) {
		case string:
			if len(x) <= 256 {
				attrs["splunk."+k] = x
				n++
			}
		case float64, bool:
			attrs["splunk."+k] = fmt.Sprint(x)
			n++
		}
	}
	for k, v := range attrs {
		if v == "" {
			delete(attrs, k)
		}
	}
}

func firstNonEmpty(vs ...string) string {
	for _, v := range vs {
		if v != "" {
			return v
		}
	}
	return ""
}
