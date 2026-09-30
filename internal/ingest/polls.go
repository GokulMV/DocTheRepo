package ingest

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/GokulMV/DocTheRepo/internal/ports"
)

// PollStore is the connector state signal polling reads and writes.
type PollStore interface {
	Get(ctx context.Context, id string) (ports.ConnectorConfig, error)
	ListByType(ctx context.Context, types ...string) ([]ports.ConnectorConfig, error)
	Cursors(ctx context.Context, id string) (map[string]string, error)
	SetCursor(ctx context.Context, id, stream, cursor string) error
	SetHealth(ctx context.Context, id string, syncErr error) error
}

// SignalPolls schedules and runs signal pollers (plan § 8.16). The leader-elected scheduler calls Enqueue;
// a worker runs each signal_batch job with Handle, so a connector is polled by one replica at a time.
type SignalPolls struct {
	Sink    *SignalIngest
	Store   PollStore
	Queue   Enqueuer
	Pollers map[string]ports.SignalPoller
	Now     func() time.Time

	mu   sync.Mutex
	last map[string]time.Time
}

// PollPayload is a signal_batch job's payload.
type PollPayload struct {
	ConnectorID string `json:"connector_id"`
}

// defaultPollInterval applies when a connector has no poll interval (plan: 60 s for signals).
const defaultPollInterval = 60 * time.Second

// Enqueue queues a poll for every enabled poll-mode connector whose interval has elapsed. A connector
// already queued or running is collapsed onto that job.
func (p *SignalPolls) Enqueue(ctx context.Context) (int, error) {
	types := make([]string, 0, len(p.Pollers))
	for t := range p.Pollers {
		types = append(types, t)
	}
	ccs, err := p.Store.ListByType(ctx, types...)
	if err != nil {
		return 0, err
	}
	now := p.now()
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.last == nil {
		p.last = map[string]time.Time{}
	}
	n := 0
	var errs []error
	for _, cc := range ccs {
		if cc.Mode != "poll" && cc.Mode != "both" {
			continue
		}
		interval := time.Duration(cc.PollSeconds) * time.Second
		if interval <= 0 {
			interval = defaultPollInterval
		}
		if now.Sub(p.last[cc.ID]) < interval {
			continue
		}
		_, created, err := p.Queue.Enqueue(ctx, ports.NewJob{Type: ports.JobSignalBatch, SerialKey: "poll:" + cc.ID,
			DedupeKey: "poll:" + cc.ID, Payload: PollPayload{ConnectorID: cc.ID}, MaxAttempts: 3})
		if err != nil {
			errs = append(errs, err)
			continue
		}
		p.last[cc.ID] = now
		if created {
			n++
		}
	}
	return n, errors.Join(errs...)
}

// Handle runs one poll: each page's events are persisted before its cursor is stored, so a crash repeats
// at most one page (deduplicated by external ID). The outcome is recorded as connector health.
func (p *SignalPolls) Handle(ctx context.Context, job ports.Job) (ports.Outcome, error) {
	var pl PollPayload
	if err := json.Unmarshal(job.Payload, &pl); err != nil || pl.ConnectorID == "" {
		return ports.Outcome{}, ports.Permanent(fmt.Errorf("signal poll payload: %v", err))
	}
	cc, err := p.Store.Get(ctx, pl.ConnectorID)
	if errors.Is(err, ports.ErrNotFound) {
		return ports.Outcome{Status: ports.JobAborted, Message: "connector deleted or disabled"}, nil
	}
	if err != nil {
		return ports.Outcome{}, err
	}
	poller, ok := p.Pollers[cc.Type]
	if !ok {
		return ports.Outcome{}, ports.Permanent(fmt.Errorf("no poller for connector type %q", cc.Type))
	}
	cursors, err := p.Store.Cursors(ctx, cc.ID)
	if err != nil {
		return ports.Outcome{}, err
	}
	events, streams := 0, 0
	err = poller.Poll(ctx, cc, cursors, func(stream, cursor string, evs []ports.SignalEvent) error {
		for i := range evs {
			evs[i].ConnectorID = cc.ID
		}
		if err := p.Sink.IngestDurable(ctx, evs); err != nil {
			return err
		}
		events += len(evs)
		streams++
		if cursor == "" || cursor == cursors[stream] {
			return nil
		}
		return p.Store.SetCursor(ctx, cc.ID, stream, cursor)
	})
	if herr := p.Store.SetHealth(context.WithoutCancel(ctx), cc.ID, err); herr != nil {
		p.Sink.log().Warn("record connector health failed", "connector_id", cc.ID, "err", herr)
	}
	if err != nil {
		var v *ports.ValidationError
		if errors.As(err, &v) { // misconfiguration: retrying will not help until an admin edits it
			return ports.Outcome{}, ports.Permanent(err)
		}
		return ports.Outcome{}, err
	}
	return ports.Outcome{Result: map[string]int{"events": events, "streams": streams}}, nil
}

func (p *SignalPolls) now() time.Time {
	if p.Now != nil {
		return p.Now()
	}
	return time.Now()
}
