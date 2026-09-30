package kafka

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/twmb/franz-go/pkg/kadm"
	"github.com/twmb/franz-go/pkg/kfake"
	"github.com/twmb/franz-go/pkg/kgo"

	"github.com/GokulMV/DocTheRepo/internal/ports"
)

func TestInspectAgainstFakeCluster(t *testing.T) {
	ctx := context.Background()
	c, err := kfake.NewCluster(kfake.NumBrokers(1), kfake.SeedTopics(2, "orders", "orders.dlq", "payments"))
	require.NoError(t, err)
	defer c.Close()
	cc := ports.ConnectorConfig{ID: "k1", Type: "kafka", Config: map[string]string{"brokers": strings.Join(c.ListenAddrs(), ",")}}

	prod, err := kgo.NewClient(kgo.SeedBrokers(c.ListenAddrs()...), kgo.RecordPartitioner(kgo.ManualPartitioner()))
	require.NoError(t, err)
	defer prod.Close()
	var recs []*kgo.Record
	for i := 0; i < 3000; i++ {
		recs = append(recs, &kgo.Record{Topic: "orders", Partition: int32(i % 2), Value: []byte("order")})
	}
	for i := 0; i < 4; i++ {
		class := "com.acme.PaymentDeclined"
		if i == 3 {
			class = "java.net.SocketTimeoutException"
		}
		recs = append(recs, &kgo.Record{Topic: "orders.dlq", Value: []byte(`{"order":` + string(rune('1'+i)) + `}`),
			Headers: []kgo.RecordHeader{{Key: "kafka_dlt-exception-fqcn", Value: []byte(class)},
				{Key: "kafka_dlt-exception-message", Value: []byte("declined")}, {Key: "kafka_dlt-original-topic", Value: []byte("orders")}}})
	}
	require.NoError(t, prod.ProduceSync(ctx, recs...).FirstErr())

	// An application group that has consumed 500 of each partition's 1,500 records.
	adm := kadm.NewClient(prod)
	offs := kadm.Offsets{}
	offs.Add(kadm.Offset{Topic: "orders", Partition: 0, At: 500})
	offs.Add(kadm.Offset{Topic: "orders", Partition: 1, At: 500})
	_, err = adm.CommitOffsets(ctx, "orders-consumer", offs)
	require.NoError(t, err)

	rs, err := New().Inspect(ctx, cc)
	require.NoError(t, err)
	byRes := map[string]ports.BusReading{}
	for _, r := range rs {
		byRes[string(r.Condition)+":"+r.Resource] = r
	}
	lag, ok := byRes["lag:orders-consumer"]
	require.True(t, ok, "%+v", rs)
	assert.Equal(t, int64(2000), lag.Backlog)
	assert.Equal(t, "orders", lag.Attrs["topics"])

	dlq, ok := byRes["dlq:orders.dlq"]
	require.True(t, ok)
	assert.Equal(t, int64(4), dlq.Backlog)
	require.Len(t, dlq.Samples, 4)
	classes := map[string]int{}
	for _, s := range dlq.Samples {
		classes[s.ErrorClass]++
		assert.Equal(t, "declined", s.Reason)
		assert.Equal(t, "orders", s.Attrs["original_topic"])
	}
	assert.Equal(t, map[string]int{"com.acme.PaymentDeclined": 3, "java.net.SocketTimeoutException": 1}, classes)
	_, isDLQ := byRes["dlq:payments"]
	assert.False(t, isDLQ)

	groups, err := adm.ListGroups(ctx)
	require.NoError(t, err)
	assert.Equal(t, []string{"orders-consumer"}, groups.Groups(), "the inspector never creates a consumer group")
	committed, err := adm.FetchOffsets(ctx, "orders-consumer")
	require.NoError(t, err)
	o, _ := committed.Lookup("orders", 0)
	assert.Equal(t, int64(500), o.At, "and never moves an application group's offsets")

	cc.Config["groups"] = "billing-*"
	rs, err = New().Inspect(ctx, cc)
	require.NoError(t, err)
	for _, r := range rs {
		assert.NotEqual(t, ports.BusLag, r.Condition, "the group allow list applies")
	}
}

func TestDialValidates(t *testing.T) {
	var ve *ports.ValidationError
	_, err := Dial(ports.ConnectorConfig{Config: map[string]string{}})
	assert.ErrorAs(t, err, &ve)
	_, err = Dial(ports.ConnectorConfig{Config: map[string]string{"brokers": "b:9092", "sasl_mechanism": "plain"}})
	assert.ErrorAs(t, err, &ve)
	_, err = Dial(ports.ConnectorConfig{Credentials: `{"username":"u","password":"p"}`, Config: map[string]string{"brokers": "b:9092", "sasl_mechanism": "gssapi"}})
	assert.ErrorAs(t, err, &ve)
	cl, err := Dial(ports.ConnectorConfig{Credentials: `{"username":"u","password":"p"}`, Config: map[string]string{"brokers": "b:9092", "sasl_mechanism": "scram-sha-512", "tls": "true"}})
	require.NoError(t, err)
	cl.Close()
	assert.True(t, matches(DefaultDLQPatterns, "orders.DLT"))
	assert.False(t, matches(DefaultDLQPatterns, "orders"))
}
