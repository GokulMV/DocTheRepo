package ingest

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/GokulMV/DocTheRepo/internal/ports"
)

func TestStreamRunnerReconciles(t *testing.T) {
	var mu sync.Mutex
	conns := []ports.ConnectorConfig{{ID: "a", Type: "pubsub", Config: map[string]string{"subscription": "s1"}},
		{ID: "b", Type: "pubsub", Config: map[string]string{"subscription": "bad"}}}
	var starts, stops atomic.Int32
	var failed []string
	r := &StreamRunner{
		List: func(context.Context) ([]ports.ConnectorConfig, error) {
			mu.Lock()
			defer mu.Unlock()
			return append([]ports.ConnectorConfig(nil), conns...), nil
		},
		Start: func(_ context.Context, cc ports.ConnectorConfig) (func(context.Context), error) {
			if cc.Config["subscription"] == "bad" {
				return nil, errors.New("no such subscription")
			}
			starts.Add(1)
			return func(ctx context.Context) { <-ctx.Done(); stops.Add(1) }, nil
		},
		OnError: func(cc ports.ConnectorConfig, err error) { failed = append(failed, cc.ID) },
	}
	ctx := context.Background()
	require.NoError(t, r.Sync(ctx))
	assert.Equal(t, []string{"a"}, r.Running())
	assert.Equal(t, []string{"b"}, failed, "a consumer that cannot start is reported and retried next sync")

	require.NoError(t, r.Sync(ctx))
	assert.Equal(t, int32(1), starts.Load(), "unchanged connectors keep their consumer")

	mu.Lock()
	conns[0].Config = map[string]string{"subscription": "s2"}
	conns[1].Config = map[string]string{"subscription": "fixed"}
	mu.Unlock()
	require.NoError(t, r.Sync(ctx))
	assert.Equal(t, []string{"a", "b"}, r.Running())
	assert.Equal(t, int32(1), stops.Load(), "a settings change restarts the consumer")
	assert.Equal(t, int32(3), starts.Load())

	mu.Lock()
	conns = conns[:1]
	mu.Unlock()
	require.NoError(t, r.Sync(ctx))
	assert.Equal(t, []string{"a"}, r.Running(), "disabled or deleted connectors stop")
	r.stopAll()
	assert.Equal(t, int32(3), stops.Load())
	assert.Empty(t, r.Running())
}
