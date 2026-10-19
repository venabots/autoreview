package tui

import (
	"strings"
	"testing"

	"github.com/venabots/autoreview/internal/babysit"
	"github.com/venabots/autoreview/internal/engine"
	"github.com/venabots/autoreview/internal/task"
)

// footerOf is the last line of the frame, with the padding cut.
func footerOf(m Model) string {
	lines := strings.Split(screen(m), "\n")
	return strings.TrimRight(lines[len(lines)-1], " ")
}

func TestTheMyPRsFooterCountsYourTasksAndNotTheReviews(t *testing.T) {
	cases := []struct {
		name string
		edit func(*engine.Snapshot)
		want string
	}{
		{"no task and no autofix", func(s *engine.Snapshot) {}, "no task running · next look in 1m30s"},
		{"one task running", func(s *engine.Snapshot) {
			s.Tasks = []engine.Job{{PR: 4, Task: task.Fix, State: engine.JobRunning, StartedAt: start}}
		}, "1 task running · next look in 1m30s"},
		{"a task that makes its worktree is running", func(s *engine.Snapshot) {
			s.Tasks = []engine.Job{{PR: 4, Task: task.Fix, State: engine.JobPreparing}}
		}, "1 task running · next look in 1m30s"},
		{"tasks in each state, with autofix on", func(s *engine.Snapshot) {
			s.Tasks = []engine.Job{
				{PR: 4, Task: task.Fix, State: engine.JobQueued},
				{PR: 3, Task: task.Fix, State: engine.JobDone, Verdict: "pushed"},
			}
			s.Babysit, _ = s.Babysit.Apply(babysit.All(true), s.Mine)
		}, "1 task queued · 1 task finished · autofixing 2 · next look in 1m30s"},
		{"two tasks that ended", func(s *engine.Snapshot) {
			s.Tasks = []engine.Job{
				{PR: 4, Task: task.Fix, State: engine.JobFailed},
				{PR: 3, Task: task.Fix, State: engine.JobTimedOut},
			}
		}, "2 tasks finished · next look in 1m30s"},
		{"only the newest task on a PR counts", func(s *engine.Snapshot) {
			s.Tasks = []engine.Job{
				{PR: 4, Task: task.Fix, State: engine.JobDone},
				{PR: 4, Task: task.Fix, State: engine.JobRunning, StartedAt: start},
			}
		}, "1 task running · next look in 1m30s"},
		{"looking again", func(s *engine.Snapshot) { s.Looking = true }, "no task running · ⠋ checking the PR list"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m, _ := onMine(t, c.edit)
			footer := footerOf(m)
			if !strings.HasPrefix(footer, c.want) {
				t.Errorf("the footer is %q, want it to start %q", footer, c.want)
			}
			for _, review := range []string{"waiting", "nothing to review"} {
				if strings.Contains(footer, review) {
					t.Errorf("the My PRs footer %q shows the review count %q", footer, review)
				}
			}
		})
	}
}

func TestTheMyPRsFooterSaysItIsReadingYourPRsBeforeTheFirstLook(t *testing.T) {
	m, _ := onMine(t, func(s *engine.Snapshot) { s.MineLooked, s.Mine = false, nil })
	if footer := footerOf(m); !strings.HasPrefix(footer, "⠋ reading your PRs") {
		t.Errorf("the footer is %q, want it to say it is reading your PRs", footer)
	}
}

func TestTheReviewFooterStillCountsTheReviews(t *testing.T) {
	m, _ := newModel(t, 120, 30)
	if footer := footerOf(m); !strings.HasPrefix(footer, "2 waiting · next look in 1m30s") {
		t.Errorf("the footer is %q, want the review count", footer)
	}
}

func TestTheTabHintNamesTheOtherTabAndNotAnAction(t *testing.T) {
	review, _ := newModel(t, 160, 30)
	mine, _ := onMine(t, nil)
	wantOnScreen(t, review, "tab to My PRs")
	wantOnScreen(t, mine, "tab to Review")
	wantNotOnScreen(t, mine, "tab review ")
}
