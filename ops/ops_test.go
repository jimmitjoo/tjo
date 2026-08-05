package ops

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jimmitjoo/tjo/admin"
	"github.com/jimmitjoo/tjo/database"
	"github.com/jimmitjoo/tjo/jobs"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
	_ "modernc.org/sqlite"
)

func testDB(t *testing.T) *sql.DB {
	t.Helper()

	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "ops.db"))
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { db.Close() })
	return db
}

func testQueue(t *testing.T, db *sql.DB) *jobs.SQLQueue {
	t.Helper()

	q := jobs.NewSQLQueue(db, "default", jobs.DialectSQLite)
	if err := q.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	return q
}

// mount puts the dashboard behind an admin panel, which is how it is used.
func mount(t *testing.T, cfg Config, authorizer admin.Authorizer) http.Handler {
	t.Helper()

	panel := admin.New(admin.Config{Authorizer: authorizer, Title: "Ops Test"})
	panel.AddPage(Page(cfg))
	if err := panel.Err(); err != nil {
		t.Fatal(err)
	}
	return panel.Handler("/_admin")
}

func get(t *testing.T, h http.Handler, path string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", path, nil))
	return rec
}

// The one hard requirement: the dashboard is not reachable without the role.
//
// A page listing slow queries and error messages is a description of the
// application's internals; served to anyone who finds the URL, it is
// reconnaissance rather than operations.
func TestTheDashboardIsNotReachableWithoutTheRole(t *testing.T) {
	db := testDB(t)
	cfg := Config{Queues: []*jobs.SQLQueue{testQueue(t, db)}}

	anonymous := mount(t, cfg, admin.DenyAll)
	if code := get(t, anonymous, "/p/ops").Code; code != http.StatusNotFound {
		t.Errorf("anonymous: %d, want 404 -- the endpoint's existence was confirmed", code)
	}

	// A known account without the permission is told no, rather than told
	// nothing: it already knows the page is there.
	known := mount(t, cfg, admin.AuthorizerFunc(func(admin.Context, admin.Query) error {
		return admin.ErrForbidden
	}))
	if code := get(t, known, "/p/ops").Code; code != http.StatusForbidden {
		t.Errorf("known account without the role: %d, want 403", code)
	}

	permitted := mount(t, cfg, admin.AllowAll)
	if code := get(t, permitted, "/p/ops").Code; code != http.StatusOK {
		t.Errorf("permitted: %d, want 200", code)
	}
}

// Reading the dashboard and pressing its buttons are different permissions.
func TestRetryingAJobNeedsMoreThanReading(t *testing.T) {
	db := testDB(t)
	queue := testQueue(t, db)

	job := jobs.NewJob("send-email", "default", nil)
	job.MaxAttempts = 1
	if err := queue.Push(job); err != nil {
		t.Fatal(err)
	}
	claimed, err := queue.TryPop(context.Background(), "w")
	if err != nil {
		t.Fatal(err)
	}
	if err := queue.Fail(context.Background(), claimed, errors.New("mail server refused")); err != nil {
		t.Fatal(err)
	}

	readOnly := admin.AuthorizerFunc(func(ctx admin.Context, q admin.Query) error {
		if q.Action == admin.ActionList {
			return nil
		}
		return admin.ErrForbidden
	})

	h := mount(t, Config{Queues: []*jobs.SQLQueue{queue}}, readOnly)

	// The failed job is visible.
	body := get(t, h, "/p/ops").Body.String()
	if !strings.Contains(body, "mail server refused") {
		t.Fatalf("the failed job is not shown:\n%s", body)
	}

	refused := post(t, h, "/p/ops", url.Values{"queue": {"default"}, "job": {job.ID}, "action": {"retry"}})
	if refused.Code != http.StatusForbidden {
		t.Fatalf("retry with read-only permission: %d, want 403", refused.Code)
	}
	if queue.CountByStatus(jobs.JobStatusFailed) != 1 {
		t.Fatal("the refused retry ran anyway")
	}
}

func post(t *testing.T, h http.Handler, path string, form url.Values) *httptest.ResponseRecorder {
	t.Helper()

	r := httptest.NewRequest("POST", path, strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.Header.Set("Sec-Fetch-Site", "same-origin")

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)
	return rec
}

func TestRetryAndDiscard(t *testing.T) {
	db := testDB(t)
	queue := testQueue(t, db)
	ctx := context.Background()

	fail := func(jobType string) *jobs.Job {
		job := jobs.NewJob(jobType, "default", nil)
		job.MaxAttempts = 1
		if err := queue.Push(job); err != nil {
			t.Fatal(err)
		}
		claimed, err := queue.TryPop(ctx, "w")
		if err != nil {
			t.Fatal(err)
		}
		if err := queue.Fail(ctx, claimed, errors.New("boom")); err != nil {
			t.Fatal(err)
		}
		return job
	}

	retried := fail("retry-me")
	discarded := fail("discard-me")

	h := mount(t, Config{Queues: []*jobs.SQLQueue{queue}}, admin.AllowAll)

	if code := post(t, h, "/p/ops", url.Values{
		"queue": {"default"}, "job": {retried.ID}, "action": {"retry"},
	}).Code; code != http.StatusSeeOther {
		t.Fatalf("retry: %d", code)
	}

	if code := post(t, h, "/p/ops", url.Values{
		"queue": {"default"}, "job": {discarded.ID}, "action": {"discard"},
	}).Code; code != http.StatusSeeOther {
		t.Fatalf("discard: %d", code)
	}

	if got := queue.CountByStatus(jobs.JobStatusPending); got != 1 {
		t.Fatalf("%d pending jobs after a retry, want 1", got)
	}
	if got := queue.CountByStatus(jobs.JobStatusFailed); got != 0 {
		t.Fatalf("%d failed jobs after retrying one and discarding the other, want 0", got)
	}

	// The retried job is due now, and has its attempts back.
	claimed, err := queue.TryPop(ctx, "w")
	if err != nil {
		t.Fatalf("the retried job is not claimable: %v", err)
	}
	if claimed.ID != retried.ID {
		t.Fatalf("claimed %s, want the retried job", claimed.ID)
	}
}

// A job id from one queue must not reach into another.
func TestAnActionCannotCrossQueues(t *testing.T) {
	db := testDB(t)
	queue := testQueue(t, db)

	h := mount(t, Config{Queues: []*jobs.SQLQueue{queue}}, admin.AllowAll)

	out := post(t, h, "/p/ops", url.Values{
		"queue": {"some-other-queue"}, "job": {"1"}, "action": {"retry"},
	})
	if out.Code == http.StatusSeeOther {
		t.Fatal("an action against an unconfigured queue was accepted")
	}
}

// Errors are grouped and counted, not listed. Five hundred identical timeouts
// are one line with a number on it.
func TestErrorsAreGrouped(t *testing.T) {
	recorder := NewRecorder(100)

	for range 5 {
		recorder.OnEnd(fakeSpan{
			name:    "GET /checkout",
			kind:    trace.SpanKindServer,
			dur:     20 * time.Millisecond,
			failed:  true,
			message: "upstream timeout",
		})
	}
	recorder.OnEnd(fakeSpan{name: "GET /health", kind: trace.SpanKindServer, dur: time.Millisecond})

	groups := recorder.Errors(10)
	if len(groups) != 1 {
		t.Fatalf("%d groups, want 1: %+v", len(groups), groups)
	}
	if groups[0].Count != 5 {
		t.Fatalf("count = %d, want 5", groups[0].Count)
	}
	if groups[0].Message != "upstream timeout" {
		t.Fatalf("message = %q", groups[0].Message)
	}
}

// The buffer is bounded. A dashboard that can exhaust the process it reports on
// is worse than no dashboard.
func TestTheRecorderIsBounded(t *testing.T) {
	recorder := NewRecorder(10)

	for i := range 1000 {
		recorder.OnEnd(fakeSpan{name: "span", kind: trace.SpanKindServer, dur: time.Duration(i) * time.Millisecond})
	}

	held, capacity := recorder.Count()
	if held != 10 || capacity != 10 {
		t.Fatalf("held %d of %d, want 10 of 10", held, capacity)
	}

	// And it kept the most recent, not the first ten.
	slow := recorder.SlowRequests(1)
	if len(slow) != 1 || slow[0].Duration != 999*time.Millisecond {
		t.Fatalf("slowest = %v, want the most recent span", slow)
	}
}

// An empty panel means one of two very different things, and the page says
// which. This is what "degrade honestly" means on SQLite, where a driver that
// is not wrapped produces no query spans at all.
func TestAnEmptyQueryPanelSaysWhy(t *testing.T) {
	recorder := NewRecorder(10)
	recorder.OnEnd(fakeSpan{name: "GET /", kind: trace.SpanKindServer, dur: time.Millisecond})

	h := mount(t, Config{Recorder: recorder}, admin.AllowAll)
	body := get(t, h, "/p/ops").Body.String()

	if !strings.Contains(body, "otel.WrapDB") {
		t.Fatalf("the empty query panel does not say why it is empty:\n%s", body)
	}
	if !strings.Contains(body, "last 1 of 10 spans") {
		t.Fatal("the page does not say how big the window is")
	}
}

func TestEveryPanelSaysWhenItIsNotConfigured(t *testing.T) {
	h := mount(t, Config{}, admin.AllowAll)
	body := get(t, h, "/p/ops").Body.String()

	for _, want := range []string{
		"No recorder is configured",
		"No queues are configured",
		"No scheduler is configured",
		"No health checker is configured",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("the page does not say %q", want)
		}
	}
}

func TestCronStatusIsShown(t *testing.T) {
	cron := CronFunc(func() []CronRun {
		return []CronRun{
			{Name: "nightly-report", LastRun: time.Now().Add(-2 * time.Hour), Duration: 1500 * time.Millisecond, Runs: 12},
			{Name: "prune-tokens", Runs: 0},
		}
	})

	h := mount(t, Config{Cron: cron}, admin.AllowAll)
	body := get(t, h, "/p/ops").Body.String()

	if !strings.Contains(body, "nightly-report") || !strings.Contains(body, "prune-tokens") {
		t.Fatal("the scheduled jobs are not listed")
	}
	// A job that has never run says so rather than showing a zero timestamp.
	if !strings.Contains(body, "never") {
		t.Fatal("a job that has never run does not say so")
	}
}

func TestHealthIsShown(t *testing.T) {
	db := testDB(t)
	checker := database.NewHealthChecker(db, 2*time.Second)

	h := mount(t, Config{Health: checker}, admin.AllowAll)
	body := get(t, h, "/p/ops").Body.String()

	if !strings.Contains(body, "Health") || !strings.Contains(body, "Connections") {
		t.Fatalf("the health panel is missing:\n%s", body)
	}
}

// spanEpoch is a fixed "now" for the fake spans.
var spanEpoch = time.Now()

// fakeSpan is the smallest thing that satisfies what the recorder reads.
type fakeSpan struct {
	sdktrace.ReadOnlySpan

	name    string
	kind    trace.SpanKind
	dur     time.Duration
	failed  bool
	message string
	attrs   []attribute.KeyValue
}

func (s fakeSpan) Name() string             { return s.name }
func (s fakeSpan) SpanKind() trace.SpanKind { return s.kind }

// Fixed, so the duration is exactly what the test asked for. Computing both
// ends from time.Now() gives a duration a few hundred microseconds short, which
// is invisible until a test compares one.
func (s fakeSpan) StartTime() time.Time             { return spanEpoch.Add(-s.dur) }
func (s fakeSpan) EndTime() time.Time               { return spanEpoch }
func (s fakeSpan) Attributes() []attribute.KeyValue { return s.attrs }

func (s fakeSpan) Status() sdktrace.Status {
	if s.failed {
		return sdktrace.Status{Code: codes.Error, Description: s.message}
	}
	return sdktrace.Status{Code: codes.Ok}
}
