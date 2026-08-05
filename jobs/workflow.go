package jobs

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// Durable execution, on the queue that is already here.
//
// A job handler that calls an API, charges a card and sends a receipt has a
// problem the queue alone does not solve: if it dies after the charge, the
// retry charges again. The usual answers are a Temporal cluster or a second
// service. Neither is necessary when the application already has a database
// with the job rows in it.
//
// A step runs once. Its result is checkpointed, and a replay after a crash
// returns the stored result instead of running it again -- so a workflow that
// fails at step three re-runs step three and nothing before it.
//
// # Parking is not optional reading
//
// Sleep and WaitForEvent suspend the job by returning a *Parked error. The
// handler must return that error unchanged:
//
//	func handle(ctx context.Context, job *jobs.Job) error {
//	    wf := workflows.For(job)
//
//	    order, err := jobs.Step(ctx, wf, "charge", charge)
//	    if err != nil {
//	        return err          // including *Parked
//	    }
//	    ...
//	}
//
// A handler that swallows it -- logs the error and returns nil -- tells the
// worker the job succeeded, and the workflow is lost at whatever step it had
// reached. Returning it is the whole protocol.
//
// # What this needs from the queue
//
// A queue that implements Settler and Parker, which SQLQueue does. On a
// MemoryQueue a parked job is simply gone: Pop removed it and there is no row
// to reschedule. The worker logs that rather than losing it quietly.

// StepStatus is what a checkpoint row records.
type StepStatus string

const (
	// StepCompleted means the step ran and its result is stored. A replay
	// returns the stored result and does not run the function again.
	StepCompleted StepStatus = "completed"

	// StepFailed means the step ran and returned an error. It is kept for the
	// sake of seeing which step a workflow is stuck on; a replay re-runs it.
	StepFailed StepStatus = "failed"

	// StepSleeping means Sleep recorded a wake time that has not arrived.
	StepSleeping StepStatus = "sleeping"
)

// Parked is returned by a step that has suspended the job until a later time.
//
// It is not a failure and must not be handled as one: the handler returns it,
// the worker reschedules the job, and the workflow resumes from its last
// checkpoint. Attempts are not counted for a park, so a workflow may sleep more
// times than the job has retries.
type Parked struct {
	// Until is when the job should be picked up again. For a workflow waiting
	// on an event this is only a re-check; Notify wakes it immediately.
	Until time.Time

	// Why is a human-readable reason, shown in logs and the ops dashboard.
	Why string
}

func (p *Parked) Error() string {
	return fmt.Sprintf("jobs: workflow parked until %s (%s) -- return this error unchanged from your handler",
		p.Until.Format(time.RFC3339), p.Why)
}

// IsParked reports whether err is a park, and what it parked on.
func IsParked(err error) (*Parked, bool) {
	var p *Parked
	ok := errors.As(err, &p)
	return p, ok
}

// Workflows stores step checkpoints and workflow events.
//
// It uses the same database as the queue, and it must: the point of durable
// execution here is that a job row and its checkpoints are consistent with each
// other without a distributed transaction.
type Workflows struct {
	db      *sql.DB
	dialect Dialect

	// WaitPoll is how long a workflow parked on WaitForEvent sleeps before
	// re-checking. Notify wakes it as soon as the event arrives, so this is a
	// backstop against a notification that was never delivered, not the
	// mechanism. Zero means DefaultWaitPoll.
	WaitPoll time.Duration
}

// DefaultWaitPoll is the backstop re-check for a workflow waiting on an event.
// Long, because Notify is what actually wakes it.
const DefaultWaitPoll = 24 * time.Hour

// NewWorkflows returns a checkpoint store. Call Migrate once before use.
func NewWorkflows(db *sql.DB, dialect Dialect) *Workflows {
	return &Workflows{db: db, dialect: dialect}
}

// Workflow is the durable context of one job.
type Workflow struct {
	ws    *Workflows
	jobID string
}

// For returns the workflow context of a job.
func (ws *Workflows) For(job *Job) *Workflow { return &Workflow{ws: ws, jobID: job.ID} }

// ForJob is For, by ID, for callers that do not have the job in hand.
func (ws *Workflows) ForJob(jobID string) *Workflow { return &Workflow{ws: ws, jobID: jobID} }

// JobID is the job this workflow belongs to.
func (w *Workflow) JobID() string { return w.jobID }

func (ws *Workflows) waitPoll() time.Duration {
	if ws.WaitPoll <= 0 {
		return DefaultWaitPoll
	}
	return ws.WaitPoll
}

// Migrate creates the checkpoint tables if they do not exist.
func (ws *Workflows) Migrate(ctx context.Context) error {
	var stmts []string

	switch ws.dialect {
	case DialectPostgres:
		stmts = []string{
			`CREATE TABLE IF NOT EXISTS tjo_workflow_steps (
				job_id     TEXT NOT NULL,
				name       TEXT NOT NULL,
				status     TEXT NOT NULL,
				result     TEXT,
				attempts   INTEGER NOT NULL DEFAULT 0,
				last_error TEXT,
				updated_at TIMESTAMPTZ NOT NULL,
				PRIMARY KEY (job_id, name)
			)`,
			`CREATE TABLE IF NOT EXISTS tjo_workflow_events (
				job_id     TEXT NOT NULL,
				name       TEXT NOT NULL,
				payload    TEXT,
				created_at TIMESTAMPTZ NOT NULL,
				PRIMARY KEY (job_id, name)
			)`,
		}
	case DialectMySQL:
		stmts = []string{
			`CREATE TABLE IF NOT EXISTS tjo_workflow_steps (
				job_id     VARCHAR(191) NOT NULL,
				name       VARCHAR(191) NOT NULL,
				status     VARCHAR(32) NOT NULL,
				result     TEXT,
				attempts   INT NOT NULL DEFAULT 0,
				last_error TEXT,
				updated_at DATETIME NOT NULL,
				PRIMARY KEY (job_id, name)
			)`,
			`CREATE TABLE IF NOT EXISTS tjo_workflow_events (
				job_id     VARCHAR(191) NOT NULL,
				name       VARCHAR(191) NOT NULL,
				payload    TEXT,
				created_at DATETIME NOT NULL,
				PRIMARY KEY (job_id, name)
			)`,
		}
	default:
		stmts = []string{
			`CREATE TABLE IF NOT EXISTS tjo_workflow_steps (
				job_id     TEXT NOT NULL,
				name       TEXT NOT NULL,
				status     TEXT NOT NULL,
				result     TEXT,
				attempts   INTEGER NOT NULL DEFAULT 0,
				last_error TEXT,
				updated_at DATETIME NOT NULL,
				PRIMARY KEY (job_id, name)
			)`,
			`CREATE TABLE IF NOT EXISTS tjo_workflow_events (
				job_id     TEXT NOT NULL,
				name       TEXT NOT NULL,
				payload    TEXT,
				created_at DATETIME NOT NULL,
				PRIMARY KEY (job_id, name)
			)`,
		}
	}

	for _, s := range stmts {
		if _, err := ws.db.ExecContext(ctx, s); err != nil {
			return fmt.Errorf("jobs: migrate workflows: %w", err)
		}
	}
	return nil
}

// StepRecord is one checkpoint, for inspection.
type StepRecord struct {
	Name      string
	Status    StepStatus
	Attempts  int
	LastError string
	UpdatedAt time.Time
}

// Steps returns the checkpoints of a job, oldest first.
//
// This is what makes a stuck workflow diagnosable: it names the step it is on
// and how many times that step has been tried.
func (ws *Workflows) Steps(ctx context.Context, jobID string) ([]StepRecord, error) {
	rows, err := ws.db.QueryContext(ctx, rebind(ws.dialect,
		`SELECT name, status, attempts, last_error, updated_at FROM tjo_workflow_steps
		 WHERE job_id = ? ORDER BY updated_at ASC, name ASC`), jobID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []StepRecord
	for rows.Next() {
		var (
			r        StepRecord
			lastErr  sql.NullString
			statusIn string
		)
		if err := rows.Scan(&r.Name, &statusIn, &r.Attempts, &lastErr, &r.UpdatedAt); err != nil {
			return nil, err
		}
		r.Status = StepStatus(statusIn)
		r.LastError = lastErr.String
		out = append(out, r)
	}
	return out, rows.Err()
}

// Discard removes a job's checkpoints, so a re-run starts from the beginning.
func (ws *Workflows) Discard(ctx context.Context, jobID string) error {
	for _, table := range []string{"tjo_workflow_steps", "tjo_workflow_events"} {
		if _, err := ws.db.ExecContext(ctx, rebind(ws.dialect,
			`DELETE FROM `+table+` WHERE job_id = ?`), jobID); err != nil {
			return err
		}
	}
	return nil
}

// load reads a checkpoint. A missing row is ("", nil, nil).
func (w *Workflow) load(ctx context.Context, name string) (StepStatus, []byte, error) {
	var (
		status string
		result sql.NullString
	)
	err := w.ws.db.QueryRowContext(ctx, rebind(w.ws.dialect,
		`SELECT status, result FROM tjo_workflow_steps WHERE job_id = ? AND name = ?`),
		w.jobID, name).Scan(&status, &result)

	switch {
	case errors.Is(err, sql.ErrNoRows):
		return "", nil, nil
	case err != nil:
		return "", nil, fmt.Errorf("jobs: load step %q: %w", name, err)
	}

	return StepStatus(status), []byte(result.String), nil
}

// record writes a checkpoint, counting an attempt when the step failed.
func (w *Workflow) record(ctx context.Context, name string, status StepStatus, result []byte, cause error) error {
	var (
		res     any
		lastErr any
		bump    int
	)
	if result != nil {
		res = string(result)
	}
	if cause != nil {
		lastErr = cause.Error()
		bump = 1
	}

	now := time.Now().UTC()

	var q string
	switch w.ws.dialect {
	case DialectMySQL:
		q = `INSERT INTO tjo_workflow_steps (job_id, name, status, result, attempts, last_error, updated_at)
		     VALUES (?, ?, ?, ?, ?, ?, ?)
		     ON DUPLICATE KEY UPDATE
		       status = VALUES(status), result = VALUES(result),
		       attempts = attempts + ?, last_error = VALUES(last_error), updated_at = VALUES(updated_at)`
	default:
		q = `INSERT INTO tjo_workflow_steps (job_id, name, status, result, attempts, last_error, updated_at)
		     VALUES (?, ?, ?, ?, ?, ?, ?)
		     ON CONFLICT (job_id, name) DO UPDATE SET
		       status = excluded.status, result = excluded.result,
		       attempts = tjo_workflow_steps.attempts + ?, last_error = excluded.last_error,
		       updated_at = excluded.updated_at`
	}

	if _, err := w.ws.db.ExecContext(ctx, rebind(w.ws.dialect, q),
		w.jobID, name, string(status), res, bump, lastErr, now, bump); err != nil {
		return fmt.Errorf("jobs: checkpoint step %q: %w", name, err)
	}
	return nil
}

// Step runs fn once for this job and checkpoints its result.
//
// On a replay -- after a crash, a retry, or a park -- it returns the stored
// result without calling fn. That is what makes a multi-step job safe to retry:
// step three failing does not charge the card that step one already charged.
//
// The result is stored as JSON, so T has to survive a round trip through it.
// A step returning a value that does not marshal is an error at checkpoint
// time, deliberately: silently checkpointing a zero value would make the replay
// return something the first run never produced.
//
// If fn succeeds but the checkpoint cannot be written, the error is returned
// and the step will run again on the retry. This queue is at-least-once and so
// is this; a step doing something irreversible should be the last thing in its
// own job, or idempotent by its own key.
func Step[T any](ctx context.Context, w *Workflow, name string, fn func(context.Context) (T, error)) (T, error) {
	var zero T

	status, result, err := w.load(ctx, name)
	if err != nil {
		return zero, err
	}

	if status == StepCompleted {
		if len(result) == 0 {
			return zero, nil
		}
		var out T
		if err := json.Unmarshal(result, &out); err != nil {
			return zero, fmt.Errorf("jobs: step %q checkpoint does not fit its type: %w", name, err)
		}
		return out, nil
	}

	out, err := fn(ctx)
	if err != nil {
		// Recorded, not swallowed. The row is what tells an operator which step
		// a workflow is stuck on; the error still fails the job so the queue
		// retries it, and the retry resumes here rather than at the top.
		if recErr := w.record(ctx, name, StepFailed, nil, err); recErr != nil {
			return zero, errors.Join(err, recErr)
		}
		return zero, err
	}

	encoded, err := json.Marshal(out)
	if err != nil {
		return zero, fmt.Errorf("jobs: step %q result is not JSON: %w", name, err)
	}
	if err := w.record(ctx, name, StepCompleted, encoded, nil); err != nil {
		return zero, err
	}
	return out, nil
}

// Do is Step for a step with no result worth keeping.
func Do(ctx context.Context, w *Workflow, name string, fn func(context.Context) error) error {
	_, err := Step(ctx, w, name, func(ctx context.Context) (struct{}, error) {
		return struct{}{}, fn(ctx)
	})
	return err
}

// Sleep suspends the workflow for d, surviving a process restart.
//
// The wake time is checkpointed on the first call, so restarting the worker
// does not restart the sleep -- a workflow that sleeps for a day and is
// redeployed twice still wakes a day after it started, not a day after the last
// deploy.
//
// It returns *Parked while the wake time is in the future. Return that error
// from the handler.
func (w *Workflow) Sleep(ctx context.Context, name string, d time.Duration) error {
	status, result, err := w.load(ctx, name)
	if err != nil {
		return err
	}

	var wake time.Time

	switch status {
	case StepCompleted:
		return nil

	case StepSleeping:
		if err := json.Unmarshal(result, &wake); err != nil {
			return fmt.Errorf("jobs: sleep %q wake time: %w", name, err)
		}

	default:
		wake = time.Now().UTC().Add(d)
		encoded, err := json.Marshal(wake)
		if err != nil {
			return err
		}
		if err := w.record(ctx, name, StepSleeping, encoded, nil); err != nil {
			return err
		}
	}

	if time.Now().Before(wake) {
		return &Parked{Until: wake, Why: "sleeping in step " + name}
	}

	// Completed rather than left sleeping, so a later replay short-circuits on
	// the status instead of re-deriving it from a clock.
	encoded, err := json.Marshal(wake)
	if err != nil {
		return err
	}
	return w.record(ctx, name, StepCompleted, encoded, nil)
}

// WaitForEvent suspends the workflow until Notify delivers name.
//
// This is how a workflow parks on something outside itself -- a human approving
// an expense, a webhook confirming a payment -- for as long as it takes. The
// job is not running while it waits, so an approval that takes a week costs
// nothing but a row.
//
// It returns *Parked until the event arrives. Return that error from the
// handler.
func WaitForEvent[T any](ctx context.Context, w *Workflow, name string) (T, error) {
	var zero T

	payload, found, err := w.event(ctx, name)
	if err != nil {
		return zero, err
	}
	if !found {
		return zero, &Parked{
			Until: time.Now().UTC().Add(w.ws.waitPoll()),
			Why:   "waiting for event " + name,
		}
	}
	if len(payload) == 0 {
		return zero, nil
	}

	var out T
	if err := json.Unmarshal(payload, &out); err != nil {
		return zero, fmt.Errorf("jobs: event %q payload does not fit its type: %w", name, err)
	}
	return out, nil
}

// Await is WaitForEvent for an event with no payload.
func (w *Workflow) Await(ctx context.Context, name string) error {
	_, err := WaitForEvent[json.RawMessage](ctx, w, name)
	return err
}

func (w *Workflow) event(ctx context.Context, name string) ([]byte, bool, error) {
	var payload sql.NullString
	err := w.ws.db.QueryRowContext(ctx, rebind(w.ws.dialect,
		`SELECT payload FROM tjo_workflow_events WHERE job_id = ? AND name = ?`),
		w.jobID, name).Scan(&payload)

	switch {
	case errors.Is(err, sql.ErrNoRows):
		return nil, false, nil
	case err != nil:
		return nil, false, fmt.Errorf("jobs: load event %q: %w", name, err)
	}

	return []byte(payload.String), true, nil
}

// Notify delivers an event to a waiting workflow and wakes it.
//
// The first notification of a name wins; a second is ignored rather than
// overwriting it. An event is a checkpoint, and rewriting a checkpoint the
// workflow has already read changes history underneath a replay.
//
// Waking is a second statement rather than part of the insert because the job
// may not be parked yet -- Notify racing a workflow that has not reached its
// WaitForEvent is normal, and the event simply being there when it arrives is
// the correct outcome.
func (ws *Workflows) Notify(ctx context.Context, jobID, name string, payload any) error {
	var encoded any
	if payload != nil {
		b, err := json.Marshal(payload)
		if err != nil {
			return fmt.Errorf("jobs: event %q payload: %w", name, err)
		}
		encoded = string(b)
	}

	var q string
	switch ws.dialect {
	case DialectMySQL:
		q = `INSERT IGNORE INTO tjo_workflow_events (job_id, name, payload, created_at) VALUES (?, ?, ?, ?)`
	default:
		q = `INSERT INTO tjo_workflow_events (job_id, name, payload, created_at) VALUES (?, ?, ?, ?)
		     ON CONFLICT (job_id, name) DO NOTHING`
	}

	now := time.Now().UTC()
	if _, err := ws.db.ExecContext(ctx, rebind(ws.dialect, q), jobID, name, encoded, now); err != nil {
		return fmt.Errorf("jobs: deliver event %q: %w", name, err)
	}

	// Due now. A parked job is 'pending' with a scheduled_at in the future;
	// pulling that forward is all it takes to have it claimed on the next poll.
	if _, err := ws.db.ExecContext(ctx, rebind(ws.dialect,
		`UPDATE tjo_jobs SET scheduled_at = ?, updated_at = ?
		 WHERE id = ? AND status IN ('pending', 'retrying', 'scheduled')`),
		now, now, jobID); err != nil {
		return fmt.Errorf("jobs: wake job %s: %w", jobID, err)
	}
	return nil
}
