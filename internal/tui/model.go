// Package tui is the full-screen view: every PR the run knows about on the
// left, and the selected one on the right, under two tabs.
//
// The view reads snapshots from the engine and sends it commands. It holds
// no run state of its own, only where the person is looking: which tab,
// which PR, how far the right pane is scrolled.
package tui

import (
	"context"
	"time"

	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"

	"github.com/venabots/autoreview/internal/engine"
)

// Engine is the run, as the view sees it.
type Engine interface {
	Snapshot() *engine.Snapshot
	Changed() <-chan struct{}
	Do(engine.Command)
}

// Opener opens one PR in the browser.
type Opener func(ctx context.Context, pr int) error

type tab int

const (
	tabReview tab = iota
	tabMine
)

// side is what the right pane shows.
type side int

const (
	sideDetail side = iota
	sideKeys
)

// flashFor is how long a message stays in the footer.
const flashFor = 5 * time.Second

// flash is a message that takes the footer's left side for a few seconds:
// what a key just did, or why it did nothing.
type flash struct {
	text  string
	until time.Time
}

// Model is the view's state.
type Model struct {
	eng  Engine
	open Opener

	snap *engine.Snapshot
	now  time.Time
	lay  layout

	tab     tab
	cursors [2]cursor
	side    side
	pane    viewport.Model
	// paneKey names what the right pane shows. A different key means a
	// different PR or side, and the pane starts from its top again.
	paneKey string

	// mouse is true while the view captures the mouse. Turned off, a drag
	// selects text in the terminal again.
	mouse    bool
	flash    flash
	quitting bool
}

// New makes the view for a run.
func New(eng Engine, open Opener) Model {
	return Model{
		eng:   eng,
		open:  open,
		snap:  eng.Snapshot(),
		now:   time.Now(),
		pane:  viewport.New(),
		mouse: true,
	}
}

type snapshotMsg struct{ snap *engine.Snapshot }

type tickMsg time.Time

type openedMsg struct {
	pr  int
	err error
}

// Init starts the two things the view waits on: the engine's next change,
// and the clock that counts down to the next look.
func (m Model) Init() tea.Cmd {
	return tea.Batch(waitForChange(m.eng), tick())
}

// waitForChange blocks until the engine has a newer snapshot and delivers
// it. The view asks again after each one, so the engine never has to hold a
// reference to the program or wait on a slow screen.
func waitForChange(eng Engine) tea.Cmd {
	return func() tea.Msg {
		<-eng.Changed()
		return snapshotMsg{snap: eng.Snapshot()}
	}
}

func tick() tea.Cmd {
	return tea.Tick(time.Second, func(t time.Time) tea.Msg { return tickMsg(t) })
}

func (m Model) rows(t tab) []listRow {
	if t == tabMine {
		return mineRows(m.snap.Mine)
	}
	return reviewRows(m.snap.Reviews)
}

// selected is the PR under the cursor on the tab that is shown.
func (m Model) selected() (int, bool) {
	rows := m.rows(m.tab)
	if len(rows) == 0 {
		return 0, false
	}
	return rows[indexOf(rows, m.cursors[m.tab].pr)].pr, true
}

func (m Model) flashed(text string) Model {
	m.flash = flash{text: text, until: m.now.Add(flashFor)}
	return m
}
