package queue

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"runtime/debug"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/GokulMV/DocTheRepo/internal/observability"
	"github.com/GokulMV/DocTheRepo/internal/ports"
)

// Handler processes one job. It returns an Outcome on success; on failure it returns a typed error from
// package ports (TransientError is retried with backoff, PermanentError fails the job, SpendBlockedError
// marks it spend_blocked). Untyped errors are treated as transient so a bug surfaces as a dead job after
// max attempts rather than silently disappearing.
type Handler func(ctx context.Context, job ports.Job) (ports.Outcome, error)

// PoolOptions configures a worker pool.
type PoolOptions struct {
	WorkerID     string
	Concurrency  map[ports.JobType]int
	LeaseTTL     time.Duration
	PollInterval time.Duration
	// ReclaimEvery sets how often this pool sweeps expired leases; 0 disables (the scheduler role does it).
	ReclaimEvery time.Duration
}

// Pool runs handlers for registered job types with per-type concurrency caps.
type Pool struct {
	q        *Queue
	pool     *pgxpool.Pool
	opts     PoolOptions
	log      *slog.Logger
	metrics  *observability.Metrics
	handlers map[ports.JobType]Handler
	wake     map[ports.JobType]chan struct{}
	wg       sync.WaitGroup
}

// NewPool builds a pool. pgPool is used for the LISTEN connection that wakes idle workers.
func NewPool(q *Queue, pgPool *pgxpool.Pool, o PoolOptions, log *slog.Logger, m *observability.Metrics) *Pool {
	if o.WorkerID == "" {
		host, _ := os.Hostname()
		o.WorkerID = fmt.Sprintf("%s-%d", host, os.Getpid())
	}
	if o.LeaseTTL <= 0 {
		o.LeaseTTL = 5 * time.Minute
	}
	if o.PollInterval <= 0 {
		o.PollInterval = time.Second
	}
	return &Pool{q: q, pool: pgPool, opts: o, log: log, metrics: m,
		handlers: map[ports.JobType]Handler{}, wake: map[ports.JobType]chan struct{}{}}
}

// Register attaches the handler for a job type. Must be called before Run.
func (p *Pool) Register(t ports.JobType, h Handler) {
	p.handlers[t] = h
	p.wake[t] = make(chan struct{}, 1)
}

// Run starts workers and blocks until ctx is cancelled and every in-flight job has been finished or
// handed back to the queue.
func (p *Pool) Run(ctx context.Context) {
	p.wg.Add(1)
	go func() { defer p.wg.Done(); p.listen(ctx) }()
	if p.opts.ReclaimEvery > 0 {
		p.wg.Add(1)
		go func() { defer p.wg.Done(); p.reclaimLoop(ctx) }()
	}
	for t, h := range p.handlers {
		n := p.opts.Concurrency[t]
		if n <= 0 {
			p.log.Info("job type disabled on this node", "job_type", t)
			continue
		}
		for i := 0; i < n; i++ {
			p.wg.Add(1)
			go func(t ports.JobType, h Handler, slot int) {
				defer p.wg.Done()
				p.workerLoop(ctx, t, h, fmt.Sprintf("%s/%s/%d", p.opts.WorkerID, t, slot))
			}(t, h, i)
		}
	}
	<-ctx.Done()
	p.wg.Wait()
}

// listen holds one connection on LISTEN dth_jobs and nudges idle workers of the notified type.
// Polling remains the fallback, so a dropped listener only adds latency, never loses work.
func (p *Pool) listen(ctx context.Context) {
	backoff := time.Second
	for ctx.Err() == nil {
		err := p.listenOnce(ctx)
		if ctx.Err() != nil {
			return
		}
		p.log.Warn("job notification listener stopped; relying on polling until it reconnects", "err", err, "retry_in", backoff)
		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
		}
		if backoff < 30*time.Second {
			backoff *= 2
		}
	}
}

func (p *Pool) listenOnce(ctx context.Context) error {
	conn, err := p.pool.Acquire(ctx)
	if err != nil {
		return err
	}
	defer conn.Release()
	if _, err := conn.Exec(ctx, "LISTEN dth_jobs"); err != nil {
		return err
	}
	for {
		n, err := conn.Conn().WaitForNotification(ctx)
		if err != nil {
			return err
		}
		if ch, ok := p.wake[ports.JobType(n.Payload)]; ok {
			select {
			case ch <- struct{}{}:
			default: // a wake-up is already pending
			}
		}
	}
}

func (p *Pool) workerLoop(ctx context.Context, t ports.JobType, h Handler, worker string) {
	ticker := time.NewTicker(p.opts.PollInterval)
	defer ticker.Stop()
	for ctx.Err() == nil {
		job, err := p.q.Claim(ctx, t, worker, p.opts.LeaseTTL)
		if err != nil {
			if ctx.Err() == nil {
				p.log.Error("claim failed", "job_type", t, "err", err)
			}
		} else if job != nil {
			p.process(ctx, *job, h, worker)
			continue // immediately look for more work
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		case <-p.wake[t]:
		}
	}
}

// process runs one job with a heartbeat and maps the handler's result onto the queue.
func (p *Pool) process(parent context.Context, job ports.Job, h Handler, worker string) {
	log := p.log.With("job_id", job.ID, "job_type", job.Type, "correlation_id", job.CorrelationID,
		"attempt", job.Attempts)
	if job.RepoID != "" {
		log = log.With("repo_id", job.RepoID)
	}
	ctx, cancel := context.WithCancel(parent)
	defer cancel()
	ctx = observability.WithCorrelationID(observability.WithLogger(ctx, log), job.CorrelationID)

	var hbWG sync.WaitGroup
	hbWG.Add(1)
	go func() { defer hbWG.Done(); p.heartbeat(ctx, cancel, job.ID, worker, log) }()

	if p.metrics != nil {
		p.metrics.JobsInFlight.WithLabelValues(string(job.Type)).Inc()
		defer p.metrics.JobsInFlight.WithLabelValues(string(job.Type)).Dec()
	}
	start := time.Now()
	log.Info("job started")
	out, err := safeRun(ctx, h, job)
	elapsed := time.Since(start)
	cancel()
	hbWG.Wait()
	if p.metrics != nil {
		p.metrics.JobDuration.WithLabelValues(string(job.Type)).Observe(elapsed.Seconds())
	}

	// Finishing must survive shutdown of the parent context, so it uses its own short deadline.
	fctx, fcancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer fcancel()
	status, ferr := p.settle(fctx, parent, job, worker, out, err, log)
	if p.metrics != nil && status != "" {
		p.metrics.JobsTotal.WithLabelValues(string(job.Type), string(status)).Inc()
	}
	switch {
	case errors.Is(ferr, ErrLeaseLost):
		log.Warn("job result discarded: lease was lost while processing", "duration_ms", elapsed.Milliseconds())
	case ferr != nil:
		log.Error("recording job result failed; the lease sweep will retry the job", "err", ferr)
	}
}

// settle records the outcome and returns the status written ("queued" for a scheduled retry).
func (p *Pool) settle(ctx, parent context.Context, job ports.Job, worker string, out ports.Outcome, err error, log *slog.Logger) (ports.JobStatus, error) {
	if err == nil {
		status := out.Status
		if status == "" {
			status = ports.JobDone
		}
		if !status.Terminal() || status == ports.JobFailed || status == ports.JobDead {
			err = ports.Permanent(fmt.Errorf("handler returned invalid outcome status %q", status))
		} else {
			log.Info("job finished", "status", status)
			return status, p.q.Finish(ctx, job.ID, worker, status, out.Result, out.Message)
		}
	}

	// Graceful shutdown: hand the job back without charging an attempt.
	if parent.Err() != nil && errors.Is(err, context.Canceled) {
		log.Info("job interrupted by shutdown; requeued")
		return ports.JobQueued, p.q.Reschedule(ctx, job.ID, worker, time.Now(), "interrupted by shutdown", true)
	}

	var spend *ports.SpendBlockedError
	var perm *ports.PermanentError
	var inval *ports.ValidationError
	switch {
	case errors.As(err, &spend):
		log.Error("job blocked by spend guard", "scope", spend.Scope, "estimated_tokens", spend.EstimatedTokens, "reason", spend.Reason)
		return ports.JobSpendBlocked, p.q.Finish(ctx, job.ID, worker, ports.JobSpendBlocked, nil, spend.Error())
	case errors.As(err, &perm), errors.As(err, &inval):
		log.Error("job failed permanently", "err", err)
		return ports.JobFailed, p.q.Finish(ctx, job.ID, worker, ports.JobFailed, nil, err.Error())
	}

	if t, ok := ports.AsTransient(err); ok && t.RetryAfter > 0 {
		log.Warn("upstream asked to retry later; attempt not charged", "retry_after", t.RetryAfter, "err", err)
		return ports.JobQueued, p.q.Reschedule(ctx, job.ID, worker, time.Now().Add(t.RetryAfter), err.Error(), true)
	}
	if _, ok := ports.AsTransient(err); !ok {
		log.Warn("handler returned an untyped error; treating as transient", "err", err)
	}
	if job.Attempts >= job.MaxAttempts {
		log.Error("job exhausted retries; moved to dead letter", "err", err)
		return ports.JobDead, p.q.Finish(ctx, job.ID, worker, ports.JobDead, nil, err.Error())
	}
	delay := p.q.Backoff(job.Attempts)
	log.Warn("job failed; retry scheduled", "err", err, "retry_in", delay)
	return ports.JobQueued, p.q.Reschedule(ctx, job.ID, worker, time.Now().Add(delay), err.Error(), false)
}

func (p *Pool) heartbeat(ctx context.Context, cancel context.CancelFunc, id, worker string, log *slog.Logger) {
	t := time.NewTicker(p.opts.LeaseTTL / 3)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			ok, err := p.q.Heartbeat(ctx, id, worker, p.opts.LeaseTTL)
			if err != nil {
				if ctx.Err() == nil {
					log.Warn("heartbeat failed", "err", err)
				}
				continue
			}
			if !ok {
				log.Warn("lease lost; cancelling handler")
				cancel()
				return
			}
		}
	}
}

func (p *Pool) reclaimLoop(ctx context.Context) {
	t := time.NewTicker(p.opts.ReclaimEvery)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			rows, err := p.q.ReclaimExpired(ctx)
			if err != nil {
				if ctx.Err() == nil {
					p.log.Error("lease reclaim failed", "err", err)
				}
				continue
			}
			for _, r := range rows {
				p.log.Warn("reclaimed job with expired lease", "job_id", r.ID, "job_type", r.Type, "new_status", r.Status)
			}
		}
	}
}

// safeRun converts a handler panic into a permanent error so one bad job cannot kill the worker.
func safeRun(ctx context.Context, h Handler, job ports.Job) (out ports.Outcome, err error) {
	defer func() {
		if r := recover(); r != nil {
			observability.Logger(ctx).Error("job handler panicked", "panic", r, "stack", string(debug.Stack()))
			err = ports.Permanent(fmt.Errorf("handler panic: %v", r))
		}
	}()
	return h(ctx, job)
}
