package ingest

import (
	"context"
	"sort"
	"time"

	"github.com/GokulMV/DocTheRepo/internal/core/busrules"
	"github.com/GokulMV/DocTheRepo/internal/ports"
)

// busSources maps a bus connector type to the event source name.
var busSources = map[string]string{"pubsub_bus": "pubsub"}

// BusPoller runs an event-platform inspector as a signal poller: it applies the lag / DLQ / failure rules
// and keeps each resource's rule state in the connector cursor table (stream "bus:<condition>:<resource>").
type BusPoller struct {
	Inspector ports.BusInspector
	// Resolve closes issues whose condition cleared.
	Resolve func(ctx context.Context, fingerprints []string) error
	Now     func() time.Time
}

// Type implements ports.SignalPoller.
func (b *BusPoller) Type() string { return b.Inspector.Type() }

// Poll implements ports.SignalPoller. All events are persisted in one step, then resolutions are applied,
// then rule state is saved: a crash repeats at most one evaluation.
func (b *BusPoller) Poll(ctx context.Context, cc ports.ConnectorConfig, cursors map[string]string,
	emit func(stream, cursor string, events []ports.SignalEvent) error) error {
	readings, err := b.Inspector.Inspect(ctx, cc)
	if err != nil {
		return err
	}
	bus := cc.Type
	if s, ok := busSources[bus]; ok {
		bus = s
	}
	th := busrules.ThresholdsFrom(cc.Config)
	now := time.Now()
	if b.Now != nil {
		now = b.Now()
	}
	states := map[string]string{}
	var events []ports.SignalEvent
	var resolve []string
	for _, r := range readings {
		stream := "bus:" + string(r.Condition) + ":" + r.Resource
		res := busrules.Evaluate(bus, cc.ID, r, busrules.ParseState(cursors[stream]), th, now)
		events = append(events, res.Events...)
		resolve = append(resolve, res.Resolve...)
		states[stream] = res.State.String()
	}
	if err := emit("", "", events); err != nil {
		return err
	}
	if c, ok := b.Inspector.(ports.BusCommitter); ok {
		if err := c.Commit(ctx, cc); err != nil {
			return err
		}
	}
	if len(resolve) > 0 && b.Resolve != nil {
		if err := b.Resolve(ctx, resolve); err != nil {
			return err
		}
	}
	streams := make([]string, 0, len(states))
	for s := range states {
		streams = append(streams, s)
	}
	sort.Strings(streams)
	for _, s := range streams {
		if err := emit(s, states[s], nil); err != nil {
			return err
		}
	}
	return nil
}
