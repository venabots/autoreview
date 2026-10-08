// Package engine owns a run. One goroutine holds the state, reacts to one
// inbox, and publishes a snapshot after every change. The view and the plain
// printer read snapshots and send commands. Neither one reaches into the
// run, and the engine never writes to the terminal.
package engine

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/venabots/autoreview/internal/mine"
	"github.com/venabots/autoreview/internal/prs"
	"github.com/venabots/autoreview/internal/queue"
	"github.com/venabots/autoreview/internal/repo"
)

// Config is what one run was asked to do.
type Config struct {
	Repo  repo.Context
	Gates prs.Gates
	// Only limits the run to these PRs. Empty means every PR.
	Only []int
	// Watch is the wait between looks. Zero means look once and end.
	Watch time.Duration
}

// Deps is the outside world, as functions, so a test hands in its own.
type Deps struct {
	List func(ctx context.Context) (prs.Listing, error)
	Mine func(ctx context.Context) ([]mine.PR, error)
}

// Engine runs one review run.
type Engine struct {
	cfg     Config
	deps    Deps
	in      *inbox
	current atomic.Pointer[Snapshot]
	changed chan struct{}
}

// New makes an engine. Nothing happens until Run.
func New(cfg Config, deps Deps) *Engine {
	e := &Engine{cfg: cfg, deps: deps, in: newInbox(), changed: make(chan struct{}, 1)}
	e.current.Store(&Snapshot{Phase: Looking, Repo: cfg.Repo, Watch: cfg.Watch})
	return e
}

// Do sends a command. It never blocks.
func (e *Engine) Do(c Command) {
	e.in.put(c)
}

// Snapshot is the newest state. It is never nil.
func (e *Engine) Snapshot() *Snapshot {
	return e.current.Load()
}

// Changed receives when a newer snapshot exists. It holds one token at
// most: a reader that was slow finds the newest snapshot, and misses none
// that still matters.
func (e *Engine) Changed() <-chan struct{} {
	return e.changed
}

// run is what the loop carries from one input to the next.
type run struct {
	snap Snapshot
	// pending counts the looks that have not answered.
	pending int
	code    int
}

// Run runs until the run ends, and returns how it ended. It returns only
// after everything it started has stopped.
func (e *Engine) Run() Result {
	ctx, cancel := context.WithCancel(context.Background())
	var looks sync.WaitGroup
	// Deferred calls run last-in first-out: cancel stops the looks, and
	// only then does Wait have something that ends.
	defer looks.Wait()
	defer cancel()

	timer := time.NewTimer(time.Hour)
	timer.Stop()
	defer timer.Stop()

	r := e.look(ctx, &looks, run{snap: *e.Snapshot()})
	e.publish(r.snap)
	for r.snap.Phase != Done {
		select {
		case <-e.in.ready:
			for _, in := range e.in.take() {
				r = e.handle(r, in)
			}
		case <-timer.C:
			r = e.look(ctx, &looks, r)
		}
		r = e.settle(r, timer)
		e.publish(r.snap)
	}
	return Result{Code: r.code, Final: e.Snapshot()}
}

// look starts reading both lists. Each answers through the inbox, exactly
// once: a look that panicked answers with an error, because a look that
// never answers would leave the run looking forever.
func (e *Engine) look(ctx context.Context, looks *sync.WaitGroup, r run) run {
	looks.Go(func() {
		listing, err := guarded(func() (prs.Listing, error) { return e.deps.List(ctx) })
		e.in.put(listed{listing: listing, err: err})
	})
	looks.Go(func() {
		list, err := guarded(func() ([]mine.PR, error) { return e.deps.Mine(ctx) })
		e.in.put(mineListed{list: list, err: err})
	})
	r.snap.Phase = Looking
	r.snap.NextLookAt = time.Time{}
	r.pending = 2
	return r
}

// guarded turns a panic in a look into that look's error.
func guarded[T any](read func() (T, error)) (value T, err error) {
	defer func() {
		if p := recover(); p != nil {
			err = fmt.Errorf("the look failed unexpectedly: %v", p)
		}
	}()
	return read()
}

func (e *Engine) handle(r run, in input) run {
	switch in := in.(type) {
	case Quit:
		return quit(r)
	case listed:
		r.pending--
		r.snap = withListing(r.snap, in, e.cfg)
	case mineListed:
		r.pending--
		r.snap = withMine(r.snap, in)
	}
	return r
}

// quit ends a run that had not ended by itself. That is an interrupt,
// whoever asked: a key in the view, or a signal.
func quit(r run) run {
	if r.snap.Phase == Done {
		return r
	}
	r.snap.Phase = Done
	r.code = ExitInterrupted
	return r
}

// settle moves a look that has fully answered on to what comes next: the
// wait for the next look, or the end of a run that looks once.
func (e *Engine) settle(r run, timer *time.Timer) run {
	if r.snap.Phase != Looking || r.pending > 0 {
		return r
	}
	if e.cfg.Watch == 0 {
		r.snap.Phase = Done
		r.code = lookedOnceCode(r.snap)
		return r
	}
	r.snap.Phase = Waiting
	r.snap.NextLookAt = time.Now().Add(e.cfg.Watch)
	timer.Reset(e.cfg.Watch)
	return r
}

// lookedOnceCode fails a run whose only look at the review list failed. A
// failed look at your own PRs does not: the run was asked to review.
func lookedOnceCode(s Snapshot) int {
	if s.ListErr != "" {
		return ExitFailed
	}
	return ExitOK
}

// withListing takes a look's answer. A failed look keeps the rows the last
// good one found: the list on screen must not empty because GitHub was slow.
func withListing(s Snapshot, in listed, cfg Config) Snapshot {
	if in.err != nil {
		s.ListErr = in.err.Error()
		return s
	}
	s.Looked = true
	s.ListErr = ""
	s.Reviews = queue.Rows(in.listing, cfg.Gates, cfg.Only)
	s.Truncated = in.listing.Truncated
	return s
}

func withMine(s Snapshot, in mineListed) Snapshot {
	if in.err != nil {
		s.MineErr = in.err.Error()
		return s
	}
	s.MineLooked = true
	s.MineErr = ""
	s.Mine = in.list
	return s
}

func (e *Engine) publish(s Snapshot) {
	s.Seq = e.Snapshot().Seq + 1
	e.current.Store(&s)
	select {
	case e.changed <- struct{}{}:
	default:
	}
}
