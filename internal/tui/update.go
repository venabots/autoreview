package tui

import (
	"context"
	"fmt"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/venabots/autoreview/internal/engine"
	"github.com/venabots/autoreview/internal/sanitize"
)

// wheelLines is how far one notch of the wheel scrolls the right pane.
const wheelLines = 3

// openTimeout bounds the browser launch, so a gh that hangs cannot leave a
// command running for the life of the view.
const openTimeout = 15 * time.Second

// Update takes one message and returns the view after it.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.lay = layoutFor(msg.Width, msg.Height)
		return m.synced(), nil
	case snapshotMsg:
		return m.took(msg.snap)
	case tickMsg:
		m.now = time.Time(msg)
		return m.synced(), tick()
	case openedMsg:
		return m.flashed(openedText(msg)), nil
	case tea.KeyPressMsg:
		return m.pressed(intentFor(msg))
	case tea.MouseWheelMsg:
		return m.wheeled(msg), nil
	case tea.MouseClickMsg:
		return m.clicked(msg), nil
	}
	return m, nil
}

// took takes a new snapshot. A run that has ended closes the view: the
// summary is printed by whoever started the program, after the screen is
// given back.
func (m Model) took(snap *engine.Snapshot) (tea.Model, tea.Cmd) {
	m.snap = snap
	m = m.synced()
	if snap.Phase == engine.Done {
		return m, tea.Quit
	}
	return m, waitForChange(m.eng)
}

func (m Model) pressed(in intent) (tea.Model, tea.Cmd) {
	switch in {
	case intentUp:
		return m.moved(-1), nil
	case intentDown:
		return m.moved(1), nil
	case intentTop:
		return m.movedTo(0), nil
	case intentBottom:
		return m.movedTo(len(m.rows(m.tab)) - 1), nil
	case intentPageDown:
		m.pane.HalfPageDown()
	case intentPageUp:
		m.pane.HalfPageUp()
	case intentSwitchTab:
		m.tab = 1 - m.tab
		return m.synced(), nil
	case intentKeys:
		m.side = sideKeys - m.side
		return m.synced(), nil
	case intentBack:
		m.side = sideDetail
		return m.synced(), nil
	case intentMouse:
		return m.mouseToggled(), nil
	case intentOpen:
		return m.opening()
	case intentQuit, intentInterrupt:
		return m.quit()
	}
	return m, nil
}

func (m Model) moved(delta int) Model {
	rows := m.rows(m.tab)
	m.cursors[m.tab] = moved(m.cursors[m.tab], rows, delta, m.lay.list.h)
	return m.synced()
}

func (m Model) movedTo(index int) Model {
	rows := m.rows(m.tab)
	m.cursors[m.tab] = placed(m.cursors[m.tab], rows, index, m.lay.list.h)
	return m.synced()
}

func (m Model) mouseToggled() Model {
	m.mouse = !m.mouse
	if m.mouse {
		return m.flashed("the screen has the mouse again")
	}
	return m.flashed("the mouse is the terminal's: drag to select text, m to take it back")
}

// opening starts the browser for the selected PR. gh runs off the update
// loop: a slow launch must not freeze the screen.
func (m Model) opening() (tea.Model, tea.Cmd) {
	pr, ok := m.selected()
	if !ok {
		return m.flashed("there is no PR to open"), nil
	}
	open := m.open
	launch := func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), openTimeout)
		defer cancel()
		return openedMsg{pr: pr, err: open(ctx, pr)}
	}
	return m.flashed(fmt.Sprintf("opening PR #%d in the browser", pr)), launch
}

func openedText(msg openedMsg) string {
	if msg.err != nil {
		return fmt.Sprintf("gh could not open PR #%d: %s", msg.pr, sanitize.Line(msg.err.Error()))
	}
	return fmt.Sprintf("opened PR #%d in the browser", msg.pr)
}

// quit asks the engine to end the run and waits for it to say it has. The
// view closes on the snapshot that says Done, so nothing the run started is
// still alive when the terminal is given back.
func (m Model) quit() (tea.Model, tea.Cmd) {
	if m.snap.Phase == engine.Done {
		return m, tea.Quit
	}
	m.quitting = true
	m.eng.Do(engine.Quit{})
	return m, nil
}

// wheeled scrolls what is under the pointer: the right pane by a few
// lines, or the list by one PR.
func (m Model) wheeled(msg tea.MouseWheelMsg) Model {
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

// clicked selects the PR under a left click in the list.
func (m Model) clicked(msg tea.MouseClickMsg) Model {
	if msg.Button != tea.MouseLeft || !m.lay.list.contains(msg.X, msg.Y) {
		return m
	}
	rows := m.rows(m.tab)
	index, ok := rowAt(msg.Y-m.lay.list.y, m.cursors[m.tab].offset, len(rows))
	if !ok {
		return m
	}
	return m.movedTo(index)
}

// synced brings the view in line with the snapshot and the screen size:
// each cursor stays on its PR and on screen, and the right pane shows the
// selected PR.
func (m Model) synced() Model {
	for _, t := range []tab{tabReview, tabMine} {
		rows := m.rows(t)
		m.cursors[t] = placed(m.cursors[t], rows, indexOf(rows, m.cursors[t].pr), m.lay.list.h)
	}
	m.pane.SetWidth(m.lay.detail.w)
	m.pane.SetHeight(m.lay.detail.h)
	key, lines := m.paneContent()
	m.pane.SetContent(wrapped(lines, m.lay.detail.w))
	if key != m.paneKey {
		m.paneKey = key
		m.pane.GotoTop()
	}
	return m
}
