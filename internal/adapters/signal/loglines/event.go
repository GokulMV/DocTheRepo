package loglines

import (
	"strings"

	"github.com/GokulMV/DocTheRepo/internal/core/signals"
	"github.com/GokulMV/DocTheRepo/internal/ports"
)

// Fill completes a log-match event from one log message. rawLevel is the level the transport reports
// (GCP LogEntry.severity); the message's own level wins when present. A line with no level at all is an
// error: subscription filters and log sinks are configured to forward problems.
func Fill(ev *ports.SignalEvent, message, rawLevel string) {
	p := Parse(message)
	ev.Kind = ports.KindLogMatch
	level := p.Level
	if level == "" {
		level = rawLevel
	}
	ev.Severity = signals.MapSeverity(ev.Source, level, ports.SeverityError)
	ev.Message = p.Message
	if ev.Message == "" {
		ev.Message = strings.TrimSpace(message)
	}
	ev.ExceptionType = p.ExceptionType
	ev.Stack = p.Stack
	ev.Title = FirstLine(ev.Message)
	if ev.ExceptionType != "" && !strings.Contains(ev.Title, ev.ExceptionType) {
		ev.Title = ev.ExceptionType + ": " + ev.Title
	}
	if ev.Attrs == nil {
		ev.Attrs = map[string]string{}
	}
	for _, k := range []string{"service", "service.name", "env", "environment", "trace_id", "logger", "logger_name", "request_id"} {
		if v := p.Fields[k]; v != "" {
			ev.Attrs[k] = v
		}
	}
}

// MinSeverity reads a connector's min_severity setting (default warning): log lines below it are dropped.
func MinSeverity(cfg map[string]string) ports.Severity {
	if s := signals.MapSeverity("", cfg["min_severity"], ""); s != "" {
		return s
	}
	return ports.SeverityWarning
}

// Keep reports whether an event meets the minimum severity.
func Keep(ev ports.SignalEvent, min ports.Severity) bool {
	return ports.SeverityRank(ev.Severity) >= ports.SeverityRank(min)
}
