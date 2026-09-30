// Package aggregate is the signal hot path (plan § 4.5, § 8.9): each replica groups events by fingerprint
// for a short window and flushes one row update per fingerprint, so database writes scale with distinct
// errors, not with log volume. It keeps a bounded sample of events (the first 5 per fingerprint plus a
// reservoir of 20 per fingerprint per hour), drops redeliveries, and applies backpressure instead of
// dropping events when the flush falls behind.
package aggregate

import (
	"context"
	"errors"
	"math/rand/v2"
	"sync"
	"time"

	"github.com/GokulMV/DocTheRepo/internal/core/knownissues"
	"github.com/GokulMV/DocTheRepo/internal/ports"
)

// Sampling limits (plan § 4.5 rule 3).
const (
	FirstSamples     = 5
	ReservoirPerHour = 20
)

// ErrOverloaded means more than MaxBuffered windows are waiting to be flushed: callers must stop pulling
// (Pub/Sub) or answer 503 (Firehose, webhooks) so the sender retries. No accepted event is ever dropped.
var ErrOverloaded = errors.New("signal pipeline is behind; retry later")

// SampleKind says why a sample was kept.
type SampleKind string

const (
	SampleFirst     SampleKind = "first"
	SampleReservoir SampleKind = "reservoir"
)

// Sample is one stored event. For reservoir samples, (Hour, Slot) is a fixed slot that a later sample of
// the same hour may replace, which keeps storage at ReservoirPerHour rows per fingerprint per hour. First
// samples use slots -1..-FirstSamples.
type Sample struct {
	Kind  SampleKind
	Hour  time.Time
	Slot  int
	Event ports.SignalEvent
}

// Minute is one minute's counts for sparklines.
type Minute struct {
	Count      int64
	Suppressed int64
}

// Group is everything one window learned about one fingerprint.
type Group struct {
	Fingerprint    string
	AltFingerprint string
	Kind           ports.SignalKind
	Source         string
	Sources        map[string]bool
	Service        string
	Environment    string
	Title          string
	SeverityMax    ports.Severity
	FirstSeen      time.Time
	LastSeen       time.Time
	Count          int64 // occurrences not suppressed
	Suppressed     int64
	SuppressedBy   string // suppressing rule (the latest in the window)
	LabeledBy      string // label-only rule
	Minutes        map[time.Time]*Minute
	Samples        []Sample
}

// Batch is one window's groups.
type Batch struct {
	Window time.Time
	Groups []*Group
}

// Events is the batch's total accepted events (suppressed included).
func (b Batch) Events() int64 {
	var n int64
	for _, g := range b.Groups {
		n += g.Count + g.Suppressed
	}
	return n
}

// Sink persists a batch. Flush must be idempotent enough to retry after an error (the aggregator retries
// the same batch until it succeeds).
type Sink interface {
	Flush(ctx context.Context, b Batch) error
}

// Options configures an Aggregator.
type Options struct {
	Window      time.Duration // default 1s
	MaxBuffered int           // default 3 windows
	LRUSize     int           // first-sample and dedupe memory, default 100k
	DedupeTTL   time.Duration // how long a (connector, external_id) is remembered, default 10m
	Now         func() time.Time
	Rand        func(n int) int // tests
	// OnFlush observes flush outcomes (metrics); optional.
	OnFlush func(b Batch, d time.Duration, err error)
}

// Aggregator is safe for concurrent use.
type Aggregator struct {
	o    Options
	sink Sink

	flushMu sync.Mutex // one drain at a time (the flusher and a shutdown flush never overlap)
	mu      sync.Mutex
	cur     map[string]*Group
	queue   []Batch
	firsts  *lru[int]          // fingerprint → first samples emitted
	seen    *lru[time.Time]    // connector/external_id → first seen (redelivery guard)
	hours   map[string]*hourly // fingerprint → this hour's reservoir state
	hourNow time.Time
	wake    chan struct{}
}

type hourly struct {
	hour time.Time
	seen int
}

// New builds an aggregator; call Run to start flushing.
func New(sink Sink, o Options) *Aggregator {
	if o.Window <= 0 {
		o.Window = time.Second
	}
	if o.MaxBuffered <= 0 {
		o.MaxBuffered = 3
	}
	if o.LRUSize <= 0 {
		o.LRUSize = 100_000
	}
	if o.DedupeTTL <= 0 {
		o.DedupeTTL = 10 * time.Minute
	}
	if o.Now == nil {
		o.Now = time.Now
	}
	if o.Rand == nil {
		o.Rand = rand.IntN
	}
	return &Aggregator{o: o, sink: sink, cur: map[string]*Group{}, firsts: newLRU[int](o.LRUSize), seen: newLRU[time.Time](o.LRUSize),
		hours: map[string]*hourly{}, wake: make(chan struct{}, 1)}
}

// Overloaded reports whether new events should be refused (see ErrOverloaded).
func (a *Aggregator) Overloaded() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return len(a.queue) > a.o.MaxBuffered
}

// Add accepts one prepared event (signals.Prepare) and its known-issue match, if any. It returns
// ErrOverloaded without accepting the event when the flush is behind, and nil for a redelivery it already
// counted.
func (a *Aggregator) Add(ev ports.SignalEvent, m *knownissues.Result) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if len(a.queue) > a.o.MaxBuffered {
		return ErrOverloaded
	}
	now := a.o.Now()
	if ev.ConnectorID != "" && ev.ExternalID != "" {
		key := ev.ConnectorID + "\x00" + ev.ExternalID
		if at, ok := a.seen.get(key); ok && now.Sub(at) < a.o.DedupeTTL {
			return nil
		}
		a.seen.put(key, now)
	}
	g := a.cur[ev.Fingerprint]
	if g == nil {
		g = &Group{Fingerprint: ev.Fingerprint, AltFingerprint: ev.AltFingerprint, Kind: ev.Kind, Source: ev.Source, Sources: map[string]bool{},
			Service: ev.Service, Environment: ev.Environment, Title: ev.Title, SeverityMax: ev.Severity, FirstSeen: ev.OccurredAt,
			LastSeen: ev.OccurredAt, Minutes: map[time.Time]*Minute{}}
		a.cur[ev.Fingerprint] = g
	}
	g.Sources[ev.Source] = true
	if ports.SeverityRank(ev.Severity) > ports.SeverityRank(g.SeverityMax) {
		g.SeverityMax = ev.Severity
	}
	if ev.OccurredAt.Before(g.FirstSeen) {
		g.FirstSeen = ev.OccurredAt
	}
	if ev.OccurredAt.After(g.LastSeen) {
		g.LastSeen = ev.OccurredAt
	}
	minute := ev.OccurredAt.UTC().Truncate(time.Minute)
	mc := g.Minutes[minute]
	if mc == nil {
		mc = &Minute{}
		g.Minutes[minute] = mc
	}
	if m != nil && m.Action == knownissues.Suppress {
		// Known and suppressed: a counter increment, no sample, never decoded.
		g.Suppressed++
		mc.Suppressed++
		g.SuppressedBy = m.RuleID
		return nil
	}
	if m != nil && m.Action == knownissues.LabelOnly {
		g.LabeledBy = m.RuleID
	}
	g.Count++
	mc.Count++
	a.sample(g, ev, now)
	return nil
}

// sample keeps the first FirstSamples events of a fingerprint, then a reservoir (Algorithm R) of
// ReservoirPerHour per hour whose slots later samples may overwrite.
func (a *Aggregator) sample(g *Group, ev ports.SignalEvent, now time.Time) {
	if n, _ := a.firsts.get(g.Fingerprint); n < FirstSamples {
		a.firsts.put(g.Fingerprint, n+1)
		g.Samples = append(g.Samples, Sample{Kind: SampleFirst, Hour: now.UTC().Truncate(time.Hour), Slot: -(n + 1), Event: ev})
		return
	}
	hour := now.UTC().Truncate(time.Hour)
	if !hour.Equal(a.hourNow) {
		a.hourNow = hour
		a.hours = map[string]*hourly{} // a new hour starts every reservoir over
	}
	h := a.hours[g.Fingerprint]
	if h == nil {
		h = &hourly{hour: hour}
		a.hours[g.Fingerprint] = h
	}
	h.seen++
	slot := h.seen - 1
	if h.seen > ReservoirPerHour {
		slot = a.o.Rand(h.seen)
		if slot >= ReservoirPerHour {
			return
		}
	}
	for i := range g.Samples { // a later sample for the same slot in this window replaces the earlier one
		if s := &g.Samples[i]; s.Kind == SampleReservoir && s.Slot == slot && s.Hour.Equal(hour) {
			s.Event = ev
			return
		}
	}
	g.Samples = append(g.Samples, Sample{Kind: SampleReservoir, Hour: hour, Slot: slot, Event: ev})
}

// cut closes the current window into the flush queue.
func (a *Aggregator) cut() {
	a.mu.Lock()
	defer a.mu.Unlock()
	if len(a.cur) == 0 {
		return
	}
	b := Batch{Window: a.o.Now(), Groups: make([]*Group, 0, len(a.cur))}
	for _, g := range a.cur {
		b.Groups = append(b.Groups, g)
	}
	a.cur = map[string]*Group{}
	a.queue = append(a.queue, b)
	select {
	case a.wake <- struct{}{}:
	default:
	}
}

// drain flushes queued batches in order; a failing batch stays at the head and is retried after backoff.
func (a *Aggregator) drain(ctx context.Context) error {
	a.flushMu.Lock()
	defer a.flushMu.Unlock()
	for {
		a.mu.Lock()
		if len(a.queue) == 0 {
			a.mu.Unlock()
			return nil
		}
		b := a.queue[0]
		a.mu.Unlock()
		t0 := time.Now()
		err := a.sink.Flush(ctx, b)
		if a.o.OnFlush != nil {
			a.o.OnFlush(b, time.Since(t0), err)
		}
		if err != nil {
			return err
		}
		a.mu.Lock()
		a.queue = a.queue[1:]
		a.mu.Unlock()
	}
}

// Flush closes the current window and flushes everything queued (tests and shutdown).
func (a *Aggregator) Flush(ctx context.Context) error {
	a.cut()
	return a.drain(ctx)
}

// Pending is the number of windows waiting to be flushed.
func (a *Aggregator) Pending() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return len(a.queue)
}

// Run cuts a window every Options.Window and flushes until ctx ends, then flushes what is left (with a
// short grace period) so a clean shutdown loses nothing.
func (a *Aggregator) Run(ctx context.Context) {
	tick := time.NewTicker(a.o.Window)
	defer tick.Stop()
	backoff := 100 * time.Millisecond
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case <-a.wake:
			}
			for {
				if err := a.drain(ctx); err == nil || ctx.Err() != nil {
					backoff = 100 * time.Millisecond
					break
				}
				select {
				case <-ctx.Done():
					return
				case <-time.After(backoff):
				}
				backoff = min(backoff*2, 10*time.Second)
			}
		}
	}()
	for {
		select {
		case <-ctx.Done():
			sctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			_ = a.Flush(sctx)
			cancel()
			return
		case <-tick.C:
			a.cut()
		}
	}
}
