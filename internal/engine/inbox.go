package engine

import (
	"sync"

	"github.com/venabots/autoreview/internal/mine"
	"github.com/venabots/autoreview/internal/prs"
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

func (Quit) input()   {}
func (Quit) command() {}

type listed struct {
	listing prs.Listing
	err     error
}

type mineListed struct {
	list []mine.PR
	err  error
}

func (listed) input()     {}
func (mineListed) input() {}

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
