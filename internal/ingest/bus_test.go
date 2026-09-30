package ingest

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/GokulMV/DocTheRepo/internal/core/busrules"
	"github.com/GokulMV/DocTheRepo/internal/ports"
)

type fakeInspector struct {
	readings []ports.BusReading
	log      *[]string
}

func (f fakeInspector) Type() string { return "pubsub_bus" }
func (f fakeInspector) Inspect(context.Context, ports.ConnectorConfig) ([]ports.BusReading, error) {
	*f.log = append(*f.log, "inspect")
	return f.readings, nil
}
func (f fakeInspector) Commit(context.Context, ports.ConnectorConfig) error {
	*f.log = append(*f.log, "commit")
	return nil
}

func TestBusPollerOrder(t *testing.T) {
	var log []string
	open := busrules.State{Open: "fp-lag"}.String()
	in := fakeInspector{log: &log, readings: []ports.BusReading{
		{Resource: "orders-worker", Condition: ports.BusLag, Backlog: 1, HasBacklog: true},
		{Resource: "hub-dlq", Condition: ports.BusDLQ, Samples: []ports.BusSample{{ErrorClass: "X"}}},
	}}
	p := &BusPoller{Inspector: in, Now: func() time.Time { return time.Unix(1790000000, 0) },
		Resolve: func(_ context.Context, fps []string) error { log = append(log, "resolve:"+fps[0]); return nil }}
	cc := ports.ConnectorConfig{ID: "c", Type: "pubsub_bus", Config: map[string]string{"resolve_seconds": "0"}}
	saved := map[string]string{}
	err := p.Poll(context.Background(), cc, map[string]string{"bus:lag:orders-worker": open}, func(stream, cursor string, evs []ports.SignalEvent) error {
		if stream == "" {
			log = append(log, "persist")
			require.Len(t, evs, 1)
			assert.Equal(t, "pubsub", evs[0].Source, "pubsub_bus connectors report as pubsub")
			return nil
		}
		saved[stream] = cursor
		log = append(log, "state")
		return nil
	})
	require.NoError(t, err)
	assert.Equal(t, []string{"inspect", "persist", "commit", "resolve:fp-lag", "state", "state"}, log,
		"acks and resolutions only after the events are persisted; state last")
	assert.Empty(t, busrules.ParseState(saved["bus:lag:orders-worker"]).Open)

	log = nil
	err = p.Poll(context.Background(), cc, nil, func(stream, _ string, _ []ports.SignalEvent) error {
		if stream == "" {
			return errors.New("database down")
		}
		t.Fatal("no state saved after a failed persist")
		return nil
	})
	assert.Error(t, err)
	assert.Equal(t, []string{"inspect"}, log, "nothing acknowledged when persisting fails")
}
