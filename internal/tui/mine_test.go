package tui

import (
	"reflect"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/venabots/autoreview/internal/babysit"
	"github.com/venabots/autoreview/internal/engine"
	"github.com/venabots/autoreview/internal/review"
	"github.com/venabots/autoreview/internal/task"
)

var mineKeys = map[string]tea.KeyPressMsg{
	"b": {Code: 'b', Text: "b"},
	"B": {Code: 'b', Text: "B", Mod: tea.ModShift},
	"c": {Code: 'c', Text: "c"},
	"u": {Code: 'u', Text: "u"},
}

func pressMine(t *testing.T, m Model, names ...string) Model {
	t.Helper()
	for _, name := range names {
		if key, ok := mineKeys[name]; ok {
			m = send(m, key)
			continue
		}
		m = press(t, m, name)
	}
	return m
}

// onMine is the view on the My PRs tab, with PR #4 selected.
func onMine(t *testing.T, edit func(*engine.Snapshot)) (Model, *harness) {
	t.Helper()
	m, hs := newModel(t, 150, 30)
	if edit != nil {
		edit(hs.eng.snap)
		m = send(m, snapshotMsg{snap: hs.eng.snap})
	}
	return press(t, m, "tab"), hs
}

func TestTheMyPRsKeysEachAskForTheirTask(t *testing.T) {
	cases := map[string]task.Kind{"f": task.Fix, "c": task.Comments, "u": task.Conflicts}
	for key, kind := range cases {
		m, hs := onMine(t, nil)
		m = pressMine(t, m, key)
		want := []engine.Command{engine.RunTask{Request: task.Request{PR: 4, Kind: kind, Title: "My own work", Branch: "me/my-own-work"}}}
		if !reflect.DeepEqual(commands(hs), want) {
			t.Errorf("%s sent %+v, want %+v", key, commands(hs), want)
		}
		wantOnScreen(t, m, kind.Doing()+" PR #4")
		if m.editing {
			t.Errorf("%s opened the focus editor on My PRs", key)
		}
	}
}

func TestATaskKeyIsRefusedInTheViewBeforeAnythingIsSent(t *testing.T) {
	fork := func(s *engine.Snapshot) { s.Mine[0].CrossRepo = true }
	refused := func(s *engine.Snapshot) {
		s.Refusals = map[task.Kind]string{task.Fix: "--no-post leaves every PR alone"}
	}
	busy := func(s *engine.Snapshot) {
		s.Tasks = []engine.Job{{PR: 4, Task: task.Comments, State: engine.JobRunning, StartedAt: start}}
	}
	cases := []struct {
		name string
		edit func(*engine.Snapshot)
		want string
	}{
		{"a run that may not change PRs", refused, "not fixing PR #4: --no-post leaves every PR alone"},
		{"a PR from a fork", fork, "PR #4 is from a fork; its branch is not on origin, so nothing can fix it here"},
		{"a PR with a task running", busy, "PR #4 has a task running already"},
	}
	for _, c := range cases {
		m, hs := onMine(t, c.edit)
		m = press(t, m, "f")
		if len(commands(hs)) != 0 {
			t.Errorf("%s: sent %+v", c.name, commands(hs))
		}
		wantOnScreen(t, m, c.want)
	}
}

func TestTheMyPRsKeysSayWhereTheyWorkOnTheReviewTab(t *testing.T) {
	for _, key := range []string{"c", "u", "b", "B"} {
		m, hs := newModel(t, 150, 30)
		m = pressMine(t, m, key)
		if len(commands(hs)) != 0 {
			t.Errorf("%s on the Review tab sent %+v", key, commands(hs))
		}
		wantOnScreen(t, m, "f, c, u, b and B work on your own PRs: tab shows them")
	}
}

func TestBTogglesBabysittingOnTheSelectedPR(t *testing.T) {
	m, hs := onMine(t, nil)
	m = pressMine(t, m, "b")
	if want := []engine.Command{engine.Babysit{Change: babysit.One(4, true)}}; !reflect.DeepEqual(commands(hs), want) {
		t.Errorf("b sent %+v, want %+v", commands(hs), want)
	}
	wantOnScreen(t, m, "babysitting PR #4: it is fixed when it changes and needs work")

	m, hs = onMine(t, func(s *engine.Snapshot) { s.Babysit, _ = s.Babysit.Apply(babysit.All(true), s.Mine) })
	m = pressMine(t, m, "b", "B")
	want := []engine.Command{engine.Babysit{Change: babysit.One(4, false)}, engine.Babysit{Change: babysit.All(false)}}
	if !reflect.DeepEqual(commands(hs), want) {
		t.Errorf("b then B sent %+v, want %+v", commands(hs), want)
	}
	wantOnScreen(t, m, "no longer babysitting your PRs")
}

func TestBabysittingIsRefusedWhereAFixWouldBe(t *testing.T) {
	m, hs := onMine(t, func(s *engine.Snapshot) {
		s.Refusals = map[task.Kind]string{task.Fix: "babysit-pr is not installed where claude looks (~/.claude/skills)"}
	})
	m = pressMine(t, m, "b")
	wantOnScreen(t, m, "not babysitting PR #4: babysit-pr is not installed")
	m = pressMine(t, m, "B")
	wantOnScreen(t, m, "not babysitting your PRs: babysit-pr is not installed")
	if len(commands(hs)) != 0 {
		t.Errorf("sent %+v", commands(hs))
	}

	m, hs = onMine(t, func(s *engine.Snapshot) { s.Mine[0].CrossRepo = true })
	m = pressMine(t, m, "b")
	wantOnScreen(t, m, "PR #4 is from a fork")
	if len(commands(hs)) != 0 {
		t.Errorf("b on a fork sent %+v", commands(hs))
	}
}

func TestARunningTaskShowsInItsRowAndDetail(t *testing.T) {
	m, _ := onMine(t, func(s *engine.Snapshot) {
		s.Tasks = []engine.Job{{PR: 4, Task: task.Fix, State: engine.JobRunning, StartedAt: start.Add(-72 * time.Second),
			Worktree: "/logs/run-1/worktrees/pr-4", Follow: "0b5c7d1e-2f3a-4b6c-8d9e-0f1a2b3c4d5e", Orchestrator: review.Claude()}}
		s.Babysit, _ = s.Babysit.Apply(babysit.One(3, true), s.Mine)
	})
	wantOnScreen(t, m,
		"#4 · fixing 1m12s",
		"#3 · babysat · 2 threads",
		" My PRs 2 · babysitting 1 ",
		"TASK", "task      fix", "state     fixing 1m12s", "worktree  /logs/run-1/worktrees/pr-4",
		"babysat   no",
	)
}

func TestAFinishedTaskShowsWhatItPushed(t *testing.T) {
	m, _ := onMine(t, func(s *engine.Snapshot) {
		s.Tasks = []engine.Job{{PR: 4, Task: task.Conflicts, State: engine.JobDone, Verdict: "pushed", Session: "0b5c7d1e-2f3a-4b6c-8d9e-0f1a2b3c4d5e",
			Worktree: "/logs/run-1/worktrees/pr-4", Kept: "uncommitted changes", Orchestrator: review.Claude()}}
	})
	wantOnScreen(t, m, "LAST TASK", "task      conflicts", "state     done", "branch    pushed",
		"worktree  /logs/run-1/worktrees/pr-4 (kept: uncommitted changes)", "session   0b5c7d1e-2f3a-4b6c-8d9e-0f1a2b3c4d5e")
}

func TestRResumesTheLastTaskInItsWorktree(t *testing.T) {
	m, hs := onMine(t, func(s *engine.Snapshot) {
		s.Tasks = []engine.Job{{PR: 4, Task: task.Fix, State: engine.JobFailed, Outcome: "exit 3", Session: "0b5c7d1e-2f3a-4b6c-8d9e-0f1a2b3c4d5e",
			Worktree: "/wt/pr-4", Kept: "commits that are not pushed", Orchestrator: review.Claude()}}
	})
	next, cmd := m.Update(keys["r"])
	m = next.(Model)
	wantOnScreen(t, m, "opening PR #4's task in a new tab")
	if cmd == nil {
		t.Fatal("r started nothing")
	}
	m = send(m, cmd())
	if want := []string{"PR 4 Resume: cd /wt/pr-4 && claude --resume 0b5c7d1e-2f3a-4b6c-8d9e-0f1a2b3c4d5e"}; !reflect.DeepEqual(hs.tabs, want) {
		t.Errorf("tabs = %q, want %q", hs.tabs, want)
	}
	wantOnScreen(t, m, "opened PR #4's task in a new cmux tab")
}

func TestRSaysWhyATaskCannotBeResumed(t *testing.T) {
	gone := engine.Job{PR: 4, Task: task.Fix, State: engine.JobDone, Session: "0b5c7d1e-2f3a-4b6c-8d9e-0f1a2b3c4d5e", Worktree: "/wt/pr-4", Orchestrator: review.Claude()}
	live := engine.Job{PR: 4, Task: task.Fix, State: engine.JobRunning, StartedAt: start}
	cases := []struct {
		tasks []engine.Job
		want  string
	}{
		{nil, "PR #4 has had no task in this run"},
		{[]engine.Job{live}, "PR #4 has a task running; resume it when it ends"},
		{[]engine.Job{gone}, "PR #4's task worktree is gone; its session was 0b5c7d1e-2f3a-4b6c-8d9e-0f1a2b3c4d5e"},
	}
	for _, c := range cases {
		m, hs := onMine(t, func(s *engine.Snapshot) { s.Tasks = c.tasks })
		m = press(t, m, "r")
		if len(hs.tabs) != 0 {
			t.Errorf("opened %q", hs.tabs)
		}
		wantOnScreen(t, m, c.want)
	}
}

func TestXStopsARunningTaskOnTheSecondPress(t *testing.T) {
	m, hs := onMine(t, func(s *engine.Snapshot) {
		s.Tasks = []engine.Job{{PR: 4, Task: task.Fix, State: engine.JobRunning, StartedAt: start}}
	})
	m = press(t, m, "x")
	wantOnScreen(t, m, "press x again to stop PR #4's task")
	m = press(t, m, "x")
	if want := []engine.Command{engine.StopJob{PR: 4}}; !reflect.DeepEqual(commands(hs), want) {
		t.Errorf("sent %+v, want %+v", commands(hs), want)
	}
	wantOnScreen(t, m, "stopping PR #4's task")
}

func TestQuitCountsTheTasksItWouldStop(t *testing.T) {
	m, _ := onMine(t, func(s *engine.Snapshot) {
		s.Tasks = []engine.Job{{PR: 4, Task: task.Fix, State: engine.JobRunning, StartedAt: start}}
	})
	m = press(t, m, "q")
	wantOnScreen(t, m, "press q again to stop 1 task and quit")
}

func TestTheMyPRsFooterNamesItsOwnKeys(t *testing.T) {
	m, _ := onMine(t, nil)
	wantOnScreen(t, m, "f fix", "b babysit")
	m = press(t, m, "?")
	got := screen(m)
	for _, want := range []string{"fix the selected PR once", "babysit all your PRs", "answer the selected PR's review comments"} {
		if !strings.Contains(got, want) {
			t.Errorf("the key help does not say %q", want)
		}
	}
}

func TestTheMyPRsKeyNamesMatchWhatBubbleTeaSends(t *testing.T) {
	want := map[string]intent{"b": intentBabysit, "B": intentBabysitAll, "c": intentComments, "u": intentConflicts}
	for name, key := range mineKeys {
		if got := intentFor(key); got != want[name] {
			t.Errorf("%s (which Bubble Tea names %q) asks for %d, want %d", name, key.String(), got, want[name])
		}
	}
}
