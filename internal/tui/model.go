// Package tui is the full-screen view: every PR the run knows about on the
// left, and the selected one on the right, under two tabs.
//
// The view reads snapshots from the engine and sends it commands. It holds
// no run state of its own, only where the person is looking: which tab,
// which PR, how far the right pane is scrolled.
package tui

import (
	"context"
	"slices"
	"time"

	"charm.land/bubbles/v2/spinner"
	"charm.land/bubbles/v2/textinput"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"

	"github.com/venabots/autoreview/internal/engine"
	"github.com/venabots/autoreview/internal/words"
)

// Engine is the run, as the view sees it.
type Engine interface {
	Snapshot() *engine.Snapshot
	Changed() <-chan struct{}
	Do(engine.Command)
}

// Actions is what the view can do outside itself. The view runs no
// process: each of these is handed in.
type Actions struct {
	// Open opens one PR in the browser.
	Open func(ctx context.Context, pr int) error
	// OpenTab opens a new terminal tab that runs command, and says which
	// terminal it used.
	OpenTab func(ctx context.Context, label, command string) (where string, err error)
	// ResumeCommand writes the command line that reopens a review in dir.
	ResumeCommand func(dir, reopen string) string
	// Transcript finds the transcript of a session, for the activity of a
	// review that is running.
	Transcript func(id string) (string, bool)
	// Copy puts text on the system clipboard. It may be nil: the view
	// also asks the terminal to do it.
	Copy func(ctx context.Context, text string) error
}

type tab int

const (
	tabReview tab = iota
	tabMine
)

// side is what the right pane shows.
type side int

const (
	sideDetail side = iota
	sideLog
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
	eng Engine
	act Actions

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
	// sel is the text a drag in the right pane selected.
	sel selection

	// spin turns while the run is busy. spinning says its ticks are on the
	// way, so a look that starts does not start a second chain of them.
	spin     spinner.Model
	spinning bool

	// mouse is true while the view captures the mouse. Turned off, a drag
	// selects text in the terminal again.
	mouse    bool
	flash    flash
	quitting bool
	// modal is the box that is open over the screen, and has the keys.
	modal modal

	// input is the one-line editor for the focus, and editing says it has
	// the keys.
	input   textinput.Model
	editing bool

	text reviewText
	log  logTail
	// tails follows each running review, by PR.
	tails map[int]followed
	// dark is true on a dark terminal. It picks the colors of a rendered
	// review.
	dark bool
}

// New makes the view for a run.
func New(eng Engine, act Actions) Model {
	snap := eng.Snapshot()
	m := Model{
		eng:   eng,
		act:   act,
		snap:  snap,
		now:   time.Now(),
		pane:  viewport.New(),
		spin:  spinner.New(spinner.WithSpinner(spinner.MiniDot)),
		input: textinput.New(),
		mouse: true,
		dark:  true,
	}
	m.input.Prompt = ""
	m.spinning = m.busy()
	return m
}

type snapshotMsg struct{ snap *engine.Snapshot }

type tickMsg time.Time

type openedMsg struct {
	pr  int
	err error
}

// copiedMsg says whether the clipboard outside the terminal took the
// text.
type copiedMsg struct{ err error }

type resumedMsg struct {
	pr int
	// what is "review" or "task".
	what  string
	where string
	err   error
}

// Init starts what the view waits on: the engine's next change, the clock,
// the spinner when the run opens busy, and the terminal's answer about its
// background.
func (m Model) Init() tea.Cmd {
	cmds := []tea.Cmd{waitForChange(m.eng), tick(), tea.RequestBackgroundColor}
	if m.spinning {
		cmds = append(cmds, m.spin.Tick)
	}
	return tea.Batch(cmds...)
}

// busy says something is in motion: the run reads the lists, or a review
// or a task is running. The spinner turns only then.
func (m Model) busy() bool {
	if m.snap.Looking {
		return true
	}
	for _, job := range append(slices.Clone(m.snap.Jobs), m.snap.Tasks...) {
		switch job.State {
		case engine.JobRunning, engine.JobFinishing, engine.JobPreparing:
			return true
		}
	}
	return false
}

// alive counts the reviews and tasks a quit would cut short or never
// start.
func (m Model) alive() int {
	reviews, tasks := m.aliveEach()
	return reviews + tasks
}

func (m Model) aliveEach() (reviews, tasks int) {
	for _, job := range m.snap.Jobs {
		if !job.State.Settled() {
			reviews++
		}
	}
	for _, job := range m.snap.Tasks {
		if !job.State.Settled() {
			tasks++
		}
	}
	return reviews, tasks
}

// aliveWords is what a quit would stop: "2 reviews", "1 task", or "3 jobs"
// when it is both.
func (m Model) aliveWords() string {
	reviews, tasks := m.aliveEach()
	switch {
	case tasks == 0:
		return words.Count(reviews, "review")
	case reviews == 0:
		return words.Count(tasks, "task")
	}
	return words.Count(reviews+tasks, "job")
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

func (m Model) reviews() []reviewRow {
	return reviewRowsOf(m.snap)
}

func (m Model) rows(t tab) []listRow {
	if t == tabMine {
		return mineListRows(mineRowsOf(m.snap), m.snap.Babysit, m.now, m.spin.View())
	}
	return reviewListRows(m.reviews(), m.now, m.spin.View())
}

// selected is the PR under the cursor on the tab that is shown.
func (m Model) selected() (int, bool) {
	rows := m.rows(m.tab)
	if len(rows) == 0 {
		return 0, false
	}
	return rows[indexOf(rows, m.cursors[m.tab].pr)].pr, true
}

// selectedReview is the row under the cursor on the Review tab.
func (m Model) selectedReview() (reviewRow, bool) {
	pr, ok := m.selected()
	if !ok || m.tab != tabReview {
		return reviewRow{}, false
	}
	for _, row := range m.reviews() {
		if row.pr == pr {
			return row, true
		}
	}
	return reviewRow{}, false
}

func (m Model) flashed(text string) Model {
	m.flash = flash{text: text, until: m.now.Add(flashFor)}
	return m
}
