package tui

import tea "charm.land/bubbletea/v2"

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

// clicked acts on a left click: a tab in the tab bar is shown, and a PR in
// the list is selected.
func (m Model) clicked(msg tea.MouseClickMsg) Model {
	switch {
	case msg.Button != tea.MouseLeft || m.modal.kind != modalNone:
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

