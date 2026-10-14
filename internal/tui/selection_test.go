package tui

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// The right pane of a 120 by 30 screen starts at column 38, row 2. Its
// first line is "#9 Add retry logic"; "review    no decision yet" and
// "checks    passing" are on rows 8 and 9.
const paneX, paneY = 38, 2

func drag(m Model, fromX, fromY, toX, toY int) (Model, tea.Cmd) {
	m = send(m, tea.MouseClickMsg{X: fromX, Y: fromY, Button: tea.MouseLeft})
	m = send(m, tea.MouseMotionMsg{X: toX, Y: toY, Button: tea.MouseLeft})
	next, cmd := m.Update(tea.MouseReleaseMsg{X: toX, Y: toY, Button: tea.MouseLeft})
	return next.(Model), cmd
}

// ran runs a command and the commands of any batch it returns, and keeps
// every message that comes back.
func ran(cmd tea.Cmd) []tea.Msg {
	if cmd == nil {
		return nil
	}
	msg := cmd()
	batch, ok := msg.(tea.BatchMsg)
	if !ok {
		return []tea.Msg{msg}
	}
	out := []tea.Msg{}
	for _, c := range batch {
		out = append(out, ran(c)...)
	}
	return out
}

// toTerminal is the text the view asked the terminal to put on its
// clipboard. Bubble Tea's message for it is unexported and is a string.
func toTerminal(msgs []tea.Msg) []string {
	out := []string{}
	for _, msg := range msgs {
		if fmt.Sprintf("%T", msg) == "tea.setClipboardMsg" {
			out = append(out, fmt.Sprint(msg))
		}
	}
	return out
}

func TestADragInTheRightPaneCopiesOnlyTheTextItSelects(t *testing.T) {
	cases := []struct {
		name                   string
		fromX, fromY, toX, toY int
		want                   string
		said                   string
	}{
		{"part of one line", paneX + 3, paneY, paneX + 17, paneY, "Add retry logic", "copied 1 line"},
		{"two lines, dragged upward", paneX + 16, paneY + 7, paneX, paneY + 6, "review    no decision yet\nchecks    passing", "copied 2 lines"},
		{"past the end of a line", paneX, paneY, paneX + 80, paneY, "#9 Add retry logic", "copied 1 line"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m, hs := newModel(t, 120, 30)
			m, cmd := drag(m, c.fromX, c.fromY, c.toX, c.toY)
			msgs := ran(cmd)
			if got := toTerminal(msgs); len(got) != 1 || got[0] != c.want {
				t.Errorf("the terminal was asked to copy %q, want %q", got, c.want)
			}
			if len(hs.copied) != 1 || hs.copied[0] != c.want {
				t.Errorf("the clipboard got %q, want %q", hs.copied, c.want)
			}
			wantOnScreen(t, m, c.said)
		})
	}
}

func TestAClickInTheRightPaneCopiesNothing(t *testing.T) {
	m, hs := newModel(t, 120, 30)
	m, cmd := drag(m, paneX+3, paneY, paneX+3, paneY)
	if cmd != nil {
		t.Errorf("a click returned a command: %v", ran(cmd))
	}
	if len(hs.copied) != 0 {
		t.Errorf("a click copied %q", hs.copied)
	}
	wantNotOnScreen(t, m, "copied")
}

func TestASelectionIsDrawnReversedUntilAnotherPRIsShown(t *testing.T) {
	m, _ := newModel(t, 120, 30)
	m = send(m, tea.MouseClickMsg{X: paneX + 3, Y: paneY, Button: tea.MouseLeft})
	m = send(m, tea.MouseMotionMsg{X: paneX + 17, Y: paneY, Button: tea.MouseLeft})
	reversed := "\x1b[7mAdd retry logic\x1b["
	if !strings.Contains(m.View().Content, reversed) {
		t.Errorf("the selection is not drawn reversed while the drag goes on")
	}
	m, _ = drag(m, paneX+3, paneY, paneX+17, paneY)
	if !strings.Contains(m.View().Content, reversed) {
		t.Errorf("the selection is not drawn after the button comes up")
	}
	m = press(t, m, "j")
	if strings.Contains(m.View().Content, "\x1b[7m#8") || strings.Contains(m.View().Content, reversed) {
		t.Errorf("the selection is still drawn on another PR")
	}
}

func TestAClickOutsideThePanePutsTheSelectionAway(t *testing.T) {
	m, _ := newModel(t, 120, 30)
	m, _ = drag(m, paneX+3, paneY, paneX+17, paneY)
	m = send(m, tea.MouseClickMsg{X: 5, Y: 2, Button: tea.MouseLeft})
	if strings.Contains(m.View().Content, "\x1b[7mAdd retry logic") {
		t.Error("the selection is still drawn after a click in the list")
	}
}

func TestADragPastTheBottomOfThePaneScrollsIt(t *testing.T) {
	m, hs := newModel(t, 120, 8)
	bottom := paneY + m.lay.detail.h
	m = send(m, tea.MouseClickMsg{X: paneX, Y: paneY, Button: tea.MouseLeft})
	m = send(m, tea.MouseMotionMsg{X: paneX + 30, Y: bottom, Button: tea.MouseLeft})
	m = send(m, tea.MouseMotionMsg{X: paneX + 30, Y: bottom, Button: tea.MouseLeft})
	ran(func() tea.Cmd { _, cmd := m.Update(tea.MouseReleaseMsg{X: paneX + 30, Y: bottom}); return cmd }())
	if len(hs.copied) != 1 || !strings.HasPrefix(hs.copied[0], "#9 Add retry logic\n") || strings.Count(hs.copied[0], "\n") != m.lay.detail.h+1 {
		t.Errorf("the clipboard got %q, want the pane's lines and two more", hs.copied)
	}
}

func TestAClipboardThatRefusesTheTextSaysWhy(t *testing.T) {
	m, hs := newModel(t, 120, 30)
	hs.cpErr = errors.New("pbcopy: exit status 1")
	m, cmd := drag(m, paneX+3, paneY, paneX+17, paneY)
	for _, msg := range ran(cmd) {
		m = send(m, msg)
	}
	wantOnScreen(t, m, "could not copy: pbcopy: exit status 1")
}
