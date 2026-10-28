package report

import (
	"fmt"
	"strings"

	"github.com/venabots/autoreview/internal/engine"
	"github.com/venabots/autoreview/internal/words"
)

var taskHeader = []string{"PR", "TASK", "RESULT", "BRANCH", "TIME", "SESSION"}

// Tasks is the report of the tasks a run did on your own PRs: one aligned
// table, then each worktree that was kept and each PR a task says needs a
// person, such as one to close. A task's columns are not a
// review's: it has no verdict or findings, and what matters is whether it
// pushed.
func Tasks(tasks []engine.Job) string {
	rows := [][]string{taskHeader}
	for _, t := range tasks {
		rows = append(rows, []string{
			fmt.Sprintf("#%d", t.PR),
			t.Task.Tag(),
			Result(t),
			orDash(t.Verdict),
			words.Duration(t.Elapsed),
			orDash(t.Session),
		})
	}
	lines := []string{align(rows)}
	for _, t := range tasks {
		if t.Kept != "" {
			lines = append(lines, KeptLine(t))
		}
		if TaskAdvice(t) != "" {
			lines = append(lines, fmt.Sprintf("PR #%d: %s", t.PR, TaskWhy(t)))
		}
	}
	lines = append(lines, ErrorLines(tasks)...)
	return strings.Join(lines, "\n") + "\n"
}

// TaskOutcome is a task that has ended, in the few words a list row has
// room for: whether it pushed, or how it failed. A task that ran to its end
// and whose branch could not be read back says only that it is done.
func TaskOutcome(t engine.Job) string {
	noun := t.Task.Noun()
	switch {
	case t.State == engine.JobTimedOut:
		return noun + " timed out"
	case t.State == engine.JobFailed && t.Outcome == "stopped":
		return noun + " stopped"
	case t.State == engine.JobFailed:
		return noun + " " + Result(t)
	case TaskAdvice(t) != "":
		return TaskAdvice(t)
	case t.Verdict == "pushed":
		return noun + " pushed"
	case t.Verdict == "nothing pushed":
		return noun + " pushed nothing"
	}
	return noun + " done"
}

// TaskAdvice is what a task's report asks of the person, such as "should
// close", or "" when it asks nothing or the task wrote no report.
func TaskAdvice(t engine.Job) string {
	if t.TaskTrailer == nil {
		return ""
	}
	return words.TaskAdvice(t.TaskTrailer.Outcome)
}

// TaskWhy is the advice and the reason a task gave, for the end of a log
// line: "should close: main already has it", or the reason alone. It is
// "" when the task gave neither.
func TaskWhy(t engine.Job) string {
	advice, why := TaskAdvice(t), ""
	if t.TaskTrailer != nil {
		why = t.TaskTrailer.Why
	}
	switch {
	case advice != "" && why != "":
		return advice + ": " + why
	case advice != "":
		return advice
	}
	return why
}

// KeptLine names a worktree a task left behind and why. Removing it would
// have lost work, so the person must find it.
func KeptLine(t engine.Job) string {
	return fmt.Sprintf("PR #%d's worktree has %s: %s", t.PR, t.Kept, t.Worktree)
}
