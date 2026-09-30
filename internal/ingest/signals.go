package ingest

import (
	"context"
	"log/slog"
	"sync/atomic"
	"time"

	"github.com/GokulMV/DocTheRepo/internal/core/aggregate"
	"github.com/GokulMV/DocTheRepo/internal/core/knownissues"
	"github.com/GokulMV/DocTheRepo/internal/core/signals"
	"github.com/GokulMV/DocTheRepo/internal/ports"
)

// RuleSource loads what the signal pipeline matches against.
type RuleSource interface {
	ActiveRules(ctx context.Context) ([]knownissues.Rule, error)
	ServiceRules(ctx context.Context) ([]signals.ServiceRule, error)
}

// Event outcomes (metrics).
const (
	OutcomeAccepted   = "accepted"
	OutcomeSuppressed = "suppressed"
	OutcomeLabeled    = "labeled"
	OutcomeInvalid    = "invalid"
	OutcomeOverloaded = "overloaded"
)

// SignalIngest is the entry point for every signal adapter (webhooks, stream receivers, pollers, bus
// inspectors): normalize and scrub, match known issues, aggregate. It implements ports.SignalSink.
type SignalIngest struct {
	Agg   *aggregate.Aggregator
	Rules RuleSource
	Log   *slog.Logger
	Now   func() time.Time
	// OnEvent observes each event's outcome (metrics); optional.
	OnEvent func(source, outcome string)

	matcher  atomic.Pointer[knownissues.Matcher]
	services atomic.Pointer[[]signals.ServiceRule]
}

// Reload recompiles known-issue rules and the service map. Called at startup, periodically, and after an
// admin edits rules; a failure keeps the previous rule set.
func (s *SignalIngest) Reload(ctx context.Context) error {
	rules, err := s.Rules.ActiveRules(ctx)
	if err != nil {
		return err
	}
	m, errs := knownissues.Compile(rules)
	for _, e := range errs {
		s.log().Warn("skipping invalid known-issue rule", "err", e)
	}
	svc, err := s.Rules.ServiceRules(ctx)
	if err != nil {
		return err
	}
	s.matcher.Store(m)
	s.services.Store(&svc)
	return nil
}

// Ingest accepts a batch of adapter events. It returns aggregate.ErrOverloaded when the pipeline is behind;
// the sender should retry the whole batch later (events already accepted are recognised as redeliveries
// by connector + external ID and not counted twice). Invalid events are dropped and counted.
func (s *SignalIngest) Ingest(ctx context.Context, events []ports.SignalEvent) error {
	var svc []signals.ServiceRule
	if p := s.services.Load(); p != nil {
		svc = *p
	}
	m := s.matcher.Load()
	now := time.Now
	if s.Now != nil {
		now = s.Now
	}
	opts := signals.Options{ServiceRules: svc, Now: now}
	for i := range events {
		ev := events[i]
		if ev.Attrs != nil { // Prepare scrubs attributes in place: never mutate the caller's map
			attrs := make(map[string]string, len(ev.Attrs))
			for k, v := range ev.Attrs {
				attrs[k] = v
			}
			ev.Attrs = attrs
		}
		if err := signals.Prepare(&ev, opts); err != nil {
			s.observe(ev.Source, OutcomeInvalid)
			continue
		}
		var res *knownissues.Result
		outcome := OutcomeAccepted
		if r, ok := m.Match(&ev, now()); ok {
			res = &r
			outcome = OutcomeSuppressed
			if r.Action == knownissues.LabelOnly {
				outcome = OutcomeLabeled
			}
		}
		if err := s.Agg.Add(ev, res); err != nil {
			s.observe(ev.Source, OutcomeOverloaded)
			return err
		}
		s.observe(ev.Source, outcome)
	}
	return nil
}

// Overloaded reports whether pull-based consumers should pause.
func (s *SignalIngest) Overloaded() bool { return s.Agg.Overloaded() }

func (s *SignalIngest) observe(source, outcome string) {
	if s.OnEvent != nil {
		s.OnEvent(source, outcome)
	}
}

func (s *SignalIngest) log() *slog.Logger {
	if s.Log != nil {
		return s.Log
	}
	return slog.Default()
}
