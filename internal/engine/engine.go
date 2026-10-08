// Package engine owns a run. One goroutine holds the state, reacts to one
// inbox, and publishes a snapshot after every change. The view and the plain
// printer read snapshots and send commands. Neither one reaches into the
// run, and the engine never writes to the terminal.
package engine

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"github.com/venabots/autoreview/internal/mine"
	"github.com/venabots/autoreview/internal/proc"
	"github.com/venabots/autoreview/internal/prs"
	"github.com/venabots/autoreview/internal/repo"
	"github.com/venabots/autoreview/internal/review"
	"github.com/venabots/autoreview/internal/rundir"
	"github.com/venabots/autoreview/internal/session"
)

// Config is what one run was asked to do.
type Config struct {
	Repo  repo.Context
	Gates prs.Gates
	// Only limits the run to these PRs. Empty means every PR.
	Only []int
	// Watch is the wait between looks. Zero means look once and end.
	Watch time.Duration

	// Review makes the run review the PRs that are ready. Without it the
	// run only looks, and the fields below are not read.
	Review bool
	// Jobs is how many reviews run at once. It is one or more.
	Jobs int
	// Continue resumes a PR's earlier review session when it has one.
	Continue bool
	// NoPost reviews and leaves the PR alone.
	NoPost bool
	// Focus is what the reviewers are told to look at, or empty.
	Focus string
	// Timeout is the limit for one review. Zero means none.
	Timeout time.Duration
	// Budget is the cost cap for one review as the person gave it, or
	// empty.
	Budget       string
	Orchestrator review.Orchestrator
	// Override is a shell command that replaces the built-in reviewer, or
	// empty.
	Override string
	// Dashp is the path of the dash-p binary.
	Dashp string
}

// Deps is the outside world, as functions, so a test hands in its own.
type Deps struct {
	List func(ctx context.Context) (prs.Listing, error)
	Mine func(ctx context.Context) ([]mine.PR, error)

	// The rest is read only by a run that reviews.

	// Procs starts the reviewers.
	Procs proc.Starter
	// Verdict asks GitHub what review landed on a PR since a time.
	Verdict func(ctx context.Context, pr int, since time.Time) review.Readback
	// Sessions says which review sessions exist and which are open.
	Sessions session.Probe
	// Transcript finds the transcript of a session.
	Transcript func(id string) (string, bool)
	// Run is the run's directory.
	Run rundir.Run
	// SkillsDir is the staged skills, or empty when none were staged.
	SkillsDir string
	// Record keeps a finished review in the ledger. It is nil when the
	// ledger is off. It runs off the engine's goroutine.
	Record func(Finished) error
	// Journal takes each event as it happens. It must not block: the
	// engine calls it from its own goroutine. It may be nil.
	Journal func(Event)
}

// Finished is a review that ran to its end and reported on itself, with
// what the ledger keeps of it.
type Finished struct {
	Job        Job
	FinishedAt time.Time
	// Text is the review as the reviewer wrote it. HasText is false when
	// there was nothing to read, which is not the same as an empty review.
	Text    string
	HasText bool
}

// Engine runs one review run.
type Engine struct {
	cfg     Config
	deps    Deps
	in      *inbox
	current atomic.Pointer[Snapshot]
	changed chan struct{}
	// quitting is set when the run is told to end. A reviewer's monitor
	// reads it, to skip the questions nobody will wait for.
	quitting atomic.Bool
}

// New makes an engine. Nothing happens until Run.
func New(cfg Config, deps Deps) *Engine {
	e := &Engine{cfg: cfg, deps: deps, in: newInbox(), changed: make(chan struct{}, 1)}
	e.current.Store(&Snapshot{Phase: Looking, Repo: cfg.Repo, Watch: cfg.Watch, RunDir: deps.Run.Root})
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
	// jobs is the reviews of the pass, with what the engine alone knows
	// about each.
	jobs []job
	pass rundir.Pass
	code int
}

// workers is what the loop hands to anything that starts a goroutine: the
// context that stops it and the group that waits for it.
type workers struct {
	ctx   context.Context
	group *sync.WaitGroup
}

// Run runs until the run ends, and returns how it ended. It returns only
// after everything it started has stopped.
func (e *Engine) Run() Result {
	ctx, cancel := context.WithCancel(context.Background())
	var group sync.WaitGroup
	// Deferred calls run last-in first-out: cancel stops the workers, and
	// only then does Wait have something that ends.
	defer group.Wait()
	defer cancel()
	w := workers{ctx: ctx, group: &group}

	timer := time.NewTimer(time.Hour)
	timer.Stop()
	defer timer.Stop()

	r := e.look(w, run{snap: *e.Snapshot()})
	e.publish(r)
	for r.snap.Phase != Done {
		select {
		case <-e.in.ready:
			for _, in := range e.in.take() {
				r = e.handle(w, r, in)
			}
		case <-timer.C:
			r = e.look(w, r)
		}
		r = e.afterLook(w, r, timer)
		e.publish(r)
	}
	return Result{Code: r.code, Final: e.Snapshot()}
}

func (e *Engine) handle(w workers, r run, in input) run {
	switch in := in.(type) {
	case Quit:
		return e.quit(r)
	case listed:
		r.pending--
		r.snap = withListing(r.snap, in, e.cfg)
	case mineListed:
		r.pending--
		r.snap = withMine(r.snap, in)
	case jobReaped:
		return e.reaped(w, r, in)
	case guardFired:
		return e.guardFired(r, in)
	case jobSettled:
		return e.settled(r, in)
	}
	return r
}

// quit ends a run that had not ended by itself. That is an interrupt,
// whoever asked: a key in the view, or a signal. A run with reviews alive
// stops them first and ends when the last one has been collected.
func (e *Engine) quit(r run) run {
	switch r.snap.Phase {
	case Done, Stopping:
		return r
	case Reviewing:
		return e.stopReviews(r)
	}
	r.snap.Phase = Done
	r.code = ExitInterrupted
	return r
}

// publish makes a snapshot of the run and tells the reader there is one.
func (e *Engine) publish(r run) {
	s := r.snap
	s.Seq = e.Snapshot().Seq + 1
	s.Jobs = views(r.jobs)
	e.current.Store(&s)
	select {
	case e.changed <- struct{}{}:
	default:
	}
}

// journal records an event, when anything is listening.
func (e *Engine) journal(ev Event) {
	if e.deps.Journal != nil {
		e.deps.Journal(ev)
	}
}
