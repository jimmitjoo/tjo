// Package ops is a self-hosted operations dashboard: errors, slow requests,
// slow queries, the job queue, cron and health, on a page you run yourself.
//
// OpenTelemetry has won as the wire format and this framework emits it. The
// unsolved part is that OTel is miserable for a small team: a collector to run,
// a backend to pay for, and a bill that is famously not small. This is the
// other end of that -- the twenty percent of observability that answers "is it
// broken and why", with no ingestion, no retention and nothing to pay for.
//
// It is not a competitor to anything. It is the thing that means a solo
// developer does not need a $200/month dependency to find out that the cron
// entry stopped firing three weeks ago.
//
// # Mounting it
//
//	recorder := ops.NewRecorder(0)
//	app.Logging.OTel.TracerProvider().RegisterSpanProcessor(recorder)
//
//	panel.AddPage(ops.Page(ops.Config{
//	    Recorder: recorder,
//	    Queues:   []*jobs.SQLQueue{queue},
//	    Health:   database.NewHealthChecker(db, 2*time.Second),
//	}))
//
// It is a page in the admin panel, which means it inherits the panel's
// authorizer and the panel's rule that an unauthenticated visitor gets 404.
// That is deliberate and it is the one hard requirement in the issue this
// implements: a dashboard that leaks slow queries and error messages to the
// internet is not a dashboard, it is a reconnaissance endpoint.
//
// # What it does not do
//
// No ingestion, no alerting, no long-term storage, nothing with a bill
// attached. The retained window is however many spans the Recorder holds, and
// the page says so rather than implying it has seen everything.
package ops

import (
	"context"
	"embed"
	"fmt"
	"html/template"
	"sort"
	"strings"
	"time"

	"github.com/jimmitjoo/tjo/admin"
	"github.com/jimmitjoo/tjo/database"
	"github.com/jimmitjoo/tjo/jobs"
	"go.opentelemetry.io/otel/trace"
)

//go:embed templates/*.html
var templateFiles embed.FS

var templates = template.Must(template.New("ops").Funcs(template.FuncMap{
	"duration": humanDuration,
	"ago":      ago,
	"stamp": func(t time.Time) string {
		if t.IsZero() {
			return "never"
		}
		return t.Local().Format("2006-01-02 15:04:05")
	},
}).ParseFS(templateFiles, "templates/*.html"))

// Config is what the dashboard has been given to look at.
//
// Every field is optional. A panel with nothing configured says so, per panel,
// rather than showing an empty table that looks like good news.
type Config struct {
	// Recorder supplies errors, slow requests and slow queries. Register it on
	// the tracer provider; see NewRecorder.
	Recorder *Recorder

	// Queues are the job queues to report on.
	Queues []*jobs.SQLQueue

	// Workflows shows which step a durable workflow is stuck on.
	Workflows *jobs.Workflows

	// Health is the database health check.
	Health *database.HealthChecker

	// Cron reports the last run of each scheduled job. See CronReporter.
	Cron CronReporter

	// Title overrides the page heading.
	Title string

	// Rows caps each table. Zero means DefaultRows.
	Rows int
}

// DefaultRows is how many entries each panel shows.
const DefaultRows = 10

// CronReporter reports scheduled-job runs.
//
// This package does not import the framework root, so it cannot name
// *tjo.BackgroundService. CronRun has the same fields as tjo.CronRun in the
// same order, which makes a direct struct conversion legal and the adapter one
// line:
//
//	Cron: ops.CronFunc(func() []ops.CronRun {
//	    runs := app.Background.CronStatus()
//	    out := make([]ops.CronRun, 0, len(runs))
//	    for _, r := range runs {
//	        out = append(out, ops.CronRun(r))
//	    }
//	    return out
//	}),
//
// Six lines in the application beats this package depending on the whole
// framework, and it keeps the dashboard usable from a program that is not a
// Tjo application at all.
type CronReporter interface {
	CronStatus() []CronRun
}

// CronFunc adapts a function.
type CronFunc func() []CronRun

func (f CronFunc) CronStatus() []CronRun { return f() }

// CronRun is the last run of a scheduled job. It mirrors tjo.CronRun field for
// field, deliberately.
type CronRun struct {
	Name      string
	LastRun   time.Time
	Duration  time.Duration
	Runs      int
	Failures  int
	LastError string
}

// Page returns the dashboard as an admin page.
//
// The permission to read it is ActionList and to press its buttons is
// ActionUpdate, so an operator who may look at the queue but not retry jobs is
// expressible without any configuration here.
func Page(cfg Config) admin.Page {
	title := cfg.Title
	if title == "" {
		title = "Ops"
	}

	return admin.Page{
		Path:  "ops",
		Title: title,
		Body: func(ctx admin.Context) (admin.Content, error) {
			return render(ctx, cfg)
		},
		Post:       func(ctx admin.Context) (string, error) { return handlePost(ctx, cfg) },
		Action:     admin.ActionList,
		PostAction: admin.ActionUpdate,
	}
}

// dashboard is what the template renders.
type dashboard struct {
	Window     string
	Errors     []ErrorGroup
	Requests   []Timing
	Queries    []Timing
	Queues     []queuePanel
	Stuck      []stuckWorkflow
	Cron       []CronRun
	Health     *database.HealthStatus
	HealthErr  string
	Notes      map[string]string
	FailedJobs []failedJob
}

type queuePanel struct {
	Name      string
	Waiting   int
	Running   int
	Failed    int
	Retrying  int
	Completed int
	Oldest    time.Duration
}

type failedJob struct {
	Queue     string
	ID        string
	Type      string
	Attempts  int
	LastError string
	UpdatedAt time.Time
}

type stuckWorkflow struct {
	JobID    string
	Step     string
	Status   string
	Attempts int
	Error    string
	Since    time.Time
}

func render(ctx admin.Context, cfg Config) (admin.Content, error) {
	rows := cfg.Rows
	if rows <= 0 {
		rows = DefaultRows
	}

	data := dashboard{Notes: map[string]string{}}

	// Errors, slow requests, slow queries.
	if cfg.Recorder == nil {
		note := "No recorder is configured. Register ops.NewRecorder on the tracer provider to see errors, slow requests and slow queries."
		data.Notes["errors"] = note
		data.Notes["requests"] = note
		data.Notes["queries"] = note
	} else {
		held, capacity := cfg.Recorder.Count()
		data.Window = fmt.Sprintf("last %d of %d spans", held, capacity)

		data.Errors = cfg.Recorder.Errors(rows)
		data.Requests = cfg.Recorder.SlowRequests(rows)
		data.Queries = cfg.Recorder.SlowQueries(rows)

		if held == 0 {
			note := "Nothing recorded yet. If this stays empty, tracing is not enabled -- see the otel module."
			data.Notes["errors"] = note
			data.Notes["requests"] = note
			data.Notes["queries"] = note
		}
		// Degrading honestly: an empty query panel means one of two very
		// different things, and saying which is the difference between a
		// dashboard and a puzzle.
		if len(data.Queries) == 0 && !cfg.Recorder.HasSpansOfKind(trace.SpanKindClient) {
			data.Notes["queries"] = "No database spans have been recorded. Wrap the pool with otel.WrapDB to see query timings; SQLite through a driver that is not wrapped will never appear here."
		}
	}

	// Job queues.
	if len(cfg.Queues) == 0 {
		data.Notes["queues"] = "No queues are configured."
	}
	for _, queue := range cfg.Queues {
		panel := queuePanel{
			Name:      queue.Name(),
			Waiting:   queue.Size(),
			Running:   queue.CountByStatus(jobs.JobStatusRunning),
			Failed:    queue.CountByStatus(jobs.JobStatusFailed),
			Retrying:  queue.CountByStatus(jobs.JobStatusRetrying),
			Completed: queue.CountByStatus(jobs.JobStatusCompleted),
		}
		if oldest, err := queue.Oldest(ctx); err == nil {
			panel.Oldest = oldest
		}
		data.Queues = append(data.Queues, panel)

		failed, err := queue.Recent(ctx, jobs.JobStatusFailed, rows)
		if err != nil {
			continue
		}
		for _, job := range failed {
			data.FailedJobs = append(data.FailedJobs, failedJob{
				Queue:     queue.Name(),
				ID:        job.ID,
				Type:      job.Type,
				Attempts:  job.Attempts,
				LastError: job.LastError,
				UpdatedAt: job.UpdatedAt,
			})
		}
	}

	// Durable workflows that are not finishing.
	if cfg.Workflows != nil {
		for _, job := range data.FailedJobs {
			steps, err := cfg.Workflows.Steps(ctx, job.ID)
			if err != nil || len(steps) == 0 {
				continue
			}
			last := steps[len(steps)-1]
			data.Stuck = append(data.Stuck, stuckWorkflow{
				JobID:    job.ID,
				Step:     last.Name,
				Status:   string(last.Status),
				Attempts: last.Attempts,
				Error:    last.LastError,
				Since:    last.UpdatedAt,
			})
		}
	}

	// Cron.
	if cfg.Cron == nil {
		data.Notes["cron"] = "No scheduler is configured."
	} else {
		data.Cron = cfg.Cron.CronStatus()
		if len(data.Cron) == 0 {
			data.Notes["cron"] = "Nothing is scheduled."
		}
		sort.Slice(data.Cron, func(i, j int) bool { return data.Cron[i].Name < data.Cron[j].Name })
	}

	// Health.
	if cfg.Health == nil {
		data.Notes["health"] = "No health checker is configured."
	} else {
		checked, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		data.Health = cfg.Health.Check(checked)
	}

	var out strings.Builder
	if err := templates.ExecuteTemplate(&out, "ops.html", data); err != nil {
		return "", err
	}
	return admin.Content(out.String()), nil
}

// handlePost runs a retry or a discard.
//
// The job id comes from the form, and every action names the queue it applies
// to, so a job id from one queue cannot be used to reach into another.
func handlePost(ctx admin.Context, cfg Config) (string, error) {
	form := ctx.Request.Form

	queueName := form.Get("queue")
	jobID := form.Get("job")

	var queue *jobs.SQLQueue
	for _, q := range cfg.Queues {
		if q.Name() == queueName {
			queue = q
			break
		}
	}
	if queue == nil || jobID == "" {
		return "", fmt.Errorf("ops: no such queue %q", queueName)
	}

	switch form.Get("action") {
	case "retry":
		return "", queue.Retry(ctx, jobID)
	case "discard":
		return "", queue.Discard(ctx, jobID)
	default:
		return "", fmt.Errorf("ops: unknown action")
	}
}

func humanDuration(d time.Duration) string {
	switch {
	case d == 0:
		return "—"
	case d < time.Millisecond:
		return fmt.Sprintf("%dµs", d.Microseconds())
	case d < time.Second:
		return fmt.Sprintf("%dms", d.Milliseconds())
	case d < time.Minute:
		return fmt.Sprintf("%.1fs", d.Seconds())
	case d < time.Hour:
		return fmt.Sprintf("%dm %ds", int(d.Minutes()), int(d.Seconds())%60)
	default:
		return fmt.Sprintf("%dh %dm", int(d.Hours()), int(d.Minutes())%60)
	}
}

func ago(t time.Time) string {
	if t.IsZero() {
		return "never"
	}
	return humanDuration(time.Since(t)) + " ago"
}
