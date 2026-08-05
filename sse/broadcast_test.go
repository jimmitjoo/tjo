package sse

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// testWriter is a ResponseWriter whose writes are observable and, optionally,
// stallable.
//
// httptest.ResponseRecorder cannot be used for the slow-subscriber test: the
// test goroutine has to read what was written while Serve is still writing, and
// reading the recorder's buffer concurrently is a data race. Publishing each
// write on a channel synchronises the two.
type testWriter struct {
	hdr    http.Header
	writes chan string
	stall  chan struct{}
}

func newTestWriter(stall chan struct{}) *testWriter {
	return &testWriter{
		hdr:    make(http.Header),
		writes: make(chan string, 64),
		stall:  stall,
	}
}

func (w *testWriter) Header() http.Header { return w.hdr }
func (w *testWriter) WriteHeader(int)     {}
func (w *testWriter) Flush()              {}

func (w *testWriter) Write(p []byte) (int, error) {
	if w.stall != nil {
		<-w.stall
	}
	w.writes <- string(p)
	return len(p), nil
}

// nextWrite returns the next thing written to w, or fails the test.
func (w *testWriter) nextWrite(t *testing.T) string {
	t.Helper()
	select {
	case s := <-w.writes:
		return s
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for a write")
		return ""
	}
}

// waitForSubscribers blocks until topic has n subscribers.
//
// Serve registers the subscription itself, so there is no moment the caller can
// observe other than by asking the broker.
func waitForSubscribers(t *testing.T, b *Broker, topic string, n int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if b.Subscribers(topic) == n {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("topic %q has %d subscribers, want %d", topic, b.Subscribers(topic), n)
}

func serve(t *testing.T, b *Broker, w http.ResponseWriter, topic string) <-chan error {
	t.Helper()

	s, err := New(w, httptest.NewRequest("GET", "/", nil))
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	done := make(chan error, 1)
	go func() { done <- b.Serve(s, topic) }()
	return done
}

// A stalled subscriber must not hold up the others. This is the failure mode
// that turns a broadcast into an outage: one backgrounded tab, and every other
// client stops receiving.
func TestASlowSubscriberIsDroppedWithoutBlockingTheOthers(t *testing.T) {
	b := &Broker{Buffer: 1, KeepAlive: -1}

	stall := make(chan struct{})
	// So the stalled goroutine can exit if the test fails before releasing it.
	defer func() {
		if stall != nil {
			close(stall)
		}
	}()

	stalled := newTestWriter(stall)
	stalledDone := serve(t, b, stalled, "invoices")

	live := newTestWriter(nil)
	liveDone := serve(t, b, live, "invoices")

	waitForSubscribers(t, b, "invoices", 2)

	// The first broadcast reaches both. The stalled subscriber blocks inside
	// its write and stops draining its buffer from here on.
	b.Patch("invoices", "patch", "one")
	if got, want := live.nextWrite(t), "event: patch\ndata: one\n\n"; got != want {
		t.Fatalf("live subscriber got %q, want %q", got, want)
	}

	// Fills the stalled subscriber's one-event buffer.
	b.Patch("invoices", "patch", "two")
	if got, want := live.nextWrite(t), "event: patch\ndata: two\n\n"; got != want {
		t.Fatalf("live subscriber got %q, want %q", got, want)
	}

	// Overflows it, which drops it.
	b.Patch("invoices", "patch", "three")
	if got, want := live.nextWrite(t), "event: patch\ndata: three\n\n"; got != want {
		t.Fatalf("live subscriber got %q, want %q", got, want)
	}

	if got := b.Subscribers("invoices"); got != 1 {
		t.Fatalf("after dropping the stalled subscriber the topic has %d subscribers, want 1", got)
	}

	// Releasing the stall lets the dropped subscriber notice.
	close(stall)
	stall = nil

	select {
	case err := <-stalledDone:
		if !errors.Is(err, ErrSlowSubscriber) {
			t.Fatalf("stalled subscriber returned %v, want ErrSlowSubscriber", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("stalled subscriber never returned")
	}

	select {
	case err := <-liveDone:
		t.Fatalf("live subscriber returned %v, want it still serving", err)
	default:
	}
}

// The request context already carries the disconnect signal, so the handler
// does not get to forget. A leaked subscription is invisible until the process
// has been up for a week.
func TestServeUnsubscribesWhenTheClientDisconnects(t *testing.T) {
	b := &Broker{KeepAlive: -1}

	ctx, cancel := context.WithCancel(context.Background())
	w := newTestWriter(nil)

	s, err := New(w, httptest.NewRequest("GET", "/", nil).WithContext(ctx))
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	done := make(chan error, 1)
	go func() { done <- b.Serve(s, "invoices") }()

	waitForSubscribers(t, b, "invoices", 1)

	cancel()

	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Serve returned %v, want context.Canceled", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Serve did not return after the client disconnected")
	}

	waitForSubscribers(t, b, "invoices", 0)
}

func TestBroadcastReachesOneTopicOnly(t *testing.T) {
	b := &Broker{KeepAlive: -1}

	w := newTestWriter(nil)
	serve(t, b, w, "invoices")
	waitForSubscribers(t, b, "invoices", 1)

	b.Patch("orders", "patch", "not for you")
	b.Patch("invoices", "patch", "yours")

	if got, want := w.nextWrite(t), "event: patch\ndata: yours\n\n"; got != want {
		t.Fatalf("got %q, want %q -- a broadcast crossed topics", got, want)
	}
}

func TestBroadcastToAnEmptyTopicIsANoop(t *testing.T) {
	var b Broker // the zero value is usable
	b.Patch("nobody-here", "patch", "<p>hi</p>")

	if got := b.Subscribers("nobody-here"); got != 0 {
		t.Fatalf("Subscribers = %d, want 0", got)
	}
}

func TestKeepAliveIsSentWhileTheTopicIsQuiet(t *testing.T) {
	b := &Broker{KeepAlive: time.Millisecond}

	w := newTestWriter(nil)
	serve(t, b, w, "invoices")

	if got, want := w.nextWrite(t), ": keep-alive\n\n"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// A subscriber that is dropped and one that disconnects both end up calling
// unsubscribe on a channel Broadcast may have already closed. Doing it twice
// panics, so the presence check and the close share one lock.
func TestConcurrentBroadcastAndDisconnectDoNotDoubleClose(t *testing.T) {
	b := &Broker{Buffer: 1, KeepAlive: -1}

	for i := 0; i < 50; i++ {
		ctx, cancel := context.WithCancel(context.Background())
		w := newTestWriter(nil)

		s, err := New(w, httptest.NewRequest("GET", "/", nil).WithContext(ctx))
		if err != nil {
			t.Fatalf("New: %v", err)
		}

		done := make(chan error, 1)
		go func() { done <- b.Serve(s, "invoices") }()
		waitForSubscribers(t, b, "invoices", 1)

		go cancel()
		for j := 0; j < 20; j++ {
			b.Patch("invoices", "patch", "spam")
		}

		<-done
		waitForSubscribers(t, b, "invoices", 0)
	}
}

// A handler subscribes; a job elsewhere in the application broadcasts.
func ExampleBroker() {
	broker := &Broker{}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		stream, err := New(w, r)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		// Returns when the client disconnects, and unsubscribes on the way out.
		broker.Serve(stream, "invoices")
	}))
	defer srv.Close()

	go func() {
		// A job, after it has written the row and committed. The wait is only
		// here to make the example deterministic; real applications broadcast
		// whenever they write and accept that nobody may be listening.
		for broker.Subscribers("invoices") == 0 {
			time.Sleep(time.Millisecond)
		}
		broker.Patch("invoices", "patch", "<li>INV-1042 paid</li>")
	}()

	resp, err := http.Get(srv.URL)
	if err != nil {
		fmt.Println(err)
		return
	}
	defer resp.Body.Close()

	sc := bufio.NewScanner(resp.Body)
	for sc.Scan() {
		line := sc.Text()
		if strings.TrimSpace(line) == "" {
			break
		}
		fmt.Println(line)
	}

	// Output:
	// event: patch
	// data: <li>INV-1042 paid</li>
}
