package jobs

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// ErrNoJob is returned by TryPop when the queue is empty.
var ErrNoJob = errors.New("jobs: no job available")

// Dialect selects the locking strategy and placeholder style.
type Dialect int

const (
	// DialectPostgres uses $1 placeholders and SELECT ... FOR UPDATE SKIP LOCKED.
	DialectPostgres Dialect = iota
	// DialectMySQL uses ? placeholders and SELECT ... FOR UPDATE SKIP LOCKED
	// (MySQL 8.0+ / MariaDB 10.6+).
	DialectMySQL
	// DialectSQLite uses ? placeholders and claim-by-update under the write
	// lock, because SQLite has no SKIP LOCKED.
	DialectSQLite
)

// TxEnqueuer is a Queue that can enqueue inside a caller's transaction.
//
// This is the entire reason SQLQueue exists. An Enqueue that opens its own
// connection cannot participate in the transaction that motivated the job, so
// the classic failure is available by construction: write the user, enqueue the
// welcome email, roll the transaction back, and the email still sends. Putting
// the insert in the same transaction as the data makes that impossible rather
// than unlikely.
type TxEnqueuer interface {
	Queue
	PushTx(ctx context.Context, tx *sql.Tx, job *Job) error
}

// SQLQueue stores jobs in the application's own database.
//
// The argument is not throughput. It is that a small team stops operating a
// second service for something the database it already runs can do, and that
// transactional enqueue removes a bug class rather than making it rarer.
type SQLQueue struct {
	db      *sql.DB
	name    string
	dialect Dialect

	// LockTimeout is how long a claimed job may stay claimed before another
	// worker may take it. It bounds recovery after a worker dies mid-job: too
	// short and a slow job runs twice, too long and a crash stalls the queue.
	// Jobs must therefore be idempotent, which is true of any at-least-once
	// queue and is worth saying out loud.
	LockTimeout time.Duration
}

// NewSQLQueue returns a queue backed by db. Call Migrate once before use.
func NewSQLQueue(db *sql.DB, name string, dialect Dialect) *SQLQueue {
	return &SQLQueue{db: db, name: name, dialect: dialect, LockTimeout: 5 * time.Minute}
}

func (q *SQLQueue) Name() string { return q.name }

// ph renders placeholder n, one-based.
func (q *SQLQueue) ph(n int) string {
	if q.dialect == DialectPostgres {
		return fmt.Sprintf("$%d", n)
	}
	return "?"
}

// rebind rewrites a query written with ? into the dialect's placeholders.
func (q *SQLQueue) rebind(query string) string {
	if q.dialect != DialectPostgres {
		return query
	}
	var b strings.Builder
	n := 0
	for _, r := range query {
		if r == '?' {
			n++
			fmt.Fprintf(&b, "$%d", n)
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

// Migrate creates the jobs table if it does not exist.
func (q *SQLQueue) Migrate(ctx context.Context) error {
	var stmts []string

	switch q.dialect {
	case DialectPostgres:
		stmts = []string{`CREATE TABLE IF NOT EXISTS tjo_jobs (
			id           TEXT PRIMARY KEY,
			queue        TEXT NOT NULL,
			type         TEXT NOT NULL,
			priority     INTEGER NOT NULL DEFAULT 0,
			payload      TEXT NOT NULL,
			status       TEXT NOT NULL,
			attempts     INTEGER NOT NULL DEFAULT 0,
			max_attempts INTEGER NOT NULL DEFAULT 3,
			scheduled_at TIMESTAMPTZ,
			locked_at    TIMESTAMPTZ,
			locked_by    TEXT,
			last_error   TEXT,
			created_at   TIMESTAMPTZ NOT NULL,
			updated_at   TIMESTAMPTZ NOT NULL
		)`}
	case DialectMySQL:
		stmts = []string{`CREATE TABLE IF NOT EXISTS tjo_jobs (
			id           VARCHAR(191) PRIMARY KEY,
			queue        VARCHAR(191) NOT NULL,
			type         VARCHAR(191) NOT NULL,
			priority     INT NOT NULL DEFAULT 0,
			payload      TEXT NOT NULL,
			status       VARCHAR(32) NOT NULL,
			attempts     INT NOT NULL DEFAULT 0,
			max_attempts INT NOT NULL DEFAULT 3,
			scheduled_at DATETIME NULL,
			locked_at    DATETIME NULL,
			locked_by    VARCHAR(191) NULL,
			last_error   TEXT NULL,
			created_at   DATETIME NOT NULL,
			updated_at   DATETIME NOT NULL
		)`}
	default:
		stmts = []string{`CREATE TABLE IF NOT EXISTS tjo_jobs (
			id           TEXT PRIMARY KEY,
			queue        TEXT NOT NULL,
			type         TEXT NOT NULL,
			priority     INTEGER NOT NULL DEFAULT 0,
			payload      TEXT NOT NULL,
			status       TEXT NOT NULL,
			attempts     INTEGER NOT NULL DEFAULT 0,
			max_attempts INTEGER NOT NULL DEFAULT 3,
			scheduled_at DATETIME,
			locked_at    DATETIME,
			locked_by    TEXT,
			last_error   TEXT,
			created_at   DATETIME NOT NULL,
			updated_at   DATETIME NOT NULL
		)`}
	}

	// Claiming orders by priority then age within a queue, so the index has to
	// match or every Pop is a table scan once the table is large.
	stmts = append(stmts,
		`CREATE INDEX IF NOT EXISTS tjo_jobs_claim ON tjo_jobs (queue, status, priority, created_at)`)

	for _, s := range stmts {
		if _, err := q.db.ExecContext(ctx, s); err != nil {
			return fmt.Errorf("jobs: migrate: %w", err)
		}
	}
	return nil
}

const insertJob = `INSERT INTO tjo_jobs
	(id, queue, type, priority, payload, status, attempts, max_attempts, scheduled_at, created_at, updated_at)
	VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`

func (q *SQLQueue) insertArgs(job *Job) ([]any, error) {
	payload, err := json.Marshal(job.Payload)
	if err != nil {
		return nil, err
	}

	status := job.Status
	if status == "" {
		status = JobStatusPending
	}

	return []any{
		job.ID, q.name, job.Type, int(job.Priority), string(payload),
		string(status), job.Attempts, job.MaxAttempts,
		job.ScheduledAt, job.CreatedAt, job.UpdatedAt,
	}, nil
}

// Push enqueues a job on its own connection.
//
// Prefer PushTx whenever the job exists because of a database write. This is
// for the cases where it genuinely does not.
func (q *SQLQueue) Push(job *Job) error {
	args, err := q.insertArgs(job)
	if err != nil {
		return err
	}
	_, err = q.db.Exec(q.rebind(insertJob), args...)
	return err
}

// PushTx enqueues a job inside the caller's transaction.
func (q *SQLQueue) PushTx(ctx context.Context, tx *sql.Tx, job *Job) error {
	args, err := q.insertArgs(job)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, q.rebind(insertJob), args...)
	return err
}

// TryPop claims one job, or returns ErrNoJob.
//
// Claiming is exclusive: two workers calling this concurrently never receive
// the same job. Postgres and MySQL get that from SELECT ... FOR UPDATE SKIP
// LOCKED. SQLite has no SKIP LOCKED, so the claim is an UPDATE that names the
// row it is taking and checks it actually took it -- serialised by SQLite's
// single writer, which is the same guarantee arrived at differently.
func (q *SQLQueue) TryPop(ctx context.Context, workerID string) (*Job, error) {
	now := time.Now().UTC()
	stale := now.Add(-q.LockTimeout)

	if q.dialect == DialectSQLite {
		return q.claimSQLite(ctx, workerID, now, stale)
	}
	return q.claimSkipLocked(ctx, workerID, now, stale)
}

// claimable is the WHERE clause shared by both strategies: this queue, due to
// run, and either waiting or abandoned.
//
// The second half of that disjunction is the recovery path and it is easy to
// leave out. A claimed job has status 'running', so a clause that only accepted
// 'pending' and 'retrying' would never see a job whose worker died -- it would
// sit at 'running' with a stale lock forever, and the queue would quietly lose
// one slot per crash.
const claimable = `queue = ?
	AND (scheduled_at IS NULL OR scheduled_at <= ?)
	AND (
		status IN ('pending', 'retrying')
		OR (status = 'running' AND locked_at IS NOT NULL AND locked_at < ?)
	)`

func (q *SQLQueue) claimSkipLocked(ctx context.Context, workerID string, now, stale time.Time) (*Job, error) {
	tx, err := q.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	row := tx.QueryRowContext(ctx, q.rebind(
		`SELECT id, type, payload, attempts, max_attempts FROM tjo_jobs
		 WHERE `+claimable+`
		 ORDER BY priority DESC, created_at ASC
		 LIMIT 1
		 FOR UPDATE SKIP LOCKED`), q.name, now, stale)

	job, err := q.scanClaim(row)
	if err != nil {
		return nil, err
	}

	if _, err := tx.ExecContext(ctx, q.rebind(
		`UPDATE tjo_jobs SET status = 'running', locked_at = ?, locked_by = ?, attempts = attempts + 1, updated_at = ?
		 WHERE id = ?`), now, workerID, now, job.ID); err != nil {
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}

	job.Attempts++
	job.Status = JobStatusRunning
	return job, nil
}

func (q *SQLQueue) claimSQLite(ctx context.Context, workerID string, now, stale time.Time) (*Job, error) {
	// One statement: pick the row and take it at the same time, so two callers
	// cannot both see it as claimable. SQLite serialises writers, so whichever
	// UPDATE runs second matches nothing.
	res, err := q.db.ExecContext(ctx,
		`UPDATE tjo_jobs
		 SET status = 'running', locked_at = ?, locked_by = ?, attempts = attempts + 1, updated_at = ?
		 WHERE id = (
			SELECT id FROM tjo_jobs
			WHERE `+claimable+`
			ORDER BY priority DESC, created_at ASC
			LIMIT 1
		 )`, now, workerID, now, q.name, now, stale)
	if err != nil {
		return nil, err
	}
	if n, err := res.RowsAffected(); err != nil || n == 0 {
		if err != nil {
			return nil, err
		}
		return nil, ErrNoJob
	}

	row := q.db.QueryRowContext(ctx,
		`SELECT id, type, payload, attempts, max_attempts FROM tjo_jobs
		 WHERE locked_by = ? AND status = 'running' ORDER BY locked_at DESC LIMIT 1`, workerID)

	job, err := q.scanClaim(row)
	if err != nil {
		return nil, err
	}
	job.Status = JobStatusRunning
	return job, nil
}

func (q *SQLQueue) scanClaim(row *sql.Row) (*Job, error) {
	var (
		id, jobType, payload string
		attempts, maxAtt     int
	)
	switch err := row.Scan(&id, &jobType, &payload, &attempts, &maxAtt); {
	case errors.Is(err, sql.ErrNoRows):
		return nil, ErrNoJob
	case err != nil:
		return nil, err
	}

	job := &Job{ID: id, Type: jobType, Queue: q.name, Attempts: attempts, MaxAttempts: maxAtt}
	if err := json.Unmarshal([]byte(payload), &job.Payload); err != nil {
		return nil, fmt.Errorf("jobs: payload of %s: %w", id, err)
	}
	return job, nil
}

// Complete marks a claimed job done.
func (q *SQLQueue) Complete(ctx context.Context, jobID string) error {
	_, err := q.db.ExecContext(ctx, q.rebind(
		`UPDATE tjo_jobs SET status = 'completed', locked_at = NULL, locked_by = NULL, updated_at = ? WHERE id = ?`),
		time.Now().UTC(), jobID)
	return err
}

// Fail records a failure, scheduling a retry while attempts remain and moving
// the job to failed once they do not.
//
// Backoff is exponential from one second, capped at an hour. Retrying
// immediately is how a queue turns one broken downstream into a denial of
// service against it.
func (q *SQLQueue) Fail(ctx context.Context, job *Job, cause error) error {
	now := time.Now().UTC()

	if job.Attempts >= job.MaxAttempts {
		_, err := q.db.ExecContext(ctx, q.rebind(
			`UPDATE tjo_jobs SET status = 'failed', locked_at = NULL, locked_by = NULL, last_error = ?, updated_at = ? WHERE id = ?`),
			cause.Error(), now, job.ID)
		return err
	}

	backoff := time.Duration(1<<uint(min(job.Attempts, 12))) * time.Second
	if backoff > time.Hour {
		backoff = time.Hour
	}
	next := now.Add(backoff)

	_, err := q.db.ExecContext(ctx, q.rebind(
		`UPDATE tjo_jobs SET status = 'retrying', locked_at = NULL, locked_by = NULL,
		 scheduled_at = ?, last_error = ?, updated_at = ? WHERE id = ?`),
		next, cause.Error(), now, job.ID)
	return err
}

// Size reports how many jobs are waiting to run.
func (q *SQLQueue) Size() int {
	var n int
	_ = q.db.QueryRow(q.rebind(
		`SELECT COUNT(*) FROM tjo_jobs WHERE queue = ? AND status IN ('pending', 'retrying')`), q.name).Scan(&n)
	return n
}

// CountByStatus reports how many jobs are in the given state.
func (q *SQLQueue) CountByStatus(status JobStatus) int {
	var n int
	_ = q.db.QueryRow(q.rebind(
		`SELECT COUNT(*) FROM tjo_jobs WHERE queue = ? AND status = ?`), q.name, string(status)).Scan(&n)
	return n
}

// Clear removes every job on this queue.
func (q *SQLQueue) Clear() error {
	_, err := q.db.Exec(q.rebind(`DELETE FROM tjo_jobs WHERE queue = ?`), q.name)
	return err
}

// Pop blocks until a job is available or ctx is done, satisfying Queue.
//
// Polling rather than listening: LISTEN/NOTIFY is Postgres-only and this has to
// work on three databases. A second of latency on a background job is not worth
// a dialect-specific code path.
func (q *SQLQueue) Pop(ctx context.Context) (*Job, error) {
	t := time.NewTicker(time.Second)
	defer t.Stop()

	for {
		job, err := q.TryPop(ctx, "pop")
		if err == nil {
			return job, nil
		}
		if !errors.Is(err, ErrNoJob) {
			return nil, err
		}

		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-t.C:
		}
	}
}

// Peek returns the next job without claiming it.
func (q *SQLQueue) Peek() (*Job, error) {
	now := time.Now().UTC()
	row := q.db.QueryRow(q.rebind(
		`SELECT id, type, payload, attempts, max_attempts FROM tjo_jobs
		 WHERE queue = ? AND status IN ('pending', 'retrying')
		   AND (scheduled_at IS NULL OR scheduled_at <= ?)
		 ORDER BY priority DESC, created_at ASC LIMIT 1`), q.name, now)
	return q.scanClaim(row)
}
