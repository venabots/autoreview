package tui

import (
	"strings"
	"testing"

	"github.com/venabots/autoreview/internal/engine"
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
