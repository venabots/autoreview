package tui

import (
	"strings"
	"testing"

	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"

	"github.com/venabots/autoreview/internal/engine"
)

// loading is the first snapshot of a run: nothing has been read yet.
func loading() *engine.Snapshot {
	s := snapshot()
	s.Phase = engine.Looking
	s.Looked, s.Reviews = false, nil
	s.MineLooked, s.Mine = false, nil
	return s
}

func loadingModel(t *testing.T) Model {
	t.Helper()
	eng := &fakeEngine{snap: loading(), changed: make(chan struct{}, 1)}
	m := New(eng, nil)
	m.now = start
	return send(m, tea.WindowSizeMsg{Width: 120, Height: 30})
}

func TestTheFirstLookShowsASpinnerWhereThePRsWillBe(t *testing.T) {
	m := loadingModel(t)
	lines := strings.Split(screen(m), "\n")

	if !strings.HasPrefix(lines[2], "⠋ reading the PR list") {
		t.Errorf("the first list row is %q, want the spinner and what it waits for", lines[2])
	}
	if !strings.HasPrefix(lines[len(lines)-1], "⠋ reading the PR list") {
		t.Errorf("the footer is %q, want the spinner too", lines[len(lines)-1])
	}
	wantOnScreen(t, m, " Review ", " My PRs ")
	wantNotOnScreen(t, m, " Review 0 ", " My PRs 0 ", "no PRs to show", "you have no open PRs")

	mine := press(t, m, "tab")
	if got := strings.Split(screen(mine), "\n")[2]; !strings.HasPrefix(got, "⠋ reading your PRs") {
		t.Errorf("the first row of My PRs is %q, want the spinner", got)
	}
}

func TestOneListCanArriveBeforeTheOther(t *testing.T) {
	m := loadingModel(t)
	half := snapshot()
	half.Phase = engine.Looking
	half.MineLooked, half.Mine = false, nil
	m = send(m, snapshotMsg{snap: half})

	wantOnScreen(t, m, "○ #9 · ready", " Review 4 ", " My PRs ")
	wantNotOnScreen(t, m, "reading the PR list", " My PRs 0 ")
	wantOnScreen(t, press(t, m, "tab"), "⠋ reading your PRs")
}

func TestTheSpinnerTurnsWhileTheRunIsLooking(t *testing.T) {
	m := loadingModel(t)
	if !m.spinning {
		t.Fatal("a view that opens on a look does not start its spinner")
	}
	if cmd := m.Init(); cmd == nil {
		t.Fatal("Init started nothing")
	}

	next, cmd := m.Update(m.spin.Tick())
	m = next.(Model)
	if cmd == nil {
		t.Fatal("the spinner stopped while the run is still looking")
	}
	wantOnScreen(t, m, "⠙ reading the PR list")
	wantNotOnScreen(t, m, "⠋")
}

func TestTheSpinnerStopsWhenTheLookEndsAndStartsWithTheNext(t *testing.T) {
	m := loadingModel(t)

	m = send(m, snapshotMsg{snap: snapshot()})
	next, cmd := m.Update(m.spin.Tick())
	m = next.(Model)
	if cmd != nil || m.spinning {
		t.Error("the spinner keeps ticking while the run waits: the screen would redraw twelve times a second for nothing")
	}
	wantNotOnScreen(t, m, "⠋", "⠙")

	again := snapshot()
	again.Phase = engine.Looking
	next, cmd = m.Update(snapshotMsg{snap: again})
	m = next.(Model)
	if cmd == nil || !m.spinning {
		t.Fatal("the spinner did not start again with the next look")
	}
	wantOnScreen(t, m, "checking the PR list", "○ #9 · ready")

	next, _ = m.Update(snapshotMsg{snap: again})
	if !next.(Model).spinning {
		t.Error("a second snapshot of the same look stopped the spinner")
	}
}

func TestASpinnerTickFromAnotherSpinnerIsIgnored(t *testing.T) {
	m := loadingModel(t)
	other := spinner.New()
	next, _ := m.Update(other.Tick())
	wantOnScreen(t, next.(Model), "⠋ reading the PR list")
}
