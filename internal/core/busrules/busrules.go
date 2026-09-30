// Package busrules turns event-platform readings into issues (plan § 8.20):
//   - lag/backlog: lag > max(1,000, 5 × p95 of the last 7 days' hourly peaks), or the oldest message older
//     than 5 minutes, sustained for 5 minutes, opens an issue per resource; it auto-resolves after 15
//     minutes below threshold;
//   - DLQ: depth increased since the last reading opens an issue per (resource, error class), the class
//     taken from the sampled messages' headers so different failure causes are different issues;
//   - failures: any delivery failures in the interval (SNS, EventBridge) open an issue per resource.
//
// Evaluation is pure: state goes in and comes out, so the caller persists it with the connector cursor.
package busrules

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/GokulMV/DocTheRepo/internal/core/signals"
	"github.com/GokulMV/DocTheRepo/internal/ports"
)

// Thresholds are the editable rule parameters (connector config keys in brackets).
type Thresholds struct {
	LagMin     int64         // [lag_min] floor of the lag threshold, default 1000
	LagFactor  float64       // [lag_factor] multiple of the 7-day p95, default 5
	OldestAge  time.Duration // [oldest_age_seconds] default 5 min
	Sustain    time.Duration // [sustain_seconds] default 5 min
	ResolveAge time.Duration // [resolve_seconds] default 15 min
}

// ThresholdsFrom reads thresholds from connector config, falling back to the plan's defaults.
func ThresholdsFrom(cfg map[string]string) Thresholds {
	t := Thresholds{LagMin: 1000, LagFactor: 5, OldestAge: 5 * time.Minute, Sustain: 5 * time.Minute, ResolveAge: 15 * time.Minute}
	if n, err := strconv.ParseInt(cfg["lag_min"], 10, 64); err == nil && n > 0 {
		t.LagMin = n
	}
	if f, err := strconv.ParseFloat(cfg["lag_factor"], 64); err == nil && f > 0 {
		t.LagFactor = f
	}
	secs := func(key string, d *time.Duration) {
		if n, err := strconv.Atoi(cfg[key]); err == nil && n >= 0 {
			*d = time.Duration(n) * time.Second
		}
	}
	secs("oldest_age_seconds", &t.OldestAge)
	secs("sustain_seconds", &t.Sustain)
	secs("resolve_seconds", &t.ResolveAge)
	return t
}

// historyHours is the p95 baseline window.
const historyHours = 7 * 24

// State is one resource's rule state (persisted as JSON).
type State struct {
	// Peaks are hourly backlog maxima, oldest first: [unix hour, max].
	Peaks       [][2]int64 `json:"peaks,omitempty"`
	BreachSince int64      `json:"breach_since,omitempty"`
	BelowSince  int64      `json:"below_since,omitempty"`
	Open        string     `json:"open,omitempty"` // fingerprint of the open lag issue
	Depth       int64      `json:"depth,omitempty"`
	HaveDepth   bool       `json:"have_depth,omitempty"`
}

// ParseState reads persisted state; bad or empty input is a fresh state.
func ParseState(s string) State {
	var st State
	_ = json.Unmarshal([]byte(s), &st)
	return st
}

// String encodes state.
func (s State) String() string {
	b, _ := json.Marshal(s)
	return string(b)
}

// Result is one evaluation's outcome.
type Result struct {
	Events  []ports.SignalEvent
	Resolve []string // fingerprints of issues to resolve
	State   State
}

// maxBody bounds a sampled message body kept on an event (plan: scrubbed and truncated to 8 KB).
const maxBody = 8 << 10

// Evaluate applies the rules to one reading. bus is the platform name used as the event source (kafka,
// sqs, sns, eventbridge, kinesis, pubsub, rabbitmq).
func Evaluate(bus, connectorID string, r ports.BusReading, st State, t Thresholds, now time.Time) Result {
	res := Result{State: st}
	base := func(rule, class string) ports.SignalEvent {
		attrs := map[string]string{"bus": bus, "bus.condition": string(r.Condition)}
		for k, v := range r.Attrs {
			attrs[k] = v
		}
		return ports.SignalEvent{ConnectorID: connectorID, Source: bus, Kind: ports.KindEventBus, ResourceID: r.Resource,
			RuleID: rule, ExceptionType: class, OccurredAt: now, Severity: ports.SeverityError, Attrs: attrs,
			ExternalID: fmt.Sprintf("%s:%s:%s:%d", r.Resource, rule, class, now.Unix())}
	}
	switch r.Condition {
	case ports.BusLag:
		res.State.Peaks = addPeak(st.Peaks, now, r.Backlog)
		// The baseline uses completed hours only, so a breach in progress never raises its own bar.
		threshold := max(t.LagMin, int64(t.LagFactor*float64(p95(completed(res.State.Peaks, now)))))
		breach := (r.HasBacklog && r.Backlog > threshold) || (t.OldestAge > 0 && r.OldestAge > t.OldestAge)
		if !breach {
			res.State.BreachSince = 0
			if st.Open == "" {
				return res
			}
			if st.BelowSince == 0 {
				res.State.BelowSince = now.Unix()
			}
			if now.Sub(time.Unix(res.State.BelowSince, 0)) >= t.ResolveAge {
				res.Resolve = append(res.Resolve, st.Open)
				res.State.Open, res.State.BelowSince = "", 0
			}
			return res
		}
		res.State.BelowSince = 0
		if st.BreachSince == 0 {
			res.State.BreachSince = now.Unix()
		}
		if now.Sub(time.Unix(res.State.BreachSince, 0)) < t.Sustain {
			return res
		}
		ev := base("lag", "")
		ev.Title = fmt.Sprintf("%s %s is falling behind", busName(bus), r.Resource)
		var parts []string
		if r.HasBacklog {
			parts = append(parts, fmt.Sprintf("backlog %d (threshold %d)", r.Backlog, threshold))
			ev.Attrs["backlog"] = strconv.FormatInt(r.Backlog, 10)
			ev.Attrs["threshold"] = strconv.FormatInt(threshold, 10)
		}
		if r.OldestAge > 0 {
			parts = append(parts, "oldest message "+r.OldestAge.Truncate(time.Second).String())
			ev.Attrs["oldest_age_seconds"] = strconv.FormatInt(int64(r.OldestAge/time.Second), 10)
		}
		ev.Message = strings.Join(parts, ", ") + "; sustained since " + time.Unix(res.State.BreachSince, 0).UTC().Format(time.RFC3339)
		if r.OldestAge > 4*t.OldestAge && t.OldestAge > 0 {
			ev.Severity = ports.SeverityCritical
		}
		signals.Fingerprint(&ev)
		res.State.Open = ev.Fingerprint
		ev.Fingerprint = "" // Prepare recomputes it; keeping the recipe in one place
		res.Events = append(res.Events, ev)
	case ports.BusDLQ:
		prev, had := st.Depth, st.HaveDepth
		if !r.HasBacklog {
			// Depth unknown (a sampling-only source): every sampled message is new.
			if len(r.Samples) > 0 {
				res.Events = append(res.Events, dlqEvents(bus, base, r, int64(len(r.Samples)))...)
			}
			return res
		}
		res.State.Depth, res.State.HaveDepth = r.Backlog, true
		if !had || r.Backlog <= prev {
			return res // first sight sets the baseline; a drained or steady DLQ is not new trouble
		}
		res.Events = append(res.Events, dlqEvents(bus, base, r, r.Backlog-prev)...)
	case ports.BusFailures:
		if r.Failures <= 0 {
			return res
		}
		ev := base("failures", "")
		ev.Title = fmt.Sprintf("%s %s failed %d deliveries", busName(bus), r.Resource, r.Failures)
		ev.Message = ev.Title
		ev.Attrs["failures"] = strconv.FormatInt(r.Failures, 10)
		res.Events = append(res.Events, ev)
	}
	return res
}

// dlqEvents opens one event per error class among the samples (unknown when there are none).
func dlqEvents(bus string, base func(rule, class string) ports.SignalEvent, r ports.BusReading, added int64) []ports.SignalEvent {
	samples := r.Samples
	if r.SamplesNewest && added < int64(len(samples)) {
		samples = samples[int64(len(samples))-added:] // older records were reported by earlier polls
	}
	byClass := map[string][]ports.BusSample{}
	for _, s := range samples {
		byClass[s.ErrorClass] = append(byClass[s.ErrorClass], s)
	}
	if len(byClass) == 0 {
		byClass[""] = nil
	}
	classes := make([]string, 0, len(byClass))
	for c := range byClass {
		classes = append(classes, c)
	}
	sort.Strings(classes)
	out := make([]ports.SignalEvent, 0, len(classes))
	for _, c := range classes {
		ev := base("dlq", c)
		ev.Title = fmt.Sprintf("%s %s received %d new dead-lettered messages", dlqName(bus), r.Resource, added)
		if c != "" {
			ev.Title += " (" + c + ")"
		}
		ev.Attrs["dlq_added"] = strconv.FormatInt(added, 10)
		if r.HasBacklog {
			ev.Attrs["dlq_depth"] = strconv.FormatInt(r.Backlog, 10)
		}
		var msg strings.Builder
		msg.WriteString(ev.Title)
		if ss := byClass[c]; len(ss) > 0 {
			s := ss[0]
			if s.Reason != "" {
				msg.WriteString("\nreason: " + s.Reason)
				ev.Attrs["dlq_reason"] = s.Reason
			}
			if s.Body != "" {
				body := s.Body
				if len(body) > maxBody {
					body = body[:maxBody]
				}
				msg.WriteString("\nsample: " + body)
			}
			for k, v := range s.Attrs {
				ev.Attrs["dlq."+k] = v
			}
		}
		ev.Message = msg.String()
		out = append(out, ev)
	}
	return out
}

// addPeak records a backlog in the current hour's peak and drops hours older than the window.
func addPeak(peaks [][2]int64, now time.Time, v int64) [][2]int64 {
	hour := now.Unix() / 3600
	out := make([][2]int64, 0, len(peaks)+1)
	for _, p := range peaks {
		if p[0] > hour-historyHours {
			out = append(out, p)
		}
	}
	if n := len(out); n > 0 && out[n-1][0] == hour {
		out[n-1][1] = max(out[n-1][1], v)
		return out
	}
	return append(out, [2]int64{hour, v})
}

// completed drops the current hour.
func completed(peaks [][2]int64, now time.Time) [][2]int64 {
	if n := len(peaks); n > 0 && peaks[n-1][0] == now.Unix()/3600 {
		return peaks[:n-1]
	}
	return peaks
}

// p95 of hourly peaks (0 with no history).
func p95(peaks [][2]int64) int64 {
	if len(peaks) == 0 {
		return 0
	}
	vals := make([]int64, len(peaks))
	for i, p := range peaks {
		vals[i] = p[1]
	}
	sort.Slice(vals, func(i, j int) bool { return vals[i] < vals[j] })
	idx := (len(vals)*95 + 99) / 100
	return vals[min(max(idx-1, 0), len(vals)-1)]
}

var busNames = map[string]string{"kafka": "Kafka consumer group", "sqs": "SQS queue", "sns": "SNS topic",
	"eventbridge": "EventBridge rule", "kinesis": "Kinesis stream", "pubsub": "Pub/Sub subscription", "rabbitmq": "RabbitMQ queue"}

var dlqNames = map[string]string{"kafka": "Kafka dead-letter topic", "pubsub": "Pub/Sub dead-letter subscription",
	"sqs": "SQS dead-letter queue", "rabbitmq": "RabbitMQ dead-letter queue"}

func dlqName(bus string) string {
	if n, ok := dlqNames[bus]; ok {
		return n
	}
	return busName(bus)
}

func busName(bus string) string {
	if n, ok := busNames[bus]; ok {
		return n
	}
	return bus
}
