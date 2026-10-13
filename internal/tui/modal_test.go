package tui

import (
	"reflect"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/venabots/autoreview/internal/engine"
	"github.com/venabots/autoreview/internal/task"
)

func TestQuestionMarkOpensTheKeysOverTheScreenAndEscClosesThem(t *testing.T) {
	m, _ := newModel(t, 120, 40)

	open := press(t, m, "?")
	wantOnScreen(t, open, "KEYS", "open the selected PR in the browser", "esc closes",
		"autoreview · acme/widgets", "? keys")

	for _, key := range []string{"esc", "?", "q"} {
		closed := press(t, open, key)
		wantNotOnScreen(t, closed, "KEYS", "esc closes", "Quit autoreview?")
		wantOnScreen(t, closed, "#9 Add retry logic")
	}
}

func TestTheKeysScrollWhenTheScreenIsShorterThanTheHelp(t *testing.T) {
	m, _ := newModel(t, 120, 14)

	m = press(t, m, "?")
	wantOnScreen(t, m, "KEYS", "move between PRs", "j/k scrolls")
	wantNotOnScreen(t, m, "quit now, stopping any review that is running")

	m = press(t, m, "G")
	wantOnScreen(t, m, "KEYS", "quit now, stopping any review that is running")
	wantNotOnScreen(t, m, "move between PRs")

	m = press(t, m, "g")
	wantOnScreen(t, m, "move between PRs")
}

func TestALongKeyLineWrapsUnderItsOwnWords(t *testing.T) {
	m, _ := newModel(t, 60, 40)
	lines := strings.Split(screen(press(t, m, "?")), "\n")
	for i, l := range lines {
		at := strings.Index(l, "resume the selected review")
		if at < 0 {
			continue
		}
		next := lines[i+1]
		if !strings.Contains(l+next, "│") || strings.TrimSpace(next[at:at+1]) == "" || strings.TrimSpace(next[strings.Index(next, "│")+len("│"):at]) != "" {
			t.Errorf("the line does not go on under its own words:\n%s\n%s", l, next)
		}
		rest := strings.Join(strings.Fields(strings.NewReplacer("│", "").Replace(l[at:]+" "+next[at:])), " ")
		if want := "resume the selected review, or your PR's last task, in a new terminal tab"; rest != want {
			t.Errorf("the wrapped line reads %q, want %q", rest, want)
		}
		return
	}
	t.Errorf("the key help does not show the resume key:\n%s", strings.Join(lines, "\n"))
}

func TestQAlwaysAsksBeforeItQuits(t *testing.T) {
	m, hs := newModel(t, 120, 30)

	asked := press(t, m, "q")
	wantOnScreen(t, asked, "Quit autoreview?", "esc stay")
	wantNotOnScreen(t, asked, "A quit stops")
	if len(commands(hs)) != 0 {
		t.Fatalf("one press of q sent the engine %v", commands(hs))
	}

	stayed := press(t, asked, "esc")
	wantNotOnScreen(t, stayed, "Quit autoreview?")
	if len(commands(hs)) != 0 {
		t.Fatalf("esc on the question sent the engine %v", commands(hs))
	}

	quit := press(t, asked, "q")
	if got := commands(hs); len(got) != 1 || got[0] != (engine.Quit{}) {
		t.Errorf("the engine was sent %v, want one Quit", got)
	}
	wantOnScreen(t, quit, "stopping…")
	wantNotOnScreen(t, quit, "Quit autoreview?")
}

func TestTheQuitQuestionSaysWhatAQuitStops(t *testing.T) {
	reviews, _ := reviewingModel(t, 150, 30)
	wantOnScreen(t, press(t, reviews, "q"), "A quit stops 2 reviews.")

	tasks, _ := onMine(t, func(s *engine.Snapshot) {
		s.Tasks = []engine.Job{{PR: 4, Task: task.Fix, State: engine.JobRunning, StartedAt: start}}
	})
	wantOnScreen(t, press(t, tasks, "q"), "A quit stops 1 task.")
}

func TestXAsksBeforeItStopsAReviewAndNamesThePR(t *testing.T) {
	m, hs := reviewingModel(t, 150, 30)

	asked := press(t, m, "x")
	wantOnScreen(t, asked, "Stop PR #9's review?", "esc keep it")
	if len(commands(hs)) != 0 {
		t.Fatal("one press of x stopped a review")
	}

	kept := press(t, asked, "esc")
	wantNotOnScreen(t, kept, "Stop PR #9's review?")
	if len(commands(hs)) != 0 {
		t.Fatal("esc on the question stopped a review")
	}

	stopped := press(t, asked, "x")
	wantOnScreen(t, stopped, "stopping PR #9's review")
	wantNotOnScreen(t, stopped, "Stop PR #9's review?")
	if got := commands(hs); len(got) != 1 || got[0] != (engine.StopJob{PR: 9}) {
		t.Errorf("the engine was sent %v, want StopJob for PR #9", got)
	}
}

func TestXAsksBeforeItStopsATask(t *testing.T) {
	m, hs := onMine(t, func(s *engine.Snapshot) {
		s.Tasks = []engine.Job{{PR: 4, Task: task.Fix, State: engine.JobRunning, StartedAt: start}}
	})
	m = press(t, m, "x")
	wantOnScreen(t, m, "Stop PR #4's task?")
	m = press(t, m, "x")
	if want := []engine.Command{engine.StopJob{PR: 4}}; !reflect.DeepEqual(commands(hs), want) {
		t.Errorf("sent %+v, want %+v", commands(hs), want)
	}
	wantOnScreen(t, m, "stopping PR #4's task")
}

func TestXWithNothingRunningAsksNothing(t *testing.T) {
	m, hs := reviewingModel(t, 150, 30)
	m = press(t, m, "j", "x")
	wantOnScreen(t, m, "PR #8 has no review running")
	wantNotOnScreen(t, m, "Stop PR")
	if len(commands(hs)) != 0 {
		t.Errorf("the engine was sent %v, want nothing", commands(hs))
	}
}

// The question names its PR and holds the keys, so a second x cannot land
// on another row, and a slow second x is still about the same review.
func TestAKeyUnderAQuestionDoesNotReachTheList(t *testing.T) {
	m, hs := reviewingModel(t, 150, 30)

	m = press(t, m, "x", "j", "tab", "o", "R", "f")
	wantOnScreen(t, m, "Stop PR #9's review?")
	m = send(m, tea.MouseClickMsg{X: 3, Y: 6, Button: tea.MouseLeft})
	m = send(m, tickMsg(start.Add(time.Minute)))
	if len(commands(hs)) != 0 {
		t.Fatalf("a key under the question sent the engine %v", commands(hs))
	}

	m = press(t, m, "x")
	if got := commands(hs); len(got) != 1 || got[0] != (engine.StopJob{PR: 9}) {
		t.Errorf("the engine was sent %v, want StopJob for PR #9", got)
	}
	wantOnScreen(t, m, " Review 4 ", "#9 Add retry logic")
}

func TestAStopQuestionClosesWhenItsReviewEnds(t *testing.T) {
	m, hs := reviewingModel(t, 150, 30)
	m = press(t, m, "x")

	ended := reviewing(t)
	ended.Jobs[0].State = engine.JobDone
	hs.eng.snap = ended
	m = send(m, snapshotMsg{snap: ended})
	wantNotOnScreen(t, m, "Stop PR #9's review?")
	wantOnScreen(t, m, "PR #9's review ended before it was stopped")

	press(t, m, "x")
	if len(commands(hs)) != 0 {
		t.Errorf("the engine was sent %v, want nothing", commands(hs))
	}
}

func TestCtrlCQuitsAtOnceFromAnyQuestion(t *testing.T) {
	for _, open := range []string{"?", "q", "x"} {
		m, hs := reviewingModel(t, 150, 30)
		press(t, m, open, "ctrl+c")
		if got := commands(hs); len(got) != 1 || got[0] != (engine.Quit{}) {
			t.Errorf("after %s, ctrl-C sent the engine %v, want one Quit", open, got)
		}
	}
}

func TestAScreenTooSmallForABoxStillAsks(t *testing.T) {
	m, hs := newModel(t, 30, 4)
	m = press(t, m, "q")
	wantOnScreen(t, m, "Quit autoreview?", "esc stay")
	wantNotOnScreen(t, m, "╭")
	press(t, m, "q")
	if got := commands(hs); len(got) != 1 || got[0] != (engine.Quit{}) {
		t.Errorf("the engine was sent %v, want one Quit", got)
	}
}

func TestABoxSitsInTheMiddleAndLeavesTheRestOfTheScreen(t *testing.T) {
	m, _ := newModel(t, 120, 30)
	lines := strings.Split(screen(press(t, m, "q")), "\n")

	top := -1
	for i, l := range lines {
		if strings.Contains(l, "╭") {
			top = i
		}
	}
	if top < 8 || top > 14 {
		t.Fatalf("the box starts on row %d of 30, want it near the middle:\n%s", top, strings.Join(lines, "\n"))
	}
	left := ansi.StringWidth(lines[top][:strings.Index(lines[top], "╭")])
	right := 120 - ansi.StringWidth(strings.TrimRight(lines[top], " "))
	if diff := left - right; diff < -1 || diff > 1 {
		t.Errorf("the box has %d columns on its left and %d on its right", left, right)
	}
	if !strings.HasPrefix(lines[0], "autoreview · acme/widgets") || !strings.Contains(lines[29], "? keys") {
		t.Errorf("the box took the header or the footer:\n%s", strings.Join(lines, "\n"))
	}
}

func TestTheKeysOfAQuestionAreDrawnInTheHintColor(t *testing.T) {
	m, _ := newModel(t, 120, 30)
	raw := press(t, m, "q").View().Content
	box := raw[strings.Index(raw, "Quit autoreview?"):]
	box = box[:strings.Index(box, "stay")]
	cyan := false
	for _, params := range styles(box) {
		for _, p := range params {
			cyan = cyan || p == "36"
		}
	}
	if !cyan {
		t.Errorf("no key in the question is cyan: %q", box)
	}
}
