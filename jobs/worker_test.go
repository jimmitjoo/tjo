package jobs

import (
	"context"
	"runtime"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestScaleQueueDoesNotOrphanWorkers covers issue #13. Worker IDs came from
// len(workers)+1 and from a loop index, so scaling down then up regenerated an
// ID that a live worker still held. The map entry was overwritten without
// stopping it, and the goroutine kept popping real jobs off the queue with no
// handle left to stop it.
func TestScaleQueueDoesNotOrphanWorkers(t *testing.T) {
	processor := NewJobProcessor(DefaultRetryConfig())
	pool := NewWorkerPool(processor)
	queue := NewMemoryQueue("scale")

	before := runtime.NumGoroutine()

	for i := 0; i < 5; i++ {
		pool.AddWorker("scale", queue)
	}
	require.NoError(t, pool.ScaleQueue("scale", queue, 2))
	require.NoError(t, pool.ScaleQueue("scale", queue, 5))

	pool.mutex.RLock()
	tracked := len(pool.workers)
	pool.mutex.RUnlock()
	assert.Equal(t, 5, tracked, "pool lost track of workers")

	pool.StopAll()

	// StopAll waits, so every worker goroutine must be gone by now.
	assert.Eventually(t, func() bool {
		return runtime.NumGoroutine() <= before+1
	}, 5*time.Second, 50*time.Millisecond,
		"goroutines leaked: %d before, %d after StopAll", before, runtime.NumGoroutine())
}

// TestStopAllWaitsForWorkers covers the other half: StopAll cancelled and
// returned immediately, so a worker inside a handler kept running after the
// manager logged that it had stopped, and Tjo.Shutdown then closed the
// database out from under it.
func TestStopAllWaitsForWorkers(t *testing.T) {
	processor := NewJobProcessor(DefaultRetryConfig())

	started := make(chan struct{})
	finished := make(chan struct{})
	processor.RegisterHandlerFunc("slow", func(ctx context.Context, job *Job) error {
		close(started)
		time.Sleep(200 * time.Millisecond)
		close(finished)
		return nil
	})

	queue := NewMemoryQueue("slow")
	pool := NewWorkerPool(processor)
	pool.AddWorker("slow", queue)

	require.NoError(t, queue.Push(NewJob("slow", "slow", nil)))

	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("handler never started")
	}

	pool.StopAll()

	select {
	case <-finished:
	default:
		t.Error("StopAll returned while a handler was still running")
	}
}

// TestJobManagerRestart covers the third defect: Stop cancelled the context
// built in NewJobManager and Start never rebuilt it, so a restarted manager
// reported running while its goroutines had already returned.
func TestJobManagerRestart(t *testing.T) {
	manager := NewJobManager(nil)

	var handled atomic.Bool
	manager.RegisterHandlerFunc("test", func(ctx context.Context, job *Job) error {
		handled.Store(true)
		return nil
	})

	require.NoError(t, manager.Start())
	require.NoError(t, manager.Stop())
	require.NoError(t, manager.Start())
	defer manager.Stop()

	require.NoError(t, manager.Enqueue(NewJob("test", "default", nil)))

	assert.Eventually(t, handled.Load, 3*time.Second, 20*time.Millisecond,
		"restarted manager never processed a job")
}
