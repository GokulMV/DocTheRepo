package signals

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"

	"github.com/GokulMV/DocTheRepo/internal/ports"
)

// FingerprintVersion changes whenever the fingerprint recipe changes (decodes record it, so a re-grouping
// is detectable).
const FingerprintVersion = 1

// families groups ingestion paths of the same backend, so an error arriving by CloudWatch polling and by
// the Firehose stream is one issue.
var families = map[string]string{
	"firehose":   "cloudwatch",
	"cloudwatch": "cloudwatch",
	"pubsub":     "gcp",
	"gcp":        "gcp",
}

// Family returns the source family used in error fingerprints.
func Family(source string) string {
	s := strings.ToLower(source)
	if f, ok := families[s]; ok {
		return f
	}
	return s
}

func hash(parts ...string) string {
	h := sha256.New()
	for _, p := range parts {
		h.Write([]byte(p))
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))
}

// Frames returns the top five in-app frames as "module:function" (no line numbers, so a deploy that moves
// code does not split the issue). When the source marks no frame in-app, the top five frames are used.
func Frames(stack []ports.StackFrame) []string {
	anyInApp := false
	for _, f := range stack {
		anyInApp = anyInApp || f.InApp
	}
	var out []string
	for _, f := range stack {
		if anyInApp && !f.InApp {
			continue
		}
		mod := strings.ToLower(f.Module)
		if mod == "" {
			mod = strings.ToLower(f.File)
		}
		fn := strings.ToLower(f.Function)
		if mod == "" && fn == "" {
			continue
		}
		out = append(out, mod+":"+fn)
		if len(out) == 5 {
			break
		}
	}
	return out
}

// errorFingerprint is the content-based fingerprint of an error (plan § 8.9 step 2).
func errorFingerprint(ev *ports.SignalEvent) string {
	msg := ev.Message
	if strings.TrimSpace(msg) == "" {
		msg = ev.Title
	}
	return hash(Family(ev.Source), strings.ToLower(ev.Service), strings.ToLower(ev.ExceptionType), NormalizeMessage(msg),
		strings.Join(Frames(ev.Stack), "|"))
}

// Fingerprint sets ev.Fingerprint (and AltFingerprint when the source groups for us):
//   - errors and log matches: source family + service + exception type + normalized message + top in-app
//     frames; a source group ID (Sentry issue, GCP Error Reporting group) wins, with the content
//     fingerprint kept as AltFingerprint for cross-source matching;
//   - alerts: source + rule/monitor/alarm + service + environment (every firing of one alarm is one issue);
//   - security findings: source + control/rule + resource;
//   - event-bus conditions: source + resource + condition (lag, dlq) + error class (plan § 8.20).
func Fingerprint(ev *ports.SignalEvent) {
	src := strings.ToLower(ev.Source)
	switch ev.Kind {
	case ports.KindAlert:
		rule := ev.RuleID
		if rule == "" {
			rule = NormalizeMessage(ev.Title)
		}
		ev.Fingerprint = hash("alert", src, rule, strings.ToLower(ev.Service), strings.ToLower(ev.Environment))
	case ports.KindSecurityFinding:
		ev.Fingerprint = hash("security", src, ev.RuleID, ev.ResourceID)
	case ports.KindEventBus:
		ev.Fingerprint = hash("event_bus", src, ev.ResourceID, strings.ToLower(ev.RuleID), strings.ToLower(ev.ExceptionType))
	default: // error, log_match
		content := errorFingerprint(ev)
		if ev.GroupID != "" {
			ev.Fingerprint = hash("group", src, ev.GroupID)
			ev.AltFingerprint = content
			return
		}
		ev.Fingerprint = content
	}
}
