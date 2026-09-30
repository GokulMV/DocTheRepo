package ingest

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"log/slog"
	"sort"
	"sync"
	"time"

	"github.com/GokulMV/DocTheRepo/internal/ports"
)

// StreamRunner keeps one long-running consumer per enabled stream connector (Pub/Sub subscriptions; later
// Kinesis and Kafka inspectors): it starts consumers for new connectors, restarts them when their settings
// change, and stops them when a connector is disabled or deleted.
type StreamRunner struct {
	// List returns the enabled connectors this runner serves, with decrypted credentials.
	List func(ctx context.Context) ([]ports.ConnectorConfig, error)
	// Start builds a consumer; its run function returns when ctx ends. A build error is reported through
	// OnError and retried at the next sync.
	Start   func(ctx context.Context, cc ports.ConnectorConfig) (run func(ctx context.Context), err error)
	OnError func(cc ports.ConnectorConfig, err error)
	Log     *slog.Logger

	mu      sync.Mutex
	running map[string]*stream
}

type stream struct {
	hash   string
	cancel context.CancelFunc
	done   chan struct{}
}

// Run syncs every interval until ctx ends, then stops all consumers and waits for them.
func (r *StreamRunner) Run(ctx context.Context, every time.Duration) {
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		if err := r.Sync(ctx); err != nil && ctx.Err() == nil {
			r.log().Warn("list stream connectors failed", "err", err)
		}
		select {
		case <-ctx.Done():
			r.stopAll()
			return
		case <-t.C:
		}
	}
}

// Sync reconciles running consumers with the enabled connectors.
func (r *StreamRunner) Sync(ctx context.Context) error {
	ccs, err := r.List(ctx)
	if err != nil {
		return err
	}
	want := map[string]ports.ConnectorConfig{}
	for _, cc := range ccs {
		want[cc.ID] = cc
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.running == nil {
		r.running = map[string]*stream{}
	}
	for id, s := range r.running {
		if cc, ok := want[id]; !ok || settingsHash(cc) != s.hash {
			s.stop()
			delete(r.running, id)
		}
	}
	for id, cc := range want {
		if _, ok := r.running[id]; ok {
			continue
		}
		run, err := r.Start(ctx, cc)
		if err != nil {
			r.log().Warn("start stream consumer failed", "connector_id", id, "type", cc.Type, "err", err)
			if r.OnError != nil {
				r.OnError(cc, err)
			}
			continue
		}
		sctx, cancel := context.WithCancel(ctx)
		s := &stream{hash: settingsHash(cc), cancel: cancel, done: make(chan struct{})}
		go func() { defer close(s.done); run(sctx) }()
		r.running[id] = s
		r.log().Info("stream consumer started", "connector_id", id, "type", cc.Type)
	}
	return nil
}

// Running lists the connector IDs with a consumer (tests, status).
func (r *StreamRunner) Running() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]string, 0, len(r.running))
	for id := range r.running {
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}

func (r *StreamRunner) stopAll() {
	r.mu.Lock()
	defer r.mu.Unlock()
	for id, s := range r.running {
		s.stop()
		delete(r.running, id)
	}
}

func (s *stream) stop() {
	s.cancel()
	<-s.done
}

// settingsHash changes whenever a consumer must restart (type, config, credentials).
func settingsHash(cc ports.ConnectorConfig) string {
	b, _ := json.Marshal([]any{cc.Type, cc.Config, cc.Credentials})
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

func (r *StreamRunner) log() *slog.Logger {
	if r.Log != nil {
		return r.Log
	}
	return slog.Default()
}
