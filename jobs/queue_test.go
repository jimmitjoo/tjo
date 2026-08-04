package jobs

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMemoryQueue(t *testing.T) {
	queue := NewMemoryQueue("test")

	assert.Equal(t, "test", queue.Name())
	assert.Equal(t, 0, queue.Size())

	job1 := NewJob("test1", "test", nil)
	job1.Priority = PriorityHigh

	job2 := NewJob("test2", "test", nil)
	job2.Priority = PriorityLow

	err := queue.Push(job1)
	require.NoError(t, err)
	assert.Equal(t, 1, queue.Size())

	err = queue.Push(job2)
	require.NoError(t, err)
	assert.Equal(t, 2, queue.Size())

	peekedJob, err := queue.Peek()
	require.NoError(t, err)
	assert.Equal(t, job1.ID, peekedJob.ID)
	assert.Equal(t, 2, queue.Size())

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	poppedJob, err := queue.Pop(ctx)
	require.NoError(t, err)
	assert.Equal(t, job1.ID, poppedJob.ID)
	assert.Equal(t, 1, queue.Size())

	err = queue.Clear()
	require.NoError(t, err)
	assert.Equal(t, 0, queue.Size())
}

func TestMemoryQueueScheduledJobs(t *testing.T) {
	queue := NewMemoryQueue("test")

	scheduledJob := NewJob("scheduled", "test", nil)
	future := time.Now().Add(time.Hour)
	scheduledJob.WithScheduledAt(future)

	readyJob := NewJob("ready", "test", nil)

	err := queue.Push(scheduledJob)
	require.NoError(t, err)

	err = queue.Push(readyJob)
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	poppedJob, err := queue.Pop(ctx)
	require.NoError(t, err)
	assert.Equal(t, readyJob.ID, poppedJob.ID)

	_, err = queue.Peek()
	assert.Error(t, err)
}

func TestMemoryQueueJobsByStatus(t *testing.T) {
	queue := NewMemoryQueue("test")

	pendingJob := NewJob("pending", "test", nil)
	runningJob := NewJob("running", "test", nil)
	runningJob.MarkRunning()

	queue.Push(pendingJob)
	queue.Push(runningJob)

	pendingJobs := queue.GetJobsByStatus(JobStatusPending)
	assert.Len(t, pendingJobs, 1)
	assert.Equal(t, pendingJob.ID, pendingJobs[0].ID)

	runningJobs := queue.GetJobsByStatus(JobStatusRunning)
	assert.Len(t, runningJobs, 1)
	assert.Equal(t, runningJob.ID, runningJobs[0].ID)
}

func TestMemoryQueueRemoveJob(t *testing.T) {
	queue := NewMemoryQueue("test")

	job := NewJob("test", "test", nil)
	queue.Push(job)

	assert.Equal(t, 1, queue.Size())

	err := queue.RemoveJob(job.ID)
	require.NoError(t, err)
	assert.Equal(t, 0, queue.Size())

	err = queue.RemoveJob("nonexistent")
	assert.Error(t, err)
}

func TestPriorityQueue(t *testing.T) {
	queue := NewPriorityQueue("priority")

	lowJob := NewJob("low", "priority", nil).WithPriority(PriorityLow)
	highJob := NewJob("high", "priority", nil).WithPriority(PriorityHigh)
	normalJob := NewJob("normal", "priority", nil)
	criticalJob := NewJob("critical", "priority", nil).WithPriority(PriorityCritical)

	queue.Push(lowJob)
	queue.Push(normalJob)
	queue.Push(highJob)
	queue.Push(criticalJob)

	ctx := context.Background()

	job1, _ := queue.Pop(ctx)
	assert.Equal(t, criticalJob.ID, job1.ID)

	job2, _ := queue.Pop(ctx)
	assert.Equal(t, highJob.ID, job2.ID)

	job3, _ := queue.Pop(ctx)
	assert.Equal(t, normalJob.ID, job3.ID)

	job4, _ := queue.Pop(ctx)
	assert.Equal(t, lowJob.ID, job4.ID)
}

func TestDelayedQueue(t *testing.T) {
	queue := NewDelayedQueue("delayed")

	job := NewJob("test", "delayed", nil)

	err := queue.Push(job)
	require.NoError(t, err)

	assert.Equal(t, JobStatusScheduled, job.Status)
	assert.NotNil(t, job.ScheduledAt)
}

func TestQueueManager(t *testing.T) {
	manager := NewQueueManager()

	assert.Empty(t, manager.ListQueues())

	queue1 := NewMemoryQueue("queue1")
	manager.RegisterQueue("queue1", queue1)

	retrievedQueue, err := manager.GetQueue("queue1")
	require.NoError(t, err)
	assert.Equal(t, queue1, retrievedQueue)

	_, err = manager.GetQueue("nonexistent")
	assert.Error(t, err)

	autoQueue := manager.GetOrCreateQueue("auto")
	assert.NotNil(t, autoQueue)
	assert.Equal(t, "auto", autoQueue.Name())

	sameQueue := manager.GetOrCreateQueue("auto")
	assert.Same(t, autoQueue, sameQueue)

	queues := manager.ListQueues()
	assert.Contains(t, queues, "queue1")
	assert.Contains(t, queues, "auto")
}

func TestQueueStats(t *testing.T) {
	manager := NewQueueManager()
	queue := manager.GetOrCreateQueue("test")

	pendingJob := NewJob("pending", "test", nil)
	runningJob := NewJob("running", "test", nil)
	runningJob.MarkRunning()
	completedJob := NewJob("completed", "test", nil)
	completedJob.MarkCompleted(nil)

	queue.Push(pendingJob)
	queue.Push(runningJob)
	queue.Push(completedJob)

	stats := manager.GetQueueStats()
	assert.Contains(t, stats, "test")

	testStats := stats["test"]
	assert.Equal(t, "test", testStats.Name)
	assert.Equal(t, 3, testStats.Size)
	assert.Equal(t, 1, testStats.Pending)
	assert.Equal(t, 1, testStats.Running)
	assert.Equal(t, 1, testStats.Completed)
}

func TestQueueManagerOperations(t *testing.T) {
	manager := NewQueueManager()
	queue := manager.GetOrCreateQueue("test")

	job := NewJob("test", "test", nil)
	queue.Push(job)

	assert.Equal(t, 1, queue.Size())

	err := manager.ClearQueue("test")
	require.NoError(t, err)
	assert.Equal(t, 0, queue.Size())

	err = manager.RemoveQueue("test")
	require.NoError(t, err)

	_, err = manager.GetQueue("test")
	assert.Error(t, err)
}

// TestQueueDoesNotShareJobPointers guards the ownership invariant behind issue
// #24: the queue keeps its own copy, so a caller holding the job it enqueued
// never races with Job.MarkRunning on a worker goroutine.
func TestQueueDoesNotShareJobPointers(t *testing.T) {
	q := NewMemoryQueue("default")

	job := NewJob("test", "default", map[string]interface{}{"k": "v"})
	if err := q.Push(job); err != nil {
		t.Fatal(err)
	}

	// Mutating the queued copy must not be visible through the caller's job.
	q.PromoteScheduled()
	queued, err := q.Peek()
	if err != nil {
		t.Fatal(err)
	}
	if queued == job {
		t.Fatal("Peek handed back the caller's job pointer")
	}

	queued.Status = JobStatusRunning
	queued.Payload["k"] = "mutated"

	if job.Status == JobStatusRunning {
		t.Error("mutating a job from the queue changed the caller's job")
	}
	if job.Payload["k"] != "v" {
		t.Errorf("payload aliased: caller sees %v", job.Payload["k"])
	}

	for _, got := range q.GetJobs() {
		if got == job {
			t.Error("GetJobs handed back the caller's job pointer")
		}
	}
}

// TestJobStatusReadDuringProcessingIsRaceFree is the -race regression: read the
// enqueued job's fields from the caller's goroutine while workers process it.
func TestJobStatusReadDuringProcessingIsRaceFree(t *testing.T) {
	manager := NewJobManager(nil)
	manager.RegisterHandlerFunc("test", func(ctx context.Context, job *Job) error {
		return nil
	})
	if err := manager.Start(); err != nil {
		t.Fatal(err)
	}
	defer manager.Stop()

	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		job := NewJob("test", "default", nil)
		if err := manager.Enqueue(job); err != nil {
			t.Fatal(err)
		}

		wg.Add(1)
		go func(j *Job) {
			defer wg.Done()
			for k := 0; k < 50; k++ {
				_ = j.Status
				_ = j.Attempts
			}
		}(job)
	}
	wg.Wait()
}

// TestPopWakesOnPush covers the dispatch latency fix in issue #4. Pop used to
// sleep a flat 100ms between scans, so a job pushed just after a scan waited
// out the remainder of that sleep before any worker started it.
func TestPopWakesOnPush(t *testing.T) {
	q := NewMemoryQueue("default")

	popped := make(chan time.Duration, 1)
	go func() {
		start := time.Now()
		if _, err := q.Pop(context.Background()); err != nil {
			return
		}
		popped <- time.Since(start)
	}()

	// Let Pop scan the empty queue and settle into its wait.
	time.Sleep(20 * time.Millisecond)

	pushedAt := time.Now()
	if err := q.Push(NewJob("test", "default", nil)); err != nil {
		t.Fatal(err)
	}

	select {
	case <-popped:
		latency := time.Since(pushedAt)
		if latency > scheduledPollInterval/2 {
			t.Errorf("Pop took %v after push; it is waiting out the poll interval rather than waking on the signal", latency)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Pop never returned")
	}
}
