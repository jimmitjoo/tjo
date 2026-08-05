package sse

import (
	"errors"
	"sync"
	"time"
)

// ErrSlowSubscriber is returned by Serve when a subscriber fell far enough
// behind to be dropped.
//
// It is a normal outcome rather than a defect: a browser tab that is
// backgrounded, a client on a stalled connection, a laptop that went to sleep.
// The alternative -- waiting for it -- makes one slow reader everyone else's
// problem, which is how a broadcast becomes an outage.
var ErrSlowSubscriber = errors.New("sse: subscriber fell behind and was dropped")

// Broker pushes events to everyone subscribed to a topic.
//
// This is the small version of "real-time": the application writes a row and
// tells the broker, the broker pushes the rendered fragment, and the client
// owns no state. It is deliberately not a sync engine (#79).
//
// # Why not LISTEN/NOTIFY or triggers
//
// Because they are PostgreSQL-only and this framework supports three databases.
// The application calling Broadcast after it writes is portable, and it is also
// the version where the broadcast happens after the transaction committed
// rather than during it. A trigger firing inside an uncommitted transaction
// pushes a fragment rendering data no reader can see yet.
//
// The cost is honest and worth stating: a write that bypasses the application
// -- a migration, a psql session, a second service -- broadcasts nothing.
//
// The zero value is ready to use.
type Broker struct {
	// Buffer is how many events a subscriber may fall behind before it is
	// dropped. Zero means DefaultBuffer.
	//
	// Bigger is not safer. The buffer is the number of events held in memory
	// per stalled client, so raising it to avoid dropping anyone converts a
	// dropped subscriber into unbounded memory growth under exactly the
	// conditions that produced the drop.
	Buffer int

	// KeepAlive is how often an idle stream gets a comment, so a proxy with an
	// idle timeout does not close a connection both ends still believe in.
	// Zero means DefaultKeepAlive; negative disables it.
	KeepAlive time.Duration

	mu     sync.Mutex
	topics map[string]map[chan Event]struct{}
}

const (
	// DefaultBuffer is how many events a subscriber may fall behind.
	DefaultBuffer = 16

	// DefaultKeepAlive is under the 30s idle timeout that nginx, HAProxy and
	// most cloud load balancers ship with.
	DefaultKeepAlive = 25 * time.Second
)

// Broadcast sends e to every subscriber of topic.
//
// It never blocks. A subscriber whose buffer is full is dropped: removed from
// the topic and its channel closed, which ends its Serve call with
// ErrSlowSubscriber. Dropping is the only option that keeps one stalled client
// from stalling the others, and a client that reconnects gets current state
// from the page it loads.
//
// Safe to call from anywhere -- a handler, a job, a cron entry.
func (b *Broker) Broadcast(topic string, e Event) {
	b.mu.Lock()
	defer b.mu.Unlock()

	// The send and the drop both happen under the lock so that a subscriber
	// unsubscribing concurrently cannot have its channel closed twice: every
	// close is guarded by the same presence check on the same map.
	for ch := range b.topics[topic] {
		select {
		case ch <- e:
		default:
			delete(b.topics[topic], ch)
			close(ch)
		}
	}

	if len(b.topics[topic]) == 0 {
		delete(b.topics, topic)
	}
}

// Patch broadcasts an HTML fragment, which is what htmx and Datastar consume.
func (b *Broker) Patch(topic, name, fragment string) {
	b.Broadcast(topic, Event{Name: name, Data: fragment})
}

// Serve subscribes s to topic and writes every broadcast to it until the client
// disconnects, sending keep-alive comments while the topic is quiet.
//
// The whole handler is:
//
//	stream, err := sse.New(w, r)
//	if err != nil {
//	    http.Error(w, err.Error(), http.StatusInternalServerError)
//	    return
//	}
//	broker.Serve(stream, "invoices")
//
// Unsubscribing is not the handler's job. Serve returns when the request
// context is done, and the subscription is removed on the way out -- so a
// closed tab cannot leak a subscription, which is the leak nobody notices until
// the process has been up for a week.
//
// One goroutine owns the stream, satisfying Stream's concurrency contract:
// producers write to the broker, and only Serve writes to the stream.
func (b *Broker) Serve(s *Stream, topic string) error {
	ch := b.subscribe(topic)
	defer b.unsubscribe(topic, ch)

	ctx := s.Context()

	interval := b.KeepAlive
	if interval == 0 {
		interval = DefaultKeepAlive
	}

	var tick <-chan time.Time
	if interval > 0 {
		t := time.NewTicker(interval)
		defer t.Stop()
		tick = t.C
	}

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()

		case e, ok := <-ch:
			if !ok {
				return ErrSlowSubscriber
			}
			if err := s.Send(e); err != nil {
				return err
			}

		case <-tick:
			if err := s.Comment("keep-alive"); err != nil {
				return err
			}
		}
	}
}

// Subscribers reports how many streams are subscribed to topic.
func (b *Broker) Subscribers(topic string) int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.topics[topic])
}

func (b *Broker) subscribe(topic string) chan Event {
	size := b.Buffer
	if size <= 0 {
		size = DefaultBuffer
	}
	ch := make(chan Event, size)

	b.mu.Lock()
	defer b.mu.Unlock()

	if b.topics == nil {
		b.topics = make(map[string]map[chan Event]struct{})
	}
	if b.topics[topic] == nil {
		b.topics[topic] = make(map[chan Event]struct{})
	}
	b.topics[topic][ch] = struct{}{}

	return ch
}

// unsubscribe removes ch, if Broadcast has not already dropped it.
func (b *Broker) unsubscribe(topic string, ch chan Event) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if _, ok := b.topics[topic][ch]; !ok {
		// Already dropped for being slow, and already closed. Closing again
		// would panic.
		return
	}

	delete(b.topics[topic], ch)
	close(ch)

	if len(b.topics[topic]) == 0 {
		delete(b.topics, topic)
	}
}
