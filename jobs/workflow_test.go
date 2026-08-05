package jobs

import (
	"context"
	"database/sql"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

// eachQueue runs fn against SQLite and PostgreSQL. The PostgreSQL half skips
// unless TJO_TEST_POSTGRES_DSN is set; CI sets it.
func eachQueue(t *testing.T, fn func(t *testing.T, q *SQLQueue, ws *Workflows)) {
	t.Helper()

	t.Run("sqlite", func(t *testing.T) {
		q := sqliteQueue(t)
		ws := NewWorkflows(q.db, DialectSQLite)
		if err := ws.Migrate(context.Background()); err != nil {
			t.Fatal(err)
		}
		fn(t, q, ws)
	})

	t.Run("postgres", func(t *testing.T) {
		q := postgresQueue(t)

		for _, table := range []string{"tjo_workflow_steps", "tjo_workflow_events"} {
			if _, err := q.db.Exec("DROP TABLE IF EXISTS " + table); err != nil {
				t.Fatal(err)
			}
		}
		t.Cleanup(func() {
			q.db.Exec("DROP TABLE IF EXISTS tjo_workflow_steps")
			q.db.Exec("DROP TABLE IF EXISTS tjo_workflow_events")
		})

		ws := NewWorkflows(q.db, DialectPostgres)
		if err := ws.Migrate(context.Background()); err != nil {
			t.Fatal(err)
		}
		fn(t, q, ws)
	})
}

func jobRow(t *testing.T, q *SQLQueue, id string) (status string, attempts int, scheduledAt sql.NullTime) {
	t.Helper()
	err := q.db.QueryRow(q.rebind(`SELECT status, attempts, scheduled_at FROM tjo_jobs WHERE id = ?`), id).
		Scan(&status, &attempts, &scheduledAt)
	if err != nil {
		t.Fatal(err)
	}
	return status, attempts, scheduledAt
}

// The point of the whole feature: a workflow that fails at step three re-runs
// step three, not steps one and two -- which may have charged a card or sent an
// email that the customer is not getting twice.
func TestRetryResumesAtTheFailedStep(t *testing.T) {
	eachQueue(t, func(t *testing.T, q *SQLQueue, ws *Workflows) {
		var one, two, three atomic.Int32

		proc := NewJobProcessor(DefaultRetryConfig())
		proc.RegisterHandlerFunc("checkout", func(ctx context.Context, job *Job) error {
			wf := ws.For(job)

			if err := Do(ctx, wf, "reserve", func(ctx context.Context) error {
				one.Add(1)
				return nil
			}); err != nil {
				return err
			}

			if err := Do(ctx, wf, "charge", func(ctx context.Context) error {
				two.Add(1)
				return nil
			}); err != nil {
				return err
			}

			return Do(ctx, wf, "receipt", func(ctx context.Context) error {
				if three.Add(1) == 1 {
					return errors.New("mail server refused")
				}
				return nil
			})
		})

		job := NewJob("checkout", "default", nil)
		job.MaxAttempts = 3
		if err := q.Push(job); err != nil {
			t.Fatal(err)
		}

		w := NewWorker("wf", q, proc)
		w.Start()
		defer w.Stop()

		waitFor(t, "the workflow to fail its third step", func() bool {
			return q.CountByStatus(JobStatusRetrying) == 1
		})

		// The failed step is named and counted, which is what makes a stuck
		// workflow diagnosable rather than a mystery in a log.
		steps, err := ws.Steps(context.Background(), job.ID)
		if err != nil {
			t.Fatal(err)
		}
		if len(steps) != 3 {
			t.Fatalf("%d checkpoints, want 3: %+v", len(steps), steps)
		}
		last := steps[len(steps)-1]
		if last.Name != "receipt" || last.Status != StepFailed || last.Attempts != 1 {
			t.Fatalf("last checkpoint = %+v, want receipt/failed/1", last)
		}
		if last.LastError != "mail server refused" {
			t.Fatalf("last error = %q, want the step's error", last.LastError)
		}

		// SQLQueue backs off two seconds after the first failure.
		waitFor(t, "the workflow to complete on retry", func() bool {
			return q.CountByStatus(JobStatusCompleted) == 1
		})

		if got := one.Load(); got != 1 {
			t.Fatalf("step one ran %d times, want 1 -- the retry restarted the workflow", got)
		}
		if got := two.Load(); got != 1 {
			t.Fatalf("step two ran %d times, want 1 -- the retry restarted the workflow", got)
		}
		if got := three.Load(); got != 2 {
			t.Fatalf("step three ran %d times, want 2", got)
		}
	})
}

// A step result is stored, not recomputed. Without this, a replay of a step
// that read a clock, generated an ID or called an API would produce a different
// answer than the one the rest of the workflow already acted on.
func TestAStepResultIsCheckpointedNotRecomputed(t *testing.T) {
	eachQueue(t, func(t *testing.T, q *SQLQueue, ws *Workflows) {
		ctx := context.Background()
		wf := ws.ForJob("job-1")

		type payment struct {
			Reference string
			Cents     int
		}

		var runs atomic.Int32
		first, err := Step(ctx, wf, "charge", func(ctx context.Context) (payment, error) {
			runs.Add(1)
			return payment{Reference: "ch_" + time.Now().Format("150405.000000000"), Cents: 4200}, nil
		})
		if err != nil {
			t.Fatal(err)
		}

		second, err := Step(ctx, wf, "charge", func(ctx context.Context) (payment, error) {
			runs.Add(1)
			return payment{Reference: "a different reference entirely", Cents: 1}, nil
		})
		if err != nil {
			t.Fatal(err)
		}

		if runs.Load() != 1 {
			t.Fatalf("the step function ran %d times, want 1", runs.Load())
		}
		if second != first {
			t.Fatalf("replay returned %+v, want the checkpointed %+v", second, first)
		}
	})
}

// A workflow parked on Sleep has to survive the worker that started it. The
// wake time is checkpointed, so a redeploy does not restart the clock.
func TestSleepSurvivesAWorkerRestart(t *testing.T) {
	eachQueue(t, func(t *testing.T, q *SQLQueue, ws *Workflows) {
		var prepared, finished atomic.Int32

		handler := func(ctx context.Context, job *Job) error {
			wf := ws.For(job)

			if err := Do(ctx, wf, "prepare", func(ctx context.Context) error {
				prepared.Add(1)
				return nil
			}); err != nil {
				return err
			}

			if err := wf.Sleep(ctx, "cool-off", 500*time.Millisecond); err != nil {
				return err
			}

			return Do(ctx, wf, "finish", func(ctx context.Context) error {
				finished.Add(1)
				return nil
			})
		}

		job := NewJob("slow-burn", "default", nil)
		job.MaxAttempts = 2
		if err := q.Push(job); err != nil {
			t.Fatal(err)
		}

		first := NewJobProcessor(DefaultRetryConfig())
		first.RegisterHandlerFunc("slow-burn", handler)

		w1 := NewWorker("wf-before-restart", q, first)
		w1.Start()

		waitFor(t, "the workflow to park on its sleep", func() bool {
			status, _, scheduled := jobRow(t, q, job.ID)
			return status == string(JobStatusPending) && scheduled.Valid && scheduled.Time.After(time.Now())
		})

		// Parking is waiting, not failing: the attempt the claim counted is
		// given back, so a workflow may sleep more often than it may fail.
		if _, attempts, _ := jobRow(t, q, job.ID); attempts != 0 {
			t.Fatalf("attempts = %d after parking, want 0 -- sleeping is spending retries", attempts)
		}

		// The process dies here.
		w1.Stop()
		if err := w1.Wait(context.Background()); err != nil {
			t.Fatal(err)
		}

		second := NewJobProcessor(DefaultRetryConfig())
		second.RegisterHandlerFunc("slow-burn", handler)

		w2 := NewWorker("wf-after-restart", q, second)
		w2.Start()
		defer w2.Stop()

		waitFor(t, "the restarted worker to finish the workflow", func() bool {
			return q.CountByStatus(JobStatusCompleted) == 1
		})

		if got := prepared.Load(); got != 1 {
			t.Fatalf("the step before the sleep ran %d times, want 1", got)
		}
		if got := finished.Load(); got != 1 {
			t.Fatalf("the step after the sleep ran %d times, want 1", got)
		}
	})
}

// A workflow can park on something outside itself -- an approval, a webhook --
// for as long as it takes, because it is a row rather than a goroutine.
func TestWaitForEventParksUntilNotified(t *testing.T) {
	eachQueue(t, func(t *testing.T, q *SQLQueue, ws *Workflows) {
		type approval struct {
			By       string
			Approved bool
		}

		var got atomic.Value

		proc := NewJobProcessor(DefaultRetryConfig())
		proc.RegisterHandlerFunc("expense", func(ctx context.Context, job *Job) error {
			wf := ws.For(job)

			decision, err := WaitForEvent[approval](ctx, wf, "decided")
			if err != nil {
				return err
			}

			return Do(ctx, wf, "record", func(ctx context.Context) error {
				got.Store(decision)
				return nil
			})
		})

		job := NewJob("expense", "default", nil)
		job.MaxAttempts = 2
		if err := q.Push(job); err != nil {
			t.Fatal(err)
		}

		w := NewWorker("wf", q, proc)
		w.Start()
		defer w.Stop()

		waitFor(t, "the workflow to park on the event", func() bool {
			status, _, scheduled := jobRow(t, q, job.ID)
			return status == string(JobStatusPending) &&
				scheduled.Valid && scheduled.Time.After(time.Now().Add(time.Hour))
		})

		if err := ws.Notify(context.Background(), job.ID, "decided", approval{By: "jimmie", Approved: true}); err != nil {
			t.Fatal(err)
		}

		waitFor(t, "the workflow to resume and complete", func() bool {
			return q.CountByStatus(JobStatusCompleted) == 1
		})

		decision, _ := got.Load().(approval)
		if decision.By != "jimmie" || !decision.Approved {
			t.Fatalf("the resumed workflow saw %+v, want the notified payload", decision)
		}
	})
}

// The first notification of a name wins. An event is a checkpoint, and letting
// a second one overwrite it changes history underneath a replay.
func TestNotifyDoesNotOverwriteAnEvent(t *testing.T) {
	eachQueue(t, func(t *testing.T, q *SQLQueue, ws *Workflows) {
		ctx := context.Background()

		if err := ws.Notify(ctx, "job-1", "decided", "first"); err != nil {
			t.Fatal(err)
		}
		if err := ws.Notify(ctx, "job-1", "decided", "second"); err != nil {
			t.Fatal(err)
		}

		wf := ws.ForJob("job-1")
		value, err := WaitForEvent[string](ctx, wf, "decided")
		if err != nil {
			t.Fatal(err)
		}
		if value != "first" {
			t.Fatalf("event payload = %q, want the first notification", value)
		}
	})
}

// Parking must not be mistaken for failing anywhere along the path. The
// processor used to run every non-nil error through handleJobFailure, which
// schedules a retry and then returns nil -- so the worker would have read a
// parked workflow as a completed one and marked the job done.
func TestParkingIsNotAFailure(t *testing.T) {
	eachQueue(t, func(t *testing.T, q *SQLQueue, ws *Workflows) {
		proc := NewJobProcessor(DefaultRetryConfig())
		proc.RegisterHandlerFunc("parker", func(ctx context.Context, job *Job) error {
			return ws.For(job).Sleep(ctx, "wait", time.Hour)
		})

		job := NewJob("parker", "default", nil)
		if err := q.Push(job); err != nil {
			t.Fatal(err)
		}

		claimed, err := q.TryPop(context.Background(), "wf")
		if err != nil {
			t.Fatal(err)
		}

		err = proc.ProcessJob(context.Background(), claimed)
		parked, ok := IsParked(err)
		if !ok {
			t.Fatalf("ProcessJob returned %v, want a *Parked error", err)
		}

		w := NewWorker("wf", q, proc)
		w.settle(claimed, claimed.Attempts, err)

		status, attempts, scheduled := jobRow(t, q, job.ID)
		if status != string(JobStatusPending) {
			t.Fatalf("status = %q after parking, want pending", status)
		}
		if attempts != 0 {
			t.Fatalf("attempts = %d after parking, want 0", attempts)
		}
		if !scheduled.Valid || scheduled.Time.Before(parked.Until.Add(-time.Minute)) {
			t.Fatalf("scheduled_at = %v, want about %v", scheduled.Time, parked.Until)
		}
		if q.CountByStatus(JobStatusCompleted) != 0 {
			t.Fatal("a parked workflow was marked completed")
		}
	})
}

// SQLQueue is what durable workflows need from a queue.
var (
	_ Settler = (*SQLQueue)(nil)
	_ Parker  = (*SQLQueue)(nil)
)
