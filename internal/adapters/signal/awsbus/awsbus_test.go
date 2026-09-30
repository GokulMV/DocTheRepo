package awsbus

import (
	"context"
	"testing"
	"time"

	awssdk "github.com/aws/aws-sdk-go-v2/aws"
	cw "github.com/aws/aws-sdk-go-v2/service/cloudwatch"
	cwtypes "github.com/aws/aws-sdk-go-v2/service/cloudwatch/types"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	sqstypes "github.com/aws/aws-sdk-go-v2/service/sqs/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/GokulMV/DocTheRepo/internal/ports"
)

var now = time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)

// fakeMetrics serves ListMetrics from a catalogue and GetMetricData from values keyed "Metric/dimvalue".
type fakeMetrics struct {
	catalogue map[string][]map[string]string // "ns/metric" → dimension sets
	values    map[string][]float64           // "metric/value" → newest first, one per minute
	queries   int
}

func (f *fakeMetrics) ListMetrics(_ context.Context, in *cw.ListMetricsInput, _ ...func(*cw.Options)) (*cw.ListMetricsOutput, error) {
	var out cw.ListMetricsOutput
	for _, d := range f.catalogue[*in.Namespace+"/"+*in.MetricName] {
		var dims []cwtypes.Dimension
		for k, v := range d {
			dims = append(dims, cwtypes.Dimension{Name: awssdk.String(k), Value: awssdk.String(v)})
		}
		out.Metrics = append(out.Metrics, cwtypes.Metric{Namespace: in.Namespace, MetricName: in.MetricName, Dimensions: dims})
	}
	return &out, nil
}

func (f *fakeMetrics) GetMetricData(_ context.Context, in *cw.GetMetricDataInput, _ ...func(*cw.Options)) (*cw.GetMetricDataOutput, error) {
	var out cw.GetMetricDataOutput
	for _, q := range in.MetricDataQueries {
		f.queries++
		key := *q.MetricStat.Metric.MetricName
		for _, d := range q.MetricStat.Metric.Dimensions {
			if *d.Name != "EventBusName" {
				key += "/" + *d.Value
			}
		}
		r := cwtypes.MetricDataResult{Id: q.Id}
		for i, v := range f.values[key] {
			r.Values = append(r.Values, v)
			r.Timestamps = append(r.Timestamps, now.Add(-time.Duration(i+1)*time.Minute))
		}
		out.MetricDataResults = append(out.MetricDataResults, r)
	}
	return &out, nil
}

type fakeSQS struct {
	attrs    map[string]map[string]string
	messages []sqstypes.Message
	received []*sqs.ReceiveMessageInput
}

func (f *fakeSQS) ListQueues(context.Context, *sqs.ListQueuesInput, ...func(*sqs.Options)) (*sqs.ListQueuesOutput, error) {
	return &sqs.ListQueuesOutput{QueueUrls: []string{"https://sqs.eu-west-1.amazonaws.com/1/orders", "https://sqs.eu-west-1.amazonaws.com/1/orders-failed",
		"https://sqs.eu-west-1.amazonaws.com/1/emails-dlq", "https://sqs.eu-west-1.amazonaws.com/1/ignored"}}, nil
}

func (f *fakeSQS) GetQueueAttributes(_ context.Context, in *sqs.GetQueueAttributesInput, _ ...func(*sqs.Options)) (*sqs.GetQueueAttributesOutput, error) {
	u := *in.QueueUrl
	return &sqs.GetQueueAttributesOutput{Attributes: f.attrs[u[len("https://sqs.eu-west-1.amazonaws.com/1/"):]]}, nil
}

func (f *fakeSQS) ReceiveMessage(_ context.Context, in *sqs.ReceiveMessageInput, _ ...func(*sqs.Options)) (*sqs.ReceiveMessageOutput, error) {
	f.received = append(f.received, in)
	return &sqs.ReceiveMessageOutput{Messages: f.messages}, nil
}

func inspector(kind string, c Clients) *Inspector {
	return &Inspector{kind: kind, Now: func() time.Time { return now }, Connect: func(context.Context, ports.ConnectorConfig) (Clients, error) { return c, nil }}
}

func TestSQS(t *testing.T) {
	q := &fakeSQS{attrs: map[string]map[string]string{
		"orders": {"ApproximateNumberOfMessages": "4200", "QueueArn": "arn:aws:sqs:eu-west-1:1:orders",
			"RedrivePolicy": `{"deadLetterTargetArn":"arn:aws:sqs:eu-west-1:1:orders-failed","maxReceiveCount":5}`},
		"orders-failed": {"ApproximateNumberOfMessages": "7", "QueueArn": "arn:aws:sqs:eu-west-1:1:orders-failed"},
		"emails-dlq":    {"ApproximateNumberOfMessages": "0", "QueueArn": "arn:aws:sqs:eu-west-1:1:emails-dlq"},
	}, messages: []sqstypes.Message{{Body: awssdk.String(`{"order":42}`), MessageAttributes: map[string]sqstypes.MessageAttributeValue{
		"ErrorClass": {DataType: awssdk.String("String"), StringValue: awssdk.String("PaymentDeclined")}}}}}
	m := &fakeMetrics{values: map[string][]float64{"ApproximateAgeOfOldestMessage/orders": {420, 360}}}
	cc := ports.ConnectorConfig{ID: "c", Type: "sqs", Config: map[string]string{"queues": "orders*,emails-dlq"}}
	rs, err := inspector("sqs", Clients{Metrics: m, SQS: q}).Inspect(context.Background(), cc)
	require.NoError(t, err)
	require.Len(t, rs, 3, "the allow list skips 'ignored'")
	assert.Equal(t, ports.BusReading{Resource: "orders", Condition: ports.BusLag, Backlog: 4200, HasBacklog: true, OldestAge: 7 * time.Minute,
		Attrs: map[string]string{"queue": "orders", "queue_url": "https://sqs.eu-west-1.amazonaws.com/1/orders"}}, rs[0])
	assert.Equal(t, ports.BusDLQ, rs[1].Condition, "discovered from the source queue's RedrivePolicy")
	assert.Equal(t, "orders", rs[1].Attrs["source_queue"])
	assert.Empty(t, rs[1].Samples, "peek is off by default")
	assert.Equal(t, ports.BusDLQ, rs[2].Condition, "recognised by name")
	assert.Empty(t, q.received, "no ReceiveMessage without opt-in")

	cc.Config["peek"] = "true"
	rs, err = inspector("sqs", Clients{Metrics: m, SQS: q}).Inspect(context.Background(), cc)
	require.NoError(t, err)
	require.Len(t, rs[1].Samples, 1)
	assert.Equal(t, "PaymentDeclined", rs[1].Samples[0].ErrorClass)
	require.Len(t, q.received, 1, "an empty DLQ is not peeked")
	assert.Equal(t, int32(0), q.received[0].VisibilityTimeout, "a peek leaves messages visible")
}

func TestFailuresAndKinesis(t *testing.T) {
	m := &fakeMetrics{
		catalogue: map[string][]map[string]string{
			"AWS/SNS/NumberOfNotificationsFailed":            {{"TopicName": "orders"}, {"TopicName": "audit"}},
			"AWS/Events/FailedInvocations":                   {{"RuleName": "to-billing", "EventBusName": "payments"}},
			"AWS/Kinesis/GetRecords.IteratorAgeMilliseconds": {{"StreamName": "clicks"}},
			"AWS/Lambda/IteratorAge":                         {{"FunctionName": "clicks-sink"}, {"FunctionName": "clicks-sink", "Resource": "clicks-sink:live"}},
		},
		values: map[string][]float64{
			"NumberOfNotificationsFailed/orders":        {2, 1, 0, 0, 0, 9, 9}, // the 9s are older than 5 minutes
			"FailedInvocations/to-billing":              {1},
			"GetRecords.IteratorAgeMilliseconds/clicks": {90000},
			"IteratorAge/clicks-sink":                   {1500},
		},
	}
	rs, err := inspector("sns", Clients{Metrics: m}).Inspect(context.Background(), ports.ConnectorConfig{Config: map[string]string{}})
	require.NoError(t, err)
	require.Len(t, rs, 2)
	assert.Equal(t, "audit", rs[0].Resource)
	assert.Equal(t, int64(0), rs[0].Failures)
	assert.Equal(t, int64(3), rs[1].Failures, "summed over the last 5 minutes")

	rs, err = inspector("eventbridge", Clients{Metrics: m}).Inspect(context.Background(), ports.ConnectorConfig{Config: map[string]string{}})
	require.NoError(t, err)
	require.Len(t, rs, 1)
	assert.Equal(t, "payments/to-billing", rs[0].Resource)

	rs, err = inspector("kinesis", Clients{Metrics: m}).Inspect(context.Background(), ports.ConnectorConfig{Config: map[string]string{}})
	require.NoError(t, err)
	require.Len(t, rs, 2, "the alias series is skipped")
	assert.Equal(t, 90*time.Second, rs[0].OldestAge)
	assert.Equal(t, "lambda:clicks-sink", rs[1].Resource)
}
