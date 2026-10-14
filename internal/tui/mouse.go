package tui

import (
	"context"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/venabots/autoreview/internal/sanitize"
	"github.com/venabots/autoreview/internal/words"
)

// wheeled scrolls what is under the pointer: the right pane by a few
// lines, or the list by one PR.
func (m Model) wheeled(msg tea.MouseWheelMsg) Model {
	if m.modal.kind != modalNone {
		return m
	}
	delta := 0
	switch msg.Button {
	case tea.MouseWheelUp:
		delta = -1
	case tea.MouseWheelDown:
		delta = 1
	}
	switch {
	case delta == 0:
		return m
	case m.lay.list.contains(msg.X, msg.Y):
		return m.moved(delta)
	case m.lay.detail.contains(msg.X, msg.Y) && delta > 0:
		m.pane.ScrollDown(wheelLines)
	case m.lay.detail.contains(msg.X, msg.Y):
		m.pane.ScrollUp(wheelLines)
	}
	return m
}

// clicked acts on a left click: a tab in the tab bar is shown, a PR in the
// list is selected, and a press in the right pane starts a selection. Any
// click puts the last selection away.
func (m Model) clicked(msg tea.MouseClickMsg) Model {
	if msg.Button != tea.MouseLeft || m.modal.kind != modalNone {
		return m
	}
	m.sel = selection{}
	switch {
	case m.lay.detail.contains(msg.X, msg.Y):
		m.sel = started(m.spotAt(msg.X, msg.Y))
		return m
	case m.lay.tabs.contains(msg.X, msg.Y):
		return m.tabClicked(msg.X)
	case !m.lay.list.contains(msg.X, msg.Y):
		return m
	}
	rows := m.rows(m.tab)
	index, ok := rowAt(msg.Y-m.lay.list.y, m.cursors[m.tab].offset, len(rows))
	if !ok {
		return m
	}
	return m.movedTo(index)
}

func (m Model) tabClicked(x int) Model {
	review, mine := tabLabels(m.snap)
	clicked, ok := tabAt(x, review, mine)
	if !ok {
		return m
	}
	m.tab = clicked
	return m.synced()
}

// dragged moves the end of the selection to the pointer. A drag past the
// top or the bottom of the pane scrolls it, so a selection can be longer
// than the pane.
func (m Model) dragged(msg tea.MouseMotionMsg) Model {
	if !m.sel.dragging {
		return m
	}
	switch d := m.lay.detail; {
	case msg.Y < d.y:
		m.pane.ScrollUp(1)
	case msg.Y >= d.y+d.h:
		m.pane.ScrollDown(1)
	}
	m.sel.head = m.spotAt(msg.X, msg.Y)
	return m
}

// released ends a drag and copies what it selected. A press that never
// moved is a click and copies nothing.
func (m Model) released() (Model, tea.Cmd) {
	if !m.sel.dragging {
		return m, nil
	}
	m.sel.dragging = false
	if m.sel.empty() {
		m.sel = selection{}
		return m, nil
	}
	text := selectedText(strings.Split(m.pane.GetContent(), "\n"), m.sel)
	m = m.flashed("copied " + words.Count(strings.Count(text, "\n")+1, "line"))
	return m, tea.Batch(tea.SetClipboard(text), m.copying(text))
}

// copying hands the text to the clipboard outside the terminal as well.
// OSC 52, which tea.SetClipboard sends, does not get through every
// terminal and multiplexer.
func (m Model) copying(text string) tea.Cmd {
	copyText := m.act.Copy
	if copyText == nil {
		return nil
	}
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), actionTimeout)
		defer cancel()
		return copiedMsg{err: copyText(ctx, text)}
	}
}

// spotAt is the cell of the right pane's text under the pointer, held
// inside the pane and the text.
func (m Model) spotAt(x, y int) spot {
	d := m.lay.detail
	row := min(max(y-d.y, 0), max(d.h-1, 0))
	line := min(m.pane.YOffset()+row, max(m.pane.TotalLineCount()-1, 0))
	return spot{line: line, col: min(max(x-d.x, 0), max(d.w-1, 0))}
}

// copied says why the clipboard did not take the text. The footer already
// says it was copied when it was.
func (m Model) copied(msg copiedMsg) Model {
	if msg.err == nil {
		return m
	}
	return m.flashed("could not copy: " + sanitize.Line(msg.err.Error()))
}
