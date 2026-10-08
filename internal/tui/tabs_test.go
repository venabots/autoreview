package tui

import (
	"testing"

	tea "charm.land/bubbletea/v2"
)

func TestAClickOnATabShowsIt(t *testing.T) {
	m, _ := newModel(t, 120, 30)
	// The tab bar is row 1: " Review 4 " at columns 0-9, two spaces, then
	// " My PRs 2 " at columns 12-21.
	click := func(m Model, x, y int) Model {
		return send(m, tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft})
	}

	m = click(m, 12, 1)
	wantOnScreen(t, m, "#4 My own work", "tab review")
	m = click(m, 21, 1)
	wantOnScreen(t, m, "#4 My own work")

	m = click(m, 9, 1)
	wantOnScreen(t, m, "#9 Add retry logic", "tab my PRs")
	m = click(m, 0, 1)
	wantOnScreen(t, m, "#9 Add retry logic")

	for _, x := range []int{10, 11, 22, 60} {
		if got := click(m, x, 1); got.tab != tabReview {
			t.Errorf("a click at column %d of the tab bar, on no tab, changed the tab", x)
		}
	}
	if got := click(m, 14, 0); got.tab != tabReview {
		t.Error("a click on the header changed the tab")
	}
	if got := send(m, tea.MouseClickMsg{X: 14, Y: 1, Button: tea.MouseRight}); got.tab != tabReview {
		t.Error("a right click on a tab changed the tab")
	}
}

func TestAClickedTabKeepsItsSelection(t *testing.T) {
	m, _ := newModel(t, 120, 30)
	m = press(t, m, "j")
	m = send(m, tea.MouseClickMsg{X: 14, Y: 1, Button: tea.MouseLeft})
	m = send(m, tea.MouseClickMsg{X: 3, Y: 1, Button: tea.MouseLeft})
	wantOnScreen(t, m, "#8 Fix the cache")
}

func TestTabAtFollowsTheLabelsAsTheyGrow(t *testing.T) {
	review, mine := " Review 12 ", " My PRs 3 "
	cases := []struct {
		x    int
		want tab
		ok   bool
	}{
		{0, tabReview, true}, {10, tabReview, true}, {11, 0, false}, {12, 0, false},
		{13, tabMine, true}, {22, tabMine, true}, {23, 0, false}, {-1, 0, false},
	}
	for _, c := range cases {
		got, ok := tabAt(c.x, review, mine)
		if ok != c.ok || (ok && got != c.want) {
			t.Errorf("tabAt(%d) = %d, %v; want %d, %v", c.x, got, ok, c.want, c.ok)
		}
	}
}
