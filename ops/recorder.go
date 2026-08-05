package ops

import (
	"context"
	"sort"
	"strings"
	"sync"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
)

// The one thing this dashboard adds rather than reads.
//
// The issue this implements says every panel reads data the framework already
// collects, and that is true of the job queue, of cron, and of health -- those
// are rows and process state. It is not true of errors, slow requests and slow
// queries. The framework *emits* those as OpenTelemetry spans and exports them
// to a collector; nothing keeps them, so there is nothing to read back.
//
// So this is honest about being a small addition: a bounded ring buffer hung
// off the span pipeline that already exists. No new service, no new agent, no
// second instrumentation of anything -- the spans are already being produced
// and this looks at them on the way past.
//
// Bounded is the whole design. It holds a fixed number of finished spans and
// overwrites the oldest, so an application under load pays a known amount of
// memory rather than an increasing one. A dashboard that can exhaust the
// process it is reporting on is worse than no dashboard.

// DefaultCapacity is how many finished spans are kept. At roughly 200 bytes of
// retained summary each, a thousand is a fifth of a megabyte.
const DefaultCapacity = 1000

// span is the part of a finished span worth keeping.
//
// Not the span itself: attributes, events and links are the bulk of a span and
// none of them are shown here. Keeping the summary is what makes the memory
// cost predictable.
type span struct {
	Name     string
	Kind     trace.SpanKind
	Start    time.Time
	Duration time.Duration
	Failed   bool
	Message  string

	// Route is the http.route attribute when there is one, so "GET /users/{id}"
	// groups rather than "GET /users/1" through "GET /users/94000".
	Route string

	// Statement is the db.statement attribute, truncated.
	Statement string
}

// Recorder keeps the most recent finished spans.
//
// It implements sdktrace.SpanProcessor, so it is registered on the tracer
// provider the otel module already builds:
//
//	recorder := ops.NewRecorder(0)
//	provider.TracerProvider().RegisterSpanProcessor(recorder)
type Recorder struct {
	mu       sync.RWMutex
	spans    []span
	next     int
	full     bool
	capacity int
}

// NewRecorder returns a recorder holding capacity spans. Zero means
// DefaultCapacity.
func NewRecorder(capacity int) *Recorder {
	if capacity <= 0 {
		capacity = DefaultCapacity
	}
	return &Recorder{spans: make([]span, capacity), capacity: capacity}
}

// OnStart implements sdktrace.SpanProcessor and does nothing: a span is only
// interesting once it has a duration and an outcome.
func (r *Recorder) OnStart(context.Context, sdktrace.ReadWriteSpan) {}

// OnEnd records a finished span.
func (r *Recorder) OnEnd(s sdktrace.ReadOnlySpan) {
	summary := span{
		Name:     s.Name(),
		Kind:     s.SpanKind(),
		Start:    s.StartTime(),
		Duration: s.EndTime().Sub(s.StartTime()),
		Failed:   s.Status().Code == codes.Error,
		Message:  s.Status().Description,
	}

	for _, attr := range s.Attributes() {
		switch attr.Key {
		case "http.route", "http.target":
			if summary.Route == "" {
				summary.Route = attr.Value.Emit()
			}
		case "http.method", "http.request.method":
			summary.Route = attr.Value.Emit() + " " + summary.Route
		case "db.statement", "db.query.text":
			summary.Statement = truncate(attr.Value.Emit(), 240)
		case "http.status_code", "http.response.status_code":
			if attr.Value.Type() == attribute.INT64 && attr.Value.AsInt64() >= 500 {
				summary.Failed = true
			}
		}
	}

	if summary.Message == "" && summary.Failed {
		summary.Message = "no message"
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	r.spans[r.next] = summary
	r.next = (r.next + 1) % r.capacity
	if r.next == 0 {
		r.full = true
	}
}

// Shutdown implements sdktrace.SpanProcessor.
func (r *Recorder) Shutdown(context.Context) error { return nil }

// ForceFlush implements sdktrace.SpanProcessor. There is nothing to flush: the
// buffer is the destination.
func (r *Recorder) ForceFlush(context.Context) error { return nil }

// snapshot copies what is currently held.
func (r *Recorder) snapshot() []span {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if !r.full {
		return append([]span(nil), r.spans[:r.next]...)
	}

	out := make([]span, 0, r.capacity)
	out = append(out, r.spans[r.next:]...)
	out = append(out, r.spans[:r.next]...)
	return out
}

// Count reports how many spans are held, and the capacity.
//
// Shown on the dashboard, because "no errors in the last 1000 spans" and "no
// errors ever" are different claims and only one of them is true.
func (r *Recorder) Count() (held, capacity int) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if r.full {
		return r.capacity, r.capacity
	}
	return r.next, r.capacity
}

// ErrorGroup is one kind of error and how often it happened.
type ErrorGroup struct {
	Name    string
	Message string
	Count   int
	Last    time.Time
}

// Errors returns the failures, grouped rather than listed.
//
// Grouped because a list of five hundred identical timeouts is not information.
// The count and the last occurrence are.
func (r *Recorder) Errors(limit int) []ErrorGroup {
	groups := map[string]*ErrorGroup{}

	for _, s := range r.snapshot() {
		if !s.Failed {
			continue
		}

		name := s.Name
		if s.Route != "" {
			name = s.Route
		}

		key := name + "\x00" + s.Message
		group := groups[key]
		if group == nil {
			group = &ErrorGroup{Name: name, Message: s.Message}
			groups[key] = group
		}
		group.Count++
		if end := s.Start.Add(s.Duration); end.After(group.Last) {
			group.Last = end
		}
	}

	out := make([]ErrorGroup, 0, len(groups))
	for _, group := range groups {
		out = append(out, *group)
	}

	sort.Slice(out, func(i, j int) bool {
		if out[i].Count != out[j].Count {
			return out[i].Count > out[j].Count
		}
		return out[i].Last.After(out[j].Last)
	})

	return limitTo(out, limit)
}

// Timing is one slow operation.
type Timing struct {
	Name     string
	Detail   string
	Duration time.Duration
	At       time.Time
}

// SlowRequests returns the slowest HTTP spans held.
func (r *Recorder) SlowRequests(limit int) []Timing {
	var out []Timing

	for _, s := range r.snapshot() {
		if s.Kind != trace.SpanKindServer && s.Route == "" {
			continue
		}
		name := s.Route
		if name == "" {
			name = s.Name
		}
		out = append(out, Timing{Name: name, Duration: s.Duration, At: s.Start})
	}

	return slowest(out, limit)
}

// SlowQueries returns the slowest database spans held.
func (r *Recorder) SlowQueries(limit int) []Timing {
	var out []Timing

	for _, s := range r.snapshot() {
		if s.Kind != trace.SpanKindClient && !strings.HasPrefix(s.Name, "db.") {
			continue
		}
		out = append(out, Timing{Name: s.Name, Detail: s.Statement, Duration: s.Duration, At: s.Start})
	}

	return slowest(out, limit)
}

// HasSpansOfKind reports whether anything of a kind has been seen.
//
// The difference between "no slow queries" and "the database is not
// instrumented" is the difference between a green panel and a broken one, and
// the dashboard says which it is.
func (r *Recorder) HasSpansOfKind(kind trace.SpanKind) bool {
	for _, s := range r.snapshot() {
		if s.Kind == kind {
			return true
		}
	}
	return false
}

func slowest(timings []Timing, limit int) []Timing {
	sort.Slice(timings, func(i, j int) bool { return timings[i].Duration > timings[j].Duration })
	return limitTo(timings, limit)
}

func limitTo[T any](items []T, limit int) []T {
	if limit > 0 && len(items) > limit {
		return items[:limit]
	}
	return items
}

func truncate(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
