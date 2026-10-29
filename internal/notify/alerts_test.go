package notify

import (
	"slices"
	"testing"

	"github.com/venabots/autoreview/internal/engine"
	"github.com/venabots/autoreview/internal/review"
	"github.com/venabots/autoreview/internal/task"
)

// Only what needs the person alerts: a fix that asks for them, a fix that
// failed, a worktree left with work, and a change in what the doctor
// would say. Everything else stays in the log.
func TestOnlyWhatNeedsAPersonAlerts(t *testing.T) {
	down := engine.Health{Seated: 3, Answering: 2, Problems: []string{"opencode:glm does not answer"}, Blocking: true}
	cases := []struct {
		name string
		ev   engine.Event
		want []Alert
	}{
		{"a fix that says to close the PR", engine.JobFinished{Job: engine.Job{PR: 2931, Task: task.Conflicts, State: engine.JobDone, Verdict: "nothing pushed",
			TaskTrailer: &review.TaskTrailer{Outcome: review.TaskShouldClose, Why: "main already has this change"}}},
			[]Alert{{Title: "PR #2931 should close", Body: "main already has this change", Urgent: true}}},
		{"a fix that needs a person", engine.JobFinished{Job: engine.Job{PR: 4, Task: task.Fix, State: engine.JobDone,
			TaskTrailer: &review.TaskTrailer{Outcome: review.TaskNeedsPerson, Why: "two migrations take number 42"}}},
			[]Alert{{Title: "PR #4 needs you", Body: "two migrations take number 42", Urgent: true}}},
		{"a fix that failed", engine.JobFinished{Job: engine.Job{PR: 4, Task: task.Fix, State: engine.JobFailed, Outcome: "exit 3", Error: "boom"}},
			[]Alert{{Title: "PR #4: fix failed (exit 3)", Body: "boom", Urgent: true}}},
		{"a fix that timed out", engine.JobFinished{Job: engine.Job{PR: 4, Task: task.Conflicts, State: engine.JobTimedOut}},
			[]Alert{{Title: "PR #4: conflict fix timed out", Urgent: true}}},
		{"a kept worktree", engine.JobFinished{Job: engine.Job{PR: 4, Task: task.Fix, State: engine.JobDone, Verdict: "pushed", Kept: "uncommitted changes", Worktree: "/run/worktrees/pr-4"}},
			[]Alert{{Title: "PR #4's worktree has uncommitted changes", Body: "/run/worktrees/pr-4"}}},
		{"a fix that pushed", engine.JobFinished{Job: engine.Job{PR: 4, Task: task.Fix, State: engine.JobDone, Verdict: "pushed"}}, nil},
		{"a review that ended", engine.JobFinished{Job: engine.Job{PR: 9, Task: task.Review, State: engine.JobFailed, Outcome: "exit 3"}}, nil},
		{"the panel went down", engine.HealthChanged{After: down},
			[]Alert{{Title: "Reviews cannot approve: panel 2/3", Body: "opencode:glm does not answer", Urgent: true}}},
		{"a problem that still lets reviews approve", engine.HealthChanged{After: engine.Health{Seated: 4, Answering: 4, Problems: []string{"1 PR got no verdict"}}},
			[]Alert{{Title: "Doctor: 1 problem", Body: "1 PR got no verdict"}}},
		{"all clear again", engine.HealthChanged{Before: down, After: engine.Health{Seated: 3, Answering: 3}},
			[]Alert{{Title: "Doctor: all clear again", Body: "panel 3/3"}}},
		{"a note", engine.Noted{Text: "anything"}, nil},
	}
	for _, c := range cases {
		if got := AlertsFor(c.ev); !slices.Equal(got, c.want) {
			t.Errorf("%s: AlertsFor = %+v, want %+v", c.name, got, c.want)
		}
	}
}

// A PR title, an error from gh and an agent's reason are other people's
// text. An alert carries none of their control bytes.
func TestAnAlertIsSafeToShow(t *testing.T) {
	ev := engine.JobFinished{Job: engine.Job{PR: 4, Task: task.Fix, State: engine.JobFailed, Outcome: "exit 3", Error: "bad\x1b[2Jthing"}}
	for _, a := range AlertsFor(ev) {
		if slices.Contains([]byte(a.Title+a.Body), 0x1b) {
			t.Errorf("alert %+v carries an escape", a)
		}
	}
}
