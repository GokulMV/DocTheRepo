package awsbus

import (
	"context"
	"encoding/json"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	awssdk "github.com/aws/aws-sdk-go-v2/aws"
	cw "github.com/aws/aws-sdk-go-v2/service/cloudwatch"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	sqstypes "github.com/aws/aws-sdk-go-v2/service/sqs/types"

	"github.com/GokulMV/DocTheRepo/internal/adapters/signal/cloudwatch"
	"github.com/GokulMV/DocTheRepo/internal/ports"
)

// SQSAPI is the SQS read API used (ReceiveMessage only for the opt-in peek, never deleting).
type SQSAPI interface {
	ListQueues(ctx context.Context, in *sqs.ListQueuesInput, opts ...func(*sqs.Options)) (*sqs.ListQueuesOutput, error)
	GetQueueAttributes(ctx context.Context, in *sqs.GetQueueAttributesInput, opts ...func(*sqs.Options)) (*sqs.GetQueueAttributesOutput, error)
	ReceiveMessage(ctx context.Context, in *sqs.ReceiveMessageInput, opts ...func(*sqs.Options)) (*sqs.ReceiveMessageOutput, error)
}

// Clients are one connector's AWS clients.
type Clients struct {
	Metrics MetricsAPI
	SQS     SQSAPI
}

// Inspector implements ports.BusInspector for one AWS connector type.
type Inspector struct {
	kind    string // sqs, sns, eventbridge, kinesis
	Connect func(ctx context.Context, cc ports.ConnectorConfig) (Clients, error)
	Now     func() time.Time

	mu    sync.Mutex
	cache map[string]cachedClients
}

type cachedClients struct {
	key string
	c   Clients
}

// New returns the inspector for an AWS connector type using the AWS SDK.
func New(kind string) *Inspector { return &Inspector{kind: kind, Connect: connect} }

func connect(ctx context.Context, cc ports.ConnectorConfig) (Clients, error) {
	cfg, _, err := cloudwatch.LoadConfig(ctx, cc)
	if err != nil {
		return Clients{}, err
	}
	return Clients{Metrics: cw.NewFromConfig(cfg), SQS: sqs.NewFromConfig(cfg)}, nil
}

// Type implements ports.BusInspector.
func (i *Inspector) Type() string { return i.kind }

// Inspect implements ports.BusInspector.
func (i *Inspector) Inspect(ctx context.Context, cc ports.ConnectorConfig) ([]ports.BusReading, error) {
	c, err := i.clients(ctx, cc)
	if err != nil {
		return nil, err
	}
	now := time.Now()
	if i.Now != nil {
		now = i.Now()
	}
	switch i.kind {
	case "sqs":
		return inspectSQS(ctx, c, cc, now)
	case "sns":
		return failures(ctx, c.Metrics, now, "AWS/SNS", "NumberOfNotificationsFailed", "TopicName", "topic", cc.Config["topics"])
	case "eventbridge":
		return failures(ctx, c.Metrics, now, "AWS/Events", "FailedInvocations", "RuleName", "rule", cc.Config["rules"])
	case "kinesis":
		return inspectKinesis(ctx, c.Metrics, cc, now)
	}
	return nil, nil
}

func (i *Inspector) clients(ctx context.Context, cc ports.ConnectorConfig) (Clients, error) {
	key := cc.Credentials + "\x00" + cc.Config["region"] + "\x00" + cc.Config["role_arn"] + "\x00" + cc.Config["external_id"]
	i.mu.Lock()
	if c, ok := i.cache[cc.ID]; ok && c.key == key {
		i.mu.Unlock()
		return c.c, nil
	}
	i.mu.Unlock()
	c, err := i.Connect(ctx, cc)
	if err != nil {
		return c, err
	}
	i.mu.Lock()
	if i.cache == nil {
		i.cache = map[string]cachedClients{}
	}
	i.cache[cc.ID] = cachedClients{key: key, c: c}
	i.mu.Unlock()
	return c, nil
}

// defaultErrorAttrs are message attributes that commonly carry the failure class.
const defaultErrorAttrs = "x-exception-class,ErrorClass,error_class,exception,ErrorType,error_type"

func inspectSQS(ctx context.Context, c Clients, cc ports.ConnectorConfig, now time.Time) ([]ports.BusReading, error) {
	var urls []string
	in := &sqs.ListQueuesInput{MaxResults: awssdk.Int32(1000)}
	if p := strings.TrimSpace(cc.Config["queue_prefix"]); p != "" {
		in.QueueNamePrefix = awssdk.String(p)
	}
	for page := 0; page < 10; page++ {
		out, err := c.SQS.ListQueues(ctx, in)
		if err != nil {
			return nil, err
		}
		urls = append(urls, out.QueueUrls...)
		if out.NextToken == nil || *out.NextToken == "" {
			break
		}
		in.NextToken = out.NextToken
	}
	type queue struct {
		url, name, arn string
		depth          int64
	}
	var queues []queue
	dlqArns := map[string]string{} // DLQ ARN → a source queue that redrives to it
	for _, u := range urls {
		name := u[strings.LastIndex(u, "/")+1:]
		if !allowed(cc.Config["queues"], name) {
			continue
		}
		out, err := c.SQS.GetQueueAttributes(ctx, &sqs.GetQueueAttributesInput{QueueUrl: awssdk.String(u),
			AttributeNames: []sqstypes.QueueAttributeName{sqstypes.QueueAttributeNameApproximateNumberOfMessages,
				sqstypes.QueueAttributeNameQueueArn, sqstypes.QueueAttributeNameRedrivePolicy}})
		if err != nil {
			return nil, err
		}
		depth, _ := strconv.ParseInt(out.Attributes[string(sqstypes.QueueAttributeNameApproximateNumberOfMessages)], 10, 64)
		if rp := out.Attributes[string(sqstypes.QueueAttributeNameRedrivePolicy)]; rp != "" {
			var p struct {
				DeadLetterTargetArn string `json:"deadLetterTargetArn"`
			}
			if json.Unmarshal([]byte(rp), &p) == nil && p.DeadLetterTargetArn != "" {
				dlqArns[p.DeadLetterTargetArn] = name
			}
		}
		queues = append(queues, queue{url: u, name: name, arn: out.Attributes[string(sqstypes.QueueAttributeNameQueueArn)], depth: depth})
	}
	qs := make([]metricQuery, len(queues))
	for i, q := range queues {
		qs[i] = metricQuery{Namespace: "AWS/SQS", Metric: "ApproximateAgeOfOldestMessage", Stat: "Maximum", Dims: map[string]string{"QueueName": q.name}}
	}
	ages, err := latest(ctx, c.Metrics, qs, now)
	if err != nil {
		return nil, err
	}
	peek := cc.Config["peek"] == "true"
	var out []ports.BusReading
	for i, q := range queues {
		attrs := map[string]string{"queue": q.name, "queue_url": q.url}
		source, isDLQ := dlqArns[q.arn]
		if isDLQ || isDLQName(q.name) {
			if source != "" {
				attrs["source_queue"] = source
			}
			r := ports.BusReading{Resource: q.name, Condition: ports.BusDLQ, Backlog: q.depth, HasBacklog: true, Attrs: attrs}
			if peek && q.depth > 0 {
				r.Samples = peekSQS(ctx, c.SQS, q.url, cc.Config["error_attributes"])
			}
			out = append(out, r)
			continue
		}
		out = append(out, ports.BusReading{Resource: q.name, Condition: ports.BusLag, Backlog: q.depth, HasBacklog: true,
			OldestAge: time.Duration(ages[i]) * time.Second, Attrs: attrs})
	}
	return out, nil
}

// peekSQS reads up to 10 messages with a zero visibility timeout and never deletes them (opt-in: a receive
// increments ApproximateReceiveCount, which can move messages along a redrive policy).
func peekSQS(ctx context.Context, api SQSAPI, url, errorAttrs string) []ports.BusSample {
	out, err := api.ReceiveMessage(ctx, &sqs.ReceiveMessageInput{QueueUrl: awssdk.String(url), MaxNumberOfMessages: 10,
		VisibilityTimeout: 0, WaitTimeSeconds: 0, MessageAttributeNames: []string{"All"}})
	if err != nil {
		return nil
	}
	if errorAttrs == "" {
		errorAttrs = defaultErrorAttrs
	}
	var samples []ports.BusSample
	for _, m := range out.Messages {
		s := ports.BusSample{Body: awssdk.ToString(m.Body), Attrs: map[string]string{}}
		for k, v := range m.MessageAttributes {
			if v.StringValue != nil {
				s.Attrs[k] = *v.StringValue
			}
		}
		for _, k := range strings.Split(errorAttrs, ",") {
			if v := s.Attrs[strings.TrimSpace(k)]; v != "" && s.ErrorClass == "" {
				s.ErrorClass = v
			}
		}
		samples = append(samples, s)
	}
	return samples
}

// isDLQName recognises dead-letter queues by convention when no RedrivePolicy in scope points at them.
func isDLQName(name string) bool {
	n := strings.ToLower(strings.TrimSuffix(name, ".fifo"))
	return strings.HasSuffix(n, "-dlq") || strings.HasSuffix(n, "_dlq") || strings.HasSuffix(n, ".dlq") ||
		strings.HasSuffix(n, "-deadletter") || strings.HasSuffix(n, "-dead-letter")
}

// failures reads a per-resource failure count metric (SNS topics, EventBridge rules).
func failures(ctx context.Context, api MetricsAPI, now time.Time, ns, metric, dim, attr, allow string) ([]ports.BusReading, error) {
	sets, err := discover(ctx, api, ns, metric)
	if err != nil {
		return nil, err
	}
	var qs []metricQuery
	for _, d := range sets {
		if name := d[dim]; name != "" && allowed(allow, name) {
			qs = append(qs, metricQuery{Namespace: ns, Metric: metric, Stat: "Sum", Dims: d})
		}
	}
	vals, err := latest(ctx, api, qs, now)
	if err != nil {
		return nil, err
	}
	var out []ports.BusReading
	for i, q := range qs {
		resource := q.Dims[dim]
		if bus := q.Dims["EventBusName"]; bus != "" && bus != "default" {
			resource = bus + "/" + resource
		}
		out = append(out, ports.BusReading{Resource: resource, Condition: ports.BusFailures, Failures: int64(vals[i]),
			Attrs: map[string]string{attr: q.Dims[dim]}})
	}
	sort.Slice(out, func(a, b int) bool { return out[a].Resource < out[b].Resource })
	return out, nil
}

// inspectKinesis reads iterator age for streams (and, with lambda=true, Lambda stream consumers).
func inspectKinesis(ctx context.Context, api MetricsAPI, cc ports.ConnectorConfig, now time.Time) ([]ports.BusReading, error) {
	type src struct{ ns, metric, dim, prefix, attr string }
	srcs := []src{{"AWS/Kinesis", "GetRecords.IteratorAgeMilliseconds", "StreamName", "", "stream"}}
	if cc.Config["lambda"] != "false" {
		srcs = append(srcs, src{"AWS/Lambda", "IteratorAge", "FunctionName", "lambda:", "function"})
	}
	var qs []metricQuery
	var meta []src
	for _, s := range srcs {
		sets, err := discover(ctx, api, s.ns, s.metric)
		if err != nil {
			return nil, err
		}
		for _, d := range sets {
			name := d[s.dim]
			if name == "" || len(d) != 1 || (s.attr == "stream" && !allowed(cc.Config["streams"], name)) {
				continue // the per-resource series only (Lambda also reports per alias/version)
			}
			qs = append(qs, metricQuery{Namespace: s.ns, Metric: s.metric, Stat: "Maximum", Dims: d})
			meta = append(meta, s)
		}
	}
	vals, err := latest(ctx, api, qs, now)
	if err != nil {
		return nil, err
	}
	var out []ports.BusReading
	for i, q := range qs {
		v, ok := vals[i]
		if !ok {
			continue
		}
		name := q.Dims[meta[i].dim]
		out = append(out, ports.BusReading{Resource: meta[i].prefix + name, Condition: ports.BusLag,
			OldestAge: time.Duration(v) * time.Millisecond, Attrs: map[string]string{meta[i].attr: name}})
	}
	return out, nil
}
