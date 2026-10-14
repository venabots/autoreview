package tui

import (
	"context"
	"fmt"
	"time"

	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"

	"github.com/venabots/autoreview/internal/engine"
	"github.com/venabots/autoreview/internal/sanitize"
	"github.com/venabots/autoreview/internal/task"
	"github.com/venabots/autoreview/internal/words"
)

// wheelLines is how far one notch of the wheel scrolls the right pane.
const wheelLines = 3

// actionTimeout bounds a browser or a tab launch, so a program that hangs
// cannot leave a command running for the life of the view.
const actionTimeout = 15 * time.Second

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
		return m.polled().synced(), tick()
	case spinner.TickMsg:
		return m.spun(msg)
	case tea.BackgroundColorMsg:
		m.dark = msg.IsDark()
		return m.synced(), nil
	case openedMsg:
		return m.flashed(openedText(msg)), nil
	case resumedMsg:
		return m.flashed(resumedText(msg)), nil
	case tea.KeyPressMsg:
		switch {
		case m.editing:
			return m.typed(msg)
		case m.modal.kind != modalNone:
			return m.modalPressed(intentFor(msg))
		}
		return m.pressed(intentFor(msg))
	case tea.PasteMsg:
		if m.editing {
			return m.pasted(msg)
		}
		return m, nil
	case tea.MouseWheelMsg:
		return m.wheeled(msg), nil
	case tea.MouseClickMsg:
		return m.clicked(msg), nil
	case tea.MouseMotionMsg:
		return m.dragged(msg), nil
	case tea.MouseReleaseMsg:
		return m.released()
	case copiedMsg:
		return m.copied(msg), nil
	}
	return m, nil
}

// took takes a new snapshot. A run that has ended closes the view: the
// summary is printed by whoever started the program, after the screen is
// given back.
func (m Model) took(snap *engine.Snapshot) (tea.Model, tea.Cmd) {
	m = m.noticing(snap)
	m.snap = snap
	m = m.synced().modalChecked()
	if snap.Phase == engine.Done {
		return m, tea.Quit
	}
	cmds := []tea.Cmd{waitForChange(m.eng)}
	if m.busy() && !m.spinning {
		m.spinning = true
		cmds = append(cmds, m.spin.Tick)
	}
	return m, tea.Batch(cmds...)
}

// spun moves the spinner on by one frame and asks for the next. It stops
// asking when nothing is in motion: an idle view must not redraw twelve
// times a second.
func (m Model) spun(msg spinner.TickMsg) (tea.Model, tea.Cmd) {
	if !m.busy() {
		m.spinning = false
		return m, nil
	}
	var cmd tea.Cmd
	m.spin, cmd = m.spin.Update(msg)
	return m.synced(), cmd
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
	case intentFirstTab:
		m.tab = tabReview
		return m.synced(), nil
	case intentSecondTab:
		m.tab = tabMine
		return m.synced(), nil
	case intentKeys:
		return m.asking(modal{kind: modalKeys}), nil
	case intentLog:
		return m.showing(sideLog), nil
	case intentBack:
		m.side = sideDetail
		return m.synced(), nil
	case intentMouse:
		return m.mouseToggled(), nil
	case intentOpen:
		return m.opening()
	case intentResume:
		if m.tab == tabMine {
			return m.resumingTask()
		}
		return m.resuming()
	case intentReviewNow:
		return m.reviewNow(), nil
	case intentStop:
		if m.tab == tabMine {
			return m.stopTaskPressed(), nil
		}
		return m.stopPressed(), nil
	case intentFocus:
		if m.tab == tabMine {
			return m.taskPressed(task.Fix), nil
		}
		return m.focusOpened()
	case intentComments:
		return m.taskPressed(task.Comments), nil
	case intentConflicts:
		return m.taskPressed(task.Conflicts), nil
	case intentAutofix:
		return m.autofixPressed(), nil
	case intentAutofixAll:
		return m.autofixAllPressed(), nil
	case intentFixAll:
		return m.fixAllPressed(), nil
	case intentWatch:
		return m.watchToggled(), nil
	case intentQuit:
		return m.quitPressed(), nil
	case intentInterrupt:
		return m.quit()
	}
	return m, nil
}

// showing puts a side in the right pane, or puts it away when it is there.
func (m Model) showing(s side) Model {
	if m.side == s {
		s = sideDetail
	}
	m.side = s
	return m.synced()
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
	open := m.act.Open
	launch := func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), actionTimeout)
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

// resuming reopens the selected review in a new terminal tab. The view
// stays up, and the tab holds the session from then on.
func (m Model) resuming() (tea.Model, tea.Cmd) {
	row, ok := m.selectedReview()
	if !ok {
		return m.flashed("there is no review to resume here"), nil
	}
	job, finished := row.finished()
	switch {
	case row.running():
		return m.flashed(fmt.Sprintf("PR #%d is being reviewed; resume it when the review ends", row.pr)), nil
	case !finished:
		return m.flashed(fmt.Sprintf("PR #%d has no finished review to resume", row.pr)), nil
	case job.Session == "":
		return m.flashed(fmt.Sprintf("PR #%d's review has no session to resume", row.pr)), nil
	}
	command := m.act.ResumeCommand(m.snap.Repo.Root, job.Orchestrator.ReopenCommand(job.Session))
	return m.flashed(fmt.Sprintf("opening PR #%d's review in a new tab", row.pr)), m.openTab(row.pr, "review", command)
}

func resumedText(msg resumedMsg) string {
	if msg.err != nil {
		return fmt.Sprintf("could not reopen PR #%d's %s: %s", msg.pr, msg.what, sanitize.Line(msg.err.Error()))
	}
	return fmt.Sprintf("opened PR #%d's %s in a new %s tab", msg.pr, msg.what, msg.where)
}

// reviewNow asks for a review of the selected PR, whatever holds it.
func (m Model) reviewNow() Model {
	if m.tab == tabMine {
		return m.flashed("R reviews other people's work; f fixes your own PR")
	}
	row, ok := m.selectedReview()
	switch {
	case !ok:
		return m.flashed("there is no PR to review")
	case row.listed == nil:
		return m.flashed(fmt.Sprintf("PR #%d is no longer on the list", row.pr))
	case row.job != nil && !row.job.State.Settled():
		return m.flashed(fmt.Sprintf("PR #%d is being reviewed already", row.pr))
	}
	m.eng.Do(engine.ReviewNow{PR: row.pr})
	return m.flashed(fmt.Sprintf("PR #%d is reviewed next", row.pr))
}

// stopPressed asks before it stops the selected review.
func (m Model) stopPressed() Model {
	row, ok := m.selectedReview()
	switch {
	case !ok:
		return m.flashed("there is no review to stop here")
	case row.job == nil || row.job.State != engine.JobRunning:
		return m.flashed(fmt.Sprintf("PR #%d has no review running", row.pr))
	}
	return m.asking(modal{kind: modalStop, pr: row.pr, what: "review"})
}

// quitPressed asks before it quits, whatever the run is doing: q is next
// to the keys a person uses all day, and a quit ends a run that watches.
// A run that is already stopping has been asked.
func (m Model) quitPressed() Model {
	if m.quitting {
		return m
	}
	return m.asking(modal{kind: modalQuit})
}

// quit asks the engine to end the run and waits for it to say it has. The
// view closes on the snapshot that says Done, so nothing the run started is
// still alive when the terminal is given back.
func (m Model) quit() (tea.Model, tea.Cmd) {
	if m.snap.Phase == engine.Done {
		return m, tea.Quit
	}
	m.quitting = true
	// What the footer says now is that the run is stopping.
	m.flash = flash{}
	m.eng.Do(engine.Quit{})
	return m, nil
}

// synced brings the view in line with the snapshot and the screen size:
// each cursor stays on its PR and on screen, and the right pane shows what
// is selected.
func (m Model) synced() Model {
	for _, t := range []tab{tabReview, tabMine} {
		rows := m.rows(t)
		m.cursors[t] = placed(m.cursors[t], rows, indexOf(rows, m.cursors[t].pr), m.lay.list.h)
	}
	m.pane.SetWidth(m.lay.detail.w)
	m.pane.SetHeight(m.lay.detail.h)
	if m.side == sideLog {
		m.log = m.log.loaded(logPath(m.snap))
	}
	if row, ok := m.selectedReview(); ok && m.side == sideDetail {
		if job, done := row.finished(); done && job.State == engine.JobDone {
			m.text = m.text.loaded(reviewPath(job), m.lay.detail.w, m.dark)
		}
	}
	atEnd := m.pane.AtBottom()
	key, content := m.paneContent()
	m.pane.SetContent(content)
	switch {
	case key != m.paneKey && key == "log":
		m.pane.GotoBottom()
	case key != m.paneKey:
		m.pane.GotoTop()
	case key == "log" && atEnd:
		// A log that was read at its end keeps showing its end.
		m.pane.GotoBottom()
	}
	if key != m.paneKey {
		m.sel = selection{}
	}
	m.paneKey = key
	return m
}

// watchToggled starts or stops watching for new work. Off, the run reviews
// only what R asks for.
func (m Model) watchToggled() Model {
	on := !m.snap.Watching
	m.eng.Do(engine.SetWatch{On: on})
	if on {
		return m.flashed("watching: reviewing what becomes ready, every " + words.Interval(m.snap.Watch))
	}
	return m.flashed("no longer watching; R still reviews a PR")
}
