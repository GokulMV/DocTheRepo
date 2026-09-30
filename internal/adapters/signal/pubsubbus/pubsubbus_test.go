package pubsubbus

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/GokulMV/DocTheRepo/internal/adapters/signal/pubsub"
	"github.com/GokulMV/DocTheRepo/internal/ports"
)

type fakePuller struct {
	msgs  []pubsub.Received
	acked []string
}

func (f *fakePuller) Pull(context.Context, int) ([]pubsub.Received, error) { return f.msgs, nil }
func (f *fakePuller) Ack(_ context.Context, ids []string) error {
	f.acked = append(f.acked, ids...)
	return nil
}
func (f *fakePuller) Nack(context.Context, []string) error { return nil }

func TestInspectPubSub(t *testing.T) {
	var paths []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		f := r.URL.Query().Get("filter")
		switch {
		case strings.Contains(f, "num_undelivered_messages"):
			_, _ = w.Write([]byte(`{"timeSeries":[
{"resource":{"labels":{"subscription_id":"orders-worker"}},"points":[{"value":{"int64Value":"5400"}},{"value":{"int64Value":"10"}}]},
{"resource":{"labels":{"subscription_id":"hub-orders-dlq"}},"points":[{"value":{"int64Value":"3"}}]}]}`))
		case strings.Contains(f, "oldest_unacked_message_age"):
			_, _ = w.Write([]byte(`{"timeSeries":[{"resource":{"labels":{"subscription_id":"orders-worker"}},"points":[{"value":{"int64Value":"420"}}]}]}`))
		}
	}))
	defer srv.Close()
	dlq := &fakePuller{msgs: []pubsub.Received{
		{AckID: "a1", Message: pubsub.Message{Data: []byte(`{"order":1}`), Attributes: map[string]string{"error_class": "PaymentDeclined",
			"CloudPubSubDeadLetterSourceSubscription": "orders-worker"}}},
		{AckID: "a2", Message: pubsub.Message{Data: []byte(`{"order":2}`)}},
	}}
	i := &Inspector{Endpoint: srv.URL + "/v3/", Now: func() time.Time { return time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC) },
		Client: func(context.Context, ports.ConnectorConfig) (*http.Client, error) { return srv.Client(), nil },
		Puller: func(_ context.Context, _ ports.ConnectorConfig, sub string) (pubsub.Puller, error) {
			assert.Equal(t, "hub-orders-dlq", sub)
			return dlq, nil
		}}
	cc := ports.ConnectorConfig{ID: "p1", Type: "pubsub_bus", Config: map[string]string{"project": "acme", "dlq_subscriptions": "hub-orders-dlq"}}
	rs, err := i.Inspect(context.Background(), cc)
	require.NoError(t, err)
	require.Len(t, rs, 2, "the Hub-owned DLQ subscription is not reported as lag")
	assert.Equal(t, ports.BusReading{Resource: "orders-worker", Condition: ports.BusLag, Backlog: 5400, HasBacklog: true, OldestAge: 7 * time.Minute,
		Attrs: map[string]string{"subscription": "orders-worker", "gcp.project": "acme"}}, rs[0])
	assert.Equal(t, "/v3/projects/acme/timeSeries", paths[0])
	d := rs[1]
	assert.Equal(t, ports.BusDLQ, d.Condition)
	assert.False(t, d.HasBacklog, "each pulled message is new")
	require.Len(t, d.Samples, 2)
	assert.Equal(t, "PaymentDeclined", d.Samples[0].ErrorClass)
	assert.Equal(t, "orders-worker", d.Attrs["source_subscription"])
	assert.Empty(t, dlq.acked, "nothing is acknowledged before the events are persisted")

	require.NoError(t, i.Commit(context.Background(), cc))
	assert.Equal(t, []string{"a1", "a2"}, dlq.acked)
	require.NoError(t, i.Commit(context.Background(), cc))
	assert.Len(t, dlq.acked, 2, "commit acknowledges once")

	var ve *ports.ValidationError
	_, err = i.Inspect(context.Background(), ports.ConnectorConfig{Config: map[string]string{}})
	assert.ErrorAs(t, err, &ve)
}
