// Package sse writes Server-Sent Events.
//
// SSE is the transport every major LLM API streams over, the one htmx 4 moves
// into core, and Datastar's native wire format. All three consume the same
// thing -- an HTML fragment, or a token, pushed from the server -- so this
// package deliberately implements the wire format rather than integrating with
// any of them. htmx 4 is still in beta with a breaking change to attribute
// inheritance, and Datastar v1 is months old; picking one would be betting on
// an unsettled market to avoid writing eighty lines.
//
// # HTTP/2 is not optional
//
// Under HTTP/1.1 browsers cap concurrent connections to an origin at six, and
// an SSE response never completes, so six open streams deadlock every other
// request to that origin -- images, stylesheets, form posts. Under HTTP/2 a
// single connection multiplexes hundreds of streams and the problem disappears.
//
// Serve this behind HTTP/2, either directly (Go 1.24's Server.Protocols
// includes UnencryptedHTTP2 for h2c without a third-party wrapper) or via a
// proxy that speaks it. Everything else here assumes you did.
//
// # No resumability, deliberately
//
// There is no Last-Event-ID handling. EventSource's reconnection resumes the
// *transport*, not whatever was producing the events, so a client that
// reconnects mid-generation gets a stream that starts over or silently skips.
// MCP removed SSE resumability from its specification in the 2026-07-28
// revision for the same reason. If a stream needs to survive disconnection, the
// thing to make durable is the session, not the connection.
package sse

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// ErrNotFlushable is returned when the ResponseWriter cannot flush, which means
// events would sit in a buffer instead of reaching the client.
var ErrNotFlushable = errors.New("sse: response writer does not support flushing")

// Event is one Server-Sent Event.
//
// Retry is the reconnection delay the browser should use. Leave it zero unless
// you have a reason; the default is generally fine and setting it per event is
// noise on the wire.
type Event struct {
	// Name maps to the SSE "event:" field. Empty means the default "message"
	// event, which is what EventSource.onmessage receives.
	Name string

	// Data is the payload. Newlines are split across multiple data: lines per
	// the specification and rejoined by the client.
	Data string

	// ID maps to "id:". Setting it makes the browser send Last-Event-ID on
	// reconnect, which this package does not act on -- see the package comment.
	ID string

	Retry time.Duration
}

// Stream writes events to a single client.
//
// Not safe for concurrent use. One goroutine owns the stream; if several
// producers need to write, funnel them through a channel.
type Stream struct {
	w    http.ResponseWriter
	rc   *http.ResponseController
	ctx  context.Context
	sent int
}

// New prepares w for streaming and writes the response headers.
//
// It returns ErrNotFlushable if the writer cannot flush -- usually a middleware
// wrapping the ResponseWriter without implementing http.Flusher, which is worth
// failing on loudly rather than discovering as a stream that never arrives.
func New(w http.ResponseWriter, r *http.Request) (*Stream, error) {
	rc := http.NewResponseController(w)

	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-cache")
	h.Set("Connection", "keep-alive")
	// Nginx buffers proxied responses by default, which holds events until the
	// buffer fills and makes a working stream look broken.
	h.Set("X-Accel-Buffering", "no")

	w.WriteHeader(http.StatusOK)

	// The flush both probes for flushability and pushes the headers to the
	// client, which is why it comes after they are set rather than before.
	// Probing first sent an empty header block and left Content-Type unset --
	// caught by the end-to-end test, and invisible to the recorder-based ones,
	// because httptest.ResponseRecorder does not care when headers are written.
	if err := rc.Flush(); err != nil {
		if errors.Is(err, http.ErrNotSupported) {
			return nil, ErrNotFlushable
		}
		return nil, err
	}

	return &Stream{w: w, rc: rc, ctx: r.Context()}, nil
}

// Send writes one event and flushes it.
//
// It returns the request context's error once the client has gone away, so a
// producer loop that checks Send's return value stops on disconnect without
// needing its own select. That matters for more than tidiness: an abandoned
// stream that keeps generating LLM tokens is billed for tokens nobody reads.
func (s *Stream) Send(e Event) error {
	if err := s.ctx.Err(); err != nil {
		return err
	}

	var b strings.Builder

	if e.ID != "" {
		fmt.Fprintf(&b, "id: %s\n", sanitizeField(e.ID))
	}
	if e.Name != "" {
		fmt.Fprintf(&b, "event: %s\n", sanitizeField(e.Name))
	}
	if e.Retry > 0 {
		fmt.Fprintf(&b, "retry: %d\n", e.Retry.Milliseconds())
	}

	// A payload containing a newline has to be split, or everything after the
	// first one is parsed as a new field and silently lost.
	for _, line := range strings.Split(e.Data, "\n") {
		fmt.Fprintf(&b, "data: %s\n", strings.TrimSuffix(line, "\r"))
	}
	b.WriteString("\n")

	if _, err := io.WriteString(s.w, b.String()); err != nil {
		return err
	}
	if err := s.rc.Flush(); err != nil {
		return err
	}

	s.sent++
	return nil
}

// Patch sends an HTML fragment.
//
// This is the shape htmx and Datastar both consume: the server decides what the
// new markup is and pushes it, the client swaps it in. Named "patch" rather
// than "html" because that is the concept, and because whichever client library
// is in fashion will keep calling it that.
func (s *Stream) Patch(name, fragment string) error {
	return s.Send(Event{Name: name, Data: fragment})
}

// Comment sends an SSE comment, which clients ignore.
//
// Useful as a keep-alive: an idle connection through a proxy with an idle
// timeout is closed without either end noticing until the next write fails.
func (s *Stream) Comment(text string) error {
	if err := s.ctx.Err(); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(s.w, ": %s\n\n", sanitizeField(text)); err != nil {
		return err
	}
	return s.rc.Flush()
}

// KeepAlive sends a comment every interval until the client disconnects or ctx
// is done. Run it in its own goroutine only if nothing else writes to the
// stream; Stream is not safe for concurrent use.
func (s *Stream) KeepAlive(interval time.Duration) error {
	t := time.NewTicker(interval)
	defer t.Stop()

	for {
		select {
		case <-s.ctx.Done():
			return s.ctx.Err()
		case <-t.C:
			if err := s.Comment("keep-alive"); err != nil {
				return err
			}
		}
	}
}

// SetWriteDeadline bounds how long a single write may block.
//
// Without one, a client that reads slower than the server produces eventually
// blocks the handler's goroutine indefinitely, which is how a stream endpoint
// becomes a way to exhaust a server's goroutines.
func (s *Stream) SetWriteDeadline(t time.Time) error {
	return s.rc.SetWriteDeadline(t)
}

// Sent reports how many events have been written.
func (s *Stream) Sent() int { return s.sent }

// Context is the request's context, done when the client goes away.
func (s *Stream) Context() context.Context { return s.ctx }

// sanitizeField strips newlines from a single-line field.
//
// An id, event name or comment carrying a newline would end the field and let
// the remainder be parsed as another one -- response splitting, at the SSE
// layer rather than the HTTP layer. Values reaching these fields are usually
// application-controlled, which is exactly the assumption worth not making.
func sanitizeField(v string) string {
	return strings.NewReplacer("\n", " ", "\r", " ").Replace(v)
}
