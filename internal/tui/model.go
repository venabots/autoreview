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

	"charm.land/bubbles/v2/spinner"
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

	// spin turns while the run is reading the lists. spinning says its
	// ticks are on the way, so a look that starts does not start a second
	// chain of them.
	spin     spinner.Model
	spinning bool

	// mouse is true while the view captures the mouse. Turned off, a drag
	// selects text in the terminal again.
	mouse    bool
	flash    flash
	quitting bool
}

// New makes the view for a run.
func New(eng Engine, open Opener) Model {
	snap := eng.Snapshot()
	return Model{
		eng:      eng,
		open:     open,
		snap:     snap,
		now:      time.Now(),
		pane:     viewport.New(),
		spin:     spinner.New(spinner.WithSpinner(spinner.MiniDot)),
		spinning: snap.Phase == engine.Looking,
		mouse:    true,
	}
}

type snapshotMsg struct{ snap *engine.Snapshot }

type tickMsg time.Time

type openedMsg struct {
	pr  int
	err error
}

// Init starts what the view waits on: the engine's next change, the clock
// that counts down to the next look, and the spinner when the run opens on
// a look.
func (m Model) Init() tea.Cmd {
	cmds := []tea.Cmd{waitForChange(m.eng), tick()}
	if m.spinning {
		cmds = append(cmds, m.spin.Tick)
	}
	return tea.Batch(cmds...)
}

// looking says the run is reading the lists now.
func (m Model) looking() bool {
	return m.snap.Phase == engine.Looking
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
