// Package scheduler runs periodic maintenance on exactly one replica at a time. Leadership is a Postgres
// session-level advisory lock held on a dedicated connection: if the leader dies, its connection closes,
// the lock is released, and another replica takes over within one election interval.
package scheduler

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// leaderLockKey is an arbitrary constant identifying the scheduler's advisory lock.
const leaderLockKey int64 = 0x4454485f5343 // "DTH_SC"

// Task is one periodic job.
type Task struct {
	Name     string
	Every    time.Duration
	RunFirst bool // run immediately on becoming leader
	Fn       func(ctx context.Context) error
}

// Scheduler elects a leader and runs tasks on it.
type Scheduler struct {
	pool          *pgxpool.Pool
	log           *slog.Logger
	tasks         []Task
	electionEvery time.Duration
}

// New returns a Scheduler.
func New(pool *pgxpool.Pool, log *slog.Logger, electionEvery time.Duration) *Scheduler {
	if electionEvery <= 0 {
		electionEvery = 10 * time.Second
	}
	return &Scheduler{pool: pool, log: log.With("component", "scheduler"), electionEvery: electionEvery}
}

// Add registers a task. Must be called before Run.
func (s *Scheduler) Add(t Task) { s.tasks = append(s.tasks, t) }

// Run blocks until ctx is cancelled, repeatedly trying to become leader.
func (s *Scheduler) Run(ctx context.Context) {
	for ctx.Err() == nil {
		err := s.leadOnce(ctx)
		if ctx.Err() != nil {
			return
		}
		if err != nil && !errors.Is(err, errNotLeader) {
			s.log.Warn("scheduler leadership lost", "err", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(s.electionEvery):
		}
	}
}

var errNotLeader = errors.New("not leader")

func (s *Scheduler) leadOnce(ctx context.Context) error {
	conn, err := s.pool.Acquire(ctx)
	if err != nil {
		return err
	}
	defer conn.Release()
	var got bool
	if err := conn.QueryRow(ctx, "SELECT pg_try_advisory_lock($1)", leaderLockKey).Scan(&got); err != nil {
		return err
	}
	if !got {
		return errNotLeader
	}
	defer func() {
		// Release explicitly so a graceful shutdown hands over leadership immediately.
		uctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, _ = conn.Exec(uctx, "SELECT pg_advisory_unlock($1)", leaderLockKey)
	}()
	s.log.Info("became scheduler leader")

	lctx, cancel := context.WithCancel(ctx)
	defer cancel()
	var wg sync.WaitGroup
	for _, t := range s.tasks {
		wg.Add(1)
		go func(t Task) { defer wg.Done(); s.loop(lctx, t) }(t)
	}
	// Keep checking the lock connection; if it breaks, stop tasks so another replica can lead.
	tick := time.NewTicker(s.electionEvery)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			cancel()
			wg.Wait()
			return nil
		case <-tick.C:
			if err := conn.Ping(ctx); err != nil {
				cancel()
				wg.Wait()
				return err
			}
		}
	}
}

func (s *Scheduler) loop(ctx context.Context, t Task) {
	run := func() {
		start := time.Now()
		if err := t.Fn(ctx); err != nil && ctx.Err() == nil {
			s.log.Error("scheduled task failed", "task", t.Name, "err", err)
			return
		}
		s.log.Debug("scheduled task ran", "task", t.Name, "duration_ms", time.Since(start).Milliseconds())
	}
	if t.RunFirst {
		run()
	}
	tick := time.NewTicker(t.Every)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			run()
		}
	}
}
