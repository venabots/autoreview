package panel

import (
	"fmt"
	"io"
	"sync"
	"time"
)

// spinnerFrames are v1's.
var spinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

const (
	spinnerEvery = 80 * time.Millisecond
	clearLine    = "\r\x1b[2K"
	magenta      = "\x1b[35m"
	plainColor   = "\x1b[0m"
)

// Status is what the run is doing before it has anything to show.
//
// Two long waits come before the first report: a checkout for each
// panelist, then the slowest model. Silence through them reads as a hung
// tool, and the first thing anyone does with one is press ctrl-C. On a
// terminal this is one spinner line that leaves nothing behind. Anywhere
// else it is one plain line for each step, so a log records what happened.
// Either way it goes to stderr: stdout is the report.
type Status struct {
	w    io.Writer
	live bool

	mu      sync.Mutex
	msg     string
	frame   int
	stopped bool
	stop    chan struct{}
	done    chan struct{}
}

// NewStatus writes to w. live says w is a terminal the spinner can redraw.
func NewStatus(w io.Writer, live bool) *Status {
	s := &Status{w: w, live: live, stop: make(chan struct{}), done: make(chan struct{})}
	if !live {
		close(s.done)
		return s
	}
	go s.spin()
	return s
}

// spin redraws the line steadily, so it keeps moving while a call blocks.
// A frozen spinner says the same thing silence does.
func (s *Status) spin() {
	defer close(s.done)
	ticker := time.NewTicker(spinnerEvery)
	defer ticker.Stop()
	for {
		select {
		case <-s.stop:
			return
		case <-ticker.C:
			s.mu.Lock()
			s.frame++
			s.draw()
			s.mu.Unlock()
		}
	}
}

// draw writes the spinner line. The caller holds the lock.
func (s *Status) draw() {
	if s.stopped || s.msg == "" {
		return
	}
	frame := spinnerFrames[s.frame%len(spinnerFrames)]
	_, _ = fmt.Fprintf(s.w, "%s%s%s%s %s", clearLine, magenta, frame, plainColor, s.msg)
}

// Step is what is happening now. It replaces the last step on a terminal
// and adds a line anywhere else.
func (s *Status) Step(msg string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.live {
		_, _ = fmt.Fprintln(s.w, msg)
		return
	}
	s.msg = msg
	s.draw()
}

// Tick is a message that changes as time passes. It is drawn only where it
// can be redrawn: off a terminal a line four times a second is a flood, not
// a log.
func (s *Status) Tick(msg string) {
	if !s.live {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.msg = msg
}

// Say writes a line that stays. The spinner steps aside for it, or the two
// would fuse into one line that reads as a progress message.
func (s *Status) Say(msg string) {
	s.Suspend(func() { _, _ = fmt.Fprintln(s.w, msg) })
}

// Suspend runs f with the spinner line cleared, so whatever f prints does
// not fight the spinner for the last line of the terminal.
func (s *Status) Suspend(f func()) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.live && !s.stopped {
		_, _ = io.WriteString(s.w, clearLine)
	}
	f()
	s.draw()
}

// Clear says nothing more is coming. The spinner leaves no trace: what the
// run found is the report's to say. It is safe to call more than once.
func (s *Status) Clear() {
	s.mu.Lock()
	if s.stopped {
		s.mu.Unlock()
		return
	}
	s.stopped = true
	if s.live {
		close(s.stop)
		_, _ = io.WriteString(s.w, clearLine)
	}
	s.mu.Unlock()
	<-s.done
}
