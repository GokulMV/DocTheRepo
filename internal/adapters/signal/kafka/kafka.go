// Package kafka inspects Kafka (self-hosted, MSK, Confluent) read-only (plan § 8.20): consumer-group lag
// from committed and end offsets, and dead-letter topics (default names *.dlq, *-dlq, *.DLT) by depth,
// with the newest records sampled directly from partitions (no consumer group, so nothing is committed)
// for the error class in their headers. The Hub never joins or commits for an application group.
package kafka

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"path"
	"sort"
	"strings"
	"time"

	"github.com/twmb/franz-go/pkg/kadm"
	"github.com/twmb/franz-go/pkg/kgo"
	"github.com/twmb/franz-go/pkg/sasl/plain"
	"github.com/twmb/franz-go/pkg/sasl/scram"

	"github.com/GokulMV/DocTheRepo/internal/ports"
)

// Defaults for DLQ detection and error headers (config dlq_patterns, error_headers, reason_headers).
const (
	DefaultDLQPatterns   = "*.dlq,*-dlq,*_dlq,*.DLT,*-dlt,*.deadletter"
	DefaultErrorHeaders  = "x-exception-class,kafka_dlt-exception-fqcn,exception-class,x-error-class,error.class"
	DefaultReasonHeaders = "kafka_dlt-exception-message,x-exception-message,exception-message,x-error-message"
	samplesPerTopic      = 10
	sampleWait           = 3 * time.Second
)

// Inspector implements ports.BusInspector.
type Inspector struct {
	// Dial builds a client (default: from connector config); tests point it at a fake cluster.
	Dial func(cc ports.ConnectorConfig) (*kgo.Client, error)
}

// New returns an inspector dialling from connector config.
func New() *Inspector { return &Inspector{Dial: Dial} }

// Type implements ports.BusInspector.
func (*Inspector) Type() string { return "kafka" }

// Inspect implements ports.BusInspector.
func (i *Inspector) Inspect(ctx context.Context, cc ports.ConnectorConfig) ([]ports.BusReading, error) {
	cl, err := i.Dial(cc)
	if err != nil {
		return nil, err
	}
	defer cl.Close()
	adm := kadm.NewClient(cl)
	var out []ports.BusReading

	groups, err := adm.ListGroups(ctx)
	if err != nil {
		return nil, fmt.Errorf("list groups: %w", err)
	}
	var names []string
	for _, g := range groups.Sorted() {
		// An empty protocol type is a group with committed offsets and no live members: a stopped consumer,
		// exactly when lag matters most. Connect and other protocols are skipped.
		if (g.ProtocolType == "consumer" || g.ProtocolType == "") && allowed(cc.Config["groups"], g.Group) {
			names = append(names, g.Group)
		}
	}
	if len(names) > 0 {
		lags, err := adm.Lag(ctx, names...)
		if err != nil {
			return nil, fmt.Errorf("group lag: %w", err)
		}
		for _, name := range names {
			l, ok := lags[name]
			// Note: GroupLag.IsEmpty means "no live members", not "no lag data"; a memberless group is
			// still inspected (its consumers are down).
			if !ok || l.Error() != nil || len(l.Lag) == 0 {
				continue
			}
			var topics []string
			for t := range l.Lag {
				topics = append(topics, t)
			}
			sort.Strings(topics)
			out = append(out, ports.BusReading{Resource: name, Condition: ports.BusLag, Backlog: l.Lag.Total(), HasBacklog: true,
				Attrs: map[string]string{"consumer_group": name, "topics": strings.Join(topics, ","), "group_state": l.State,
					"members": fmt.Sprint(len(l.Members))}})
		}
	}

	topics, err := adm.ListTopics(ctx)
	if err != nil {
		return nil, fmt.Errorf("list topics: %w", err)
	}
	patterns := cc.Config["dlq_patterns"]
	if patterns == "" {
		patterns = DefaultDLQPatterns
	}
	var dlqs []string
	for _, t := range topics.Names() {
		if matches(patterns, t) {
			dlqs = append(dlqs, t)
		}
	}
	if len(dlqs) == 0 {
		return out, nil
	}
	starts, err := adm.ListStartOffsets(ctx, dlqs...)
	if err != nil {
		return nil, fmt.Errorf("start offsets: %w", err)
	}
	ends, err := adm.ListEndOffsets(ctx, dlqs...)
	if err != nil {
		return nil, fmt.Errorf("end offsets: %w", err)
	}
	for _, t := range dlqs {
		var depth int64
		from := map[int32]kgo.Offset{}
		want := 0
		ends.Each(func(o kadm.ListedOffset) {
			if o.Topic != t || o.Err != nil {
				return
			}
			start := int64(0)
			if s, ok := starts.Lookup(t, o.Partition); ok && s.Err == nil {
				start = s.Offset
			}
			depth += o.Offset - start
			if o.Offset > start {
				at := max(start, o.Offset-samplesPerTopic)
				from[o.Partition] = kgo.NewOffset().At(at)
				want += int(o.Offset - at)
			}
		})
		r := ports.BusReading{Resource: t, Condition: ports.BusDLQ, Backlog: depth, HasBacklog: true, Attrs: map[string]string{"topic": t}}
		if len(from) > 0 && cc.Config["sample"] != "false" {
			r.Samples, r.SamplesNewest = i.sample(ctx, cc, t, from, min(want, samplesPerTopic)), true
		}
		out = append(out, r)
	}
	return out, nil
}

// sample reads the newest records of a DLQ topic by direct partition assignment (no group, no commits).
func (i *Inspector) sample(ctx context.Context, cc ports.ConnectorConfig, topic string, from map[int32]kgo.Offset, want int) []ports.BusSample {
	cl, err := i.Dial(cc)
	if err != nil {
		return nil
	}
	defer cl.Close()
	cl.AddConsumePartitions(map[string]map[int32]kgo.Offset{topic: from})
	errHdrs, reasonHdrs := headerList(cc.Config["error_headers"], DefaultErrorHeaders), headerList(cc.Config["reason_headers"], DefaultReasonHeaders)
	var out []ports.BusSample
	wctx, cancel := context.WithTimeout(ctx, sampleWait)
	defer cancel()
	for len(out) < want && wctx.Err() == nil {
		fs := cl.PollRecords(wctx, want-len(out))
		fs.EachRecord(func(rec *kgo.Record) {
			if len(out) >= want {
				return
			}
			s := ports.BusSample{Body: string(rec.Value), Attrs: map[string]string{"partition": fmt.Sprint(rec.Partition), "offset": fmt.Sprint(rec.Offset),
				"timestamp": fmt.Sprint(rec.Timestamp.UnixMilli())}}
			hdr := map[string]string{}
			for _, h := range rec.Headers {
				hdr[strings.ToLower(h.Key)] = string(h.Value)
			}
			s.ErrorClass = first(hdr, errHdrs)
			s.Reason = first(hdr, reasonHdrs)
			if topicHdr := first(hdr, []string{"kafka_dlt-original-topic", "x-original-topic"}); topicHdr != "" {
				s.Attrs["original_topic"] = topicHdr
			}
			out = append(out, s)
		})
		if fs.Empty() {
			break
		}
	}
	// Oldest first across partitions (each partition already is), so the newest are at the end.
	sort.SliceStable(out, func(a, b int) bool { return out[a].Attrs["timestamp"] < out[b].Attrs["timestamp"] })
	return out
}

// credentials are SASL credentials in the connector's secret.
type credentials struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

// Dial builds a client from config brokers (comma-separated), tls, sasl_mechanism (plain, scram-sha-256,
// scram-sha-512) and credentials {"username","password"}.
func Dial(cc ports.ConnectorConfig) (*kgo.Client, error) {
	var brokers []string
	for _, b := range strings.Split(cc.Config["brokers"], ",") {
		if b = strings.TrimSpace(b); b != "" {
			brokers = append(brokers, b)
		}
	}
	if len(brokers) == 0 {
		return nil, &ports.ValidationError{Code: "INVALID_CONFIG", Message: "brokers is required"}
	}
	opts := []kgo.Opt{kgo.SeedBrokers(brokers...), kgo.ClientID("doctherepo-hub"), kgo.RequestTimeoutOverhead(10 * time.Second)}
	if cc.Config["tls"] == "true" {
		opts = append(opts, kgo.DialTLSConfig(&tls.Config{MinVersion: tls.VersionTLS12}))
	}
	if mech := strings.ToLower(cc.Config["sasl_mechanism"]); mech != "" {
		var c credentials
		if err := json.Unmarshal([]byte(cc.Credentials), &c); err != nil || c.Username == "" {
			return nil, &ports.ValidationError{Code: "INVALID_CREDENTIALS", Message: `credentials must be {"username","password"} for SASL`}
		}
		switch mech {
		case "plain":
			opts = append(opts, kgo.SASL(plain.Auth{User: c.Username, Pass: c.Password}.AsMechanism()))
		case "scram-sha-256":
			opts = append(opts, kgo.SASL(scram.Auth{User: c.Username, Pass: c.Password}.AsSha256Mechanism()))
		case "scram-sha-512":
			opts = append(opts, kgo.SASL(scram.Auth{User: c.Username, Pass: c.Password}.AsSha512Mechanism()))
		default:
			return nil, &ports.ValidationError{Code: "INVALID_CONFIG", Message: "sasl_mechanism must be plain, scram-sha-256, or scram-sha-512"}
		}
	}
	return kgo.NewClient(opts...)
}

func matches(patterns, name string) bool {
	for _, p := range strings.Split(patterns, ",") {
		if ok, _ := path.Match(strings.TrimSpace(p), name); ok {
			return true
		}
	}
	return false
}

func allowed(list, name string) bool {
	return strings.TrimSpace(list) == "" || matches(list, name)
}

func headerList(cfg, def string) []string {
	if cfg == "" {
		cfg = def
	}
	var out []string
	for _, h := range strings.Split(cfg, ",") {
		if h = strings.ToLower(strings.TrimSpace(h)); h != "" {
			out = append(out, h)
		}
	}
	return out
}

func first(hdr map[string]string, keys []string) string {
	for _, k := range keys {
		if v := hdr[k]; v != "" {
			return v
		}
	}
	return ""
}
