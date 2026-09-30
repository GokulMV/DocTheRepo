package aggregate

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/GokulMV/DocTheRepo/internal/core/knownissues"
	"github.com/GokulMV/DocTheRepo/internal/ports"
)

type memSink struct {
	mu      sync.Mutex
	batches []Batch
	fail    atomic.Bool
}

func (s *memSink) Flush(_ context.Context, b Batch) error {
	if s.fail.Load() {
		return errors.New("db down")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.batches = append(s.batches, b)
	return nil
}

func (s *memSink) groups(fp string) []*Group {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []*Group
	for _, b := range s.batches {
		for _, g := range b.Groups {
			if g.Fingerprint == fp {
				out = append(out, g)
			}
		}
	}
	return out
}

func (s *memSink) total() int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	var n int64
	for _, b := range s.batches {
		n += b.Events()
	}
	return n
}

var t0 = time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)

type clock struct{ t time.Time }

func (c *clock) now() time.Time { return c.t }

func evt(fp string, i int, sev ports.Severity) ports.SignalEvent {
	return ports.SignalEvent{ConnectorID: "c1", ExternalID: fmt.Sprintf("%s-%d", fp, i), Source: "sentry", Kind: ports.KindError, Service: "api",
		Title: "boom", Severity: sev, Fingerprint: fp, OccurredAt: t0.Add(time.Duration(i) * time.Second)}
}

func TestGroupsCountsAndMinutes(t *testing.T) {
	s := &memSink{}
	c := &clock{t0}
	a := New(s, Options{Now: c.now})
	require.NoError(t, a.Add(evt("a", 1, ports.SeverityWarning), nil))
	require.NoError(t, a.Add(evt("a", 70, ports.SeverityCritical), nil))
	e := evt("a", 2, ports.SeverityError)
	e.Source = "datadog"
	require.NoError(t, a.Add(e, nil))
	require.NoError(t, a.Add(evt("b", 1, ports.SeverityError), nil))
	require.NoError(t, a.Flush(context.Background()))
	require.Len(t, s.batches, 1)
	g := s.groups("a")[0]
	assert.Equal(t, int64(3), g.Count)
	assert.Equal(t, ports.SeverityCritical, g.SeverityMax)
	assert.Equal(t, t0.Add(time.Second), g.FirstSeen)
	assert.Equal(t, t0.Add(70*time.Second), g.LastSeen)
	assert.Equal(t, map[string]bool{"sentry": true, "datadog": true}, g.Sources)
	assert.Equal(t, int64(2), g.Minutes[t0].Count)
	assert.Equal(t, int64(1), g.Minutes[t0.Add(time.Minute)].Count)
	assert.Len(t, s.groups("b"), 1)
	require.NoError(t, a.Flush(context.Background()))
	assert.Len(t, s.batches, 1, "an empty window writes nothing")
}

// Plan § 10 incident spike: one fingerprint at a very high rate → exactly one issue update per window,
// counts exact, samples bounded.
func TestIncidentSpike_ExactCountBoundedSamples(t *testing.T) {
	s := &memSink{}
	c := &clock{t0}
	a := New(s, Options{Now: c.now})
	const n = 50_000
	for i := 0; i < n; i++ {
		e := evt("spike", i, ports.SeverityError)
		e.OccurredAt = t0
		require.NoError(t, a.Add(e, nil))
	}
	require.NoError(t, a.Flush(context.Background()))
	gs := s.groups("spike")
	require.Len(t, gs, 1)
	assert.Equal(t, int64(n), gs[0].Count)
	var first, res int
	for _, smp := range gs[0].Samples {
		switch smp.Kind {
		case SampleFirst:
			first++
		case SampleReservoir:
			res++
			assert.GreaterOrEqual(t, smp.Slot, 0)
			assert.Less(t, smp.Slot, ReservoirPerHour)
		}
	}
	assert.Equal(t, FirstSamples, first)
	assert.Equal(t, ReservoirPerHour, res, "each reservoir slot appears once per window")
}

func TestSamplingAcrossWindowsAndHours(t *testing.T) {
	s := &memSink{}
	c := &clock{t0}
	pick := 0
	a := New(s, Options{Now: c.now, Rand: func(n int) int { return pick }})
	add := func(k int) {
		for i := 0; i < k; i++ {
			require.NoError(t, a.Add(evt("x", int(c.t.Unix())*1000+i, ports.SeverityError), nil))
		}
		require.NoError(t, a.Flush(context.Background()))
	}
	add(30) // 5 first + 20 reservoir; events 26..30 replace slot 0 (pick = 0)
	last := s.batches[len(s.batches)-1].Groups[0]
	assert.Len(t, last.Samples, 25)
	c.t = c.t.Add(time.Second)
	pick = ReservoirPerHour // beyond the reservoir: not kept
	add(10)
	last = s.batches[len(s.batches)-1].Groups[0]
	assert.Empty(t, last.Samples, "first samples are only the first five ever; the hour's reservoir is full")
	c.t = t0.Add(time.Hour)
	add(3)
	last = s.batches[len(s.batches)-1].Groups[0]
	require.Len(t, last.Samples, 3, "a new hour starts a new reservoir")
	assert.Equal(t, t0.Add(time.Hour), last.Samples[0].Hour)
	assert.Equal(t, 0, last.Samples[0].Slot)
}

func TestSuppressedAndLabeled(t *testing.T) {
	s := &memSink{}
	a := New(s, Options{Now: (&clock{t0}).now})
	require.NoError(t, a.Add(evt("k", 1, ports.SeverityError), &knownissues.Result{RuleID: "r1", Action: knownissues.Suppress}))
	require.NoError(t, a.Add(evt("k", 2, ports.SeverityError), &knownissues.Result{RuleID: "r1", Action: knownissues.Suppress}))
	require.NoError(t, a.Add(evt("l", 1, ports.SeverityError), &knownissues.Result{RuleID: "r2", Action: knownissues.LabelOnly}))
	require.NoError(t, a.Flush(context.Background()))
	k := s.groups("k")[0]
	assert.Zero(t, k.Count)
	assert.Equal(t, int64(2), k.Suppressed)
	assert.Equal(t, "r1", k.SuppressedBy)
	assert.Empty(t, k.Samples, "suppressed events cost a counter, not a sample")
	assert.Equal(t, int64(2), k.Minutes[t0].Suppressed)
	l := s.groups("l")[0]
	assert.Equal(t, int64(1), l.Count)
	assert.Equal(t, "r2", l.LabeledBy)
	assert.Len(t, l.Samples, 1)
}

func TestRedeliveryCountedOnce(t *testing.T) {
	s := &memSink{}
	c := &clock{t0}
	a := New(s, Options{Now: c.now, DedupeTTL: time.Minute})
	e := evt("d", 1, ports.SeverityError)
	require.NoError(t, a.Add(e, nil))
	require.NoError(t, a.Add(e, nil))
	c.t = c.t.Add(2 * time.Minute)
	require.NoError(t, a.Add(e, nil), "after the TTL the same ID counts again (a genuinely repeated event)")
	noID := e
	noID.ExternalID = ""
	require.NoError(t, a.Add(noID, nil))
	require.NoError(t, a.Add(noID, nil))
	require.NoError(t, a.Flush(context.Background()))
	assert.Equal(t, int64(4), s.groups("d")[0].Count)
}

func TestBackpressureNeverDropsAcceptedEvents(t *testing.T) {
	s := &memSink{}
	s.fail.Store(true)
	a := New(s, Options{Now: (&clock{t0}).now, MaxBuffered: 3})
	accepted := 0
	for w := 0; w < 10; w++ {
		for i := 0; i < 5; i++ {
			if err := a.Add(evt("bp", w*10+i, ports.SeverityError), nil); err == nil {
				accepted++
			} else {
				assert.ErrorIs(t, err, ErrOverloaded)
			}
		}
		assert.Error(t, a.Flush(context.Background()))
	}
	assert.True(t, a.Overloaded())
	assert.Equal(t, 4, a.Pending(), "windows stop growing once callers are refused")
	assert.Equal(t, 20, accepted)
	s.fail.Store(false)
	require.NoError(t, a.Flush(context.Background()))
	assert.False(t, a.Overloaded())
	assert.Equal(t, int64(accepted), s.total(), "every accepted event is flushed exactly once")
	require.NoError(t, a.Add(evt("bp", 999, ports.SeverityError), nil))
}

func TestRunFlushesConcurrentlyAndOnShutdown(t *testing.T) {
	s := &memSink{}
	a := New(s, Options{Window: 5 * time.Millisecond})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { a.Run(ctx); close(done) }()
	var wg sync.WaitGroup
	var added atomic.Int64
	for g := 0; g < 8; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			for i := 0; i < 2000; i++ {
				e := evt(fmt.Sprintf("fp%d", i%50), g*100000+i, ports.SeverityError)
				if a.Add(e, nil) == nil {
					added.Add(1)
				}
			}
		}(g)
	}
	wg.Wait()
	cancel()
	<-done
	assert.Equal(t, added.Load(), s.total(), "the shutdown flush writes what is left")
	assert.Greater(t, len(s.batches), 1)
}

func TestLRU(t *testing.T) {
	c := newLRU[int](2)
	c.put("a", 1)
	c.put("b", 2)
	c.get("a")
	c.put("c", 3)
	_, ok := c.get("b")
	assert.False(t, ok, "least recently used evicted")
	v, ok := c.get("a")
	assert.True(t, ok)
	assert.Equal(t, 1, v)
	c.put("a", 9)
	v, _ = c.get("a")
	assert.Equal(t, 9, v)
	assert.Equal(t, 2, c.len())
}

func BenchmarkAdd(b *testing.B) {
	a := New(&memSink{}, Options{})
	evs := make([]ports.SignalEvent, 2000)
	for i := range evs {
		evs[i] = evt(fmt.Sprintf("fp%d", i), i, ports.SeverityError)
		evs[i].ExternalID = ""
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = a.Add(evs[i%len(evs)], nil)
		if i%20000 == 0 {
			_ = a.Flush(context.Background())
		}
	}
}

func TestWaitFlushed(t *testing.T) {
	s := &memSink{}
	a := New(s, Options{})
	ctx := context.Background()
	require.NoError(t, a.Add(evt("w", 1, ports.SeverityError), nil))
	seq := a.OpenWindow()
	short, cancel := context.WithTimeout(ctx, 20*time.Millisecond)
	defer cancel()
	assert.ErrorIs(t, a.WaitFlushed(short, seq), context.DeadlineExceeded, "not persisted yet")

	s.fail.Store(true)
	assert.Error(t, a.Flush(ctx))
	short2, cancel2 := context.WithTimeout(ctx, 20*time.Millisecond)
	defer cancel2()
	assert.ErrorIs(t, a.WaitFlushed(short2, seq), context.DeadlineExceeded, "a failed flush is not durable")

	s.fail.Store(false)
	done := make(chan error, 1)
	go func() { done <- a.WaitFlushed(ctx, seq) }()
	require.NoError(t, a.Flush(ctx))
	require.NoError(t, <-done)

	// A quiet period: an empty cut with nothing queued counts as flushed, so waiters never hang.
	idle := a.OpenWindow()
	require.NoError(t, a.Flush(ctx))
	require.NoError(t, a.WaitFlushed(ctx, idle))
}
