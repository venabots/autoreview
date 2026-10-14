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
	"github.com/venabots/autoreview/internal/proc"
	"github.com/venabots/autoreview/internal/prs"
	"github.com/venabots/autoreview/internal/queue"
	"github.com/venabots/autoreview/internal/repo"
	"github.com/venabots/autoreview/internal/review"
	"github.com/venabots/autoreview/internal/rundir"
	"github.com/venabots/autoreview/internal/session"
	"github.com/venabots/autoreview/internal/task"
	"github.com/venabots/autoreview/internal/worktree"
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
	// HoldOpen keeps the run alive when its work is done, for a person to
	// read the results and ask for more. Only Quit ends it then.
	HoldOpen bool
	// Loop is what the run does after its first pass.
	Loop Loop
	// Watching starts the loop on. The view turns it off and on again.
	Watching bool
	// Autofix starts the run with all your PRs autofixed, as if "A" was
	// pressed. Without it, a PR of yours that gets a review comment or a
	// conflict waits for a person to notice and press a key.
	Autofix bool
	// Rest is how long a PR waits after its review before it may be
	// reviewed again. MaxPasses is how many reviews one PR gets in a run.
	Rest      time.Duration
	MaxPasses int
	// MaxIdle is how many looks in a row may find nothing to review before
	// a run that babysits stops. Zero means no limit.
	MaxIdle int
	// AutofixCap is the most fixes one of your PRs gets in a row while
	// nobody else acts on it. Zero means babysit.MaxStreak.
	AutofixCap int
	// CIWait is how long a run that looks once waits for checks that are
	// still running before it starts its pass. Zero means it does not wait.
	CIWait time.Duration
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
	// Fallback retries a review whose orchestrator failed with exit 10, or
	// is nil for no retry.
	Fallback *review.Orchestrator
	// Override is a shell command that replaces the built-in reviewer, or
	// empty.
	Override string
	// Dashp is the path of the dash-p binary.
	Dashp string
}

// Loop is what a run does after its first pass.
type Loop int

const (
	// LoopNone makes one pass and ends, or holds under the view.
	LoopNone Loop = iota
	// LoopWatch reviews what becomes ready at each look, for as long as the
	// run lasts. A reviewed PR rests, and a push starts its count again.
	LoopWatch
	// LoopBabysit looks again after each pass, and ends when nothing is
	// left to review or to wait for.
	LoopBabysit
)

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
	// ledger is off. It runs on the engine's goroutine: it is the one
	// write to a file that the goroutine makes itself.
	Record func(Finished) error
	// Prepare makes the worktree a task runs in, and Finish gives it back
	// or says why it is kept. Pushed reads whether a PR's head moved from a
	// commit. TaskRefusal says why a kind of task cannot run, or is empty.
	Prepare     func(ctx context.Context, r task.Request) (worktree.Prepared, error)
	Finish      func(ctx context.Context, p worktree.Prepared) (kept string, err error)
	Pushed      func(ctx context.Context, pr int, before string) (string, bool)
	TaskRefusal func(k task.Kind) string
	// Marks puts a reaction on a PR while its review runs. It is nil under
	// --no-post, which promises to leave the PR alone.
	Marks Marks
	// Journal takes each event as it happens. It must not block: the
	// engine calls it from its own goroutine. It may be nil.
	Journal func(Event)
}

// Marks is the reaction a review leaves on its PR while it runs. Show and
// Clear return at once; Settle waits for every reaction on its way off and
// names the PRs whose reaction could not be set or removed.
type Marks interface {
	Show(pr int)
	Clear(pr int)
	Settle() []int
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
	e.current.Store(&Snapshot{
		Phase: Idle, Looking: true, Repo: cfg.Repo, Watch: cfg.Watch, RunDir: deps.Run.Root, Focus: cfg.Focus,
		Watching: cfg.Watching, Rest: cfg.Rest, Refusals: e.refusals(),
	})
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
	// looks counts the looks that have fully answered. lookFails counts
	// the failed ones since the last that worked.
	looks     int
	lookFails int
	// listing is the last PR list that was read, and track what this run
	// remembers about each PR it reviewed.
	listing prs.Listing
	track   queue.Track
	// idle counts the looks in a row that found nothing to review.
	// passEnded says a pass ended since the last look: the look straight
	// after a pass is not idle, it is the first chance to see an answer.
	idle      int
	passEnded bool
	// ciUntil is when a run that waits for pending checks stops waiting.
	// It is zero when the run does not wait.
	ciUntil time.Time
	// mineAsked numbers each read of your PRs as it starts, and mineTaken
	// is the newest one whose answer was taken.
	mineAsked, mineTaken int
	// looked is your PRs as the last look found them. snap.Mine can be
	// newer: it also takes the read that follows a task.
	looked []mine.PR
	// jobs is the reviews of the newest pass, with what the engine alone
	// knows about each.
	jobs []job
	// tasks is every task of the run on your own PRs.
	tasks []taskJob
	pass  rundir.Pass
	// requested is the PRs a person asked for while a pass was running,
	// for the pass after it.
	requested []int
	code      int
	// namedChecked says the PRs a person named were checked against the
	// list, and absent says one of them is not on it.
	namedChecked, absent bool
}

// workers is what the loop hands to anything that starts a goroutine: the
// context that stops it and the group that waits for it.
type workers struct {
	ctx   context.Context
	group *sync.WaitGroup
	// timer starts the next look.
	timer *time.Timer
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
	timer := time.NewTimer(time.Hour)
	timer.Stop()
	defer timer.Stop()
	w := workers{ctx: ctx, group: &group, timer: timer}

	r := e.look(w, e.autofixAtStart(w, run{snap: *e.Snapshot()}))
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
		r = e.afterLook(w, r)
		r = e.stoppedIfQuiet(r)
		e.publish(r)
	}
	e.settleMarks()
	return Result{Code: endCode(r), Final: e.Snapshot()}
}

func (e *Engine) handle(w workers, r run, in input) run {
	switch in := in.(type) {
	case Quit:
		return e.quit(w, r)
	case listed:
		r.pending--
		r = e.withListing(r, in)
	case mineListed:
		r.pending--
		return e.mineListed(w, r, in)
	case mineRefreshed:
		return mineRefreshedInto(r, in)
	case jobReaped:
		return e.reaped(w, r, in)
	case guardFired:
		return e.guardFired(r, in)
	case jobSettled:
		return e.settled(w, r, in)
	case ReviewNow:
		return e.reviewNow(w, r, in.PR)
	case StopJob:
		return e.stopJob(r, in.PR)
	case SetFocus:
		r.snap.Focus = in.Text
	case SetWatch:
		return e.setWatch(w, r, in.On)
	case RunTask:
		return e.requestTask(w, r, in.Request)
	case Babysit:
		return e.setBabysit(w, r, in.Change)
	case FixAll:
		return e.fixAll(w, r)
	case taskReady:
		return e.taskReady(w, r, in)
	case taskReaped:
		return e.taskReaped(w, r, in)
	case taskSettled:
		return e.taskSettled(w, r, in)
	case taskGuard:
		return e.taskGuard(r, in)
	}
	return r
}

// quit ends a run that had not ended by itself. That is an interrupt,
// whoever asked: a key in the view, or a signal. A run with reviews alive
// stops them first and ends when the last one has been collected.
func (e *Engine) quit(w workers, r run) run {
	switch {
	case r.snap.Phase == Done || r.snap.Phase == Stopping:
		return r
	case r.snap.Phase == Reviewing || !tasksQuiet(r):
		return e.stopAll(w, r)
	}
	r.snap.Phase = Done
	// A held run that is not watching has done its work, and ends with the
	// status of that work. Any other run would have gone on.
	if !e.cfg.HoldOpen || r.snap.Watching || !r.snap.Looked {
		r.code = ExitInterrupted
	}
	return withTaskStatus(r)
}

// withTaskStatus fails a run whose own status is clean when a task did not
// run to its end. A failed task is a failed job, as a failed review is.
func withTaskStatus(r run) run {
	if r.code == ExitOK && tasksFailed(r) {
		r.code = ExitFailed
	}
	return r
}

// stoppedIfQuiet ends a run that is stopping once nothing it started is
// still alive: no review, no task, no worktree being made.
func (e *Engine) stoppedIfQuiet(r run) run {
	if r.snap.Phase != Stopping || !tasksQuiet(r) {
		return r
	}
	for _, j := range r.jobs {
		if !j.view.State.Settled() {
			return r
		}
	}
	r.snap.Phase = Done
	r.code = ExitInterrupted
	return r
}

// publish makes a snapshot of the run and tells the reader there is one.
func (e *Engine) publish(r run) {
	s := r.snap
	s.Seq = e.Snapshot().Seq + 1
	s.At = time.Now()
	s.Jobs = views(r.jobs)
	s.Tasks = taskViews(r.tasks)
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

// settleMarks waits for every reaction on its way off before the run ends.
// The process may end right after Run, and a call it did not wait for is a
// reaction left on a PR that says a review is running.
func (e *Engine) settleMarks() {
	if e.deps.Marks == nil {
		return
	}
	for _, pr := range e.deps.Marks.Settle() {
		e.journal(Noted{Text: fmt.Sprintf("could not set or remove the eyes reaction on PR #%d; the review is unaffected", pr)})
	}
}
