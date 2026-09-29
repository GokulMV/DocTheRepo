package queue

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/GokulMV/DocTheRepo/internal/observability"
	"github.com/GokulMV/DocTheRepo/internal/ports"
	"github.com/GokulMV/DocTheRepo/internal/store"
	"github.com/GokulMV/DocTheRepo/internal/store/storetest"
)

var quiet = slog.New(slog.NewTextHandler(io.Discard, nil))

func newQueue(t *testing.T) (*Queue, *store.Store) {
	st := storetest.New(t)
	return New(st, Options{MaxAttempts: 3, BackoffBase: 10 * time.Millisecond, BackoffMax: 50 * time.Millisecond}), st
}

func TestQueue_EnqueueClaimFinish_RoundTrip(t *testing.T) {
	q, _ := newQueue(t)
	ctx := context.Background()
	job, created, err := q.Enqueue(ctx, ports.NewJob{Type: ports.JobCodePush, Payload: map[string]string{"sha": "abc"}})
	require.NoError(t, err)
	require.True(t, created)
	assert.Equal(t, ports.JobQueued, job.Status)
	assert.NotEmpty(t, job.CorrelationID)
	assert.JSONEq(t, `{"sha":"abc"}`, string(job.Payload))

	got, err := q.Claim(ctx, ports.JobCodePush, "w1", time.Minute)
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, job.ID, got.ID)
	assert.Equal(t, ports.JobProcessing, got.Status)
	assert.Equal(t, 1, got.Attempts)

	none, err := q.Claim(ctx, ports.JobCodePush, "w2", time.Minute)
	require.NoError(t, err)
	assert.Nil(t, none, "a claimed job is not claimable again")

	require.NoError(t, q.Finish(ctx, job.ID, "w1", ports.JobDone, map[string]int{"chunks": 3}, ""))
	final, err := q.Get(ctx, job.ID)
	require.NoError(t, err)
	assert.Equal(t, ports.JobDone, final.Status)
	assert.JSONEq(t, `{"chunks":3}`, string(final.Result))
	assert.Empty(t, final.LockedBy)
}

func TestQueue_ClaimIsTypeScoped(t *testing.T) {
	q, _ := newQueue(t)
	ctx := context.Background()
	_, _, err := q.Enqueue(ctx, ports.NewJob{Type: ports.JobDecodeIssue})
	require.NoError(t, err)
	j, err := q.Claim(ctx, ports.JobCodePush, "w", time.Minute)
	require.NoError(t, err)
	assert.Nil(t, j)
}

func TestQueue_Finish_RejectsNonOwnerAndNonTerminal(t *testing.T) {
	q, _ := newQueue(t)
	ctx := context.Background()
	job, _, _ := q.Enqueue(ctx, ports.NewJob{Type: ports.JobCodePush})
	_, err := q.Claim(ctx, ports.JobCodePush, "owner", time.Minute)
	require.NoError(t, err)
	assert.ErrorIs(t, q.Finish(ctx, job.ID, "intruder", ports.JobDone, nil, ""), ErrLeaseLost)
	assert.Error(t, q.Finish(ctx, job.ID, "owner", ports.JobQueued, nil, ""))
}

func TestQueue_DedupeKey_CollapsesLiveJobs(t *testing.T) {
	q, _ := newQueue(t)
	ctx := context.Background()
	a, created, err := q.Enqueue(ctx, ports.NewJob{Type: ports.JobKnowledgeSync, DedupeKey: "confluence:ENG"})
	require.NoError(t, err)
	require.True(t, created)
	b, created, err := q.Enqueue(ctx, ports.NewJob{Type: ports.JobKnowledgeSync, DedupeKey: "confluence:ENG"})
	require.NoError(t, err)
	assert.False(t, created)
	assert.Equal(t, a.ID, b.ID)

	_, err = q.Claim(ctx, ports.JobKnowledgeSync, "w", time.Minute)
	require.NoError(t, err)
	require.NoError(t, q.Finish(ctx, a.ID, "w", ports.JobDone, nil, ""))
	c, created, err := q.Enqueue(ctx, ports.NewJob{Type: ports.JobKnowledgeSync, DedupeKey: "confluence:ENG"})
	require.NoError(t, err)
	assert.True(t, created, "a finished job no longer deduplicates")
	assert.NotEqual(t, a.ID, c.ID)
}

func TestQueue_SerialKey_RunsInOrderOneAtATime(t *testing.T) {
	q, _ := newQueue(t)
	ctx := context.Background()
	first, _, _ := q.Enqueue(ctx, ports.NewJob{Type: ports.JobCodePush, SerialKey: "repo:A"})
	second, _, _ := q.Enqueue(ctx, ports.NewJob{Type: ports.JobCodePush, SerialKey: "repo:A"})
	other, _, _ := q.Enqueue(ctx, ports.NewJob{Type: ports.JobCodePush, SerialKey: "repo:B"})

	j1, err := q.Claim(ctx, ports.JobCodePush, "w1", time.Minute)
	require.NoError(t, err)
	assert.Equal(t, first.ID, j1.ID)
	j2, err := q.Claim(ctx, ports.JobCodePush, "w2", time.Minute)
	require.NoError(t, err)
	assert.Equal(t, other.ID, j2.ID, "a different repo runs in parallel; repo A's second push waits")
	j3, err := q.Claim(ctx, ports.JobCodePush, "w3", time.Minute)
	require.NoError(t, err)
	assert.Nil(t, j3)

	require.NoError(t, q.Finish(ctx, first.ID, "w1", ports.JobDone, nil, ""))
	j4, err := q.Claim(ctx, ports.JobCodePush, "w3", time.Minute)
	require.NoError(t, err)
	assert.Equal(t, second.ID, j4.ID)
}

func TestQueue_SerialKey_RetryKeepsOrder(t *testing.T) {
	q, _ := newQueue(t)
	ctx := context.Background()
	first, _, _ := q.Enqueue(ctx, ports.NewJob{Type: ports.JobCodePush, SerialKey: "repo:A"})
	_, _, _ = q.Enqueue(ctx, ports.NewJob{Type: ports.JobCodePush, SerialKey: "repo:A"})
	_, err := q.Claim(ctx, ports.JobCodePush, "w1", time.Minute)
	require.NoError(t, err)
	require.NoError(t, q.Reschedule(ctx, first.ID, "w1", time.Now().Add(time.Hour), "boom", false))
	j, err := q.Claim(ctx, ports.JobCodePush, "w2", time.Minute)
	require.NoError(t, err)
	assert.Nil(t, j, "a later push must not overtake an earlier one waiting to retry")
}

func TestQueue_Priority_BeatsAge(t *testing.T) {
	q, _ := newQueue(t)
	ctx := context.Background()
	_, _, _ = q.Enqueue(ctx, ports.NewJob{Type: ports.JobDecodeIssue})
	urgent, _, _ := q.Enqueue(ctx, ports.NewJob{Type: ports.JobDecodeIssue, Priority: 10})
	j, err := q.Claim(ctx, ports.JobDecodeIssue, "w", time.Minute)
	require.NoError(t, err)
	assert.Equal(t, urgent.ID, j.ID)
}

func TestQueue_RunAfter_DelaysClaim(t *testing.T) {
	q, _ := newQueue(t)
	ctx := context.Background()
	_, _, _ = q.Enqueue(ctx, ports.NewJob{Type: ports.JobReindex, RunAfter: time.Now().Add(time.Hour)})
	j, err := q.Claim(ctx, ports.JobReindex, "w", time.Minute)
	require.NoError(t, err)
	assert.Nil(t, j)
}

func TestQueue_ConcurrentClaims_AreExactlyOnce(t *testing.T) {
	q, _ := newQueue(t)
	ctx := context.Background()
	const n = 60
	for i := 0; i < n; i++ {
		_, _, err := q.Enqueue(ctx, ports.NewJob{Type: ports.JobSignalBatch})
		require.NoError(t, err)
	}
	var mu sync.Mutex
	seen := map[string]int{}
	var wg sync.WaitGroup
	for w := 0; w < 12; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			for {
				j, err := q.Claim(ctx, ports.JobSignalBatch, "w", time.Minute)
				if err != nil {
					t.Error(err)
					return
				}
				if j == nil {
					return
				}
				mu.Lock()
				seen[j.ID]++
				mu.Unlock()
			}
		}(w)
	}
	wg.Wait()
	assert.Len(t, seen, n)
	for id, c := range seen {
		assert.Equal(t, 1, c, "job %s claimed %d times", id, c)
	}
}

func TestQueue_ReclaimExpired_RequeuesThenDeadLetters(t *testing.T) {
	q, st := newQueue(t)
	ctx := context.Background()
	job, _, _ := q.Enqueue(ctx, ports.NewJob{Type: ports.JobCodePush, MaxAttempts: 2})
	for attempt := 1; attempt <= 2; attempt++ {
		_, err := q.Claim(ctx, ports.JobCodePush, "crashed", time.Minute)
		require.NoError(t, err)
		_, err = st.Pool.Exec(ctx, "UPDATE jobs SET locked_until = now() - interval '1 second' WHERE id = $1", job.ID)
		require.NoError(t, err)
		rows, err := q.ReclaimExpired(ctx)
		require.NoError(t, err)
		require.Len(t, rows, 1)
	}
	got, err := q.Get(ctx, job.ID)
	require.NoError(t, err)
	assert.Equal(t, ports.JobDead, got.Status)
	assert.Contains(t, got.Error, "lease expired")
}

func TestQueue_Heartbeat_ExtendsAndDetectsLoss(t *testing.T) {
	q, st := newQueue(t)
	ctx := context.Background()
	job, _, _ := q.Enqueue(ctx, ports.NewJob{Type: ports.JobCodePush})
	_, _ = q.Claim(ctx, ports.JobCodePush, "w", time.Minute)
	ok, err := q.Heartbeat(ctx, job.ID, "w", time.Hour)
	require.NoError(t, err)
	assert.True(t, ok)
	_, err = st.Pool.Exec(ctx, "UPDATE jobs SET locked_by = 'someone-else' WHERE id = $1", job.ID)
	require.NoError(t, err)
	ok, err = q.Heartbeat(ctx, job.ID, "w", time.Hour)
	require.NoError(t, err)
	assert.False(t, ok)
}

func TestQueue_ListAndDepth(t *testing.T) {
	q, _ := newQueue(t)
	ctx := context.Background()
	for i := 0; i < 5; i++ {
		_, _, _ = q.Enqueue(ctx, ports.NewJob{Type: ports.JobCodePush})
	}
	_, _, _ = q.Enqueue(ctx, ports.NewJob{Type: ports.JobDecodeIssue})
	d, err := q.Depth(ctx)
	require.NoError(t, err)
	assert.Equal(t, int64(5), d[ports.JobCodePush])
	assert.Equal(t, int64(1), d[ports.JobDecodeIssue])

	page1, next, err := q.List(ctx, Filter{Type: ports.JobCodePush, Limit: 3})
	require.NoError(t, err)
	assert.Len(t, page1, 3)
	require.NotZero(t, next)
	page2, next2, err := q.List(ctx, Filter{Type: ports.JobCodePush, Limit: 3, BeforeSeq: next})
	require.NoError(t, err)
	assert.Len(t, page2, 2)
	assert.Zero(t, next2)
	assert.Greater(t, page1[0].Seq, page2[0].Seq, "newest first")

	queued, _, err := q.List(ctx, Filter{Status: ports.JobQueued})
	require.NoError(t, err)
	assert.Len(t, queued, 6)

	_, err = q.Get(ctx, ports.NewID())
	assert.ErrorIs(t, err, ports.ErrNotFound)
}

func TestQueue_Backoff_GrowsAndCaps(t *testing.T) {
	q := New(nil, Options{BackoffBase: time.Second, BackoffMax: 10 * time.Second})
	for attempt, want := range map[int]time.Duration{1: time.Second, 2: 2 * time.Second, 3: 4 * time.Second, 10: 10 * time.Second} {
		got := q.Backoff(attempt)
		assert.InDelta(t, float64(want), float64(got), float64(want)*0.21, "attempt %d", attempt)
	}
}

// --- Pool tests ---

func runPool(t *testing.T, q *Queue, st *store.Store, conc int, reg func(p *Pool)) (*observability.Metrics, context.CancelFunc, chan struct{}) {
	t.Helper()
	m := observability.NewMetrics()
	p := NewPool(q, st.Pool, PoolOptions{
		WorkerID: "test", Concurrency: map[ports.JobType]int{ports.JobCodePush: conc, ports.JobDecodeIssue: conc},
		LeaseTTL: 3 * time.Second, PollInterval: 20 * time.Millisecond, ReclaimEvery: 100 * time.Millisecond,
	}, quiet, m)
	reg(p)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { p.Run(ctx); close(done) }()
	t.Cleanup(func() { cancel(); <-done })
	return m, cancel, done
}

func waitStatus(t *testing.T, q *Queue, id string, want ports.JobStatus) ports.Job {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		j, err := q.Get(context.Background(), id)
		require.NoError(t, err)
		if j.Status == want {
			return j
		}
		if time.Now().After(deadline) {
			t.Fatalf("job %s never reached %s (last: %s %q)", id, want, j.Status, j.Error)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestPool_ProcessesJobsAndRecordsOutcome(t *testing.T) {
	q, st := newQueue(t)
	runPool(t, q, st, 2, func(p *Pool) {
		p.Register(ports.JobCodePush, func(ctx context.Context, j ports.Job) (ports.Outcome, error) {
			assert.NotEmpty(t, observability.CorrelationID(ctx))
			return ports.Outcome{Result: map[string]bool{"ok": true}}, nil
		})
	})
	job, _, err := q.Enqueue(context.Background(), ports.NewJob{Type: ports.JobCodePush})
	require.NoError(t, err)
	got := waitStatus(t, q, job.ID, ports.JobDone)
	assert.JSONEq(t, `{"ok":true}`, string(got.Result))
}

func TestPool_TransientErrorRetriesThenSucceeds(t *testing.T) {
	q, st := newQueue(t)
	var calls atomic.Int32
	runPool(t, q, st, 1, func(p *Pool) {
		p.Register(ports.JobCodePush, func(ctx context.Context, j ports.Job) (ports.Outcome, error) {
			if calls.Add(1) < 3 {
				return ports.Outcome{}, ports.Transient(errors.New("vector db timeout"))
			}
			return ports.Outcome{}, nil
		})
	})
	job, _, _ := q.Enqueue(context.Background(), ports.NewJob{Type: ports.JobCodePush})
	got := waitStatus(t, q, job.ID, ports.JobDone)
	assert.Equal(t, 3, got.Attempts)
}

func TestPool_TransientExhausted_GoesDead(t *testing.T) {
	q, st := newQueue(t)
	runPool(t, q, st, 1, func(p *Pool) {
		p.Register(ports.JobCodePush, func(ctx context.Context, j ports.Job) (ports.Outcome, error) {
			return ports.Outcome{}, errors.New("untyped failure")
		})
	})
	job, _, _ := q.Enqueue(context.Background(), ports.NewJob{Type: ports.JobCodePush})
	got := waitStatus(t, q, job.ID, ports.JobDead)
	assert.Equal(t, 3, got.Attempts)
	assert.Contains(t, got.Error, "untyped failure")
}

func TestPool_PermanentError_FailsWithoutRetry(t *testing.T) {
	q, st := newQueue(t)
	var calls atomic.Int32
	runPool(t, q, st, 1, func(p *Pool) {
		p.Register(ports.JobCodePush, func(ctx context.Context, j ports.Job) (ports.Outcome, error) {
			calls.Add(1)
			return ports.Outcome{}, ports.Permanent(errors.New("bad credentials"))
		})
	})
	job, _, _ := q.Enqueue(context.Background(), ports.NewJob{Type: ports.JobCodePush})
	got := waitStatus(t, q, job.ID, ports.JobFailed)
	assert.Equal(t, int32(1), calls.Load())
	assert.Contains(t, got.Error, "bad credentials")
}

func TestPool_SpendBlocked_MarksJob(t *testing.T) {
	q, st := newQueue(t)
	runPool(t, q, st, 1, func(p *Pool) {
		p.Register(ports.JobCodePush, func(ctx context.Context, j ports.Job) (ports.Outcome, error) {
			return ports.Outcome{}, &ports.SpendBlockedError{Scope: "global/day", EstimatedTokens: 9e6, Reason: "over ceiling"}
		})
	})
	job, _, _ := q.Enqueue(context.Background(), ports.NewJob{Type: ports.JobCodePush})
	got := waitStatus(t, q, job.ID, ports.JobSpendBlocked)
	assert.Contains(t, got.Error, "global/day")
}

func TestPool_RetryAfter_DoesNotChargeAttempt(t *testing.T) {
	q, st := newQueue(t)
	var calls atomic.Int32
	runPool(t, q, st, 1, func(p *Pool) {
		p.Register(ports.JobCodePush, func(ctx context.Context, j ports.Job) (ports.Outcome, error) {
			if calls.Add(1) <= 4 { // more than max_attempts=3 rate-limit responses
				return ports.Outcome{}, ports.TransientAfter(errors.New("429"), 10*time.Millisecond)
			}
			return ports.Outcome{}, nil
		})
	})
	job, _, _ := q.Enqueue(context.Background(), ports.NewJob{Type: ports.JobCodePush})
	got := waitStatus(t, q, job.ID, ports.JobDone)
	assert.Equal(t, 1, got.Attempts, "rate-limit retries are free")
}

func TestPool_Panic_FailsJobAndKeepsWorking(t *testing.T) {
	q, st := newQueue(t)
	runPool(t, q, st, 1, func(p *Pool) {
		p.Register(ports.JobCodePush, func(ctx context.Context, j ports.Job) (ports.Outcome, error) {
			var in struct{ Panic bool }
			_ = json.Unmarshal(j.Payload, &in)
			if in.Panic {
				panic("nil map")
			}
			return ports.Outcome{}, nil
		})
	})
	bad, _, _ := q.Enqueue(context.Background(), ports.NewJob{Type: ports.JobCodePush, Payload: map[string]bool{"panic": true}})
	good, _, _ := q.Enqueue(context.Background(), ports.NewJob{Type: ports.JobCodePush})
	waitStatus(t, q, bad.ID, ports.JobFailed)
	waitStatus(t, q, good.ID, ports.JobDone)
}

func TestPool_ConcurrencyCapIsRespected(t *testing.T) {
	q, st := newQueue(t)
	var inflight, peak atomic.Int32
	runPool(t, q, st, 3, func(p *Pool) {
		p.Register(ports.JobDecodeIssue, func(ctx context.Context, j ports.Job) (ports.Outcome, error) {
			n := inflight.Add(1)
			for {
				old := peak.Load()
				if n <= old || peak.CompareAndSwap(old, n) {
					break
				}
			}
			time.Sleep(50 * time.Millisecond)
			inflight.Add(-1)
			return ports.Outcome{}, nil
		})
	})
	var ids []string
	for i := 0; i < 15; i++ {
		j, _, _ := q.Enqueue(context.Background(), ports.NewJob{Type: ports.JobDecodeIssue})
		ids = append(ids, j.ID)
	}
	for _, id := range ids {
		waitStatus(t, q, id, ports.JobDone)
	}
	assert.LessOrEqual(t, peak.Load(), int32(3))
	assert.GreaterOrEqual(t, peak.Load(), int32(2), "work actually ran in parallel")
}

func TestPool_Shutdown_RequeuesInterruptedJobWithoutCharge(t *testing.T) {
	q, st := newQueue(t)
	started := make(chan struct{})
	_, cancel, done := runPool(t, q, st, 1, func(p *Pool) {
		p.Register(ports.JobCodePush, func(ctx context.Context, j ports.Job) (ports.Outcome, error) {
			close(started)
			<-ctx.Done()
			return ports.Outcome{}, ctx.Err()
		})
	})
	job, _, _ := q.Enqueue(context.Background(), ports.NewJob{Type: ports.JobCodePush})
	<-started
	cancel()
	<-done
	got, err := q.Get(context.Background(), job.ID)
	require.NoError(t, err)
	assert.Equal(t, ports.JobQueued, got.Status)
	assert.Equal(t, 0, got.Attempts)
}

func TestPool_NotificationWakesIdleWorkerQuickly(t *testing.T) {
	q, st := newQueue(t)
	m := observability.NewMetrics()
	p := NewPool(q, st.Pool, PoolOptions{
		Concurrency: map[ports.JobType]int{ports.JobCodePush: 1}, LeaseTTL: 3 * time.Second,
		PollInterval: time.Hour, // polling effectively off: only LISTEN/NOTIFY can wake the worker
	}, quiet, m)
	p.Register(ports.JobCodePush, func(ctx context.Context, j ports.Job) (ports.Outcome, error) { return ports.Outcome{}, nil })
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { p.Run(ctx); close(done) }()
	defer func() { cancel(); <-done }()
	time.Sleep(300 * time.Millisecond) // let the worker go idle and the listener subscribe
	job, _, _ := q.Enqueue(context.Background(), ports.NewJob{Type: ports.JobCodePush})
	waitStatus(t, q, job.ID, ports.JobDone)
}
