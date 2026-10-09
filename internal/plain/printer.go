package plain

import (
	"io"
	"sync"

	"github.com/venabots/autoreview/internal/engine"
)

// Printer writes a run's events as plain lines, as they happen.
//
// The engine hands it events from its own goroutine and must never wait on
// a slow reader: a pager that has stopped reading must not stop ctrl-C from
// ending the reviews. So Emit only queues, and one goroutine writes.
type Printer struct {
	out  io.Writer
	errs io.Writer
	// log gets every line of both streams. It is the run's own log, and it
	// may be nil.
	log io.Writer

	mu     sync.Mutex
	queue  []engine.Event
	closed bool
	wake   chan struct{}
	done   chan struct{}
}

// NewPrinter starts a printer. Close it to write what is still queued.
func NewPrinter(out, errs, log io.Writer) *Printer {
	p := &Printer{out: out, errs: errs, log: log, wake: make(chan struct{}, 1), done: make(chan struct{})}
	go p.write()
	return p
}

// Emit queues one event. It never blocks.
func (p *Printer) Emit(ev engine.Event) {
	p.mu.Lock()
	p.queue = append(p.queue, ev)
	p.mu.Unlock()
	p.signal()
}

// Close writes every queued event and stops the printer. Events sent after
// Close are dropped.
func (p *Printer) Close() {
	p.mu.Lock()
	p.closed = true
	p.mu.Unlock()
	p.signal()
	<-p.done
}

func (p *Printer) signal() {
	select {
	case p.wake <- struct{}{}:
	default:
	}
}

func (p *Printer) write() {
	defer close(p.done)
	for range p.wake {
		p.mu.Lock()
		events, closed := p.queue, p.closed
		p.queue = nil
		p.mu.Unlock()
		for _, ev := range events {
			p.put(say(ev))
		}
		if closed {
			return
		}
	}
}

// put writes text to its streams and to the log. A failed write is not
// reported: the stream that failed is where the report would go.
func (p *Printer) put(t text) {
	for _, part := range []struct {
		w io.Writer
		s string
	}{{p.out, t.out}, {p.errs, t.errs}} {
		if part.s == "" {
			continue
		}
		_, _ = io.WriteString(part.w, part.s)
		if p.log != nil {
			_, _ = io.WriteString(p.log, part.s)
		}
	}
}

// Finish prints the end of a run that reviewed. Call it after Close.
func (p *Printer) Finish(result engine.Result, noPost bool) {
	out, errs := Final(result, noPost)
	p.put(text{out: out, errs: errs})
}
