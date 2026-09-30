package pubsub

import (
	"context"
	"errors"
	"log/slog"
	"strconv"
	"time"

	"github.com/GokulMV/DocTheRepo/internal/core/aggregate"
	"github.com/GokulMV/DocTheRepo/internal/ports"
)

// Puller is the subscription API the consumer needs (Client, or a fake in tests).
type Puller interface {
	Pull(ctx context.Context, max int) ([]Received, error)
	Ack(ctx context.Context, ackIDs []string) error
	Nack(ctx context.Context, ackIDs []string) error
}

// Sink persists events before the consumer acknowledges them.
type Sink interface {
	IngestDurable(ctx context.Context, events []ports.SignalEvent) error
	Overloaded() bool
}

// Consumer pulls one subscription until its context ends. Several Hub workers may run a consumer for the
// same subscription: Pub/Sub spreads messages across them.
type Consumer struct {
	Client Puller
	CC     ports.ConnectorConfig
	Sink   Sink
	Log    *slog.Logger
	// OnHealth reports a change between working (nil) and failing; optional.
	OnHealth func(err error)
	// Sleep waits between retries (tests shorten it).
	Sleep func(ctx context.Context, d time.Duration)
}

// maxMessages reads config max_messages (default 500, at most 1000: the API's cap).
func (c *Consumer) maxMessages() int {
	n, err := strconv.Atoi(c.CC.Config["max_messages"])
	if err != nil || n <= 0 {
		return 500
	}
	return min(n, 1000)
}

// Run is the pull loop: pull, map, persist, then ack. When persistence fails the batch is nacked for
// redelivery; messages that are not problems (or not parseable) are acked so they never loop.
func (c *Consumer) Run(ctx context.Context) {
	backoff := time.Second
	var failing error
	health := func(err error) {
		if c.OnHealth != nil && (err == nil) != (failing == nil) {
			c.OnHealth(err)
		}
		failing = err
	}
	for ctx.Err() == nil {
		if c.Sink.Overloaded() {
			c.sleep(ctx, time.Second) // leave messages in the subscription until the pipeline catches up
			continue
		}
		msgs, err := c.Client.Pull(ctx, c.maxMessages())
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			c.log().Warn("pubsub pull failed", "connector_id", c.CC.ID, "err", err, "retry_in", backoff)
			health(err)
			c.sleep(ctx, backoff)
			backoff = min(backoff*2, time.Minute)
			continue
		}
		backoff = time.Second
		if len(msgs) == 0 {
			health(nil)
			continue
		}
		ids := make([]string, len(msgs))
		var events []ports.SignalEvent
		for i, m := range msgs {
			ids[i] = m.AckID
			events = append(events, Events(m.Message, c.CC)...)
		}
		if err := c.Sink.IngestDurable(ctx, events); err != nil {
			if nerr := c.Client.Nack(context.WithoutCancel(ctx), ids); nerr != nil {
				c.log().Warn("pubsub nack failed; messages redeliver after the ack deadline", "connector_id", c.CC.ID, "err", nerr)
			}
			if ctx.Err() != nil {
				return
			}
			wait := 2 * time.Second
			if !errors.Is(err, aggregate.ErrOverloaded) {
				c.log().Warn("pubsub batch not persisted; redelivering", "connector_id", c.CC.ID, "messages", len(msgs), "err", err)
				wait = 5 * time.Second
			}
			c.sleep(ctx, wait)
			continue
		}
		// A failed ack means redelivery, which the pipeline recognises by insertId: log, don't retry.
		if err := c.Client.Ack(context.WithoutCancel(ctx), ids); err != nil {
			c.log().Warn("pubsub ack failed", "connector_id", c.CC.ID, "err", err)
		}
		health(nil)
	}
}

func (c *Consumer) sleep(ctx context.Context, d time.Duration) {
	if c.Sleep != nil {
		c.Sleep(ctx, d)
		return
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
	case <-t.C:
	}
}

func (c *Consumer) log() *slog.Logger {
	if c.Log != nil {
		return c.Log
	}
	return slog.Default()
}
