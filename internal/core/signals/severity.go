package signals

import (
	"strings"

	"github.com/GokulMV/DocTheRepo/internal/ports"
)

// sourceSeverity holds per-source mappings where a source's words mean something different from the
// generic reading (plan § 8.8: PagerDuty high→error, Alertmanager critical→critical, Wiz CRITICAL→critical).
var sourceSeverity = map[string]map[string]ports.Severity{
	"pagerduty": {"high": ports.SeverityError, "low": ports.SeverityWarning, "critical": ports.SeverityCritical,
		"error": ports.SeverityError, "warning": ports.SeverityWarning, "info": ports.SeverityInfo},
	"opsgenie":   {"p1": ports.SeverityCritical, "p2": ports.SeverityError, "p3": ports.SeverityWarning, "p4": ports.SeverityInfo, "p5": ports.SeverityInfo},
	"wiz":        {"critical": ports.SeverityCritical, "high": ports.SeverityError, "medium": ports.SeverityWarning, "low": ports.SeverityInfo, "informational": ports.SeverityInfo},
	"sentry":     {"fatal": ports.SeverityCritical, "error": ports.SeverityError, "warning": ports.SeverityWarning, "info": ports.SeverityInfo, "debug": ports.SeverityInfo},
	"datadog":    {"alert": ports.SeverityError, "warn": ports.SeverityWarning, "no data": ports.SeverityWarning, "ok": ports.SeverityInfo},
	"cloudwatch": {"alarm": ports.SeverityError, "insufficient_data": ports.SeverityWarning, "ok": ports.SeverityInfo},
}

// generic maps common severity words (syslog, GCP LogSeverity, log levels, P-levels).
var generic = map[string]ports.Severity{
	"emergency": ports.SeverityCritical, "emerg": ports.SeverityCritical, "alert": ports.SeverityCritical,
	"critical": ports.SeverityCritical, "crit": ports.SeverityCritical, "fatal": ports.SeverityCritical,
	"panic": ports.SeverityCritical, "sev1": ports.SeverityCritical, "p1": ports.SeverityCritical, "disaster": ports.SeverityCritical,
	"error": ports.SeverityError, "err": ports.SeverityError, "high": ports.SeverityError, "major": ports.SeverityError,
	"severe": ports.SeverityError, "sev2": ports.SeverityError, "p2": ports.SeverityError, "failure": ports.SeverityError, "failed": ports.SeverityError,
	"warning": ports.SeverityWarning, "warn": ports.SeverityWarning, "medium": ports.SeverityWarning, "moderate": ports.SeverityWarning,
	"minor": ports.SeverityWarning, "sev3": ports.SeverityWarning, "p3": ports.SeverityWarning, "notice": ports.SeverityWarning,
	"info": ports.SeverityInfo, "informational": ports.SeverityInfo, "low": ports.SeverityInfo, "debug": ports.SeverityInfo,
	"trace": ports.SeverityInfo, "default": ports.SeverityInfo, "ok": ports.SeverityInfo, "resolved": ports.SeverityInfo, "p4": ports.SeverityInfo, "p5": ports.SeverityInfo,
}

// MapSeverity reads a source's severity word. Unknown words map to fallback (adapters pass the kind's
// sensible default, e.g. error for exceptions, warning for alerts).
func MapSeverity(source, raw string, fallback ports.Severity) ports.Severity {
	w := strings.ToLower(strings.TrimSpace(raw))
	if w == "" {
		return fallback
	}
	if m, ok := sourceSeverity[strings.ToLower(source)]; ok {
		if s, ok := m[w]; ok {
			return s
		}
	}
	if s, ok := generic[w]; ok {
		return s
	}
	return fallback
}
