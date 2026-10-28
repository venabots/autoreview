package tui

import (
	"testing"

	"github.com/venabots/autoreview/internal/engine"
	"github.com/venabots/autoreview/internal/review"
	"github.com/venabots/autoreview/internal/task"
)

// PR #2931: the conflict fix stopped because main already had the change.
// The row and the pane said only "pushed nothing", and the PR looked
// forgotten. They now say what the task advised, and why.
func TestATaskThatAdvisesClosingThePRSaysSoOnTheRowAndInThePane(t *testing.T) {
	m, _ := onMine(t, func(s *engine.Snapshot) {
		s.Tasks = []engine.Job{{PR: 4, Task: task.Conflicts, State: engine.JobDone, Verdict: "nothing pushed",
			TaskTrailer: &review.TaskTrailer{Outcome: review.TaskShouldClose, Why: "main already has this change, from #2943"}}}
	})
	wantOnScreen(t, m,
		"#4 · should close · conflicts",
		"advice    should close",
		"why       main already has this change, from #2943",
	)
	wantNotOnScreen(t, m, "pushed nothing")
}

func TestATaskThatAdvisesNothingStillSaysWhy(t *testing.T) {
	m, _ := onMine(t, func(s *engine.Snapshot) {
		s.Tasks = []engine.Job{{PR: 4, Task: task.Fix, State: engine.JobDone, Verdict: "nothing pushed",
			TaskTrailer: &review.TaskTrailer{Outcome: review.TaskNothingNeeded, Why: "every thread is answered"}}}
	})
	wantOnScreen(t, m, "fix pushed nothing", "why       every thread is answered")
	wantNotOnScreen(t, m, "advice")
}
