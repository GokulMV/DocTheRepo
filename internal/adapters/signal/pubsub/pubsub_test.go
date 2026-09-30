package pubsub

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/GokulMV/DocTheRepo/internal/core/aggregate"
	"github.com/GokulMV/DocTheRepo/internal/core/signals"
	"github.com/GokulMV/DocTheRepo/internal/ports"
)

var cc = ports.ConnectorConfig{ID: "ps1", Type: "pubsub", Config: map[string]string{"subscription": "projects/p/subscriptions/logs"}}

const cloudRunEntry = `{"insertId":"66e1f2a3000abc","logName":"projects/acme-prod/logs/run.googleapis.com%2Fstderr",
"resource":{"type":"cloud_run_revision","labels":{"service_name":"billing-api","revision_name":"billing-api-00042","project_id":"acme-prod","location":"europe-west1"}},
"timestamp":"2026-09-30T10:00:00.123Z","severity":"ERROR","labels":{"instanceId":"00abc"},
"textPayload":"Traceback (most recent call last):\n  File \"/app/billing/invoice.py\", line 88, in render\n    total = sum(lines) / count\nZeroDivisionError: division by zero"}`

const errorReportingEntry = `{"insertId":"ins-2","logName":"projects/acme-prod/logs/app","resource":{"type":"k8s_container","labels":{"container_name":"worker","namespace_name":"orders","project_id":"acme-prod"}},
"timestamp":"2026-09-30T10:01:00Z","severity":"ERROR","labels":{"k8s-pod/app":"orders-worker"},
"jsonPayload":{"message":"lock wait timeout exceeded","serviceContext":{"service":"orders"},"stack_trace":"java.sql.SQLTransientException: lock wait timeout exceeded\n\tat com.acme.orders.Repo.save(Repo.java:42)\n\tat com.acme.orders.Worker.run(Worker.java:17)"}}`

func TestLogEntries(t *testing.T) {
	evs := Events(Message{Data: []byte(cloudRunEntry), MessageID: "m1"}, cc)
	require.Len(t, evs, 1)
	ev := evs[0]
	assert.Equal(t, "66e1f2a3000abc", ev.ExternalID)
	assert.Equal(t, ports.SeverityError, ev.Severity)
	assert.Equal(t, "ZeroDivisionError", ev.ExceptionType)
	assert.Equal(t, "acme-prod", ev.Attrs["gcp.project"])
	assert.Equal(t, "billing-api-00042", ev.Attrs["gcp.resource.labels.revision_name"])
	noon := signals.Options{Now: func() time.Time { return time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC) }}
	require.NoError(t, signals.Prepare(&ev, noon))
	assert.Equal(t, "billing-api", ev.Service, "the Cloud Run service names the service")
	assert.Equal(t, time.Date(2026, 9, 30, 10, 0, 0, 123e6, time.UTC), ev.OccurredAt)

	evs = Events(Message{Data: []byte(errorReportingEntry)}, cc)
	require.Len(t, evs, 1)
	ev = evs[0]
	assert.Equal(t, "java.sql.SQLTransientException", ev.ExceptionType)
	assert.Equal(t, "lock wait timeout exceeded", ev.Message)
	require.NoError(t, signals.Prepare(&ev, noon))
	assert.Equal(t, "orders", ev.Service, "Error Reporting's serviceContext is explicit")
	assert.Equal(t, []string{"com.acme.orders.repo:save", "com.acme.orders.worker:run"}, signals.Frames(ev.Stack))

	info := `{"insertId":"i3","logName":"l","severity":"INFO","textPayload":"GET /healthz 200"}`
	assert.Empty(t, Events(Message{Data: []byte(info)}, cc), "below min_severity")
	warnInText := `{"insertId":"i4","logName":"l","severity":"DEFAULT","textPayload":"WARNING cache miss storm"}`
	assert.Len(t, Events(Message{Data: []byte(warnInText)}, cc), 1, "the message's own level counts")
	critical := `{"insertId":"i5","logName":"l","severity":"CRITICAL","textPayload":"out of memory"}`
	assert.Equal(t, ports.SeverityCritical, Events(Message{Data: []byte(critical)}, cc)[0].Severity, "the entry severity can raise")

	plain := Events(Message{Data: []byte("ERROR queue consumer died"), MessageID: "m9", Attributes: map[string]string{"k": "v"}}, cc)
	require.Len(t, plain, 1)
	assert.Equal(t, "m9", plain[0].ExternalID)
	assert.Equal(t, "v", plain[0].Attrs["pubsub.k"])
	assert.Empty(t, Events(Message{Data: []byte("  ")}, cc))
}

// fakeAPI is a minimal Pub/Sub REST server.
type fakeAPI struct {
	mu     sync.Mutex
	queue  []string
	acked  []string
	nacked []string
	paths  []string
}

func (f *fakeAPI) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.paths = append(f.paths, r.URL.Path)
	var in struct {
		MaxMessages int      `json:"maxMessages"`
		AckIDs      []string `json:"ackIds"`
	}
	b, _ := io.ReadAll(r.Body)
	_ = json.Unmarshal(b, &in)
	switch {
	case strings.HasSuffix(r.URL.Path, ":pull"):
		var out []map[string]any
		for i, d := range f.queue {
			if i >= in.MaxMessages {
				break
			}
			out = append(out, map[string]any{"ackId": "ack-" + d[:2], "message": map[string]any{
				"data": base64.StdEncoding.EncodeToString([]byte(d[2:])), "messageId": d[:2], "publishTime": "2026-09-30T10:00:00Z"}})
		}
		f.queue = f.queue[len(out):]
		_ = json.NewEncoder(w).Encode(map[string]any{"receivedMessages": out})
	case strings.HasSuffix(r.URL.Path, ":acknowledge"):
		f.acked = append(f.acked, in.AckIDs...)
		_, _ = w.Write([]byte(`{}`))
	case strings.HasSuffix(r.URL.Path, ":modifyAckDeadline"):
		f.nacked = append(f.nacked, in.AckIDs...)
		_, _ = w.Write([]byte(`{}`))
	default:
		http.Error(w, `{"error":{"code":404}}`, http.StatusNotFound)
	}
}

func TestClientAgainstREST(t *testing.T) {
	api := &fakeAPI{queue: []string{"01ERROR one", "02ERROR two", "03ERROR three"}}
	srv := httptest.NewServer(api)
	defer srv.Close()
	c := &Client{HTTP: srv.Client(), Endpoint: srv.URL + "/v1/", Subscription: "projects/p/subscriptions/logs"}
	got, err := c.Pull(context.Background(), 2)
	require.NoError(t, err)
	require.Len(t, got, 2)
	assert.Equal(t, "ERROR one", string(got[0].Message.Data))
	assert.Equal(t, "01", got[0].Message.MessageID)
	require.NoError(t, c.Ack(context.Background(), []string{got[0].AckID}))
	require.NoError(t, c.Nack(context.Background(), []string{got[1].AckID}))
	assert.Equal(t, []string{"ack-01"}, api.acked)
	assert.Equal(t, []string{"ack-02"}, api.nacked)
	assert.Equal(t, "/v1/projects/p/subscriptions/logs:pull", api.paths[0])

	gone := httptest.NewServer(http.NotFoundHandler())
	defer gone.Close()
	bad := &Client{HTTP: gone.Client(), Endpoint: gone.URL + "/v1/", Subscription: "x"}
	_, err = bad.Pull(context.Background(), 1)
	var se *StatusError
	require.ErrorAs(t, err, &se)
	assert.Equal(t, http.StatusNotFound, se.Status)
}

func TestNewClientConfig(t *testing.T) {
	t.Setenv("PUBSUB_EMULATOR_HOST", "localhost:8085")
	c, err := NewClient(context.Background(), ports.ConnectorConfig{Config: map[string]string{"subscription": "logs", "project": "acme"}})
	require.NoError(t, err)
	assert.Equal(t, "projects/acme/subscriptions/logs", c.Subscription)
	assert.Equal(t, "http://localhost:8085/v1/", c.Endpoint)
	var ve *ports.ValidationError
	_, err = NewClient(context.Background(), ports.ConnectorConfig{Config: map[string]string{"subscription": "logs"}})
	assert.ErrorAs(t, err, &ve)
	_, err = NewClient(context.Background(), ports.ConnectorConfig{Config: map[string]string{}})
	assert.ErrorAs(t, err, &ve)
	t.Setenv("PUBSUB_EMULATOR_HOST", "")
	_, err = NewClient(context.Background(), ports.ConnectorConfig{Credentials: "{not a key", Config: map[string]string{"subscription": "projects/p/subscriptions/s"}})
	assert.ErrorAs(t, err, &ve)
}

type fakeSink struct {
	mu         sync.Mutex
	errs       []error
	got        [][]ports.SignalEvent
	overloaded int // report overloaded this many times
}

func (s *fakeSink) IngestDurable(_ context.Context, evs []ports.SignalEvent) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.got = append(s.got, evs)
	if len(s.errs) > 0 {
		err := s.errs[0]
		s.errs = s.errs[1:]
		return err
	}
	return nil
}

func (s *fakeSink) Overloaded() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.overloaded > 0 {
		s.overloaded--
		return true
	}
	return false
}

func TestConsumerAcksOnlyAfterPersisting(t *testing.T) {
	api := &fakeAPI{queue: []string{"01ERROR one", "02INFO skipped", "03ERROR three"}}
	srv := httptest.NewServer(api)
	defer srv.Close()
	sink := &fakeSink{errs: []error{aggregate.ErrOverloaded}, overloaded: 2}
	var health []error
	ctx, cancel := context.WithCancel(context.Background())
	sleeps := 0
	c := &Consumer{Client: &Client{HTTP: srv.Client(), Endpoint: srv.URL + "/v1/", Subscription: "s"}, CC: cc, Sink: sink,
		OnHealth: func(err error) { health = append(health, err) },
		Sleep: func(_ context.Context, d time.Duration) {
			sleeps++
			api.mu.Lock()
			if len(api.nacked) > 0 && len(api.queue) == 0 { // Pub/Sub redelivers nacked messages
				api.queue = []string{"01ERROR one", "02INFO skipped", "03ERROR three"}
				api.nacked = api.nacked[:0]
			}
			api.mu.Unlock()
		}}
	done := make(chan struct{})
	go func() {
		defer close(done)
		c.Run(ctx)
	}()
	require.Eventually(t, func() bool {
		api.mu.Lock()
		defer api.mu.Unlock()
		return len(api.acked) == 3
	}, 5*time.Second, 5*time.Millisecond)
	cancel()
	<-done
	sink.mu.Lock()
	defer sink.mu.Unlock()
	require.GreaterOrEqual(t, len(sink.got), 2, "the first attempt failed (overloaded) and was redelivered")
	assert.Len(t, sink.got[1], 2, "the INFO line is dropped but still acked")
	assert.GreaterOrEqual(t, sleeps, 3, "paused while overloaded, then backed off after the failed batch")
	assert.Equal(t, []string{"ack-01", "ack-02", "ack-03"}, api.acked)
	assert.Empty(t, health, "never failing, so no health change")
}

func TestConsumerReportsPullFailures(t *testing.T) {
	var calls int
	var mu sync.Mutex
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		calls++
		n := calls
		mu.Unlock()
		if n <= 2 {
			http.Error(w, "denied", http.StatusForbidden)
			return
		}
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()
	var health []error
	var hmu sync.Mutex
	ctx, cancel := context.WithCancel(context.Background())
	c := &Consumer{Client: &Client{HTTP: srv.Client(), Endpoint: srv.URL + "/v1/", Subscription: "s"}, CC: cc, Sink: &fakeSink{},
		OnHealth: func(err error) { hmu.Lock(); health = append(health, err); hmu.Unlock() },
		Sleep:    func(context.Context, time.Duration) {}}
	done := make(chan struct{})
	go func() { defer close(done); c.Run(ctx) }()
	require.Eventually(t, func() bool { hmu.Lock(); defer hmu.Unlock(); return len(health) == 2 }, 5*time.Second, 5*time.Millisecond)
	cancel()
	<-done
	var se *StatusError
	assert.True(t, errors.As(health[0], &se) && se.Status == http.StatusForbidden, "failing once, reported once")
	assert.NoError(t, health[1], "recovered")
}
