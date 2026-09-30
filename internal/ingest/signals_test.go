package ingest_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/GokulMV/DocTheRepo/internal/core/aggregate"
	"github.com/GokulMV/DocTheRepo/internal/core/knownissues"
	"github.com/GokulMV/DocTheRepo/internal/core/signals"
	"github.com/GokulMV/DocTheRepo/internal/ingest"
	"github.com/GokulMV/DocTheRepo/internal/ports"
)

type rules struct {
	r   []knownissues.Rule
	svc []signals.ServiceRule
	err error
}

func (f *rules) ActiveRules(context.Context) ([]knownissues.Rule, error)     { return f.r, f.err }
func (f *rules) ServiceRules(context.Context) ([]signals.ServiceRule, error) { return f.svc, f.err }

type sink struct {
	mu   sync.Mutex
	b    []aggregate.Batch
	fail bool
}

func (s *sink) Flush(_ context.Context, b aggregate.Batch) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.fail {
		return errors.New("down")
	}
	s.b = append(s.b, b)
	return nil
}

func TestSignalIngest(t *testing.T) {
	ctx := context.Background()
	src := &rules{
		r: []knownissues.Rule{
			{ID: "mute", Match: knownissues.Match{Services: []string{"cart"}}, Action: knownissues.Suppress},
			{ID: "tag", Match: knownissues.Match{MessageRegex: "deprecated"}, Action: knownissues.LabelOnly},
		},
		svc: []signals.ServiceRule{{Service: "orders", Patterns: map[string]string{"log_group": "/aws/lambda/orders-*"}}},
	}
	sk := &sink{}
	agg := aggregate.New(sk, aggregate.Options{})
	var outcomes []string
	s := &ingest.SignalIngest{Agg: agg, Rules: src, Now: func() time.Time { return time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC) },
		OnEvent: func(_, o string) { outcomes = append(outcomes, o) }}
	require.NoError(t, s.Reload(ctx))

	attrs := map[string]string{"log_group": "/aws/lambda/orders-prod", "auth": "Bearer abcdefghijklmnopqrstuvwxyz"}
	in := []ports.SignalEvent{
		{ConnectorID: "c", ExternalID: "1", Source: "cloudwatch", Kind: ports.KindError, Message: "db password=hunter2 refused", Attrs: attrs},
		{ConnectorID: "c", ExternalID: "2", Source: "sentry", Service: "cart", Message: "timeout"},
		{ConnectorID: "c", ExternalID: "3", Source: "sentry", Service: "api", Message: "deprecated call"},
		{ExternalID: "4", Message: "no source"},
	}
	require.NoError(t, s.Ingest(ctx, in))
	assert.Equal(t, []string{ingest.OutcomeAccepted, ingest.OutcomeSuppressed, ingest.OutcomeLabeled, ingest.OutcomeInvalid}, outcomes)
	assert.Contains(t, attrs["auth"], "abcdefghij", "the caller's attributes are not modified")
	require.NoError(t, agg.Flush(ctx))
	require.Len(t, sk.b, 1)
	byService := map[string]*aggregate.Group{}
	for _, g := range sk.b[0].Groups {
		byService[g.Service] = g
	}
	orders := byService["orders"]
	require.NotNil(t, orders, "service resolved from the service map")
	require.Len(t, orders.Samples, 1)
	assert.Equal(t, "db password=<PASSWORD> refused", orders.Samples[0].Event.Message, "secrets are scrubbed before anything is stored")
	assert.Equal(t, "Bearer <TOKEN>", orders.Samples[0].Event.Attrs["auth"])
	assert.Equal(t, int64(1), byService["cart"].Suppressed)
	assert.Equal(t, "tag", byService["api"].LabeledBy)

	// Redelivery of the same batch after an error: already-accepted events are not counted twice.
	require.NoError(t, s.Ingest(ctx, in[:1]))
	require.NoError(t, agg.Flush(ctx))
	assert.Len(t, sk.b, 1, "nothing new")

	// A failed reload keeps the last good rule set.
	src.err = errors.New("db down")
	assert.Error(t, s.Reload(ctx))
	outcomes = nil
	require.NoError(t, s.Ingest(ctx, []ports.SignalEvent{{ConnectorID: "c", ExternalID: "5", Source: "sentry", Service: "cart", Message: "x"}}))
	assert.Equal(t, []string{ingest.OutcomeSuppressed}, outcomes)

	// Backpressure surfaces to the caller.
	sk.fail = true
	for i := 0; i < 10; i++ {
		_ = agg.Flush(ctx)
		_ = s.Ingest(ctx, []ports.SignalEvent{{ConnectorID: "c", ExternalID: string(rune('a' + i)), Source: "sentry", Message: "y"}})
	}
	assert.True(t, s.Overloaded())
	assert.ErrorIs(t, s.Ingest(ctx, []ports.SignalEvent{{ConnectorID: "c", ExternalID: "z", Source: "sentry", Message: "y"}}), aggregate.ErrOverloaded)
}
