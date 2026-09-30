// Package rabbitmq inspects RabbitMQ through the management HTTP API with a "monitoring"-tagged user
// (plan § 8.20): per-queue ready backlog, and dead-letter queues (bound to an exchange that some queue
// names in x-dead-letter-exchange, or named *.dlq / *-dlq) by depth. An opt-in peek reads up to 10
// messages with ackmode reject_requeue_true, which returns them to the queue.
package rabbitmq

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/GokulMV/DocTheRepo/internal/ports"
)

// DefaultErrorHeaders are message headers that commonly carry the failure class.
const DefaultErrorHeaders = "x-exception-class,x-exception-type,exception,error_class"

// Inspector implements ports.BusInspector.
type Inspector struct {
	HTTP *http.Client
}

// New returns an inspector.
func New() *Inspector { return &Inspector{HTTP: &http.Client{Timeout: 30 * time.Second}} }

// Type implements ports.BusInspector.
func (*Inspector) Type() string { return "rabbitmq" }

type queue struct {
	Name          string         `json:"name"`
	VHost         string         `json:"vhost"`
	Ready         int64          `json:"messages_ready"`
	Unacked       int64          `json:"messages_unacknowledged"`
	Consumers     int            `json:"consumers"`
	Arguments     map[string]any `json:"arguments"`
	HeadMessageTS *int64         `json:"head_message_timestamp"`
}

type binding struct {
	Source          string `json:"source"`
	VHost           string `json:"vhost"`
	Destination     string `json:"destination"`
	DestinationType string `json:"destination_type"`
}

type credentials struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

// Inspect implements ports.BusInspector. Config: url (management API base, e.g. https://mq:15671),
// vhost (optional filter), queues (allow list), peek. Credentials: {"username","password"}.
func (i *Inspector) Inspect(ctx context.Context, cc ports.ConnectorConfig) ([]ports.BusReading, error) {
	base := strings.TrimRight(strings.TrimSpace(cc.Config["url"]), "/")
	if base == "" {
		return nil, &ports.ValidationError{Code: "INVALID_CONFIG", Message: "url (the management API) is required"}
	}
	var cred credentials
	if err := json.Unmarshal([]byte(cc.Credentials), &cred); err != nil || cred.Username == "" {
		return nil, &ports.ValidationError{Code: "INVALID_CREDENTIALS", Message: `credentials must be {"username","password"}`}
	}
	scope := ""
	if vh := cc.Config["vhost"]; vh != "" {
		scope = "/" + url.PathEscape(vh)
	}
	var queues []queue
	if err := i.get(ctx, base+"/api/queues"+scope+"?columns=name,vhost,messages_ready,messages_unacknowledged,consumers,arguments,head_message_timestamp", cred, &queues); err != nil {
		return nil, err
	}
	var bindings []binding
	if err := i.get(ctx, base+"/api/bindings"+scope, cred, &bindings); err != nil {
		return nil, err
	}
	// Exchanges some queue dead-letters into; queues bound from them are dead-letter queues.
	dlx := map[string]bool{}
	for _, q := range queues {
		if x, ok := q.Arguments["x-dead-letter-exchange"].(string); ok {
			dlx[q.VHost+"\x00"+x] = true
		}
	}
	dlq := map[string]bool{}
	for _, b := range bindings {
		if b.DestinationType == "queue" && dlx[b.VHost+"\x00"+b.Source] {
			dlq[b.VHost+"\x00"+b.Destination] = true
		}
	}
	sort.Slice(queues, func(a, b int) bool { return queues[a].VHost+queues[a].Name < queues[b].VHost+queues[b].Name })
	now := time.Now()
	var out []ports.BusReading
	for _, q := range queues {
		if !allowed(cc.Config["queues"], q.Name) || strings.HasPrefix(q.Name, "amq.") {
			continue
		}
		resource := q.Name
		if q.VHost != "/" && q.VHost != "" {
			resource = q.VHost + "/" + q.Name
		}
		attrs := map[string]string{"queue": q.Name, "vhost": q.VHost, "consumers": fmt.Sprint(q.Consumers)}
		if dlq[q.VHost+"\x00"+q.Name] || isDLQName(q.Name) {
			r := ports.BusReading{Resource: resource, Condition: ports.BusDLQ, Backlog: q.Ready + q.Unacked, HasBacklog: true, Attrs: attrs}
			if cc.Config["peek"] == "true" && q.Ready > 0 {
				r.Samples = i.peek(ctx, base, cred, q, cc.Config["error_headers"])
			}
			out = append(out, r)
			continue
		}
		r := ports.BusReading{Resource: resource, Condition: ports.BusLag, Backlog: q.Ready, HasBacklog: true, Attrs: attrs}
		if q.HeadMessageTS != nil && *q.HeadMessageTS > 0 {
			r.OldestAge = now.Sub(time.Unix(*q.HeadMessageTS, 0))
		}
		out = append(out, r)
	}
	return out, nil
}

// peek reads up to 10 messages and requeues them (ackmode reject_requeue_true).
func (i *Inspector) peek(ctx context.Context, base string, cred credentials, q queue, errHeaders string) []ports.BusSample {
	body, _ := json.Marshal(map[string]any{"count": 10, "ackmode": "reject_requeue_true", "encoding": "auto", "truncate": 8192})
	u := base + "/api/queues/" + url.PathEscape(q.VHost) + "/" + url.PathEscape(q.Name) + "/get"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u, bytes.NewReader(body))
	if err != nil {
		return nil
	}
	req.SetBasicAuth(cred.Username, cred.Password)
	req.Header.Set("Content-Type", "application/json")
	resp, err := i.HTTP.Do(req)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return nil
	}
	var msgs []struct {
		Payload    string `json:"payload"`
		RoutingKey string `json:"routing_key"`
		Properties struct {
			Headers map[string]any `json:"headers"`
		} `json:"properties"`
	}
	if json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(&msgs) != nil {
		return nil
	}
	if errHeaders == "" {
		errHeaders = DefaultErrorHeaders
	}
	var out []ports.BusSample
	for _, m := range msgs {
		s := ports.BusSample{Body: m.Payload, Attrs: map[string]string{"routing_key": m.RoutingKey}}
		for _, k := range strings.Split(errHeaders, ",") {
			if v, ok := m.Properties.Headers[strings.TrimSpace(k)].(string); ok && v != "" && s.ErrorClass == "" {
				s.ErrorClass = v
			}
		}
		// x-death: [{reason: rejected|expired|maxlen, queue: <source>, count: n}, …], newest first.
		if deaths, ok := m.Properties.Headers["x-death"].([]any); ok && len(deaths) > 0 {
			if d, ok := deaths[0].(map[string]any); ok {
				s.Reason, _ = d["reason"].(string)
				if src, ok := d["queue"].(string); ok {
					s.Attrs["source_queue"] = src
				}
			}
		}
		out = append(out, s)
	}
	return out
}

func (i *Inspector) get(ctx context.Context, u string, cred credentials, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return err
	}
	req.SetBasicAuth(cred.Username, cred.Password)
	resp, err := i.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return ports.Permanent(fmt.Errorf("rabbitmq management API: HTTP %d (the user needs the monitoring tag)", resp.StatusCode))
	}
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("rabbitmq management API: HTTP %d", resp.StatusCode)
	}
	return json.NewDecoder(io.LimitReader(resp.Body, 64<<20)).Decode(out)
}

func isDLQName(name string) bool {
	n := strings.ToLower(name)
	return strings.HasSuffix(n, ".dlq") || strings.HasSuffix(n, "-dlq") || strings.HasSuffix(n, "_dlq") ||
		strings.HasSuffix(n, ".dead-letter") || strings.HasSuffix(n, ".deadletter")
}

func allowed(allow, name string) bool {
	if strings.TrimSpace(allow) == "" {
		return true
	}
	for _, p := range strings.Split(allow, ",") {
		p = strings.TrimSpace(p)
		if p == name || (strings.HasSuffix(p, "*") && strings.HasPrefix(name, strings.TrimSuffix(p, "*"))) {
			return true
		}
	}
	return false
}
