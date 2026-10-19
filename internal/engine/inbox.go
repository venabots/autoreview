package engine

import (
	"sync"
	"time"

	"github.com/venabots/autoreview/internal/mine"
	"github.com/venabots/autoreview/internal/proc"
	"github.com/venabots/autoreview/internal/prs"
	"github.com/venabots/autoreview/internal/review"
)

// input is one thing the engine reacts to. Commands, results from GitHub
// and signals all arrive through one inbox, so the engine handles them in
// the order they were sent. Separate channels would let Go's select pick
// among ready ones at random.
type input interface{ input() }

// Command is what a person or the process asks the run to do.
type Command interface {
	input
	command()
}

// Quit ends the run.
type Quit struct{}

// ReviewNow reviews one PR on the list now, whatever holds it: in a pass of
// its own when the run is idle, or first among the reviews that wait.
type ReviewNow struct{ PR int }

// StopJob stops the review of one PR.
type StopJob struct{ PR int }

// SetWatch turns the loop on or off. Off, the run reviews only what a
// person asks for; on, it reviews what becomes ready at each look.
type SetWatch struct{ On bool }

func (SetWatch) input()   {}
func (SetWatch) command() {}

// IncludeBots puts Dependabot's PRs on the Maintain tab beside yours, or
// takes them off.
type IncludeBots struct{ On bool }

func (IncludeBots) input()   {}
func (IncludeBots) command() {}

// SetFocus changes what the reviewers are told to look at, for every
// review that has not started. An empty text tells them nothing.
type SetFocus struct{ Text string }

func (Quit) input()        {}
func (Quit) command()      {}
func (ReviewNow) input()   {}
func (ReviewNow) command() {}
func (StopJob) input()     {}
func (StopJob) command()   {}
func (SetFocus) input()    {}
func (SetFocus) command()  {}

type listed struct {
	listing prs.Listing
	err     error
}

// mineListed is a look's answer about your PRs, and mineRefreshed is the
// answer of the read that follows a task. seq is the number the read got
// when it started.
type mineListed struct {
	seq  int
	list []mine.PR
	err  error
}

type mineRefreshed struct {
	seq  int
	list []mine.PR
	err  error
}

func (mineRefreshed) input() {}

// jobReaped says a review's process has ended. It is sent before anything
// the process left behind is read, so the slot is free at once and the
// time the process took does not include the reading.
type jobReaped struct {
	pr      int
	exit    proc.Exit
	elapsed time.Duration
	meta    *review.Meta
}

// jobSettled says everything about a finished review is known.
type jobSettled struct {
	pr      int
	outcome outcome
}

// guardFired says a review ran past the time its own harness should have
// stopped it at.
type guardFired struct{ pr int }

func (listed) input()     {}
func (mineListed) input() {}
func (jobReaped) input()  {}
func (jobSettled) input() {}
func (guardFired) input() {}

// inbox is an unbounded queue. put never blocks: the view calls it from its
// update loop, and a full queue must not freeze the screen or lose a key.
type inbox struct {
	mu    sync.Mutex
	items []input
	// ready holds at most one token. It says "there is something to take".
	ready chan struct{}
}

func newInbox() *inbox {
	return &inbox{ready: make(chan struct{}, 1)}
}

func (b *inbox) put(in input) {
	b.mu.Lock()
	b.items = append(b.items, in)
	b.mu.Unlock()
	select {
	case b.ready <- struct{}{}:
	default:
	}
}

// take removes everything queued, in the order it was put.
func (b *inbox) take() []input {
	b.mu.Lock()
	defer b.mu.Unlock()
	items := b.items
	b.items = nil
	return items
}
