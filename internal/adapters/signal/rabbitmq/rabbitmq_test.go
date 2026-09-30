package rabbitmq

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/GokulMV/DocTheRepo/internal/ports"
)

func TestInspectRabbit(t *testing.T) {
	var gets []map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u, p, _ := r.BasicAuth()
		if u != "hub" || p != "pw" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		switch r.URL.Path {
		case "/api/queues":
			_, _ = w.Write([]byte(`[
{"name":"orders","vhost":"/","messages_ready":1500,"messages_unacknowledged":20,"consumers":0,"arguments":{"x-dead-letter-exchange":"orders.dlx"}},
{"name":"orders.failed","vhost":"/","messages_ready":6,"messages_unacknowledged":0,"consumers":0,"arguments":{}},
{"name":"mail.dlq","vhost":"shop","messages_ready":0,"messages_unacknowledged":0,"consumers":0,"arguments":{}},
{"name":"amq.gen-xyz","vhost":"/","messages_ready":0,"messages_unacknowledged":0,"consumers":1,"arguments":{}}]`))
		case "/api/bindings":
			_, _ = w.Write([]byte(`[{"source":"orders.dlx","vhost":"/","destination":"orders.failed","destination_type":"queue"},
{"source":"orders.dlx","vhost":"/","destination":"audit","destination_type":"exchange"}]`))
		case "/api/queues/%2F/orders.failed/get", "/api/queues///orders.failed/get":
			var in map[string]any
			_ = json.NewDecoder(r.Body).Decode(&in)
			gets = append(gets, in)
			_, _ = w.Write([]byte(`[{"payload":"{\"order\":9}","routing_key":"orders","properties":{"headers":{
"x-exception-class":"InventoryMissing","x-death":[{"reason":"rejected","queue":"orders","count":3}]}}}]`))
		default:
			t.Errorf("unexpected %s", r.URL.Path)
		}
	}))
	defer srv.Close()
	cc := ports.ConnectorConfig{ID: "r1", Type: "rabbitmq", Credentials: `{"username":"hub","password":"pw"}`, Config: map[string]string{"url": srv.URL + "/"}}
	inspect := func() map[string]ports.BusReading {
		rs, err := New().Inspect(context.Background(), cc)
		require.NoError(t, err)
		m := map[string]ports.BusReading{}
		for _, r := range rs {
			m[r.Resource] = r
		}
		require.Len(t, m, 3, "amq.* server-named queues are skipped")
		return m
	}
	rs := inspect()
	assert.Equal(t, ports.BusDLQ, rs["orders.failed"].Condition, "bound to a dead-letter exchange")
	assert.Equal(t, int64(6), rs["orders.failed"].Backlog)
	assert.Empty(t, gets, "no peek without opt-in")
	assert.Equal(t, ports.BusReading{Resource: "orders", Condition: ports.BusLag, Backlog: 1500, HasBacklog: true,
		Attrs: map[string]string{"queue": "orders", "vhost": "/", "consumers": "0"}}, rs["orders"])
	assert.Equal(t, ports.BusDLQ, rs["shop/mail.dlq"].Condition, "recognised by name")

	cc.Config["peek"] = "true"
	rs = inspect()
	require.Len(t, rs["orders.failed"].Samples, 1)
	s := rs["orders.failed"].Samples[0]
	assert.Equal(t, "InventoryMissing", s.ErrorClass)
	assert.Equal(t, "rejected", s.Reason)
	assert.Equal(t, "orders", s.Attrs["source_queue"])
	require.Len(t, gets, 1, "the empty DLQ is not peeked")
	assert.Equal(t, "reject_requeue_true", gets[0]["ackmode"], "peeked messages go back to the queue")

	cc.Credentials = `{"username":"hub","password":"wrong"}`
	_, err := New().Inspect(context.Background(), cc)
	var perm *ports.PermanentError
	assert.ErrorAs(t, err, &perm)
	var ve *ports.ValidationError
	_, err = New().Inspect(context.Background(), ports.ConnectorConfig{Config: map[string]string{"url": "x"}})
	assert.ErrorAs(t, err, &ve)
}
