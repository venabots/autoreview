package tui

import (
	"fmt"
	"time"

	"github.com/venabots/autoreview/internal/babysit"
	"github.com/venabots/autoreview/internal/engine"
	"github.com/venabots/autoreview/internal/mine"
	"github.com/venabots/autoreview/internal/report"
	"github.com/venabots/autoreview/internal/sanitize"
	"github.com/venabots/autoreview/internal/words"
)

// mineRow is one of your PRs: where it stands on GitHub, and the newest
// task this run did on it.
type mineRow struct {
	pr mine.PR
	// task is the newest task on the PR in this run, or nil.
	task *engine.Job
	// notStarted is why the last task asked for on the PR never ran, or
	// empty.
	notStarted string
}

func mineRowsOf(s *engine.Snapshot) []mineRow {
	rows := make([]mineRow, len(s.Mine))
	for i, pr := range s.Mine {
		rows[i] = mineRow{pr: pr, notStarted: s.NotStarted[pr.Number]}
		for j := range s.Tasks {
			if s.Tasks[j].PR == pr.Number {
				rows[i].task = &s.Tasks[j]
			}
		}
	}
	return rows
}

// live says the PR has a task that has not ended: one being prepared,
// waiting for a slot, or running.
func (r mineRow) live() bool {
	return r.task != nil && !r.task.State.Settled()
}

// moving says the task is doing something now, so its row has the
// spinner.
func (r mineRow) moving() bool {
	return r.live() && r.task.State != engine.JobQueued
}

// tallyTasks counts the newest task on each of your PRs by where it
// stands, or is empty when no PR has had one.
func tallyTasks(rows []mineRow) string {
	running, queued, finished := 0, 0, 0
	for _, row := range rows {
		switch {
		case row.task == nil:
		case row.task.State.Settled():
			finished++
		case row.task.State == engine.JobQueued:
			queued++
		default:
			running++
		}
	}
	parts := []string{}
	for _, part := range []struct {
		n    int
		what string
	}{{running, "running"}, {queued, "queued"}, {finished, "finished"}} {
		if part.n > 0 {
			parts = append(parts, words.Count(part.n, "task")+" "+part.what)
		}
	}
	return joinDots(parts)
}

func mineListRows(rows []mineRow, babysat babysit.State, now time.Time, spin string) []listRow {
	out := make([]listRow, len(rows))
	for i, row := range rows {
		out[i] = listRow{
			pr:   row.pr.Number,
			head: headLine(mineIcon(row, spin), row.pr.Number, mineWord(row, babysat, now)),
			sub:  subLine(row.pr.Branch, row.pr.Title),
		}
	}
	return out
}

func mineIcon(row mineRow, spin string) span {
	switch {
	case row.moving():
		return styled(spin, spinnerStyle)
	case row.live():
		return styled("·", dimStyle)
	}
	return reviewIcon(row.pr)
}

// mineWord is what a task is doing now, or what blocks the PR. A babysat
// PR says so first: it is the one thing on the row the person chose. How
// the last task ended is said next, or that it never ran: without it the
// row reads after a fix as it did before, and the fix looks like nothing.
func mineWord(row mineRow, babysat babysit.State, now time.Time) word {
	if row.live() {
		style := spinnerStyle
		if row.task.State == engine.JobQueued {
			style = dimStyle
		}
		return word{report.Progress(*row.task, now), style}
	}
	w := blockerWord(row.pr)
	switch {
	case row.notStarted != "":
		w.text = "did not start · " + w.text
	case row.task != nil:
		w.text = report.TaskOutcome(*row.task) + " · " + w.text
	}
	if babysat.Babysat(row.pr.Number) && !row.pr.CrossRepo {
		w.text = "autofix · " + w.text
	}
	return w
}

// mineDetail is the right pane for one of your PRs: where it stands, then
// whether it is babysat, then its task.
func mineDetail(row mineRow, babysat babysit.State, now time.Time) []line {
	pr := row.pr
	lines := []line{
		titleLine(pr.Number, pr.Title),
		note(sanitize.Line(pr.Branch), dimStyle),
		{},
		field("review", reviewersWord(pr)),
		field("checks", ciWord(pr.CI)),
		field("threads", threadsWord(pr)),
		field("merge", mergeWord(pr.Merge)),
	}
	if pr.Draft {
		lines = append(lines, field("draft", word{"yes", plainStyle}))
	}
	if pr.CrossRepo {
		lines = append(lines, field("from", word{"a fork", plainStyle}))
	}
	lines = append(lines, field("autofix", babysatWord(pr, babysat)))
	if row.notStarted != "" {
		lines = append(lines, line{}, note(sanitize.Line(row.notStarted), redStyle))
	}
	if row.task != nil {
		lines = append(lines, taskLines(*row.task, row.live(), now)...)
	}
	return lines
}

func babysatWord(pr mine.PR, s babysit.State) word {
	switch n := s.Fixes(pr.Number); {
	case !s.Babysat(pr.Number) || pr.CrossRepo:
		return word{"off", dimStyle}
	case babysit.LeftForAPerson(pr):
		return word{"on · approved, so only conflicts get a fix; f fixes the rest", plainStyle}
	case pr.Approved():
		// A push can dismiss the approval, so only a conflict, which stops
		// the merge anyway, gets a fix by itself.
		return word{"on · approved, so only conflicts get a fix", plainStyle}
	case n == 1:
		return word{"on · 1 fix so far", plainStyle}
	case n > 1:
		return word{fmt.Sprintf("on · %d fixes so far", n), plainStyle}
	}
	return word{"on", plainStyle}
}

// taskLines is the TASK section: what the task is or was doing, and where
// it ran.
func taskLines(job engine.Job, live bool, now time.Time) []line {
	heading := "LAST TASK"
	state := word{report.Result(job), outcomeColor(job)}
	if live {
		heading = "TASK"
		state = word{report.Progress(job, now), spinnerStyle}
	}
	lines := []line{{}, {styled(heading, headingStyle)}, field("task", word{job.Task.Tag(), plainStyle}), field("state", state)}
	if job.Verdict != "" {
		lines = append(lines, field("branch", word{job.Verdict, plainStyle}))
	}
	if job.Worktree != "" {
		where := job.Worktree
		switch {
		case job.Kept != "":
			where += " (kept: " + job.Kept + ")"
		case job.Yours:
			where += " (yours)"
		}
		lines = append(lines, field("worktree", word{where, dimStyle}))
	}
	if session := sessionOf(job); session != "" {
		lines = append(lines, field("session", word{session, dimStyle}))
	}
	if job.Error != "" {
		lines = append(lines, field("error", word{job.Error, redStyle}))
	}
	return lines
}

// sessionOf is the session a task ran in: the one its harness reported,
// or the one the run pinned for it.
func sessionOf(job engine.Job) string {
	if job.Session != "" {
		return job.Session
	}
	return job.Follow
}
