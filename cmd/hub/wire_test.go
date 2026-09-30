package main

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/twmb/franz-go/pkg/kadm"
	"github.com/twmb/franz-go/pkg/kfake"
	"github.com/twmb/franz-go/pkg/kgo"

	"github.com/GokulMV/DocTheRepo/internal/adapters/secrets/localfile"
	"github.com/GokulMV/DocTheRepo/internal/api"
	"github.com/GokulMV/DocTheRepo/internal/config"
	"github.com/GokulMV/DocTheRepo/internal/core/knownissues"
	"github.com/GokulMV/DocTheRepo/internal/observability"
	"github.com/GokulMV/DocTheRepo/internal/ports"
	"github.com/GokulMV/DocTheRepo/internal/queue"
	"github.com/GokulMV/DocTheRepo/internal/secrets"
	"github.com/GokulMV/DocTheRepo/internal/store"
	"github.com/GokulMV/DocTheRepo/internal/store/storetest"
)

// TestWire builds the whole application against a real database, runs every scheduled task once, and
// checks the webhook route is mounted.
func TestWire(t *testing.T) {
	st := storetest.New(t)
	kek, err := localfile.Open(filepath.Join(t.TempDir(), "k"))
	require.NoError(t, err)
	cfg := config.Default()
	log := slog.New(slog.DiscardHandler)
	m := observability.NewMetrics()
	q := queue.New(st, queue.Options{})
	a, err := wire(context.Background(), cfg, st, secrets.NewBox(kek), q, log, m)
	require.NoError(t, err)
	a.registerHandlers(queue.NewPool(q, st.Pool, queue.PoolOptions{}, log, m))
	for _, task := range a.tasks() {
		assert.NoError(t, task.Fn(context.Background()), task.Name)
	}
	assert.Equal(t, "pgvector", a.index.Kind())

	srv := httptest.NewServer(api.NewRouter(api.Deps{Log: log, Metrics: m, Git: a.ingest}))
	defer srv.Close()
	resp, err := http.Post(srv.URL+"/hooks/github/"+ports.NewID(), "application/json", strings.NewReader("{}"))
	require.NoError(t, err)
	resp.Body.Close()
	assert.Equal(t, http.StatusNotFound, resp.StatusCode, "unknown connector")
	resp, err = http.Post(srv.URL+"/hooks/gitlab/not-a-uuid", "application/json", strings.NewReader("{}"))
	require.NoError(t, err)
	resp.Body.Close()
	assert.Equal(t, http.StatusNotFound, resp.StatusCode, "malformed connector IDs are unknown, not errors")

	cfg.Vector.Backend, cfg.Vector.QdrantURL = "qdrant", "http://127.0.0.1:1"
	b, err := wire(context.Background(), cfg, st, secrets.NewBox(kek), q, log, m)
	require.NoError(t, err)
	assert.Equal(t, "qdrant", b.index.Kind())
}

// TestSignalPipelineWired: events enter through the app's ingest service, are aggregated and flushed, and
// known-issue rules (loaded from the database) suppress matching ones.
func TestSignalPipelineWired(t *testing.T) {
	ctx := context.Background()
	st := storetest.New(t)
	kek, err := localfile.Open(filepath.Join(t.TempDir(), "k"))
	require.NoError(t, err)
	m := observability.NewMetrics()
	a, err := wire(ctx, config.Default(), st, secrets.NewBox(kek), queue.New(st, queue.Options{}), slog.New(slog.DiscardHandler), m)
	require.NoError(t, err)
	_, err = a.knownIssues.Create(ctx, store.KnownIssue{Title: "noisy health checks", Reason: "expected_noise", Enabled: true,
		Match: knownissues.Match{Services: []string{"lb"}, MessageRegex: "health check"}})
	require.NoError(t, err)
	require.NoError(t, a.signals.Reload(ctx))

	var evs []ports.SignalEvent
	for i := 0; i < 100; i++ {
		evs = append(evs, ports.SignalEvent{ConnectorID: ports.NewID(), ExternalID: "e" + string(rune('a'+i%26)) + strings.Repeat("x", i/26), Source: "sentry",
			Kind: ports.KindError, Service: "checkout", ExceptionType: "TimeoutError", Message: "order " + ports.NewID() + " timed out"})
	}
	evs = append(evs, ports.SignalEvent{Source: "alertmanager", Kind: ports.KindAlert, Service: "lb", Title: "health check failed", RuleID: "LBHealth"})
	require.NoError(t, a.signals.Ingest(ctx, evs))
	require.NoError(t, a.agg.Flush(ctx))

	var issues, occurrences, suppressed int64
	require.NoError(t, st.Pool.QueryRow(ctx, `SELECT count(*), sum(occurrences), sum(suppressed_count) FROM issues`).Scan(&issues, &occurrences, &suppressed))
	assert.Equal(t, int64(2), issues, "100 timeouts with different order IDs are one issue")
	assert.Equal(t, int64(100), occurrences)
	assert.Equal(t, int64(1), suppressed)
	assert.Equal(t, 1.0, testutil.ToFloat64(m.IssuesNew.WithLabelValues("new")), "the suppressed issue is not reported for decoding")
	assert.Equal(t, 100.0, testutil.ToFloat64(m.SignalEvents.WithLabelValues("sentry", "accepted")))
	assert.Equal(t, 101.0, testutil.ToFloat64(m.EventsFlushed))
}

// scriptedPoller emits one page per stream and fails on demand.
type scriptedPoller struct{ fail error }

func (scriptedPoller) Type() string { return "cloudwatch" }

func (p scriptedPoller) Poll(_ context.Context, _ ports.ConnectorConfig, cursors map[string]string,
	emit func(string, string, []ports.SignalEvent) error) error {
	if p.fail != nil {
		return p.fail
	}
	n := len(cursors["logs:/ecs/api"])
	ev := ports.SignalEvent{Source: "cloudwatch", Kind: ports.KindLogMatch, ExternalID: "ev-" + strings.Repeat("x", n),
		Message: "db timeout", Attrs: map[string]string{"service.derived": "api"}}
	return emit("logs:/ecs/api", cursors["logs:/ecs/api"]+"x", []ports.SignalEvent{ev})
}

func TestSignalPollJob(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	st := storetest.New(t)
	kek, err := localfile.Open(filepath.Join(t.TempDir(), "k"))
	require.NoError(t, err)
	q := queue.New(st, queue.Options{})
	a, err := wire(ctx, config.Default(), st, secrets.NewBox(kek), q, slog.New(slog.DiscardHandler), observability.NewMetrics())
	require.NoError(t, err)
	require.NoError(t, a.signals.Reload(ctx))
	go a.agg.Run(ctx)
	id, err := a.conns.Create(ctx, store.NewConnector{Type: "cloudwatch", Name: "AWS", Mode: "poll", PollSeconds: 60,
		Config: map[string]string{"region": "eu-west-1", "log_groups": "/ecs/api"}})
	require.NoError(t, err)
	_, err = a.conns.Create(ctx, store.NewConnector{Type: "cloudwatch", Name: "push only", Mode: "webhook"})
	require.NoError(t, err)
	a.polls.Pollers["cloudwatch"] = scriptedPoller{}

	n, err := a.polls.Enqueue(ctx)
	require.NoError(t, err)
	assert.Equal(t, 1, n, "only poll-mode connectors are polled")
	n, err = a.polls.Enqueue(ctx)
	require.NoError(t, err)
	assert.Equal(t, 0, n, "not due again within the interval")

	job := ports.Job{Type: ports.JobSignalBatch, Payload: []byte(`{"connector_id":"` + id + `"}`)}
	for i := 0; i < 2; i++ {
		out, err := a.polls.Handle(ctx, job)
		require.NoError(t, err)
		assert.Equal(t, map[string]int{"events": 1, "streams": 1}, out.Result)
	}
	cursors, err := a.conns.Cursors(ctx, id)
	require.NoError(t, err)
	assert.Equal(t, "xx", cursors["logs:/ecs/api"], "each committed page advances the cursor")
	var service string
	var occ int64
	require.NoError(t, st.Pool.QueryRow(ctx, `SELECT service, occurrences FROM issues`).Scan(&service, &occ), "persisted before the handler returns")
	assert.Equal(t, "api", service)
	assert.Equal(t, int64(2), occ)

	a.polls.Pollers["cloudwatch"] = scriptedPoller{fail: &ports.ValidationError{Code: "INVALID_CONFIG", Message: "region is required"}}
	_, err = a.polls.Handle(ctx, job)
	var perm *ports.PermanentError
	assert.ErrorAs(t, err, &perm, "misconfiguration is not retried")
	var health, lastErr string
	require.NoError(t, st.Pool.QueryRow(ctx, `SELECT health::text, last_error FROM connectors WHERE id = $1`, id).Scan(&health, &lastErr))
	assert.Equal(t, "failing", health)
	assert.Contains(t, lastErr, "region is required")

	out, err := a.polls.Handle(ctx, ports.Job{Payload: []byte(`{"connector_id":"` + ports.NewID() + `"}`)})
	require.NoError(t, err)
	assert.Equal(t, ports.JobAborted, out.Status, "a deleted connector ends its queued poll quietly")
}

// TestKafkaInspectionEndToEnd: a lagging consumer group opens an event-bus issue, catching up resolves
// it, and dead-letter growth opens an issue per error class, all through the scheduled poll job.
func TestKafkaInspectionEndToEnd(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	st := storetest.New(t)
	kek, err := localfile.Open(filepath.Join(t.TempDir(), "k"))
	require.NoError(t, err)
	a, err := wire(ctx, config.Default(), st, secrets.NewBox(kek), queue.New(st, queue.Options{}), slog.New(slog.DiscardHandler), observability.NewMetrics())
	require.NoError(t, err)
	require.NoError(t, a.signals.Reload(ctx))
	go a.agg.Run(ctx)

	cluster, err := kfake.NewCluster(kfake.NumBrokers(1), kfake.SeedTopics(1, "orders", "orders.dlq"))
	require.NoError(t, err)
	defer cluster.Close()
	cl, err := kgo.NewClient(kgo.SeedBrokers(cluster.ListenAddrs()...))
	require.NoError(t, err)
	defer cl.Close()
	produce := func(topic string, n int, class string) {
		var recs []*kgo.Record
		for i := 0; i < n; i++ {
			r := &kgo.Record{Topic: topic, Value: []byte("m")}
			if class != "" {
				r.Headers = []kgo.RecordHeader{{Key: "x-exception-class", Value: []byte(class)}}
			}
			recs = append(recs, r)
		}
		require.NoError(t, cl.ProduceSync(ctx, recs...).FirstErr())
	}
	commit := func(at int64) {
		offs := kadm.Offsets{}
		offs.Add(kadm.Offset{Topic: "orders", Partition: 0, At: at})
		_, err := kadm.NewClient(cl).CommitOffsets(ctx, "orders-consumer", offs)
		require.NoError(t, err)
	}
	produce("orders", 1500, "")
	produce("orders.dlq", 2, "Old")
	commit(100)

	id, err := a.conns.Create(ctx, store.NewConnector{Type: "kafka", Name: "Kafka", Mode: "poll",
		Config: map[string]string{"brokers": strings.Join(cluster.ListenAddrs(), ","), "sustain_seconds": "0", "resolve_seconds": "0"}})
	require.NoError(t, err)
	cc, err := a.conns.Get(ctx, id)
	require.NoError(t, err)
	assert.Equal(t, int64(30), cc.PollSeconds, "event platforms default to a 30 s poll")
	job := ports.Job{Type: ports.JobSignalBatch, Payload: []byte(`{"connector_id":"` + id + `"}`)}
	poll := func() {
		_, err := a.polls.Handle(ctx, job)
		require.NoError(t, err)
	}
	status := func(rule string) (string, int64) {
		var s string
		var n int64
		require.NoError(t, st.Pool.QueryRow(ctx, `SELECT status::text, occurrences FROM issues WHERE kind = 'event_bus' AND title LIKE $1`, "%"+rule+"%").Scan(&s, &n))
		return s, n
	}

	poll()
	s, n := status("falling behind")
	assert.Equal(t, "new", s, "lag 1,400 > 1,000")
	assert.Equal(t, int64(1), n)
	var dlqIssues int
	require.NoError(t, st.Pool.QueryRow(ctx, `SELECT count(*) FROM issues WHERE title LIKE '%dead-lettered%'`).Scan(&dlqIssues))
	assert.Equal(t, 0, dlqIssues, "existing DLQ depth is the baseline, not an incident")

	commit(1500)
	produce("orders.dlq", 3, "com.acme.PaymentDeclined")
	poll()
	s, _ = status("falling behind")
	assert.Equal(t, "resolved", s, "caught up: auto-resolved")
	var title string
	require.NoError(t, st.Pool.QueryRow(ctx, `SELECT count(*) FROM issues WHERE title LIKE '%dead-lettered%'`).Scan(&dlqIssues))
	assert.Equal(t, 1, dlqIssues, "only the new messages' class: the two older 'Old' records are not re-reported")
	require.NoError(t, st.Pool.QueryRow(ctx, `SELECT title FROM issues WHERE title LIKE '%dead-lettered%'`).Scan(&title))
	assert.Equal(t, "Kafka dead-letter topic orders.dlq received 3 new dead-lettered messages (com.acme.PaymentDeclined)", title,
		"the samples are the newest records, whose header names the class")

	produce("orders", 5000, "")
	poll()
	s, n = status("falling behind")
	assert.Equal(t, "regressed", s, "lag again: the resolved issue regresses")
	assert.Equal(t, int64(2), n)
}
