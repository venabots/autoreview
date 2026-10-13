package report

import (
	"fmt"
	"strings"

	"github.com/venabots/autoreview/internal/engine"
	"github.com/venabots/autoreview/internal/words"
)

var taskHeader = []string{"PR", "TASK", "RESULT", "BRANCH", "TIME", "SESSION"}

// Tasks is the report of the tasks a run did on your own PRs: one aligned
// table, then each worktree that was kept. A task's columns are not a
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
	case t.Verdict == "pushed":
		return noun + " pushed"
	case t.Verdict == "nothing pushed":
		return noun + " pushed nothing"
	}
	return noun + " done"
}

// KeptLine names a worktree a task left behind and why. Removing it would
// have lost work, so the person must find it.
func KeptLine(t engine.Job) string {
	return fmt.Sprintf("PR #%d's worktree has %s: %s", t.PR, t.Kept, t.Worktree)
}
