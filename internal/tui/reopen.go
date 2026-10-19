package tui

import (
	"fmt"
	"slices"

	tea "charm.land/bubbletea/v2"

	"github.com/venabots/autoreview/internal/engine"
	"github.com/venabots/autoreview/internal/sanitize"
)

// reopenWait is a removed worktree that r asked the engine to make again.
// seen is how many tries had ended when r was pressed, so a later answer
// is told from an earlier one, even when snapshots arrive merged.
type reopenWait struct {
	pr   int
	seen int
}

// reopenPressed asks the engine to make the task's worktree again. A
// second r while that runs asks nothing: one worktree, one tab.
func (m Model) reopenPressed(job engine.Job) Model {
	if job.Reopening || slices.ContainsFunc(m.reopens, func(w reopenWait) bool { return w.pr == job.PR }) {
		return m.flashed(fmt.Sprintf("still making PR #%d's worktree again", job.PR))
	}
	m.eng.Do(engine.ReopenTask{PR: job.PR})
	m.reopens = append(slices.Clone(m.reopens), reopenWait{pr: job.PR, seen: job.Reopens})
	return m.flashed(fmt.Sprintf("making PR #%d's worktree again, to resume its session", job.PR))
}

// reopened takes the engine's answers to the worktrees r is waiting on:
// a tab for each one made again, and the reason for one that was not.
func (m Model) reopened() (Model, []tea.Cmd) {
	waiting := []reopenWait{}
	cmds := []tea.Cmd{}
	for _, w := range m.reopens {
		job, ok := m.reopenAnswer(w)
		switch {
		case !ok:
			waiting = append(waiting, w)
		case job.ReopenError != "":
			m = m.flashed(fmt.Sprintf("not resuming PR #%d's task: %s", w.pr, sanitize.Line(job.ReopenError)))
		default:
			m = m.flashed(fmt.Sprintf("made PR #%d's worktree again; opening its task in a new tab", w.pr))
			cmds = append(cmds, m.openTab(w.pr, "task", m.taskCommand(job)))
		}
	}
	m.reopens = waiting
	return m, cmds
}

// reopenAnswer is the task on the PR whose try ended after r was pressed.
func (m Model) reopenAnswer(w reopenWait) (engine.Job, bool) {
	i := slices.IndexFunc(m.snap.Tasks, func(j engine.Job) bool { return j.PR == w.pr && j.Reopens > w.seen })
	if i < 0 {
		return engine.Job{}, false
	}
	return m.snap.Tasks[i], true
}
