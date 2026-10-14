package engine

import (
	"context"
	"crypto/rand"
	"fmt"
	"slices"
	"time"

	"github.com/venabots/autoreview/internal/proc"
	"github.com/venabots/autoreview/internal/review"
	"github.com/venabots/autoreview/internal/rundir"
	"github.com/venabots/autoreview/internal/sanitize"
	"github.com/venabots/autoreview/internal/session"
	"github.com/venabots/autoreview/internal/task"
	"github.com/venabots/autoreview/internal/worktree"
)

// Tasks run beside the passes. They share the slots that --jobs sets, but
// a pass does not wait for them: a task starts only from a key, never
// enters the review queue, and has no cap and no rest.

// RunTask starts a task on one of your own PRs.
type RunTask struct{ Request task.Request }

func (RunTask) input()   {}
func (RunTask) command() {}

// taskReady says a task's worktree has been made, or could not be.
type taskReady struct {
	pr   int
	tree worktree.Prepared
	err  error
}

// taskReaped, taskSettled and taskGuard are the task lane's versions of a
// review's messages.
type taskReaped struct {
	pr      int
	exit    proc.Exit
	elapsed time.Duration
	meta    *review.Meta
}

type taskSettled struct {
	pr      int
	verdict string
	read    bool
	kept    string
}

type taskGuard struct{ pr int }

func (taskReady) input()   {}
func (taskReaped) input()  {}
func (taskSettled) input() {}
func (taskGuard) input()   {}

// taskJob is one task as the engine holds it.
type taskJob struct {
	job
	request task.Request
	tree    worktree.Prepared
	// dropped is set for a task whose run ended while its worktree was
	// being made. The worktree is removed as soon as it is made.
	dropped bool
}

// live says the task holds a worktree or a slot, or will.
func (t taskJob) live() bool {
	return !t.dropped && !t.view.State.Settled()
}

func taskViews(tasks []taskJob) []Job {
	out := make([]Job, len(tasks))
	for i, t := range tasks {
		out[i] = t.view
	}
	return out
}

func liveTask(tasks []taskJob, pr int) int {
	return slices.IndexFunc(tasks, func(t taskJob) bool { return t.view.PR == pr && t.live() })
}

func withTask(r run, i int, t taskJob) run {
	tasks := slices.Clone(r.tasks)
	tasks[i] = t
	r.tasks = tasks
	return r
}

// withoutTask forgets a task that never ran. Kept, it would hold its PR
// as busy and keep a stopping run from ending.
func withoutTask(r run, i int) run {
	r.tasks = slices.Delete(slices.Clone(r.tasks), i, i+1)
	return r
}

// requestTask starts a task, or says why it does not. A task changes your
// PR's branch, so it is refused for any reason that would make it run
// wrong, before anything is made.
func (e *Engine) requestTask(w workers, r run, req task.Request) run {
	why := ""
	switch {
	case r.snap.Phase == Stopping || r.snap.Phase == Done:
		return r
	case liveTask(r.tasks, req.PR) >= 0:
		why = "a job on it is already running"
	default:
		why = e.refusal(req.Kind)
	}
	if why != "" {
		e.journal(Noted{Text: fmt.Sprintf("not %s PR #%d: %s", req.Kind.Doing(), req.PR, why)})
		return r
	}
	t := taskJob{request: req, job: job{view: Job{
		PR: req.PR, Title: req.Title, Author: e.cfg.Repo.Me, Task: req.Kind,
		State: JobPreparing, Orchestrator: e.cfg.Orchestrator,
	}}}
	r.tasks = append(slices.Clone(r.tasks), t)
	r.snap.NotStarted = withoutNote(r.snap.NotStarted, req.PR)
	w.group.Go(func() {
		tree, err := e.deps.Prepare(w.ctx, req)
		e.in.put(taskReady{pr: req.PR, tree: tree, err: err})
	})
	return r
}

// taskReady takes a worktree that was made. A task whose run is ending
// gives its worktree back at once.
func (e *Engine) taskReady(w workers, r run, in taskReady) run {
	i := slices.IndexFunc(r.tasks, func(t taskJob) bool { return t.view.PR == in.pr && t.view.State == JobPreparing })
	if i < 0 {
		return r
	}
	t := r.tasks[i]
	switch {
	case in.err != nil:
		why := fmt.Sprintf("not %s PR #%d: %s", t.request.Kind.Doing(), t.view.PR, sanitize.Line(in.err.Error()))
		e.journal(Noted{Text: why})
		r.snap.NotStarted = withNote(r.snap.NotStarted, t.view.PR, why)
		return fixEnded(withoutTask(r, i), t.request.Kind, t.view.PR)
	case t.dropped || r.snap.Phase == Stopping:
		e.release(w, in.tree)
		return withoutTask(r, i)
	}
	t.tree = in.tree
	t.view.State = JobQueued
	t.view.Worktree = in.tree.Path
	return e.fill(w, withTask(r, i, t))
}

// release removes a worktree, off the engine's goroutine. Run waits for it
// before it returns.
func (e *Engine) release(w workers, tree worktree.Prepared) {
	w.group.Go(func() { _, _ = e.deps.Finish(context.Background(), tree) })
}

// fillTasks starts queued tasks while a slot is free.
func (e *Engine) fillTasks(w workers, r run) run {
	for i := range r.tasks {
		if busySlots(r) >= max(e.cfg.Jobs, 1) {
			break
		}
		if r.tasks[i].view.State == JobQueued {
			r = e.startTask(w, r, i)
		}
	}
	return r
}

// busySlots counts the reviews and the tasks whose process is alive.
func busySlots(r run) int {
	n := running(r.jobs)
	for _, t := range r.tasks {
		if t.view.State == JobRunning {
			n++
		}
	}
	return n
}

// startTask runs one task in its worktree. It gets a session of its own,
// pinned so the view can follow it and r can reopen it, and never the PR's
// review session.
func (e *Engine) startTask(w workers, r run, i int) run {
	t := r.tasks[i]
	pr := t.view.PR
	dir, err := e.deps.Run.Tasks(t.request.Kind.Tag())
	if err != nil {
		return e.taskFailedToStart(w, r, i, err)
	}
	if t.view.Orchestrator.SupportsSessions() {
		if id, err := session.FreshID(rand.Reader); err == nil {
			t.plan = session.Plan{ID: id, Flag: session.FlagPin}
		}
	}
	t.spec = review.SpecFor(e.taskRequest(t, dir))
	started, err := e.deps.Procs.Start(proc.Command{
		Path: t.spec.Path, Args: t.spec.Args, Env: slices.Concat(t.spec.Env, t.tree.Env()), Dir: t.spec.Dir, Stdout: t.spec.Stdout, Stderr: t.spec.Stderr,
	})
	if err != nil {
		return e.taskFailedToStart(w, r, i, err)
	}
	t.proc = started
	t.view.State = JobRunning
	t.view.StartedAt = time.Now()
	t.view.PassDir = dir.Dir
	t.view.Stderr = t.spec.Stderr
	t.view.Follow = t.plan.ID
	if t.spec.Guard > 0 {
		t.guard = time.AfterFunc(t.spec.Guard, func() { e.in.put(taskGuard{pr: pr}) })
	}
	f := facts{view: t.view, plan: t.plan, spec: t.spec, pass: dir}
	tree, before := t.tree, t.tree.Head
	w.group.Go(func() {
		exit := started.Wait()
		elapsed := time.Since(f.view.StartedAt)
		e.in.put(taskReaped{pr: pr, exit: exit, elapsed: elapsed, meta: readMeta(f)})
		e.in.put(e.taskOutcome(w.ctx, pr, before, tree))
	})
	e.journal(JobStarted{Job: t.view})
	return withTask(r, i, t)
}

func (e *Engine) taskRequest(t taskJob, dir rundir.Pass) review.Request {
	pr := t.view.PR
	return review.Request{
		PR: pr, Orchestrator: t.view.Orchestrator, Task: t.request.Kind.Skill(), Session: t.plan,
		Timeout: e.cfg.Timeout, Budget: e.cfg.Budget, Dashp: e.cfg.Dashp, Dir: t.tree.Path,
		Stdout: dir.Stdout(pr), Stderr: dir.Stderr(pr), Meta: dir.Meta(pr),
	}
}

// taskOutcome reads what a task did to its PR, then gives its worktree
// back or says why it is kept.
func (e *Engine) taskOutcome(ctx context.Context, pr int, before string, tree worktree.Prepared) taskSettled {
	verdict, read := "unknown", false
	if e.deps.Pushed != nil {
		verdict, read = e.deps.Pushed(ctx, pr, before)
	}
	kept, _ := e.deps.Finish(context.Background(), tree)
	return taskSettled{pr: pr, verdict: verdict, read: read, kept: kept}
}

// taskFailedToStart fails a task whose process never ran, and gives its
// worktree back: nothing else would, and it would stay on disk.
func (e *Engine) taskFailedToStart(w workers, r run, i int, err error) run {
	t := r.tasks[i]
	t.view.State = JobFailed
	t.view.Outcome = review.OutcomeWord(review.Exit{Code: exitNotStarted}, false)
	t.view.Error = sanitize.Line(err.Error())
	e.release(w, t.tree)
	e.journal(JobFinished{Job: t.view})
	return fixEnded(withTask(r, i, t), t.request.Kind, t.view.PR)
}

func runningTask(r run, pr int) int {
	return slices.IndexFunc(r.tasks, func(t taskJob) bool { return t.view.PR == pr && t.view.State == JobRunning })
}

// taskReaped frees the slot of a task whose process ended.
func (e *Engine) taskReaped(w workers, r run, in taskReaped) run {
	i := runningTask(r, in.pr)
	if i < 0 {
		return r
	}
	t := r.tasks[i]
	if t.guard != nil {
		t.guard.Stop()
	}
	t.exit = review.Exit{Code: in.exit.Code, Signaled: in.exit.Signaled}
	t.view.State = JobFinishing
	t.view.Elapsed = in.elapsed
	t.view = withMeta(t.view, t.plan, in.meta, false)
	return e.fill(w, withTask(r, i, t))
}

func (e *Engine) taskGuard(r run, in taskGuard) run {
	i := runningTask(r, in.pr)
	if i < 0 {
		return r
	}
	t := r.tasks[i]
	t.guardTripped = true
	t.proc.Stop()
	return withTask(r, i, t)
}

// taskSettled gives a task its end state. Its result is GitHub's word on
// whether the branch moved, as a review's verdict is GitHub's word. Your
// PRs are read again, so the row shows what the task left.
func (e *Engine) taskSettled(w workers, r run, in taskSettled) run {
	i := slices.IndexFunc(r.tasks, func(t taskJob) bool { return t.view.PR == in.pr && t.view.State == JobFinishing })
	if i < 0 {
		return r
	}
	t := r.tasks[i]
	switch state := review.Classify(t.exit, t.guardTripped, false); {
	case t.stopped:
		t.view.State = JobFailed
		t.view.Outcome = review.OutcomeWord(t.exit, true)
	case state == review.Done:
		t.view.State = JobDone
	case state == review.TimedOut:
		t.view.State = JobTimedOut
	default:
		t.view.State = JobFailed
		t.view.Outcome = review.OutcomeWord(t.exit, false)
	}
	t.view.Verdict = in.verdict
	t.view.Kept = in.kept
	if !in.read {
		e.journal(Noted{Text: fmt.Sprintf("could not read PR #%d's branch back from GitHub; whether it pushed is unknown", t.view.PR)})
	}
	if in.kept != "" {
		e.journal(Noted{Text: fmt.Sprintf("PR #%d's worktree has %s: %s", t.view.PR, in.kept, t.tree.Path)})
	}
	e.journal(JobFinished{Job: t.view})
	return e.refreshMine(w, fixEnded(withTask(r, i, t), t.request.Kind, t.view.PR))
}

// stopTask stops one task a person pointed at.
func (e *Engine) stopTask(r run, pr int) (run, bool) {
	i := runningTask(r, pr)
	if i < 0 {
		return r, false
	}
	t := r.tasks[i]
	t.stopped = true
	t.proc.Stop()
	e.journal(Noted{Text: fmt.Sprintf("stopped the task on PR #%d", pr)})
	return withTask(r, i, t), true
}

// stopTasks stops every live task when the run ends. A queued task never
// starts and gives its worktree back. A task still making its worktree
// gives it back when it is made.
func (e *Engine) stopTasks(w workers, r run) run {
	tasks := []taskJob{}
	for _, t := range r.tasks {
		switch t.view.State {
		case JobRunning:
			t.stopped = true
			t.proc.Stop()
		case JobQueued:
			e.release(w, t.tree)
			continue
		case JobPreparing:
			t.dropped = true
		}
		tasks = append(tasks, t)
	}
	r.tasks = tasks
	return r
}

// tasksQuiet says no task holds a process or a worktree it has not given
// back.
func tasksQuiet(r run) bool {
	for _, t := range r.tasks {
		switch t.view.State {
		case JobPreparing, JobRunning, JobFinishing:
			return false
		}
	}
	return true
}

// tasksFailed says a task did not run to its end.
func tasksFailed(r run) bool {
	return slices.ContainsFunc(r.tasks, func(t taskJob) bool {
		return t.view.State.Settled() && t.view.State != JobDone
	})
}
