// Package queue is the Postgres-backed job queue and the worker pool that drains it (plan § 8.15).
package queue

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand/v2"
	"time"

	"github.com/GokulMV/DocTheRepo/internal/ports"
	"github.com/GokulMV/DocTheRepo/internal/store"
	"github.com/GokulMV/DocTheRepo/internal/store/gen"
)

// Queue enqueues, claims, and finishes jobs. Delivery is at-least-once: handlers must be idempotent.
type Queue struct {
	st          *store.Store
	maxAttempts int
	backoffBase time.Duration
	backoffMax  time.Duration
	now         func() time.Time
}

// Options configures a Queue.
type Options struct {
	MaxAttempts int
	BackoffBase time.Duration
	BackoffMax  time.Duration
}

// New returns a Queue on st.
func New(st *store.Store, o Options) *Queue {
	if o.MaxAttempts <= 0 {
		o.MaxAttempts = 5
	}
	if o.BackoffBase <= 0 {
		o.BackoffBase = time.Second
	}
	if o.BackoffMax < o.BackoffBase {
		o.BackoffMax = 5 * time.Minute
	}
	return &Queue{st: st, maxAttempts: o.MaxAttempts, backoffBase: o.BackoffBase, backoffMax: o.BackoffMax, now: time.Now}
}

// Enqueue inserts a job and wakes workers. If DedupeKey matches a live job, that job is returned instead
// and created is false.
func (q *Queue) Enqueue(ctx context.Context, nj ports.NewJob) (job ports.Job, created bool, err error) {
	if nj.Type == "" {
		return ports.Job{}, false, errors.New("enqueue: job type is required")
	}
	payload, err := json.Marshal(nj.Payload)
	if err != nil {
		return ports.Job{}, false, fmt.Errorf("enqueue: marshal payload: %w", err)
	}
	if nj.Payload == nil {
		payload = []byte("{}")
	}
	maxAttempts := nj.MaxAttempts
	if maxAttempts <= 0 {
		maxAttempts = q.maxAttempts
	}
	runAfter := nj.RunAfter
	if runAfter.IsZero() {
		runAfter = q.now()
	}
	corr := nj.CorrelationID
	if corr == "" {
		corr = ports.NewID()
	}
	row, err := q.st.Q.EnqueueJob(ctx, gen.EnqueueJobParams{
		ID: ports.NewID(), Type: string(nj.Type), RepoID: strPtr(nj.RepoID), SerialKey: strPtr(nj.SerialKey),
		DedupeKey: strPtr(nj.DedupeKey), Payload: payload, Priority: nj.Priority,
		MaxAttempts: int32(maxAttempts), RunAfter: runAfter, CorrelationID: corr,
		ReplayedFrom: strPtr(nj.ReplayedFrom),
	})
	if store.IsNoRows(err) && nj.DedupeKey != "" {
		// ON CONFLICT DO NOTHING returned no row: a live job with this key already exists.
		existing, gerr := q.st.Q.GetLiveJobByDedupeKey(ctx, &nj.DedupeKey)
		if gerr == nil {
			return toJob(existing), false, nil
		}
		if !store.IsNoRows(gerr) {
			return ports.Job{}, false, fmt.Errorf("enqueue: load deduplicated job: %w", gerr)
		}
		// The live job finished between the insert and the lookup; retry once.
		return q.Enqueue(ctx, nj)
	}
	if err != nil {
		return ports.Job{}, false, fmt.Errorf("enqueue %s: %w", nj.Type, err)
	}
	if err := q.st.Q.NotifyJobs(ctx, string(nj.Type)); err != nil {
		return toJob(row), true, fmt.Errorf("enqueue: notify workers: %w", err)
	}
	return toJob(row), true, nil
}

// Claim atomically takes the next runnable job of the given type, or returns (nil, nil) if none.
func (q *Queue) Claim(ctx context.Context, t ports.JobType, worker string, ttl time.Duration) (*ports.Job, error) {
	row, err := q.st.Q.ClaimJob(ctx, gen.ClaimJobParams{Worker: &worker, TtlSeconds: ttl.Seconds(), JobType: string(t)})
	if store.IsNoRows(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("claim %s: %w", t, err)
	}
	j := toJob(row)
	return &j, nil
}

// Heartbeat extends a lease. It returns false when the lease was lost (reclaimed by the sweeper).
func (q *Queue) Heartbeat(ctx context.Context, id, worker string, ttl time.Duration) (bool, error) {
	n, err := q.st.Q.HeartbeatJob(ctx, gen.HeartbeatJobParams{ID: id, Worker: &worker, TtlSeconds: ttl.Seconds()})
	if err != nil {
		return false, fmt.Errorf("heartbeat %s: %w", id, err)
	}
	return n == 1, nil
}

// ErrLeaseLost is returned when a worker tries to finish a job it no longer owns.
var ErrLeaseLost = errors.New("job lease lost: another worker owns this job now")

// Finish records a terminal status. Only the lease holder can finish a job.
func (q *Queue) Finish(ctx context.Context, id, worker string, status ports.JobStatus, result any, msg string) error {
	if !status.Terminal() {
		return fmt.Errorf("finish %s: status %q is not terminal", id, status)
	}
	var raw json.RawMessage
	if result != nil {
		b, err := json.Marshal(result)
		if err != nil {
			return fmt.Errorf("finish %s: marshal result: %w", id, err)
		}
		raw = b
	}
	n, err := q.st.Q.FinishJob(ctx, gen.FinishJobParams{
		ID: id, Worker: &worker, Status: gen.JobStatus(status), Result: raw, Error: truncate(msg, 4000),
	})
	if err != nil {
		return fmt.Errorf("finish %s: %w", id, err)
	}
	if n == 0 {
		return ErrLeaseLost
	}
	return nil
}

// Reschedule puts a claimed job back in the queue to run after the given time. refund returns the
// attempt consumed by the claim (used for upstream Retry-After and graceful shutdown).
func (q *Queue) Reschedule(ctx context.Context, id, worker string, runAfter time.Time, msg string, refund bool) error {
	r := int32(0)
	if refund {
		r = 1
	}
	n, err := q.st.Q.RescheduleJob(ctx, gen.RescheduleJobParams{
		ID: id, Worker: &worker, RunAfter: runAfter, Error: truncate(msg, 4000), RefundAttempts: r,
	})
	if err != nil {
		return fmt.Errorf("reschedule %s: %w", id, err)
	}
	if n == 0 {
		return ErrLeaseLost
	}
	return nil
}

// Backoff returns the delay before retry number attempt (1-based): base·2^(attempt-1) with ±20% jitter,
// capped at max.
func (q *Queue) Backoff(attempt int) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	d := q.backoffBase
	for i := 1; i < attempt && d < q.backoffMax; i++ {
		d *= 2
	}
	if d > q.backoffMax {
		d = q.backoffMax
	}
	jitter := 0.8 + 0.4*rand.Float64()
	return time.Duration(float64(d) * jitter)
}

// ReclaimExpired returns jobs whose worker stopped heartbeating to the queue (or to dead if out of attempts).
func (q *Queue) ReclaimExpired(ctx context.Context) ([]gen.ReclaimExpiredLeasesRow, error) {
	rows, err := q.st.Q.ReclaimExpiredLeases(ctx)
	if err != nil {
		return nil, fmt.Errorf("reclaim expired leases: %w", err)
	}
	return rows, nil
}

// Get loads one job.
func (q *Queue) Get(ctx context.Context, id string) (ports.Job, error) {
	row, err := q.st.Q.GetJob(ctx, id)
	if store.IsNoRows(err) {
		return ports.Job{}, ports.ErrNotFound
	}
	if err != nil {
		return ports.Job{}, fmt.Errorf("get job %s: %w", id, err)
	}
	return toJob(row), nil
}

// Filter narrows List.
type Filter struct {
	Status    ports.JobStatus
	Type      ports.JobType
	RepoID    string
	BeforeSeq int64
	Limit     int
}

// List returns jobs newest first, with the seq to pass as BeforeSeq for the next page (0 when done).
func (q *Queue) List(ctx context.Context, f Filter) ([]ports.Job, int64, error) {
	limit := f.Limit
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	p := gen.ListJobsParams{MaxRows: int32(limit + 1), RepoID: strPtr(f.RepoID)}
	if f.Status != "" {
		s := gen.JobStatus(f.Status)
		p.Status = &s
	}
	if f.Type != "" {
		t := string(f.Type)
		p.Type = &t
	}
	if f.BeforeSeq > 0 {
		p.BeforeSeq = &f.BeforeSeq
	}
	rows, err := q.st.Q.ListJobs(ctx, p)
	if err != nil {
		return nil, 0, fmt.Errorf("list jobs: %w", err)
	}
	var next int64
	if len(rows) > limit {
		rows = rows[:limit]
		next = rows[len(rows)-1].Seq
	}
	out := make([]ports.Job, len(rows))
	for i, r := range rows {
		out[i] = toJob(r)
	}
	return out, next, nil
}

// Depth returns queued-job counts by type.
func (q *Queue) Depth(ctx context.Context) (map[ports.JobType]int64, error) {
	rows, err := q.st.Q.QueueDepth(ctx)
	if err != nil {
		return nil, fmt.Errorf("queue depth: %w", err)
	}
	out := make(map[ports.JobType]int64, len(rows))
	for _, r := range rows {
		out[ports.JobType(r.Type)] = r.Depth
	}
	return out, nil
}

func toJob(r gen.Job) ports.Job {
	return ports.Job{
		ID: r.ID, Seq: r.Seq, Type: ports.JobType(r.Type), RepoID: deref(r.RepoID), SerialKey: deref(r.SerialKey),
		DedupeKey: deref(r.DedupeKey), Payload: r.Payload, Status: ports.JobStatus(r.Status), Priority: r.Priority,
		Attempts: int(r.Attempts), MaxAttempts: int(r.MaxAttempts), RunAfter: r.RunAfter, LockedBy: deref(r.LockedBy),
		LockedUntil: r.LockedUntil, CorrelationID: r.CorrelationID, Error: r.Error, Result: r.Result,
		ReplayedFrom: deref(r.ReplayedFrom), CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt,
	}
}

func strPtr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
