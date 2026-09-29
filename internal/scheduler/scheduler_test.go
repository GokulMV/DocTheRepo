package scheduler

import (
	"context"
	"io"
	"log/slog"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/GokulMV/DocTheRepo/internal/store/storetest"
)

var quiet = slog.New(slog.NewTextHandler(io.Discard, nil))

func TestScheduler_OnlyOneLeaderRunsTasks_AndFailsOver(t *testing.T) {
	st := storetest.New(t)
	var runsA, runsB atomic.Int32
	a := New(st.Pool, quiet, 50*time.Millisecond)
	a.Add(Task{Name: "tick", Every: 20 * time.Millisecond, RunFirst: true, Fn: func(context.Context) error { runsA.Add(1); return nil }})
	b := New(st.Pool, quiet, 50*time.Millisecond)
	b.Add(Task{Name: "tick", Every: 20 * time.Millisecond, RunFirst: true, Fn: func(context.Context) error { runsB.Add(1); return nil }})

	ctxA, cancelA := context.WithCancel(context.Background())
	doneA := make(chan struct{})
	go func() { a.Run(ctxA); close(doneA) }()
	require.Eventually(t, func() bool { return runsA.Load() > 0 }, 5*time.Second, 10*time.Millisecond)

	ctxB, cancelB := context.WithCancel(context.Background())
	doneB := make(chan struct{})
	go func() { b.Run(ctxB); close(doneB) }()
	defer func() { cancelB(); <-doneB }()
	time.Sleep(300 * time.Millisecond)
	assert.Zero(t, runsB.Load(), "a second replica must not run tasks while the first leads")

	cancelA()
	<-doneA
	require.Eventually(t, func() bool { return runsB.Load() > 0 }, 5*time.Second, 10*time.Millisecond,
		"leadership fails over after the leader stops")
}
