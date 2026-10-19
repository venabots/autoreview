package tui

import (
	"context"
	"fmt"
	"slices"

	tea "charm.land/bubbletea/v2"

	"github.com/venabots/autoreview/internal/babysit"
	"github.com/venabots/autoreview/internal/engine"
	"github.com/venabots/autoreview/internal/mine"
	"github.com/venabots/autoreview/internal/task"
	"github.com/venabots/autoreview/internal/words"
)

// notMine is what a key for your own PRs says on the Review tab.
const notMine = "F, a and A work on your own PRs: tab shows them"

// selectedMine is the row under the cursor on the My PRs tab.
func (m Model) selectedMine() (mineRow, bool) {
	pr, ok := m.selected()
	if !ok || m.tab != tabMine {
		return mineRow{}, false
	}
	for _, row := range mineRowsOf(m.snap) {
		if row.pr.Number == pr {
			return row, true
		}
	}
	return mineRow{}, false
}

// minePicked is the selected one of your PRs, or the view after it said
// why there is none.
func (m Model) minePicked() (mineRow, Model, bool) {
	if m.tab != tabMine {
		return mineRow{}, m.flashed(notMine), false
	}
	row, ok := m.selectedMine()
	if !ok {
		return mineRow{}, m.flashed("you have no open PRs in this repo"), false
	}
	return row, m, true
}

// taskPressed asks for a task on the selected PR. Everything the engine
// would refuse is refused here first, so the footer says why at once.
func (m Model) taskPressed(k task.Kind) Model {
	row, m, ok := m.minePicked()
	if !ok {
		return m
	}
	pr := row.pr.Number
	switch {
	case m.snap.Refusals[k] != "":
		return m.flashed(fmt.Sprintf("not %s PR #%d: %s", k.Doing(), pr, m.snap.Refusals[k]))
	case row.pr.CrossRepo:
		return m.flashed(babysit.ForkError{PR: pr}.Error())
	case row.live():
		return m.flashed(fmt.Sprintf("PR #%d has a task running already", pr))
	}
	m.eng.Do(engine.RunTask{Request: task.Request{PR: pr, Kind: k, Title: row.pr.Title, Branch: row.pr.Branch, CrossRepo: row.pr.CrossRepo}})
	return m.flashed(fmt.Sprintf("%s PR #%d", k.Doing(), pr))
}

// fixAllPressed asks for one fix on each of your PRs that needs one. The
// view counts them by the engine's own rule, so the footer says at once
// how many start, or why none does.
func (m Model) fixAllPressed() Model {
	if m.tab != tabMine {
		return m.flashed(notMine)
	}
	if why := m.snap.Refusals[task.Fix]; why != "" {
		return m.flashed("not fixing your PRs: " + why)
	}
	n := len(babysit.ToFix(m.snap.Mine, m.taskLive))
	if n == 0 {
		return m.flashed("none of your PRs needs a fix now")
	}
	m.eng.Do(engine.FixAll{})
	return m.flashed("fixing " + words.Count(n, "PR") + " of yours")
}

// taskLive says the PR has a task that has not ended.
func (m Model) taskLive(pr int) bool {
	return slices.ContainsFunc(m.snap.Tasks, func(j engine.Job) bool { return j.PR == pr && !j.State.Settled() })
}

// autofixPressed turns autofix on or off for the selected PR.
func (m Model) autofixPressed() Model {
	row, m, ok := m.minePicked()
	if !ok {
		return m
	}
	c, err := m.snap.Babysit.Toggle(row.pr)
	if err != nil {
		return m.flashed(err.Error())
	}
	return m.babysat(c, fmt.Sprintf("PR #%d", row.pr.Number), autofixSaid(c, row.pr))
}

// autofixSaid is what turning autofix on or off says. On an approved PR
// with work left, autofix starts nothing, so it says why and names the key
// that fixes the PR now.
func autofixSaid(c babysit.Change, pr mine.PR) string {
	if c.On && babysit.LeftForAPerson(pr) {
		return fmt.Sprintf("autofixing PR #%d; it is approved, so autofix fixes only its conflicts: f fixes the rest", pr.Number)
	}
	return c.Said()
}

// autofixAllPressed turns autofix on or off for every PR of yours.
func (m Model) autofixAllPressed() Model {
	if m.tab != tabMine {
		return m.flashed(notMine)
	}
	c := m.snap.Babysit.ToggleAll()
	return m.babysat(c, "your PRs", c.Said())
}

// babysat sends a change and flashes said. Autofix runs the fix, so a fix
// that is refused refuses autofix too.
func (m Model) babysat(c babysit.Change, what, said string) Model {
	if why := m.snap.Refusals[task.Fix]; c.On && why != "" {
		return m.flashed(fmt.Sprintf("not autofixing %s: %s", what, why))
	}
	m.eng.Do(engine.Babysit{Change: c})
	return m.flashed(said)
}

// resumingTask reopens the selected PR's last task in its worktree. A
// session belongs to the directory it ran in, so a worktree that was
// removed leaves nothing to reopen it in.
func (m Model) resumingTask() (tea.Model, tea.Cmd) {
	row, m, ok := m.minePicked()
	if !ok {
		return m, nil
	}
	pr := row.pr.Number
	switch {
	case row.task == nil:
		return m.flashed(fmt.Sprintf("PR #%d has had no task in this run", pr)), nil
	case row.live():
		return m.flashed(fmt.Sprintf("PR #%d has a task running; resume it when it ends", pr)), nil
	case sessionOf(*row.task) == "":
		return m.flashed(fmt.Sprintf("PR #%d's task has no session to resume", pr)), nil
	case row.task.Kept == "" && !row.task.Yours:
		return m.flashed(fmt.Sprintf("PR #%d's task worktree is gone; its session was %s", pr, sessionOf(*row.task))), nil
	}
	command := m.act.ResumeCommand(row.task.Worktree, row.task.Orchestrator.ReopenCommand(sessionOf(*row.task)))
	return m.flashed(fmt.Sprintf("opening PR #%d's task in a new tab", pr)), m.openTab(pr, "task", command)
}

// openTab opens a terminal tab off the update loop: a slow launch must
// not freeze the screen.
func (m Model) openTab(pr int, what, command string) tea.Cmd {
	label := fmt.Sprintf("PR %d Resume", pr)
	openTab := m.act.OpenTab
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), actionTimeout)
		defer cancel()
		where, err := openTab(ctx, label, command)
		return resumedMsg{pr: pr, what: what, where: where, err: err}
	}
}

// stopTaskPressed asks before it stops the selected PR's task.
func (m Model) stopTaskPressed() Model {
	row, ok := m.selectedMine()
	switch {
	case !ok:
		return m.flashed("there is no task to stop here")
	case row.task == nil || row.task.State != engine.JobRunning:
		return m.flashed(fmt.Sprintf("PR #%d has no task running", row.pr.Number))
	}
	return m.asking(modal{kind: modalStop, pr: row.pr.Number, what: "task"})
}
