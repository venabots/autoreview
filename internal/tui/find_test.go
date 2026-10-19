package tui

import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/venabots/autoreview/internal/engine"
	"github.com/venabots/autoreview/internal/prs"
	"github.com/venabots/autoreview/internal/queue"
)

// withFrank adds PR #2734 to the Review tab: a number long enough that a
// person types a prefix of it on the way.
func withFrank(s *engine.Snapshot) {
	s.Reviews = append(s.Reviews, queue.Row{PR: prs.PR{Number: 2734, Title: "Rotate the API keys", Author: "frank", Branch: "frank/rotate", Base: "main"}})
}

// finding is the view on the Review tab, wide enough for every message,
// after the keys named in open.
func finding(t *testing.T, open ...string) (Model, *harness) {
	t.Helper()
	m, hs := newModel(t, 200, 30)
	withFrank(hs.eng.snap)
	m = send(m, snapshotMsg{snap: hs.eng.snap})
	return press(t, m, open...), hs
}

func wantSelected(t *testing.T, m Model, wantTab tab, wantPR int) {
	t.Helper()
	pr, _ := m.selected()
	if m.tab != wantTab || pr != wantPR {
		t.Errorf("the view shows tab %d with PR #%d selected, want tab %d with PR #%d", m.tab, pr, wantTab, wantPR)
	}
}

func TestSlashOpensAnEmptyFindLine(t *testing.T) {
	m, hs := finding(t, "/")
	if footer := lastLine(m); !strings.HasPrefix(footer, "/ ") || !strings.Contains(footer, "enter keeps it · esc goes back") {
		t.Errorf("the footer is %q, want the find line", footer)
	}
	if len(commands(hs)) != 0 {
		t.Errorf("opening the find line reached the engine: %v", commands(hs))
	}
}

func TestADigitOpensTheFindLineWithThatDigitTyped(t *testing.T) {
	m, _ := finding(t, "8")
	if footer := lastLine(m); !strings.HasPrefix(footer, "/ 8") {
		t.Errorf("the footer is %q, want the find line with 8 in it", footer)
	}
	wantSelected(t, m, tabReview, 8)
}

func TestTheFindLineSelectsTheFirstPRThatMatches(t *testing.T) {
	cases := []struct {
		typed string
		tab   tab
		pr    int
	}{
		{"8", tabReview, 8},
		{"#6", tabReview, 6},
		{"2734", tabReview, 2734},
		{"cache", tabReview, 8},
		{"TIDY", tabReview, 6},
		{"dave", tabReview, 5},
		{"carol/docs", tabReview, 6},
		{"frank", tabReview, 2734},
		{"own work", tabMine, 4},
		{"me/second", tabMine, 3},
		{"3", tabMine, 3},
	}
	for _, c := range cases {
		m, _ := finding(t, "/")
		m = typeText(t, m, c.typed)
		t.Run(c.typed, func(t *testing.T) { wantSelected(t, m, c.tab, c.pr) })
	}
}

func TestTheShownTabIsSearchedFirst(t *testing.T) {
	m, _ := finding(t, "tab", "/")
	// "e" is in titles on both tabs; the first on My PRs wins there.
	m = typeText(t, m, "e")
	wantSelected(t, m, tabMine, 4)
}

func TestANumberThatIsOnlyTheStartOfAPRNumberDoesNotMove(t *testing.T) {
	m, _ := finding(t, "/")
	for _, typed := range []string{"2", "27", "273"} {
		m = typeText(t, m, typed[len(typed)-1:])
		wantSelected(t, m, tabReview, 9)
		wantNotOnScreen(t, m, "is not on either tab")
	}
	m = typeText(t, m, "4")
	wantSelected(t, m, tabReview, 2734)
	wantOnScreen(t, m, "#2734 Rotate the API keys")
}

func TestEnterClosesTheFindLineAndKeepsThePR(t *testing.T) {
	m, _ := finding(t, "/")
	m = press(t, typeText(t, m, "second"), "enter")
	wantSelected(t, m, tabMine, 3)
	if m.find.open {
		t.Fatal("the find line is still open after enter")
	}
	wantOnScreen(t, m, "q quit")
}

func TestEscPutsTheSelectionAndTheTabBack(t *testing.T) {
	m, _ := finding(t, "j")
	reviewBefore, _ := m.selected()
	mineBefore := m.cursors[tabMine]
	m = typeText(t, press(t, m, "/"), "second")
	wantSelected(t, m, tabMine, 3)
	m = press(t, m, "esc")
	wantSelected(t, m, tabReview, reviewBefore)
	if m.find.open {
		t.Fatal("the find line is still open after esc")
	}
	if m.cursors[tabMine] != mineBefore {
		t.Errorf("My PRs has cursor %+v, want it back at %+v", m.cursors[tabMine], mineBefore)
	}
}

func TestBackspaceOnAnEmptyFindLineClosesItAsEscDoes(t *testing.T) {
	backspace := tea.KeyPressMsg{Code: tea.KeyBackspace}
	m, _ := finding(t, "/")
	m = typeText(t, m, "6")
	wantSelected(t, m, tabReview, 6)
	m = send(m, backspace)
	if !m.find.open {
		t.Fatal("a backspace that left text to erase closed the find line")
	}
	wantSelected(t, m, tabReview, 9)
	m = send(m, backspace)
	if m.find.open {
		t.Fatal("a backspace on an empty find line left it open")
	}
	wantSelected(t, m, tabReview, 9)
}

func TestKeysTypedIntoTheFindLineDoNothingElse(t *testing.T) {
	m, hs := finding(t, "/")
	m = typeText(t, m, "qj?x")
	if m.modal.kind != modalNone {
		t.Errorf("a key typed into the find line opened a box: %v", m.modal.kind)
	}
	if len(commands(hs)) != 0 {
		t.Errorf("a key typed into the find line reached the engine: %v", commands(hs))
	}
	if footer := lastLine(m); !strings.HasPrefix(footer, "/ qj?x") {
		t.Errorf("the footer is %q, want what was typed", footer)
	}
}

func TestCtrlCInTheFindLineQuitsAtOnce(t *testing.T) {
	m, hs := finding(t, "/")
	m = press(t, m, "ctrl+c")
	if got := commands(hs); len(got) != 1 || got[0] != (engine.Quit{}) {
		t.Errorf("the engine was sent %v, want one Quit", got)
	}
	if m.find.open {
		t.Error("the find line is still open while the run stops")
	}
}

func TestAFindWithNoMatchSaysSo(t *testing.T) {
	cases := map[string]string{
		"zzz":  `no PR on either tab matches "zzz"`,
		"4242": "PR #4242 is not on either tab: it may be closed, a draft or a bot's, or older than the 50 newest open PRs",
		"#88":  "PR #88 is not on either tab",
	}
	for typed, want := range cases {
		m, _ := finding(t, "/")
		m = typeText(t, m, typed)
		t.Run(typed, func(t *testing.T) { wantOnScreen(t, m, want) })
	}
}

// Typing goes through every prefix of what the person means, and a
// prefix can match a PR on the way: "#88" passes "#8". The selection
// stays on the last PR that matched rather than jumping back.
func TestTheSelectionStaysOnTheLastMatchWhenMoreTypingMatchesNothing(t *testing.T) {
	m, _ := finding(t, "/")
	m = typeText(t, m, "zzz")
	wantSelected(t, m, tabReview, 9)
	m, _ = finding(t, "/")
	m = typeText(t, m, "#88")
	wantSelected(t, m, tabReview, 8)
}

func TestANoMatchMessageGoesWhenTheTextMatchesAgain(t *testing.T) {
	m, _ := finding(t, "/")
	m = typeText(t, m, "cachex")
	wantOnScreen(t, m, `no PR on either tab matches "cachex"`)
	m = send(m, tea.KeyPressMsg{Code: tea.KeyBackspace})
	wantNotOnScreen(t, m, "no PR on either tab")
	wantSelected(t, m, tabReview, 8)
}

// The message names a number the listing does not ask for. If the query
// asked for more, the message would send a person looking for a reason
// that is not true.
func TestTheNotListedMessageNamesTheListingsOwnLimit(t *testing.T) {
	if want := fmt.Sprintf("first:%d,", newestListed); !strings.Contains(prs.Query, want) {
		t.Errorf("the PR listing does not ask for %q; the find message names %d", want, newestListed)
	}
}

func TestTheFooterNamesTheFindKeyOnBothTabs(t *testing.T) {
	m, _ := newModel(t, 250, 30)
	wantOnScreen(t, m, "/ find")
	wantOnScreen(t, press(t, m, "tab"), "/ find")
}

func TestTheKeyHelpNamesTheFindKey(t *testing.T) {
	m, _ := newModel(t, 150, 60)
	wantOnScreen(t, press(t, m, "?"), "find a PR by its number")
}

func TestAPasteGoesIntoTheFindLine(t *testing.T) {
	m, _ := finding(t, "/")
	m = send(m, tea.PasteMsg{Content: "2734"})
	wantSelected(t, m, tabReview, 2734)
}
