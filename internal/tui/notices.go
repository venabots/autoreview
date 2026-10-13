package tui

import (
	"fmt"

	"github.com/venabots/autoreview/internal/engine"
	"github.com/venabots/autoreview/internal/report"
	"github.com/venabots/autoreview/internal/sanitize"
)

// noticing says in the footer what a new snapshot brought that nobody
// pressed a key for just now. A task is asked for with one key and ends
// minutes later, or never starts, and the person may be on another row or
// another tab by then.
//
// It compares the snapshot on screen with the next one, so the same news
// is said once.
func (m Model) noticing(next *engine.Snapshot) Model {
	for _, pr := range next.Mine {
		why := next.NotStarted[pr.Number]
		if why != "" && why != m.snap.NotStarted[pr.Number] {
			return m.flashed(sanitize.Line(why))
		}
	}
	for i := len(next.Tasks) - 1; i >= 0; i-- {
		if t := next.Tasks[i]; t.State.Settled() && !settledBefore(m.snap.Tasks, i, t) {
			return m.flashed(fmt.Sprintf("PR #%d: %s", t.PR, report.TaskOutcome(t)))
		}
	}
	return m
}

// settledBefore says the task had already ended in the snapshot on screen.
// Tasks keep their place in the list, so the same place holds the same
// task.
func settledBefore(tasks []engine.Job, i int, t engine.Job) bool {
	return i < len(tasks) && tasks[i].PR == t.PR && tasks[i].State.Settled()
}
