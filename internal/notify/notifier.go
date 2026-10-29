package notify

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/venabots/autoreview/internal/engine"
)

// Sender delivers one alert on one channel: Herdr, a desktop notification,
// a chat. Each channel is a Sender, so a new one is one more function.
type Sender func(ctx context.Context, a Alert) error

// sendTimeout bounds one alert on one channel. A channel that hangs must
// not hold the alerts after it, or the end of the run.
const sendTimeout = 10 * time.Second

// queueLimit is the most alerts that wait. A channel too slow for them
// loses the newest: a person flooded with alerts reads none of them.
const queueLimit = 64

// Fanout is one Sender that sends to each of senders in turn. A channel
// that fails does not stop the others; the error joins every failure.
func Fanout(senders ...Sender) Sender {
	return func(ctx context.Context, a Alert) error {
		var errs []error
		for _, send := range senders {
			errs = append(errs, send(ctx, a))
		}
		return errors.Join(errs...)
	}
}

// Notifier turns the run's events into alerts and sends them off the
// engine's goroutine. Emit never blocks.
type Notifier struct {
	send   Sender
	prefix string
	failed func(error)
	queue  chan Alert
	done   chan struct{}
	once   sync.Once
	// mu guards closed, so an Emit after Close is dropped and does not
	// send on a closed channel.
	mu     sync.Mutex
	closed bool
}

// New starts a notifier. prefix names the repo in each title, for a
// person who runs more than one. failed hears the first failure to send,
// once; it may be nil.
func New(send Sender, prefix string, failed func(error)) *Notifier {
	n := &Notifier{send: send, prefix: prefix, failed: failed, queue: make(chan Alert, queueLimit), done: make(chan struct{})}
	go n.run()
	return n
}

// Emit queues the alerts of one event. It never blocks: with the queue
// full, or the notifier closed, the alert is dropped.
func (n *Notifier) Emit(ev engine.Event) {
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.closed {
		return
	}
	for _, a := range AlertsFor(ev) {
		if n.prefix != "" {
			a.Title = n.prefix + ": " + a.Title
		}
		select {
		case n.queue <- a:
		default:
		}
	}
}

// Close sends what is queued and stops. Call it once, after the engine
// has stopped emitting.
func (n *Notifier) Close() {
	n.mu.Lock()
	if !n.closed {
		n.closed = true
		close(n.queue)
	}
	n.mu.Unlock()
	<-n.done
}

func (n *Notifier) run() {
	defer close(n.done)
	for a := range n.queue {
		ctx, cancel := context.WithTimeout(context.Background(), sendTimeout)
		err := n.send(ctx, a)
		cancel()
		if err != nil && n.failed != nil {
			n.once.Do(func() { n.failed(err) })
		}
	}
}
