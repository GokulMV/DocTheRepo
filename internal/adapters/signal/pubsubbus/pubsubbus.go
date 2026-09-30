// Package pubsubbus inspects Google Cloud Pub/Sub read-only (plan § 8.20): per-subscription backlog and
// oldest unacked message age from Cloud Monitoring, and dead-letter messages read through Hub-owned
// subscriptions on the dead-letter topics (created by the readonly-roles Terraform module). The Hub never
// pulls or acknowledges an application subscription.
package pubsubbus

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/GokulMV/DocTheRepo/internal/adapters/signal/pubsub"
	"github.com/GokulMV/DocTheRepo/internal/adapters/signal/sigutil"
	"github.com/GokulMV/DocTheRepo/internal/ports"
)

// Scopes: Monitoring read for metrics, Pub/Sub for the Hub-owned dead-letter subscriptions.
const (
	MonitoringScope = "https://www.googleapis.com/auth/monitoring.read"
	DefaultEndpoint = "https://monitoring.googleapis.com/v3/"
	// DefaultErrorAttrs are message attributes that commonly carry the failure class.
	DefaultErrorAttrs = "error_class,x-exception-class,exception,error_type"
	maxDLQPull        = 100
)

// Inspector implements ports.BusInspector and ports.BusCommitter.
type Inspector struct {
	// Client returns an authenticated client for Monitoring (default: connector credentials or ADC).
	Client   func(ctx context.Context, cc ports.ConnectorConfig) (*http.Client, error)
	Endpoint string
	// Puller opens a Hub-owned dead-letter subscription (default: the Pub/Sub REST client).
	Puller func(ctx context.Context, cc ports.ConnectorConfig, subscription string) (pubsub.Puller, error)
	Now    func() time.Time

	mu      sync.Mutex
	pending map[string][]pendingAck // connector → acks to send after the events are persisted
}

type pendingAck struct {
	p   pubsub.Puller
	ids []string
}

// New returns an inspector using Google credentials.
func New() *Inspector {
	return &Inspector{Endpoint: DefaultEndpoint,
		Client: func(ctx context.Context, cc ports.ConnectorConfig) (*http.Client, error) {
			return sigutil.GoogleClient(ctx, cc.Credentials, MonitoringScope)
		},
		Puller: func(ctx context.Context, cc ports.ConnectorConfig, sub string) (pubsub.Puller, error) {
			cfg := map[string]string{"subscription": sub, "project": cc.Config["project"]}
			return pubsub.NewClient(ctx, ports.ConnectorConfig{ID: cc.ID, Config: cfg, Credentials: cc.Credentials})
		}}
}

// Type implements ports.BusInspector.
func (*Inspector) Type() string { return "pubsub_bus" }

// Inspect implements ports.BusInspector.
func (i *Inspector) Inspect(ctx context.Context, cc ports.ConnectorConfig) ([]ports.BusReading, error) {
	project := strings.TrimSpace(cc.Config["project"])
	if project == "" {
		return nil, &ports.ValidationError{Code: "INVALID_CONFIG", Message: "project is required"}
	}
	hc, err := i.Client(ctx, cc)
	if err != nil {
		return nil, err
	}
	dlqSubs := list(cc.Config["dlq_subscriptions"])
	own := map[string]bool{}
	for _, s := range dlqSubs {
		own[s] = true
	}
	backlog, err := i.series(ctx, hc, project, "pubsub.googleapis.com/subscription/num_undelivered_messages")
	if err != nil {
		return nil, err
	}
	age, err := i.series(ctx, hc, project, "pubsub.googleapis.com/subscription/oldest_unacked_message_age")
	if err != nil {
		return nil, err
	}
	var out []ports.BusReading
	for _, sub := range sortedKeys(backlog, age) {
		if own[sub] || !allowed(cc.Config["subscriptions"], sub) {
			continue
		}
		b, hasB := backlog[sub]
		out = append(out, ports.BusReading{Resource: sub, Condition: ports.BusLag, Backlog: b, HasBacklog: hasB,
			OldestAge: time.Duration(age[sub]) * time.Second, Attrs: map[string]string{"subscription": sub, "gcp.project": project}})
	}

	errAttrs := cc.Config["error_attributes"]
	if errAttrs == "" {
		errAttrs = DefaultErrorAttrs
	}
	var acks []pendingAck
	for _, sub := range dlqSubs {
		p, err := i.Puller(ctx, cc, sub)
		if err != nil {
			return nil, err
		}
		msgs, err := p.Pull(ctx, maxDLQPull)
		if err != nil {
			return nil, fmt.Errorf("dead-letter subscription %s: %w", sub, err)
		}
		r := ports.BusReading{Resource: sub, Condition: ports.BusDLQ, Attrs: map[string]string{"subscription": sub, "gcp.project": project}}
		ids := make([]string, 0, len(msgs))
		for _, m := range msgs {
			ids = append(ids, m.AckID)
			s := ports.BusSample{Body: string(m.Message.Data), Attrs: map[string]string{}}
			for k, v := range m.Message.Attributes {
				s.Attrs[k] = v
			}
			for _, k := range strings.Split(errAttrs, ",") {
				if v := s.Attrs[strings.TrimSpace(k)]; v != "" && s.ErrorClass == "" {
					s.ErrorClass = v
				}
			}
			if src := s.Attrs["CloudPubSubDeadLetterSourceSubscription"]; src != "" {
				r.Attrs["source_subscription"] = src
			}
			r.Samples = append(r.Samples, s)
		}
		if len(ids) > 0 {
			acks = append(acks, pendingAck{p: p, ids: ids})
		}
		out = append(out, r)
	}
	i.mu.Lock()
	if i.pending == nil {
		i.pending = map[string][]pendingAck{}
	}
	i.pending[cc.ID] = acks // an earlier uncommitted read is redelivered after its ack deadline
	i.mu.Unlock()
	return out, nil
}

// Commit implements ports.BusCommitter: acknowledge the dead-letter messages the last Inspect read.
func (i *Inspector) Commit(ctx context.Context, cc ports.ConnectorConfig) error {
	i.mu.Lock()
	acks := i.pending[cc.ID]
	delete(i.pending, cc.ID)
	i.mu.Unlock()
	for _, a := range acks {
		if err := a.p.Ack(ctx, a.ids); err != nil {
			return err
		}
	}
	return nil
}

// series returns each subscription's newest value of a metric over the last 10 minutes.
func (i *Inspector) series(ctx context.Context, hc *http.Client, project, metric string) (map[string]int64, error) {
	now := time.Now()
	if i.Now != nil {
		now = i.Now()
	}
	q := url.Values{}
	q.Set("filter", fmt.Sprintf(`metric.type = "%s" AND resource.type = "pubsub_subscription"`, metric))
	q.Set("interval.startTime", now.Add(-10*time.Minute).UTC().Format(time.RFC3339))
	q.Set("interval.endTime", now.UTC().Format(time.RFC3339))
	q.Set("pageSize", "1000")
	out := map[string]int64{}
	for page := 0; page < 10; page++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, i.Endpoint+"projects/"+project+"/timeSeries?"+q.Encode(), nil)
		if err != nil {
			return nil, err
		}
		resp, err := hc.Do(req)
		if err != nil {
			return nil, err
		}
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
		resp.Body.Close()
		if resp.StatusCode/100 != 2 {
			if len(body) > 512 {
				body = body[:512]
			}
			return nil, fmt.Errorf("cloud monitoring: HTTP %d: %s", resp.StatusCode, body)
		}
		var res struct {
			TimeSeries []struct {
				Resource struct {
					Labels map[string]string `json:"labels"`
				} `json:"resource"`
				Points []struct {
					Value struct {
						Int64Value  string  `json:"int64Value"`
						DoubleValue float64 `json:"doubleValue"`
					} `json:"value"`
				} `json:"points"`
			} `json:"timeSeries"`
			NextPageToken string `json:"nextPageToken"`
		}
		if err := json.Unmarshal(body, &res); err != nil {
			return nil, fmt.Errorf("cloud monitoring: %w", err)
		}
		for _, ts := range res.TimeSeries {
			sub := ts.Resource.Labels["subscription_id"]
			if sub == "" || len(ts.Points) == 0 {
				continue
			}
			v := ts.Points[0].Value // newest first
			n, err := strconv.ParseInt(v.Int64Value, 10, 64)
			if err != nil {
				n = int64(v.DoubleValue)
			}
			out[sub] = n
		}
		if res.NextPageToken == "" {
			break
		}
		q.Set("pageToken", res.NextPageToken)
	}
	return out, nil
}

func list(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func allowed(allow, name string) bool {
	if strings.TrimSpace(allow) == "" {
		return true
	}
	for _, p := range list(allow) {
		if p == name || (strings.HasSuffix(p, "*") && strings.HasPrefix(name, strings.TrimSuffix(p, "*"))) {
			return true
		}
	}
	return false
}

func sortedKeys(ms ...map[string]int64) []string {
	seen := map[string]bool{}
	var out []string
	for _, m := range ms {
		for k := range m {
			if !seen[k] {
				seen[k] = true
				out = append(out, k)
			}
		}
	}
	sort.Strings(out)
	return out
}
