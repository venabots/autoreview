package tui

import (
	"reflect"
	"slices"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/venabots/autoreview/internal/engine"
	"github.com/venabots/autoreview/internal/review"
	"github.com/venabots/autoreview/internal/task"
)

const goneSession = "0b5c7d1e-2f3a-4b6c-8d9e-0f1a2b3c4d5e"

// goneTask is a task that ended and whose worktree was removed.
func goneTask() engine.Job {
	return engine.Job{PR: 4, Task: task.Fix, State: engine.JobDone, Session: goneSession, Worktree: "/wt/pr-4", Orchestrator: review.Claude()}
}

// took sends a new snapshot and runs what the view asks for after it. The
// view's wait for the next snapshot is let through at once.
func took(t *testing.T, m Model, hs *harness, edit func(*engine.Job)) Model {
	t.Helper()
	snap := *hs.eng.snap
	job := goneTask()
	edit(&job)
	snap.Tasks = []engine.Job{job}
	hs.eng.snap = &snap
	next, cmd := m.Update(snapshotMsg{snap: &snap})
	m = next.(Model)
	for _, msg := range runCmd(hs, cmd) {
		if _, again := msg.(snapshotMsg); !again {
			m = send(m, msg)
		}
	}
	return m
}

func runCmd(hs *harness, cmd tea.Cmd) []tea.Msg {
	if cmd == nil {
		return nil
	}
	select {
	case hs.eng.changed <- struct{}{}:
	default:
	}
	msg := cmd()
	batch, ok := msg.(tea.BatchMsg)
	if !ok {
		return []tea.Msg{msg}
	}
	out := []tea.Msg{}
	for _, c := range batch {
		out = append(out, runCmd(hs, c)...)
	}
	return out
}

func reopensAsked(hs *harness) int {
	n := 0
	for _, c := range hs.eng.got {
		if c == (engine.ReopenTask{PR: 4}) {
			n++
		}
	}
	return n
}

func TestRMakesARemovedWorktreeAgainAndThenResumesInIt(t *testing.T) {
	m, hs := onMine(t, func(s *engine.Snapshot) { s.Tasks = []engine.Job{goneTask()} })
	m = press(t, m, "r")
	if n := reopensAsked(hs); n != 1 {
		t.Fatalf("r asked the engine %d times to make the worktree again, want 1", n)
	}
	wantOnScreen(t, m, "making PR #4's worktree again, to resume its session")

	m = press(t, m, "r")
	if n := reopensAsked(hs); n != 1 {
		t.Errorf("a second r asked again: %d asks", n)
	}
	wantOnScreen(t, m, "still making PR #4's worktree again")

	m = took(t, m, hs, func(j *engine.Job) { j.Reopening = true })
	if len(hs.tabs) != 0 {
		t.Errorf("a tab opened before the worktree was made: %q", hs.tabs)
	}
	m = took(t, m, hs, func(j *engine.Job) { j.Reopens, j.Kept = 1, engine.KeptToResume })
	if want := []string{"PR 4 Resume: cd /wt/pr-4 && claude --resume " + goneSession}; !reflect.DeepEqual(hs.tabs, want) {
		t.Errorf("tabs = %q, want %q", hs.tabs, want)
	}
	wantOnScreen(t, m, "opened PR #4's task in a new cmux tab")

	took(t, m, hs, func(j *engine.Job) { j.Reopens, j.Kept = 1, engine.KeptToResume })
	if len(hs.tabs) != 1 {
		t.Errorf("the same answer opened a second tab: %q", hs.tabs)
	}
}

func TestAWorktreeThatCannotBeMadeAgainIsSaidAndOpensNothing(t *testing.T) {
	m, hs := onMine(t, func(s *engine.Snapshot) { s.Tasks = []engine.Job{goneTask()} })
	m = press(t, m, "r")
	why := "PR #4's branch me/work is checked out at /home/me/work, so its worktree cannot be made again at /wt/pr-4"
	m = took(t, m, hs, func(j *engine.Job) { j.Reopens, j.ReopenError = 1, why })
	if len(hs.tabs) != 0 {
		t.Errorf("opened %q", hs.tabs)
	}
	wantOnScreen(t, m, "not resuming PR #4's task:")

	press(t, m, "r")
	if n := reopensAsked(hs); n != 2 {
		t.Errorf("r after a failure asked %d times in all, want 2: it tries again", n)
	}
	if !slices.ContainsFunc(hs.eng.got, func(c engine.Command) bool { return c == engine.ReopenTask{PR: 4} }) {
		t.Errorf("commands = %v", hs.eng.got)
	}
}

func TestRResumesATaskWhoseWorktreeWasMadeAgainWithoutMakingItTwice(t *testing.T) {
	m, hs := onMine(t, func(s *engine.Snapshot) {
		job := goneTask()
		job.Reopens, job.Kept = 1, engine.KeptToResume
		s.Tasks = []engine.Job{job}
	})
	wantOnScreen(t, m, "worktree  /wt/pr-4 (kept: made again to resume its session)")
	next, cmd := m.Update(keys["r"])
	m = next.(Model)
	if cmd == nil || reopensAsked(hs) != 0 {
		t.Fatalf("r did not resume at once: cmd %v, asks %d", cmd != nil, reopensAsked(hs))
	}
	send(m, cmd())
	if want := []string{"PR 4 Resume: cd /wt/pr-4 && claude --resume " + goneSession}; !reflect.DeepEqual(hs.tabs, want) {
		t.Errorf("tabs = %q, want %q", hs.tabs, want)
	}
}
