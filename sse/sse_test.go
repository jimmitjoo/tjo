package sse

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestSendWritesTheWireFormat(t *testing.T) {
	tests := []struct {
		name  string
		event Event
		want  string
	}{
		{
			name:  "data only is the default message event",
			event: Event{Data: "hello"},
			want:  "data: hello\n\n",
		},
		{
			name:  "named event",
			event: Event{Name: "patch", Data: "<p>hi</p>"},
			want:  "event: patch\ndata: <p>hi</p>\n\n",
		},
		{
			name:  "id and retry",
			event: Event{ID: "7", Data: "x", Retry: 3 * time.Second},
			want:  "id: 7\nretry: 3000\ndata: x\n\n",
		},
		{
			// Everything after the first newline would otherwise be parsed as a
			// new field and silently dropped.
			name:  "multi-line data is split across data: lines",
			event: Event{Data: "line one\nline two"},
			want:  "data: line one\ndata: line two\n\n",
		},
		{
			name:  "CRLF payloads do not emit stray carriage returns",
			event: Event{Data: "a\r\nb"},
			want:  "data: a\ndata: b\n\n",
		},
		{
			// Response splitting at the SSE layer: a newline in a single-line
			// field ends it and lets the rest be read as another field.
			name:  "newlines are stripped from single-line fields",
			event: Event{Name: "evt\ndata: injected", Data: "ok"},
			want:  "event: evt data: injected\ndata: ok\n\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			s, err := New(rec, httptest.NewRequest("GET", "/", nil))
			if err != nil {
				t.Fatal(err)
			}

			if err := s.Send(tt.event); err != nil {
				t.Fatal(err)
			}

			if got := rec.Body.String(); got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestNewSetsStreamingHeaders(t *testing.T) {
	rec := httptest.NewRecorder()
	if _, err := New(rec, httptest.NewRequest("GET", "/", nil)); err != nil {
		t.Fatal(err)
	}

	want := map[string]string{
		"Content-Type":  "text/event-stream",
		"Cache-Control": "no-cache",
		// Without this nginx buffers the response and holds events until the
		// buffer fills, which makes a working stream look broken.
		"X-Accel-Buffering": "no",
	}
	for k, v := range want {
		if got := rec.Header().Get(k); got != v {
			t.Errorf("%s = %q, want %q", k, got, v)
		}
	}
}

// A producer loop that only checks Send's error must stop when the client goes
// away. Without this, an abandoned stream keeps generating -- for an LLM
// endpoint, that is billed tokens nobody will read.
func TestSendStopsWhenTheClientDisconnects(t *testing.T) {
	req := httptest.NewRequest("GET", "/", nil)
	ctx, cancel := context.WithCancel(req.Context())
	req = req.WithContext(ctx)

	rec := httptest.NewRecorder()
	s, err := New(rec, req)
	if err != nil {
		t.Fatal(err)
	}

	if err := s.Send(Event{Data: "first"}); err != nil {
		t.Fatalf("first send: %v", err)
	}

	cancel()

	if err := s.Send(Event{Data: "second"}); !errors.Is(err, context.Canceled) {
		t.Errorf("Send after disconnect returned %v, want context.Canceled", err)
	}
	if s.Sent() != 1 {
		t.Errorf("Sent() = %d, want 1", s.Sent())
	}
	if strings.Contains(rec.Body.String(), "second") {
		t.Error("an event was written after the client disconnected")
	}
}

// A ResponseWriter that cannot flush means events sit in a buffer instead of
// reaching the client. Failing loudly beats a stream that silently never
// arrives.
func TestNewRejectsAWriterThatCannotFlush(t *testing.T) {
	if _, err := New(unflushable{httptest.NewRecorder()}, httptest.NewRequest("GET", "/", nil)); !errors.Is(err, ErrNotFlushable) {
		t.Errorf("err = %v, want ErrNotFlushable", err)
	}
}

type unflushable struct{ http.ResponseWriter }

func (unflushable) Unwrap() http.ResponseWriter { return nil }

// Patch is the shape htmx and Datastar both consume.
func TestPatchSendsAnHTMLFragment(t *testing.T) {
	rec := httptest.NewRecorder()
	s, err := New(rec, httptest.NewRequest("GET", "/", nil))
	if err != nil {
		t.Fatal(err)
	}

	if err := s.Patch("row-updated", `<tr id="r1"><td>42</td></tr>`); err != nil {
		t.Fatal(err)
	}

	want := "event: row-updated\ndata: <tr id=\"r1\"><td>42</td></tr>\n\n"
	if got := rec.Body.String(); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestCommentIsIgnorableKeepAlive(t *testing.T) {
	rec := httptest.NewRecorder()
	s, err := New(rec, httptest.NewRequest("GET", "/", nil))
	if err != nil {
		t.Fatal(err)
	}

	if err := s.Comment("still here"); err != nil {
		t.Fatal(err)
	}
	if got, want := rec.Body.String(), ": still here\n\n"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// End to end over a real connection, because httptest.ResponseRecorder buffers
// and therefore cannot show that anything was actually flushed to a client
// before the handler returned.
func TestStreamReachesAClientBeforeTheHandlerReturns(t *testing.T) {
	release := make(chan struct{})

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s, err := New(w, r)
		if err != nil {
			t.Error(err)
			return
		}
		if err := s.Patch("greeting", "<p>one</p>"); err != nil {
			return
		}
		<-release
		s.Patch("greeting", "<p>two</p>")
	}))
	defer srv.Close()
	defer close(release)

	resp, err := http.Get(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if ct := resp.Header.Get("Content-Type"); ct != "text/event-stream" {
		t.Fatalf("Content-Type = %q", ct)
	}

	// The handler has not returned, so this only succeeds if the first event
	// was flushed rather than buffered.
	buf := make([]byte, 64)
	n, err := resp.Body.Read(buf)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if got := string(buf[:n]); !strings.Contains(got, "<p>one</p>") {
		t.Errorf("first read = %q, want the first event", got)
	}
}

// Example shows the shape a streaming handler takes. It is compiled and run by
// `go test`, so it cannot drift from the API the way a README snippet can.
func Example() {
	handler := func(w http.ResponseWriter, r *http.Request) {
		s, err := New(w, r)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		for _, row := range []string{"<tr><td>1</td></tr>", "<tr><td>2</td></tr>"} {
			// Send returns the request context's error once the client has
			// gone away, so this loop stops on disconnect without a select.
			if err := s.Patch("row", row); err != nil {
				return
			}
		}
	}

	srv := httptest.NewServer(http.HandlerFunc(handler))
	defer srv.Close()

	resp, err := http.Get(srv.URL)
	if err != nil {
		fmt.Println(err)
		return
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	fmt.Print(string(body))

	// Output:
	// event: row
	// data: <tr><td>1</td></tr>
	//
	// event: row
	// data: <tr><td>2</td></tr>
}
