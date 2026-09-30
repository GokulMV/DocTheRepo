package busrules

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/GokulMV/DocTheRepo/internal/core/signals"
	"github.com/GokulMV/DocTheRepo/internal/ports"
)

var t0 = time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)

func lag(n int64) ports.BusReading {
	return ports.BusReading{Resource: "orders-consumer", Condition: ports.BusLag, Backlog: n, HasBacklog: true,
		Attrs: map[string]string{"consumer_group": "orders-consumer"}}
}

func TestLag_SustainedBreachOpensThenResolves(t *testing.T) {
	th := ThresholdsFrom(nil)
	st := State{}
	step := func(at time.Duration, r ports.BusReading) Result {
		res := Evaluate("kafka", "c1", r, st, th, t0.Add(at))
		st = ParseState(res.State.String()) // round-trips through persistence every time
		return res
	}
	assert.Empty(t, step(0, lag(500)).Events, "under the 1,000 floor")
	assert.Empty(t, step(30*time.Second, lag(5000)).Events, "breach starts; not yet sustained")
	assert.Empty(t, step(4*time.Minute, lag(6000)).Events)
	res := step(5*time.Minute+30*time.Second, lag(7000))
	require.Len(t, res.Events, 1, "sustained for 5 minutes")
	ev := res.Events[0]
	assert.Equal(t, ports.KindEventBus, ev.Kind)
	assert.Equal(t, "lag", ev.RuleID)
	assert.Contains(t, ev.Message, "backlog 7000 (threshold 1000)")
	require.NoError(t, signals.Prepare(&ev, signals.Options{Now: func() time.Time { return t0.Add(time.Hour) },
		ServiceRules: []signals.ServiceRule{{Service: "orders", Patterns: map[string]string{"consumer_group": "orders-*"}}}}))
	assert.Equal(t, st.Open, ev.Fingerprint, "the state remembers the issue it opened")
	assert.Equal(t, "orders", ev.Service, "service_map maps the consumer group to its service")

	assert.Len(t, step(6*time.Minute, lag(8000)).Events, 1, "every poll while breaching counts an occurrence")
	assert.Empty(t, step(7*time.Minute, lag(10)).Resolve, "below threshold starts the resolve clock")
	assert.Empty(t, step(10*time.Minute, lag(5000)).Resolve, "a relapse resets it")
	step(11*time.Minute, lag(10))
	assert.Empty(t, step(25*time.Minute, lag(10)).Resolve, "14 minutes below")
	res = step(26*time.Minute+time.Second, lag(10))
	assert.Equal(t, []string{ev.Fingerprint}, res.Resolve, "15 minutes below resolves")
	assert.Empty(t, st.Open)
	assert.Empty(t, step(27*time.Minute, lag(10)).Resolve, "resolves once")
}

func TestLag_BaselineAndOldestAge(t *testing.T) {
	th := ThresholdsFrom(map[string]string{"sustain_seconds": "0"})
	st := State{}
	// A week where this group routinely peaks at 4,000: 5 × p95 = 20,000.
	for h := 0; h < historyHours; h++ {
		st = Evaluate("kafka", "c1", lag(4000), st, th, t0.Add(time.Duration(h-historyHours)*time.Hour)).State
	}
	assert.LessOrEqual(t, len(st.Peaks), historyHours)
	assert.Empty(t, Evaluate("kafka", "c1", lag(15000), st, th, t0).Events, "normal for this group")
	res := Evaluate("kafka", "c1", lag(25000), st, th, t0)
	require.Len(t, res.Events, 1)
	assert.Equal(t, "20000", res.Events[0].Attrs["threshold"])

	old := ports.BusReading{Resource: "q", Condition: ports.BusLag, OldestAge: 6 * time.Minute}
	res = Evaluate("sqs", "c1", old, State{}, th, t0)
	require.Len(t, res.Events, 1, "an old message breaches even with a small backlog")
	assert.Equal(t, ports.SeverityError, res.Events[0].Severity)
	old.OldestAge = 30 * time.Minute
	assert.Equal(t, ports.SeverityCritical, Evaluate("sqs", "c1", old, State{}, th, t0).Events[0].Severity)
}

func TestDLQ_GrowthPerErrorClass(t *testing.T) {
	th := ThresholdsFrom(nil)
	r := ports.BusReading{Resource: "orders.dlq", Condition: ports.BusDLQ, Backlog: 10, HasBacklog: true}
	res := Evaluate("kafka", "c1", r, State{}, th, t0)
	assert.Empty(t, res.Events, "the first reading is the baseline")
	st := res.State

	r.Backlog = 13
	r.Samples = []ports.BusSample{
		{ErrorClass: "com.acme.PaymentDeclined", Reason: "card declined", Body: `{"order":1}`, Attrs: map[string]string{"topic": "orders"}},
		{ErrorClass: "com.acme.PaymentDeclined"},
		{ErrorClass: "java.net.SocketTimeoutException"},
	}
	res = Evaluate("kafka", "c1", r, st, th, t0.Add(time.Minute))
	require.Len(t, res.Events, 2, "one issue per error class")
	assert.Equal(t, "com.acme.PaymentDeclined", res.Events[0].ExceptionType)
	assert.Contains(t, res.Events[0].Title, "received 3 new dead-lettered messages (com.acme.PaymentDeclined)")
	assert.Contains(t, res.Events[0].Message, "reason: card declined")
	assert.Equal(t, "orders", res.Events[0].Attrs["dlq.topic"])
	a, b := res.Events[0], res.Events[1]
	signals.Fingerprint(&a)
	signals.Fingerprint(&b)
	assert.NotEqual(t, a.Fingerprint, b.Fingerprint, "different failure causes are different issues")

	st = res.State
	r.Backlog, r.Samples = 13, nil
	assert.Empty(t, Evaluate("kafka", "c1", r, st, th, t0.Add(2*time.Minute)).Events, "steady depth")
	r.Backlog = 2
	st = Evaluate("kafka", "c1", r, st, th, t0.Add(3*time.Minute)).State
	r.Backlog = 3
	res = Evaluate("kafka", "c1", r, st, th, t0.Add(4*time.Minute))
	require.Len(t, res.Events, 1, "growth after a drain is new trouble")
	assert.Equal(t, "", res.Events[0].ExceptionType, "no samples: unknown class")

	sampled := ports.BusReading{Resource: "dlq-sub", Condition: ports.BusDLQ, Samples: []ports.BusSample{{ErrorClass: "X"}}}
	assert.Len(t, Evaluate("pubsub", "c1", sampled, State{}, th, t0).Events, 1, "depth unknown: samples are new messages")
}

func TestFailuresAndThresholdConfig(t *testing.T) {
	r := ports.BusReading{Resource: "orders-topic", Condition: ports.BusFailures, Failures: 4}
	res := Evaluate("sns", "c1", r, State{}, ThresholdsFrom(nil), t0)
	require.Len(t, res.Events, 1)
	assert.Equal(t, "SNS topic orders-topic failed 4 deliveries", res.Events[0].Title)
	r.Failures = 0
	assert.Empty(t, Evaluate("sns", "c1", r, State{}, ThresholdsFrom(nil), t0).Events)

	th := ThresholdsFrom(map[string]string{"lag_min": "50", "lag_factor": "2.5", "oldest_age_seconds": "60", "sustain_seconds": "0", "resolve_seconds": "120"})
	assert.Equal(t, Thresholds{LagMin: 50, LagFactor: 2.5, OldestAge: time.Minute, ResolveAge: 2 * time.Minute}, th)
	assert.Equal(t, State{}, ParseState("not json"))
}

func TestDLQ_NewestSamplesTrimmedToGrowth(t *testing.T) {
	st := State{Depth: 2, HaveDepth: true}
	r := ports.BusReading{Resource: "d", Condition: ports.BusDLQ, Backlog: 3, HasBacklog: true, SamplesNewest: true,
		Samples: []ports.BusSample{{ErrorClass: "Old"}, {ErrorClass: "Old"}, {ErrorClass: "New"}}}
	res := Evaluate("kafka", "c1", r, st, ThresholdsFrom(nil), t0)
	require.Len(t, res.Events, 1)
	assert.Equal(t, "New", res.Events[0].ExceptionType)
	r.SamplesNewest = false
	assert.Len(t, Evaluate("sqs", "c1", r, st, ThresholdsFrom(nil), t0).Events, 2, "head-of-queue peeks are not trimmed")
}
