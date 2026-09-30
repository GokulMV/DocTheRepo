package ingest

import (
	"context"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/GokulMV/DocTheRepo/internal/adapters/signal/firehose"
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

	// Webhooks are the push adapters by path segment; Accepts says which connector types a path serves.
	Webhooks map[string]ports.SignalWebhook
	Accepts  func(source, connectorType string) bool
	// LoadConnector returns an enabled connector with decrypted secrets (ports.ErrNotFound otherwise).
	LoadConnector func(ctx context.Context, id string) (ports.ConnectorConfig, error)

	matcher  atomic.Pointer[knownissues.Matcher]
	services atomic.Pointer[[]signals.ServiceRule]

	cacheMu sync.Mutex
	cache   map[string]cachedConnector
}

type cachedConnector struct {
	cc ports.ConnectorConfig
	at time.Time
}

// connectorTTL bounds how long a connector (and its decrypted secret) is reused across deliveries; with a
// cloud KMS every uncached load is a key-unwrap call. Admin edits also invalidate immediately.
const connectorTTL = time.Minute

// InvalidateConnector drops a cached connector after an edit or delete.
func (s *SignalIngest) InvalidateConnector(id string) {
	s.cacheMu.Lock()
	delete(s.cache, id)
	s.cacheMu.Unlock()
}

func (s *SignalIngest) connector(ctx context.Context, id string) (ports.ConnectorConfig, error) {
	s.cacheMu.Lock()
	c, ok := s.cache[id]
	s.cacheMu.Unlock()
	if ok && time.Since(c.at) < connectorTTL {
		return c.cc, nil
	}
	cc, err := s.LoadConnector(ctx, id)
	if err != nil {
		return cc, err
	}
	s.cacheMu.Lock()
	if s.cache == nil {
		s.cache = map[string]cachedConnector{}
	}
	s.cache[id] = cachedConnector{cc: cc, at: time.Now()}
	s.cacheMu.Unlock()
	return cc, nil
}

// Webhook verifies, parses, and ingests one push delivery for /hooks/{source}/{connector_id}. It returns
// how many events the delivery carried. Errors: ports.ErrNotFound (unknown source, unknown or disabled
// connector, or a connector of another type), ports.ErrInvalidSignature, *ports.ValidationError (malformed
// payload), aggregate.ErrOverloaded (retry later).
func (s *SignalIngest) Webhook(ctx context.Context, source, connectorID string, req ports.WebhookRequest) (int, error) {
	a, ok := s.Webhooks[source]
	if !ok {
		return 0, ports.ErrNotFound
	}
	cc, err := s.connector(ctx, connectorID)
	if err != nil {
		return 0, err
	}
	if s.Accepts != nil && !s.Accepts(source, cc.Type) {
		return 0, ports.ErrNotFound
	}
	if err := a.Verify(req, cc); err != nil {
		return 0, err
	}
	events, err := a.Parse(req, cc)
	if err != nil {
		return 0, err
	}
	for i := range events {
		events[i].ConnectorID = cc.ID
	}
	if err := s.Ingest(ctx, events); err != nil {
		return 0, err
	}
	return len(events), nil
}

// FirehoseTypes are the connector types that may receive Firehose deliveries.
var FirehoseTypes = map[string]bool{"firehose": true, "cloudwatch": true}

// durableWait bounds how long a stream receiver waits for its events to be persisted before telling the
// sender to retry (Firehose and Pub/Sub both redeliver; redeliveries are deduplicated).
const durableWait = 30 * time.Second

// Firehose verifies, decodes, and ingests one Amazon Data Firehose delivery, returning only once its events
// are persisted: Firehose treats a 200 as delivered and never resends. Errors are those of Webhook plus a
// context error when persistence did not finish in time (the sender retries).
func (s *SignalIngest) Firehose(ctx context.Context, connectorID string, req ports.WebhookRequest) (string, int, error) {
	requestID := req.HeaderValue("X-Amz-Firehose-Request-Id")
	cc, err := s.connector(ctx, connectorID)
	if err != nil {
		return requestID, 0, err
	}
	if !FirehoseTypes[cc.Type] {
		return requestID, 0, ports.ErrNotFound
	}
	if err := firehose.Verify(req, cc.WebhookSecret); err != nil {
		return requestID, 0, err
	}
	r, err := firehose.DecodeBody(req)
	if err != nil {
		return requestID, 0, err
	}
	requestID = r.RequestID
	events, err := firehose.Parse(r, cc)
	if err != nil {
		return requestID, 0, err
	}
	return requestID, len(events), s.IngestDurable(ctx, events)
}

// IngestDurable ingests and then waits until the events are persisted, for receivers that acknowledge to
// the sender (Firehose responses, Pub/Sub acks).
func (s *SignalIngest) IngestDurable(ctx context.Context, events []ports.SignalEvent) error {
	if err := s.Ingest(ctx, events); err != nil {
		return err
	}
	if len(events) == 0 {
		return nil
	}
	// OpenWindow after adding: the events sit in that window or an earlier one, so waiting for it covers them.
	seq := s.Agg.OpenWindow()
	wctx, cancel := context.WithTimeout(ctx, durableWait)
	defer cancel()
	return s.Agg.WaitFlushed(wctx, seq)
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
