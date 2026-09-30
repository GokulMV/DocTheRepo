package pubsub

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/GokulMV/DocTheRepo/internal/adapters/signal/sigutil"
	"github.com/GokulMV/DocTheRepo/internal/ports"
)

// Scope is the OAuth scope for pull and acknowledge.
const Scope = "https://www.googleapis.com/auth/pubsub"

// DefaultEndpoint is the Pub/Sub REST API; PUBSUB_EMULATOR_HOST overrides it.
const DefaultEndpoint = "https://pubsub.googleapis.com/v1/"

// Client pulls and acknowledges messages on one subscription.
type Client struct {
	HTTP         *http.Client
	Endpoint     string // ends with "/"
	Subscription string // projects/<p>/subscriptions/<s>
}

// NewClient builds a client from a connector: config subscription ("projects/p/subscriptions/s", or a short
// name with config project), credentials a service-account key JSON or empty for Application Default
// Credentials (Workload Identity on GKE and Cloud Run).
func NewClient(ctx context.Context, cc ports.ConnectorConfig) (*Client, error) {
	sub := strings.TrimSpace(cc.Config["subscription"])
	if sub == "" {
		return nil, &ports.ValidationError{Code: "INVALID_CONFIG", Message: "subscription is required"}
	}
	if !strings.HasPrefix(sub, "projects/") {
		p := strings.TrimSpace(cc.Config["project"])
		if p == "" {
			return nil, &ports.ValidationError{Code: "INVALID_CONFIG", Message: "use projects/<project>/subscriptions/<name> or set project"}
		}
		sub = "projects/" + p + "/subscriptions/" + sub
	}
	c := &Client{Endpoint: DefaultEndpoint, Subscription: sub}
	if host := os.Getenv("PUBSUB_EMULATOR_HOST"); host != "" {
		c.Endpoint, c.HTTP = "http://"+host+"/v1/", &http.Client{Timeout: 90 * time.Second}
		return c, nil
	}
	hc, err := sigutil.GoogleClient(ctx, cc.Credentials, Scope)
	if err != nil {
		return nil, err
	}
	c.HTTP = hc
	return c, nil
}

// Received is a pulled message with its ack ID.
type Received struct {
	AckID   string
	Message Message
}

// Pull waits for up to max messages (the server returns early when some are available).
func (c *Client) Pull(ctx context.Context, max int) ([]Received, error) {
	var out struct {
		ReceivedMessages []struct {
			AckID   string `json:"ackId"`
			Message struct {
				Data        string            `json:"data"`
				Attributes  map[string]string `json:"attributes"`
				MessageID   string            `json:"messageId"`
				PublishTime time.Time         `json:"publishTime"`
			} `json:"message"`
		} `json:"receivedMessages"`
	}
	if err := c.call(ctx, ":pull", map[string]any{"maxMessages": max}, &out); err != nil {
		return nil, err
	}
	res := make([]Received, 0, len(out.ReceivedMessages))
	for _, r := range out.ReceivedMessages {
		data, err := base64.StdEncoding.DecodeString(r.Message.Data)
		if err != nil {
			data = []byte(r.Message.Data)
		}
		res = append(res, Received{AckID: r.AckID, Message: Message{Data: data, Attributes: r.Message.Attributes,
			MessageID: r.Message.MessageID, PublishTime: r.Message.PublishTime}})
	}
	return res, nil
}

// Ack acknowledges messages (they are not redelivered).
func (c *Client) Ack(ctx context.Context, ackIDs []string) error {
	if len(ackIDs) == 0 {
		return nil
	}
	return c.call(ctx, ":acknowledge", map[string]any{"ackIds": ackIDs}, nil)
}

// Nack returns messages for immediate redelivery.
func (c *Client) Nack(ctx context.Context, ackIDs []string) error {
	if len(ackIDs) == 0 {
		return nil
	}
	return c.call(ctx, ":modifyAckDeadline", map[string]any{"ackIds": ackIDs, "ackDeadlineSeconds": 0}, nil)
}

// StatusError is a non-2xx answer from the API.
type StatusError struct {
	Status int
	Body   string
}

func (e *StatusError) Error() string { return fmt.Sprintf("pubsub: HTTP %d: %s", e.Status, e.Body) }

func (c *Client) call(ctx context.Context, verb string, in, out any) error {
	b, _ := json.Marshal(in)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.Endpoint+c.Subscription+verb, bytes.NewReader(b))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<20))
	if resp.StatusCode/100 != 2 {
		if len(body) > 512 {
			body = body[:512]
		}
		return &StatusError{Status: resp.StatusCode, Body: string(body)}
	}
	if out == nil {
		return nil
	}
	return json.Unmarshal(body, out)
}
