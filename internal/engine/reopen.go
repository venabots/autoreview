package engine

import (
	"fmt"
	"slices"

	"github.com/venabots/autoreview/internal/sanitize"
	"github.com/venabots/autoreview/internal/worktree"
)

// KeptToResume is why a worktree made again is kept: a person resumes the
// task's session in it, so the run never removes it.
const KeptToResume = "made again to resume its session"

// ReopenTask makes the worktree of the PR's last task again, where it was,
// after the task ended and its worktree was removed. A session belongs to
// the directory it ran in, so without the worktree it cannot be resumed.
type ReopenTask struct{ PR int }

func (ReopenTask) input()   {}
func (ReopenTask) command() {}

// taskReopened says a worktree was made again, or could not be.
type taskReopened struct {
	pr   int
	tree worktree.Prepared
	err  error
}

func (taskReopened) input() {}

// lastTask is the PR's newest task, the one the view shows and resumes.
func lastTask(tasks []taskJob, pr int) int {
	for i := len(tasks) - 1; i >= 0; i-- {
		if tasks[i].view.PR == pr {
			return i
		}
	}
	return -1
}

// reopening says a worktree of the PR is being made again. A task on the
// PR must wait for it: both would make a worktree at the same path.
func reopening(tasks []taskJob, pr int) bool {
	return slices.ContainsFunc(tasks, func(t taskJob) bool { return t.view.PR == pr && t.view.Reopening })
}

// keptToResume says a worktree of the PR was made again for a person. A
// task on the PR must not clear it: the session may be open in it.
func keptToResume(tasks []taskJob, pr int) bool {
	return slices.ContainsFunc(tasks, func(t taskJob) bool { return t.view.PR == pr && t.view.Kept == KeptToResume })
}

// reopenTask starts making a worktree again, off the engine's goroutine,
// or says why it does not. A second ask while one is under way does
// nothing: the first one's answer is the one the view waits for.
func (e *Engine) reopenTask(w workers, r run, pr int) run {
	i := lastTask(r.tasks, pr)
	why := ""
	switch {
	case r.snap.Phase == Stopping || r.snap.Phase == Done:
		return r
	case i >= 0 && r.tasks[i].view.Reopening:
		return r
	case e.deps.Reopen == nil:
		why = "this run makes no worktrees"
	case i < 0:
		why = "it has had no task in this run"
	case !r.tasks[i].view.State.Settled():
		why = "its task is still running"
	case r.tasks[i].view.Kept != "" || r.tasks[i].view.Yours:
		why = "it is still there"
	}
	if why != "" {
		e.journal(Noted{Text: fmt.Sprintf("not making PR #%d's worktree again: %s", pr, why)})
		return r
	}
	t := r.tasks[i]
	t.view.Reopening = true
	t.view.ReopenError = ""
	req := t.request
	w.group.Go(func() {
		tree, err := e.deps.Reopen(w.ctx, req)
		e.in.put(taskReopened{pr: pr, tree: tree, err: err})
	})
	return withTask(r, i, t)
}

// taskReopened takes the answer. The worktree made again is the person's:
// the run keeps it and never gives it back.
func (e *Engine) taskReopened(r run, in taskReopened) run {
	i := slices.IndexFunc(r.tasks, func(t taskJob) bool { return t.view.PR == in.pr && t.view.Reopening })
	if i < 0 {
		return r
	}
	t := r.tasks[i]
	t.view.Reopening = false
	t.view.Reopens++
	if in.err != nil {
		t.view.ReopenError = sanitize.Line(in.err.Error())
		e.journal(Noted{Text: fmt.Sprintf("not resuming PR #%d's task: %s", in.pr, t.view.ReopenError)})
		return withTask(r, i, t)
	}
	t.view.Worktree = in.tree.Path
	t.view.Kept = KeptToResume
	e.journal(Noted{Text: fmt.Sprintf("made PR #%d's worktree again at %s, to resume its session", in.pr, in.tree.Path)})
	return withTask(r, i, t)
}
