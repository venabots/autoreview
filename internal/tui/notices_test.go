package tui

import (
	"strings"
	"testing"

	"github.com/venabots/autoreview/internal/babysit"
	"github.com/venabots/autoreview/internal/engine"
	"github.com/venabots/autoreview/internal/task"
)

// A task whose worktree cannot be made never runs. The engine keeps it as
// a note, and the view is the only place a person looks.
func TestATaskThatDidNotStartSaysWhyInTheFooterTheRowAndTheDetail(t *testing.T) {
	m, hs := onMine(t, nil)

	failed := snapshot()
	failed.NotStarted = map[int]string{4: "not fixing PR #4: git fetch \x1b[2Jfailed"}
	hs.eng.snap = failed
	m = send(m, snapshotMsg{snap: failed})
	wantOnScreen(t, m, "✓ #4 · did not start · conflicts")
	if got := screen(m); strings.Count(got, "not fixing PR #4: git fetch [2Jfailed") != 2 {
		t.Errorf("the reason is not in both the footer and the detail:\n%s", got)
	}
	if strings.Contains(m.View().Content, "\x1b[2J") {
		t.Error("an escape sequence in the reason reached the terminal")
	}

	m = send(m, tickMsg(start.Add(flashFor+1)))
	m = send(m, snapshotMsg{snap: failed})
	if got := screen(m); strings.Count(got, "not fixing PR #4: git fetch [2Jfailed") != 1 {
		t.Errorf("a snapshot with the same note said it in the footer again:\n%s", got)
	}

	running := snapshot()
	running.Tasks = []engine.Job{{PR: 4, State: engine.JobPreparing}}
	m = send(m, snapshotMsg{snap: running})
	wantNotOnScreen(t, m, "did not start", "git fetch")
}

func fixOn4(state engine.JobState, verdict string) *engine.Snapshot {
	s := snapshot()
	s.Tasks = []engine.Job{{PR: 4, Task: task.Fix, State: state, Verdict: verdict, StartedAt: start}}
	return s
}

// A fix is asked for with one key and ends minutes later. The footer says
// so wherever the person is by then, and the row keeps the result: without
// it the row reads after the fix as it did before.
func TestATaskThatEndsSaysSoInTheFooterAndStaysOnItsRow(t *testing.T) {
	m, hs := newModel(t, 150, 30)
	hs.eng.snap = fixOn4(engine.JobRunning, "")
	m = send(m, snapshotMsg{snap: hs.eng.snap})
	wantNotOnScreen(t, m, "PR #4:")

	done := fixOn4(engine.JobDone, "pushed")
	hs.eng.snap = done
	m = send(m, snapshotMsg{snap: done})
	wantOnScreen(t, m, "PR #4: fix pushed", " Review 4 ")

	m = press(t, m, "tab")
	wantOnScreen(t, m, "✓ #4 · fix pushed · conflicts")

	m = send(m, tickMsg(start.Add(flashFor+1)))
	m = send(m, snapshotMsg{snap: done})
	wantNotOnScreen(t, m, "PR #4: fix pushed")
	wantOnScreen(t, m, "✓ #4 · fix pushed · conflicts")
}

func TestTheRowSaysHowTheLastTaskEnded(t *testing.T) {
	cases := []struct {
		state   engine.JobState
		verdict string
		want    string
	}{
		{engine.JobDone, "nothing pushed", "#4 · fix pushed nothing · conflicts"},
		{engine.JobTimedOut, "nothing pushed", "#4 · fix timed out · conflicts"},
		{engine.JobFailed, "", "#4 · fix failed (exit 3) · conflicts"},
	}
	for _, c := range cases {
		m, _ := onMine(t, func(s *engine.Snapshot) {
			s.Tasks = fixOn4(c.state, c.verdict).Tasks
			s.Tasks[0].Outcome = "exit 3"
		})
		wantOnScreen(t, m, c.want)
	}

	babysat, _ := onMine(t, func(s *engine.Snapshot) {
		s.Tasks = fixOn4(engine.JobDone, "pushed").Tasks
		s.Babysit, _ = s.Babysit.Apply(babysit.All(true), s.Mine)
	})
	wantOnScreen(t, babysat, "#4 · autofix · fix pushed · conflicts")
}
