package tui

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/venabots/autoreview/internal/engine"
	"github.com/venabots/autoreview/internal/prs"
	"github.com/venabots/autoreview/internal/queue"
)

func TestAMessageInTheFooterGoesAwayAfterFiveSeconds(t *testing.T) {
	m, _ := newModel(t, 120, 30)
	m = press(t, m, "m")
	wantOnScreen(t, m, "the mouse is the terminal's: drag to select text, m to take it back")

	m = send(m, tickMsg(start.Add(4*time.Second)))
	wantOnScreen(t, m, "the mouse is the terminal's")

	m = send(m, tickMsg(start.Add(5*time.Second)))
	wantNotOnScreen(t, m, "the mouse is the terminal's")
	wantOnScreen(t, m, "next look in 1m25s")
}

func TestMGivesTheMouseBackAndTakesItAgain(t *testing.T) {
	m, _ := newModel(t, 120, 30)
	if m.View().MouseMode != tea.MouseModeCellMotion {
		t.Fatal("the view does not capture the mouse at the start")
	}
	m = press(t, m, "m")
	if m.View().MouseMode != tea.MouseModeNone {
		t.Error("after m the mouse is still captured")
	}
	m = press(t, m, "m")
	if m.View().MouseMode != tea.MouseModeCellMotion {
		t.Error("after a second m the mouse is not captured again")
	}
	wantOnScreen(t, m, "the screen has the mouse again")
}

func TestAClickSelectsTheRowUnderIt(t *testing.T) {
	m, _ := newModel(t, 120, 30)

	m = send(m, tea.MouseClickMsg{X: 5, Y: 6, Button: tea.MouseLeft})
	wantOnScreen(t, m, "#6 Tidy the docs")

	m = send(m, tea.MouseClickMsg{X: 5, Y: 20, Button: tea.MouseLeft})
	wantOnScreen(t, m, "#6 Tidy the docs")

	m = send(m, tea.MouseClickMsg{X: 60, Y: 2, Button: tea.MouseLeft})
	wantOnScreen(t, m, "#6 Tidy the docs")

	m = send(m, tea.MouseClickMsg{X: 5, Y: 2, Button: tea.MouseRight})
	wantOnScreen(t, m, "#6 Tidy the docs")
}

func TestTheWheelMovesTheListOrScrollsTheDetail(t *testing.T) {
	m, _ := newModel(t, 120, 8)

	m = send(m, tea.MouseWheelMsg{X: 5, Y: 3, Button: tea.MouseWheelDown})
	wantOnScreen(t, m, "#8 Fix the cache")
	m = send(m, tea.MouseWheelMsg{X: 5, Y: 3, Button: tea.MouseWheelUp})
	wantOnScreen(t, m, "#9 Add retry logic")

	m = send(m, tea.MouseWheelMsg{X: 60, Y: 3, Button: tea.MouseWheelDown})
	wantNotOnScreen(t, m, "#9 Add retry logic")
	wantOnScreen(t, m, "○ #9 · ready", "checks    passing")

	m = send(m, tea.MouseWheelMsg{X: 60, Y: 3, Button: tea.MouseWheelUp})
	wantOnScreen(t, m, "#9 Add retry logic")
}

func TestTheScrollKeysMoveTheDetail(t *testing.T) {
	m, _ := newModel(t, 120, 8)

	down := press(t, m, "ctrl+d")
	wantNotOnScreen(t, down, "#9 Add retry logic")
	wantOnScreen(t, press(t, down, "ctrl+u"), "#9 Add retry logic")
	wantNotOnScreen(t, press(t, m, "space"), "#9 Add retry logic")

	other := press(t, down, "j")
	wantOnScreen(t, other, "#8 Fix the cache")
}

func TestAnUnknownKeyDoesNothing(t *testing.T) {
	m, hs := newModel(t, 120, 30)
	before := screen(m)
	if after := screen(press(t, m, "z")); after != before {
		t.Errorf("z changed the screen:\n%s", after)
	}
	if len(hs.eng.got) != 0 {
		t.Errorf("z sent the engine %v", hs.eng.got)
	}
}

func TestTheScreenSaysWhatIsNotKnownYet(t *testing.T) {
	cases := []struct {
		name  string
		edit  func(*engine.Snapshot)
		tab   bool
		wants []string
	}{
		{"before the first look", func(s *engine.Snapshot) {
			s.Looked, s.Reviews, s.Looking = false, nil, true
		}, false, []string{"⠋ reading the PR list"}},
		{"a first look that failed", func(s *engine.Snapshot) {
			s.Looked, s.Reviews, s.ListErr = false, nil, "gh api graphql timed out"
		}, false, []string{"could not read the PR list: gh api graphql timed out", "the next look tries again"}},
		{"a later look that failed keeps the rows", func(s *engine.Snapshot) {
			s.ListErr = "rate limited"
		}, false, []string{"could not read the PR list: rate limited", "○ #9 · ready", "#9 Add retry logic"}},
		{"an empty list", func(s *engine.Snapshot) { s.Reviews = nil }, false, []string{"no PRs to show", "nothing to review"}},
		{"looking again", func(s *engine.Snapshot) { s.Looking = true }, false, []string{"⠋ checking the PR list", "○ #9 · ready"}},
		{"your PRs not read yet", func(s *engine.Snapshot) { s.MineLooked, s.Mine = false, nil }, true, []string{"⠋ reading your PRs"}},
		{"your PRs could not be read", func(s *engine.Snapshot) {
			s.MineLooked, s.Mine, s.MineErr = false, nil, "search is down"
		}, true, []string{"your PRs could not be read: search is down"}},
		{"you have no PRs", func(s *engine.Snapshot) { s.Mine = nil }, true, []string{"you have no open PRs in this repo", " My PRs 0 "}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m, _ := newModel(t, 120, 30)
			snap := snapshot()
			c.edit(snap)
			m = send(m, snapshotMsg{snap: snap})
			if c.tab {
				m = press(t, m, "tab")
			}
			wantOnScreen(t, m, c.wants...)
		})
	}
}

func TestOtherPeoplesTextCannotDrawOnTheScreen(t *testing.T) {
	m, _ := newModel(t, 120, 30)
	hostile := snapshot()
	hostile.Reviews[0].PR.Title = "nice\x1b[2J\x1b[Htitle\u202E"
	hostile.Reviews[0].PR.Branch = "evil\x07\nbranch"
	hostile.Reviews[0].PR.Author = "al\x1bice"
	m = send(m, snapshotMsg{snap: hostile})

	raw := m.View().Content
	for _, bad := range []string{"\x1b[2J", "\x1b[H", "\x07", "\u202E"} {
		if strings.Contains(raw, bad) {
			t.Errorf("the frame carries %q from a PR's text", bad)
		}
	}
	wantOnScreen(t, m, "#9 nice[2J[Htitle", "@alice evilbranch")
}

func TestANarrowFrameHasARuleBetweenTheListAndTheDetail(t *testing.T) {
	m, _ := newModel(t, 80, 24)
	lines := strings.Split(screen(m), "\n")
	if !strings.HasPrefix(lines[2], "○ #9 · ready") {
		t.Errorf("row 2 is %q, want the first PR", lines[2])
	}
	if lines[10] != strings.Repeat("─", 80) {
		t.Errorf("row 10 is %q, want the rule between the panes", lines[10])
	}
	if !strings.HasPrefix(lines[11], "#9 Add retry logic") {
		t.Errorf("row 11 is %q, want the detail", lines[11])
	}
}

func TestTheFooterDropsHintsThatDoNotFitButNeverTheKeysHint(t *testing.T) {
	for _, width := range []int{120, 80, 60, 40, 20} {
		m, _ := newModel(t, width, 30)
		lines := strings.Split(screen(m), "\n")
		footer := strings.TrimRight(lines[len(lines)-1], " ")
		if !strings.HasSuffix(footer, "? keys") {
			t.Errorf("at %d columns the footer is %q, want it to end with the keys hint", width, footer)
		}
	}
	wide, _ := newModel(t, 175, 30)
	narrow, _ := newModel(t, 80, 30)
	wantOnScreen(t, wide, "q quit  j/k move  / find  r resume  o open  R review now  tab my PRs  w watch  m mouse  f focus  x stop  l log  ^d/^u scroll  ? keys")
	wantNotOnScreen(t, narrow, "^d/^u scroll")
	wantOnScreen(t, narrow, "2 waiting · next look in 1m30s", "q quit", "? keys")

	tiny, _ := newModel(t, 20, 30)
	wantOnScreen(t, tiny, "2 waiting ·…  ? keys")
}

func TestTheWaitReasonNamesTheParentOfAStackedPR(t *testing.T) {
	cases := []struct {
		name   string
		parent *prs.Parent
		want   string
	}{
		{"a declared base", &prs.Parent{Number: 15, Evidence: prs.Declared}, "held until #15 lands (its base is #15's branch)"},
		{"shared commits", &prs.Parent{Number: 15, Evidence: prs.Overlap, Shared: 3}, "held until #15 lands (3 commits also in #15)"},
		{"one shared commit", &prs.Parent{Number: 15, Evidence: prs.CarriesTip, Shared: 1}, "held until #15 lands (1 commit also in #15)"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m, _ := newModel(t, 120, 30)
			snap := snapshot()
			snap.Reviews[0].Wait = queue.WaitStacked
			snap.Reviews[0].PR.Parent = c.parent
			m = send(m, snapshotMsg{snap: snap})
			wantOnScreen(t, m, c.want, "○ #9 · on #15")
		})
	}
}
