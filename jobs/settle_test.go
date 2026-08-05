package jobs

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

// waitFor polls cond until it holds or the deadline passes.
func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

// A worker must record that an SQL job finished.
//
// Nothing did. The worker popped the job, ran it, and returned; the row stayed
// at 'running' with a lock that expired after LockTimeout, and the next worker
// claimed and ran the very same job. Every job on an SQLQueue therefore ran
// again every five minutes, forever, with nothing failing and nothing logged.
//
// LockTimeout is zero here so that "forever" is observable in a test rather
// than in production five minutes later.
func TestWorkerMarksAnSQLJobCompletedInsteadOfRunningItForever(t *testing.T) {
	q := sqliteQueue(t)
	q.LockTimeout = 0

	var runs atomic.Int32

	proc := NewJobProcessor(DefaultRetryConfig())
	proc.RegisterHandlerFunc("count", func(ctx context.Context, job *Job) error {
		runs.Add(1)
		return nil
	})

	job := NewJob("count", "default", nil)
	job.MaxAttempts = 5
	if err := q.Push(job); err != nil {
		t.Fatal(err)
	}

	w := NewWorker("settle-test", q, proc)
	w.Start()
	defer w.Stop()

	waitFor(t, "the job to be marked completed", func() bool {
		return q.CountByStatus(JobStatusCompleted) == 1
	})

	// Two Pop ticks with a zero lock timeout: ample opportunity to re-claim a
	// job that was never settled.
	time.Sleep(2200 * time.Millisecond)

	if got := runs.Load(); got != 1 {
		t.Fatalf("handler ran %d times, want 1 -- the job is being re-claimed", got)
	}
	if got := q.CountByStatus(JobStatusCompleted); got != 1 {
		t.Fatalf("%d completed rows, want 1", got)
	}
}

// A failure has to reach the row too, or a job that failed is indistinguishable
// from one whose worker died.
func TestWorkerRecordsAFailureAsARetry(t *testing.T) {
	q := sqliteQueue(t)

	var runs atomic.Int32

	proc := NewJobProcessor(DefaultRetryConfig())
	proc.RegisterHandlerFunc("boom", func(ctx context.Context, job *Job) error {
		runs.Add(1)
		return errors.New("downstream is down")
	})

	job := NewJob("boom", "default", nil)
	job.MaxAttempts = 3
	if err := q.Push(job); err != nil {
		t.Fatal(err)
	}

	w := NewWorker("settle-test", q, proc)
	w.Start()
	defer w.Stop()

	waitFor(t, "the job to be marked retrying", func() bool {
		return q.CountByStatus(JobStatusRetrying) == 1
	})

	var (
		attempts    int
		scheduledAt time.Time
		lastError   string
	)
	row := q.db.QueryRow(`SELECT attempts, scheduled_at, last_error FROM tjo_jobs WHERE id = ?`, job.ID)
	if err := row.Scan(&attempts, &scheduledAt, &lastError); err != nil {
		t.Fatal(err)
	}

	// One run, one attempt. The queue counts the attempt when it claims the row
	// and the processor counts it again in memory; settling with the in-memory
	// value would burn two of three attempts on the first failure.
	if attempts != 1 {
		t.Fatalf("attempts = %d after one run, want 1", attempts)
	}
	if !scheduledAt.After(time.Now()) {
		t.Fatalf("scheduled_at = %v, want a time in the future -- an immediate retry is a denial of service against whatever just failed", scheduledAt)
	}
	if lastError != "downstream is down" {
		t.Fatalf("last_error = %q, want the handler's error", lastError)
	}
}

// The outcome must be written even when the worker is being shut down. It used
// to be tempting to settle with the worker's own context, which is cancelled by
// Stop -- so every job in flight at shutdown would look abandoned and run again
// on the next boot.
func TestSettleSurvivesWorkerCancellation(t *testing.T) {
	q := sqliteQueue(t)

	proc := NewJobProcessor(DefaultRetryConfig())
	proc.RegisterHandlerFunc("slow", func(ctx context.Context, job *Job) error {
		return nil
	})

	job := NewJob("slow", "default", nil)
	if err := q.Push(job); err != nil {
		t.Fatal(err)
	}

	w := NewWorker("settle-test", q, proc)

	claimed, err := q.TryPop(context.Background(), w.id)
	if err != nil {
		t.Fatal(err)
	}
	claimed.MarkCompleted(nil)

	w.Stop() // cancels w.ctx before the settle
	w.settle(claimed, claimed.Attempts, nil)

	if got := q.CountByStatus(JobStatusCompleted); got != 1 {
		t.Fatalf("%d completed rows after settling during shutdown, want 1", got)
	}
}

// Oldest is the number that says whether the queue is moving. Depth says how
// much work there is; this says whether anything is doing it, and a depth of
// ten with an oldest of four hours is a stopped worker rather than a busy one.
func TestOldestReportsHowLongTheQueueHasBeenWaiting(t *testing.T) {
	q := sqliteQueue(t)
	ctx := context.Background()

	if got, err := q.Oldest(ctx); err != nil || got != 0 {
		t.Fatalf("empty queue: %v, %v -- want zero and no error", got, err)
	}

	job := NewJob("work", "default", nil)
	job.CreatedAt = time.Now().Add(-90 * time.Minute).UTC()
	job.UpdatedAt = job.CreatedAt
	if err := q.Push(job); err != nil {
		t.Fatal(err)
	}

	got, err := q.Oldest(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if got < 80*time.Minute || got > 100*time.Minute {
		t.Fatalf("Oldest = %v, want about 90 minutes", got)
	}
}
