// Package pubsub consumes Google Cloud Logging entries routed by a log sink to a Pub/Sub topic (plan
// § 8.8): a pull consumer over the Pub/Sub REST API that acknowledges only after the events are persisted.
package pubsub

import (
	"encoding/json"
	"strconv"
	"strings"
	"time"

	"github.com/GokulMV/DocTheRepo/internal/adapters/signal/loglines"
	"github.com/GokulMV/DocTheRepo/internal/adapters/signal/sigutil"
	"github.com/GokulMV/DocTheRepo/internal/core/signals"
	"github.com/GokulMV/DocTheRepo/internal/ports"
)

// Message is one Pub/Sub message.
type Message struct {
	Data        []byte
	Attributes  map[string]string
	MessageID   string
	PublishTime time.Time
}

// logEntry is the subset of a Cloud Logging LogEntry the Hub reads.
type logEntry struct {
	LogName  string `json:"logName"`
	InsertID string `json:"insertId"`
	Resource struct {
		Type   string            `json:"type"`
		Labels map[string]string `json:"labels"`
	} `json:"resource"`
	Timestamp    string            `json:"timestamp"`
	Severity     string            `json:"severity"`
	Labels       map[string]string `json:"labels"`
	TextPayload  string            `json:"textPayload"`
	JSONPayload  json.RawMessage   `json:"jsonPayload"`
	ProtoPayload json.RawMessage   `json:"protoPayload"`
	Trace        string            `json:"trace"`
	HTTPRequest  *struct {
		Status        int    `json:"status"`
		RequestMethod string `json:"requestMethod"`
		RequestURL    string `json:"requestUrl"`
	} `json:"httpRequest"`
}

// resourceService lists the resource labels that name a service, most specific first (Cloud Run, Cloud
// Functions, App Engine, GKE).
var resourceService = []string{"service_name", "function_name", "module_id", "container_name", "job_name"}

// Events maps a message: a LogEntry (the log-sink format) becomes one log-match event; anything else is
// read as plain log lines. Entries below min_severity map to none.
func Events(m Message, cc ports.ConnectorConfig) []ports.SignalEvent {
	min := loglines.MinSeverity(cc.Config)
	var le logEntry
	if err := json.Unmarshal(m.Data, &le); err != nil || (le.LogName == "" && le.InsertID == "") {
		return plain(m, cc, min)
	}
	attrs := map[string]string{"gcp.log_name": le.LogName, "gcp.resource_type": le.Resource.Type, "trace": le.Trace}
	for k, v := range le.Resource.Labels {
		attrs["gcp.resource.labels."+k] = v
	}
	for k, v := range le.Labels {
		attrs["gcp.labels."+k] = v
	}
	if app := le.Labels["k8s-pod/app"]; app != "" {
		attrs["k8s.label.app"] = app
	}
	for _, k := range resourceService {
		if v := le.Resource.Labels[k]; v != "" {
			attrs[signals.DerivedServiceAttr] = v
			break
		}
	}
	if p := le.Resource.Labels["project_id"]; p != "" {
		attrs["gcp.project"] = p
	}
	ev := ports.SignalEvent{ConnectorID: cc.ID, Source: "gcp", ExternalID: le.InsertID, OccurredAt: sigutil.Time(le.Timestamp), Attrs: attrs}
	if ev.ExternalID == "" {
		ev.ExternalID = m.MessageID
	}
	if ev.OccurredAt.IsZero() {
		ev.OccurredAt = m.PublishTime
	}
	msg := le.TextPayload
	if msg == "" && len(le.JSONPayload) > 0 {
		msg = string(le.JSONPayload) // loglines reads message, severity, and stack_trace fields from it
		var jp map[string]any
		if json.Unmarshal(le.JSONPayload, &jp) == nil {
			if s := sigutil.Str(jp, "serviceContext.service"); s != "" {
				attrs["service"] = s // Error Reporting's explicit service
			}
		}
	}
	if msg == "" && len(le.ProtoPayload) > 0 {
		var pp any
		if json.Unmarshal(le.ProtoPayload, &pp) == nil {
			msg = strings.TrimSpace(sigutil.First(pp, "status.message") + " " + sigutil.Str(pp, "methodName"))
			attrs["gcp.audit.method"] = sigutil.Str(pp, "methodName")
			attrs["gcp.audit.service"] = sigutil.Str(pp, "serviceName")
		}
	}
	if msg == "" && le.HTTPRequest != nil {
		msg = le.HTTPRequest.RequestMethod + " " + le.HTTPRequest.RequestURL + " returned " + strconv.Itoa(le.HTTPRequest.Status)
	}
	if msg == "" {
		return nil
	}
	loglines.Fill(&ev, msg, le.Severity)
	if lvl := signals.MapSeverity("gcp", le.Severity, ""); ports.SeverityRank(lvl) > ports.SeverityRank(ev.Severity) {
		ev.Severity = lvl // the entry's own severity can raise, never lower, what the text says
	}
	if !loglines.Keep(ev, min) {
		return nil
	}
	return []ports.SignalEvent{ev}
}

func plain(m Message, cc ports.ConnectorConfig, min ports.Severity) []ports.SignalEvent {
	text := strings.TrimSpace(string(m.Data))
	if text == "" {
		return nil
	}
	ev := ports.SignalEvent{ConnectorID: cc.ID, Source: "gcp", ExternalID: m.MessageID, OccurredAt: m.PublishTime, Attrs: map[string]string{}}
	for k, v := range m.Attributes {
		ev.Attrs["pubsub."+k] = v
	}
	loglines.Fill(&ev, text, m.Attributes["severity"])
	if !loglines.Keep(ev, min) {
		return nil
	}
	return []ports.SignalEvent{ev}
}
