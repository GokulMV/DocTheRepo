package signals

import (
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/GokulMV/DocTheRepo/internal/ports"
)

// Limits on stored text.
const (
	MaxTitle   = 512
	MaxMessage = 8192
	maxAttr    = 2048
	maxFrames  = 50
)

// Options configures Prepare.
type Options struct {
	ServiceRules []ServiceRule
	Now          func() time.Time
}

var errNoSource = errors.New("signal event has no source")

// defaultSeverity is used when an adapter could not map one.
var defaultSeverity = map[ports.SignalKind]ports.Severity{
	ports.KindError: ports.SeverityError, ports.KindLogMatch: ports.SeverityError, ports.KindAlert: ports.SeverityWarning,
	ports.KindSecurityFinding: ports.SeverityWarning, ports.KindEventBus: ports.SeverityWarning,
}

// Prepare normalizes an adapter's event in place: defaults, secret scrubbing of every text field,
// truncation, environment and service resolution, external ID, and the fingerprint. After Prepare nothing
// secret-shaped remains in the event, so it is safe to store and to aggregate.
func Prepare(ev *ports.SignalEvent, o Options) error {
	if strings.TrimSpace(ev.Source) == "" {
		return errNoSource
	}
	now := time.Now().UTC()
	if o.Now != nil {
		now = o.Now().UTC()
	}
	if _, ok := defaultSeverity[ev.Kind]; !ok {
		ev.Kind = ports.KindError
	}
	if ports.SeverityRank(ev.Severity) == 0 {
		ev.Severity = defaultSeverity[ev.Kind]
	}
	if ev.ReceivedAt.IsZero() {
		ev.ReceivedAt = now
	}
	// Missing or implausibly future timestamps (clock skew) fall back to when the Hub received the event.
	if ev.OccurredAt.IsZero() || ev.OccurredAt.After(ev.ReceivedAt.Add(5*time.Minute)) {
		ev.OccurredAt = ev.ReceivedAt
	}

	ev.Title = truncate(Scrub(strings.TrimSpace(ev.Title)), MaxTitle)
	ev.Message = truncate(Scrub(ev.Message), MaxMessage)
	ev.ExceptionType = truncate(Scrub(strings.TrimSpace(ev.ExceptionType)), MaxTitle)
	for k, v := range ev.Attrs {
		ev.Attrs[k] = truncate(Scrub(v), maxAttr)
	}
	if len(ev.Stack) > maxFrames {
		ev.Stack = ev.Stack[:maxFrames]
	}
	for i := range ev.Stack {
		f := &ev.Stack[i]
		f.Module, f.Function, f.File = Scrub(f.Module), Scrub(f.Function), Scrub(f.File)
	}
	if ev.Title == "" {
		ev.Title = deriveTitle(ev)
	}

	if ev.Environment == "" {
		for _, k := range []string{"environment", "env", "deployment.environment", "stage"} {
			if v := ev.Attrs[k]; v != "" {
				ev.Environment = v
				break
			}
		}
	}
	ev.Environment = strings.ToLower(strings.TrimSpace(ev.Environment))
	ev.Service = ResolveService(ev.Service, ev.Attrs, o.ServiceRules)
	ev.Source = strings.ToLower(ev.Source)
	if ev.ExternalID == "" {
		// Sources without event IDs: identical events at the same instant collapse (idempotent retries).
		ev.ExternalID = "h:" + hash(ev.Source, ev.OccurredAt.UTC().Format(time.RFC3339Nano), ev.Title, ev.Message)[:32]
	}
	Fingerprint(ev)
	return nil
}

func deriveTitle(ev *ports.SignalEvent) string {
	first, _, _ := strings.Cut(strings.TrimSpace(ev.Message), "\n")
	switch {
	case ev.ExceptionType != "" && first != "":
		return truncate(ev.ExceptionType+": "+first, MaxTitle)
	case ev.ExceptionType != "":
		return ev.ExceptionType
	case first != "":
		return truncate(first, MaxTitle)
	}
	return string(ev.Kind) + " from " + ev.Source
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	s = s[:n]
	for !utf8.ValidString(s) {
		s = s[:len(s)-1]
	}
	return s
}
