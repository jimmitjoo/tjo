package jobs

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	_ "modernc.org/sqlite"
)

func sqliteQueue(t *testing.T) *SQLQueue {
	t.Helper()

	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "jobs.db"))
	if err != nil {
		t.Fatal(err)
	}
	// One writer, as SQLite wants; a symmetric pool serialises badly and the
	// claim below relies on write serialisation for its exclusivity.
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { db.Close() })

	q := NewSQLQueue(db, "default", DialectSQLite)
	if err := q.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	return q
}

func postgresQueue(t *testing.T) *SQLQueue {
	t.Helper()

	dsn := os.Getenv("TJO_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("TJO_TEST_POSTGRES_DSN is not set")
	}

	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Ping(); err != nil {
		t.Fatalf("ping: %v", err)
	}
	t.Cleanup(func() {
		db.Exec("DROP TABLE IF EXISTS tjo_jobs")
		db.Close()
	})

	q := NewSQLQueue(db, "default", DialectPostgres)
	if _, err := db.Exec("DROP TABLE IF EXISTS tjo_jobs"); err != nil {
		t.Fatal(err)
	}
	if err := q.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	return q
}

// TestPushTxRollbackDoesNotEnqueue is the whole reason this queue exists.
//
// An enqueue that opens its own connection cannot participate in the
// transaction that motivated the job, so rolling back the data leaves the job
// behind: the user was never created and the welcome email sends anyway. This
// asserts the insert dies with the transaction.
func TestPushTxRollbackDoesNotEnqueue(t *testing.T) {
	q := sqliteQueue(t)
	ctx := context.Background()

	tx, err := q.db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}

	job := NewJob("welcome-email", "default", map[string]any{"user": 1})
	if err := q.PushTx(ctx, tx, job); err != nil {
		t.Fatal(err)
	}

	if err := tx.Rollback(); err != nil {
		t.Fatal(err)
	}

	if n := q.Size(); n != 0 {
		t.Fatalf("%d jobs survived a rolled-back transaction", n)
	}
	if _, err := q.TryPop(ctx, "w1"); !errors.Is(err, ErrNoJob) {
		t.Fatalf("TryPop returned %v, want ErrNoJob", err)
	}
}

func TestPushTxCommitEnqueues(t *testing.T) {
	q := sqliteQueue(t)
	ctx := context.Background()

	tx, _ := q.db.BeginTx(ctx, nil)
	job := NewJob("welcome-email", "default", map[string]any{"user": 1})
	if err := q.PushTx(ctx, tx, job); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}

	got, err := q.TryPop(ctx, "w1")
	if err != nil {
		t.Fatalf("TryPop: %v", err)
	}
	if got.Type != "welcome-email" {
		t.Errorf("Type = %q", got.Type)
	}
	if got.Payload["user"] != float64(1) {
		t.Errorf("payload did not survive the round trip: %v", got.Payload)
	}
	if got.Attempts != 1 {
		t.Errorf("Attempts = %d, want 1 -- claiming counts as an attempt", got.Attempts)
	}
}

// Two workers must never receive the same job. On SQLite this rests on the
// claim being a single UPDATE serialised by the write lock rather than a SELECT
// followed by an UPDATE, which would let both see the row as claimable.
func TestConcurrentClaimsAreExclusive(t *testing.T) {
	q := sqliteQueue(t)
	ctx := context.Background()

	const jobs = 40
	for i := 0; i < jobs; i++ {
		if err := q.Push(NewJob("work", "default", map[string]any{"n": i})); err != nil {
			t.Fatal(err)
		}
	}

	var (
		mu     sync.Mutex
		claims = map[string]int{}
		wg     sync.WaitGroup
	)

	for w := 0; w < 8; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			for {
				job, err := q.TryPop(ctx, fmt.Sprintf("w%d", w))
				if errors.Is(err, ErrNoJob) {
					return
				}
				if err != nil {
					t.Errorf("TryPop: %v", err)
					return
				}
				mu.Lock()
				claims[job.ID]++
				mu.Unlock()
			}
		}(w)
	}
	wg.Wait()

	if len(claims) != jobs {
		t.Errorf("claimed %d distinct jobs, want %d", len(claims), jobs)
	}
	for id, n := range claims {
		if n > 1 {
			t.Errorf("job %s was claimed %d times", id, n)
		}
	}
}

// A worker that dies mid-job must not strand it. The lock timeout is what
// bounds that, and it is why jobs have to be idempotent.
func TestStaleClaimsBecomeAvailableAgain(t *testing.T) {
	q := sqliteQueue(t)
	q.LockTimeout = 50 * time.Millisecond
	ctx := context.Background()

	if err := q.Push(NewJob("work", "default", nil)); err != nil {
		t.Fatal(err)
	}

	first, err := q.TryPop(ctx, "doomed-worker")
	if err != nil {
		t.Fatal(err)
	}

	// Still claimed, so nobody else may take it.
	if _, err := q.TryPop(ctx, "other"); !errors.Is(err, ErrNoJob) {
		t.Fatalf("a claimed job was handed out again: %v", err)
	}

	time.Sleep(80 * time.Millisecond)

	second, err := q.TryPop(ctx, "recovering-worker")
	if err != nil {
		t.Fatalf("a job whose worker died was not recovered: %v", err)
	}
	if second.ID != first.ID {
		t.Errorf("recovered %s, want %s", second.ID, first.ID)
	}
	if second.Attempts != 2 {
		t.Errorf("Attempts = %d, want 2 -- recovery is a second attempt", second.Attempts)
	}
}

func TestFailRetriesThenGivesUp(t *testing.T) {
	q := sqliteQueue(t)
	ctx := context.Background()

	job := NewJob("work", "default", nil)
	job.MaxAttempts = 2
	if err := q.Push(job); err != nil {
		t.Fatal(err)
	}

	claimed, err := q.TryPop(ctx, "w1")
	if err != nil {
		t.Fatal(err)
	}

	if err := q.Fail(ctx, claimed, errors.New("downstream is down")); err != nil {
		t.Fatal(err)
	}
	if n := q.CountByStatus(JobStatusRetrying); n != 1 {
		t.Fatalf("retrying = %d, want 1", n)
	}

	// Backoff means it is not immediately claimable, which is what stops one
	// broken downstream becoming a denial of service against it.
	if _, err := q.TryPop(ctx, "w1"); !errors.Is(err, ErrNoJob) {
		t.Error("a job scheduled for retry was claimable immediately")
	}

	claimed.Attempts = claimed.MaxAttempts
	if err := q.Fail(ctx, claimed, errors.New("still down")); err != nil {
		t.Fatal(err)
	}
	if n := q.CountByStatus(JobStatusFailed); n != 1 {
		t.Errorf("failed = %d, want 1", n)
	}
}

func TestCompleteRemovesTheJobFromTheQueue(t *testing.T) {
	q := sqliteQueue(t)
	ctx := context.Background()

	if err := q.Push(NewJob("work", "default", nil)); err != nil {
		t.Fatal(err)
	}
	job, err := q.TryPop(ctx, "w1")
	if err != nil {
		t.Fatal(err)
	}
	if err := q.Complete(ctx, job.ID); err != nil {
		t.Fatal(err)
	}

	if n := q.Size(); n != 0 {
		t.Errorf("Size() = %d after completion, want 0", n)
	}
	if n := q.CountByStatus(JobStatusCompleted); n != 1 {
		t.Errorf("completed = %d, want 1", n)
	}
}

func TestScheduledJobsAreNotClaimedEarly(t *testing.T) {
	q := sqliteQueue(t)
	ctx := context.Background()

	job := NewJob("work", "default", nil).WithScheduledAt(time.Now().Add(time.Hour))
	if err := q.Push(job); err != nil {
		t.Fatal(err)
	}

	if _, err := q.TryPop(ctx, "w1"); !errors.Is(err, ErrNoJob) {
		t.Error("a job scheduled for the future was claimed now")
	}
}

// SQLQueue must satisfy the existing Queue interface, so it drops into the
// worker pool without changing it, and TxEnqueuer, which is the new capability.
var (
	_ Queue      = (*SQLQueue)(nil)
	_ TxEnqueuer = (*SQLQueue)(nil)
)

// The same guarantees on PostgreSQL, where exclusivity comes from
// FOR UPDATE SKIP LOCKED rather than from SQLite's single writer. Skips unless
// TJO_TEST_POSTGRES_DSN is set; CI sets it.
func TestPostgresClaimsAreExclusive(t *testing.T) {
	q := postgresQueue(t)
	ctx := context.Background()

	const jobs = 40
	for i := 0; i < jobs; i++ {
		if err := q.Push(NewJob("work", "default", map[string]any{"n": i})); err != nil {
			t.Fatal(err)
		}
	}

	var (
		mu     sync.Mutex
		claims = map[string]int{}
		wg     sync.WaitGroup
	)
	for w := 0; w < 8; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			for {
				job, err := q.TryPop(ctx, fmt.Sprintf("w%d", w))
				if errors.Is(err, ErrNoJob) {
					return
				}
				if err != nil {
					t.Errorf("TryPop: %v", err)
					return
				}
				mu.Lock()
				claims[job.ID]++
				mu.Unlock()
			}
		}(w)
	}
	wg.Wait()

	if len(claims) != jobs {
		t.Errorf("claimed %d distinct jobs, want %d", len(claims), jobs)
	}
	for id, n := range claims {
		if n > 1 {
			t.Errorf("job %s was claimed %d times", id, n)
		}
	}
}

func TestPostgresPushTxRollbackDoesNotEnqueue(t *testing.T) {
	q := postgresQueue(t)
	ctx := context.Background()

	tx, err := q.db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := q.PushTx(ctx, tx, NewJob("welcome-email", "default", nil)); err != nil {
		t.Fatal(err)
	}
	if err := tx.Rollback(); err != nil {
		t.Fatal(err)
	}

	if n := q.Size(); n != 0 {
		t.Fatalf("%d jobs survived a rolled-back transaction", n)
	}
}
