package tui

import (
	"context"
	"fmt"
	"time"

	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"

	"github.com/venabots/autoreview/internal/engine"
	"github.com/venabots/autoreview/internal/sanitize"
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
		if m.editing {
			return m.typed(msg)
		}
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
	// Any key but the one that was armed puts the arming away.
	if in != intentStop && in != intentQuit {
		m.armed = confirm{}
	}
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
		return m.showing(sideKeys), nil
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
		return m.resuming()
	case intentReviewNow:
		return m.reviewNow(), nil
	case intentStop:
		return m.stopPressed(), nil
	case intentFocus:
		return m.focusOpened()
	case intentWatch:
		return m.watchToggled(), nil
	case intentQuit:
		return m.quitPressed()
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
	label := fmt.Sprintf("PR %d Resume", row.pr)
	openTab := m.act.OpenTab
	launch := func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), actionTimeout)
		defer cancel()
		where, err := openTab(ctx, label, command)
		return resumedMsg{pr: row.pr, where: where, err: err}
	}
	return m.flashed(fmt.Sprintf("opening PR #%d's review in a new tab", row.pr)), launch
}

func resumedText(msg resumedMsg) string {
	if msg.err != nil {
		return fmt.Sprintf("could not reopen PR #%d's review: %s", msg.pr, sanitize.Line(msg.err.Error()))
	}
	return fmt.Sprintf("opened PR #%d's review in a new %s tab", msg.pr, msg.where)
}

// reviewNow asks for a review of the selected PR, whatever holds it.
func (m Model) reviewNow() Model {
	if m.tab == tabMine {
		return m.flashed("the run reviews other people's work; your own PRs are handled from keys that come later")
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

// stopPressed stops the selected review on the second press.
func (m Model) stopPressed() Model {
	row, ok := m.selectedReview()
	if !ok || row.job == nil || row.job.State != engine.JobRunning {
		m.armed = confirm{}
		if !ok {
			return m.flashed("there is no review to stop here")
		}
		return m.flashed(fmt.Sprintf("PR #%d has no review running", row.pr))
	}
	if !m.armed.holds(confirmStop, row.pr, m.now) {
		m.armed = confirm{kind: confirmStop, pr: row.pr, at: m.now}
		return m.flashed(fmt.Sprintf("press x again to stop PR #%d's review", row.pr))
	}
	m.armed = confirm{}
	m.eng.Do(engine.StopJob{PR: row.pr})
	return m.flashed(fmt.Sprintf("stopping PR #%d's review", row.pr))
}

// quitPressed quits at once when nothing is running. With reviews alive it
// quits on the second press, because quitting stops them.
func (m Model) quitPressed() (tea.Model, tea.Cmd) {
	alive := m.alive()
	if alive == 0 || m.armed.holds(confirmQuit, 0, m.now) {
		return m.quit()
	}
	m.armed = confirm{kind: confirmQuit, at: m.now}
	reviews := "reviews"
	if alive == 1 {
		reviews = "review"
	}
	return m.flashed(fmt.Sprintf("press q again to stop %d %s and quit", alive, reviews)), nil
}

// quit asks the engine to end the run and waits for it to say it has. The
// view closes on the snapshot that says Done, so nothing the run started is
// still alive when the terminal is given back.
func (m Model) quit() (tea.Model, tea.Cmd) {
	if m.snap.Phase == engine.Done {
		return m, tea.Quit
	}
	m.quitting = true
	m.armed = confirm{}
	// The message about the second press is answered. What the footer
	// says now is that the run is stopping.
	m.flash = flash{}
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

// clicked acts on a left click: a tab in the tab bar is shown, and a PR in
// the list is selected.
func (m Model) clicked(msg tea.MouseClickMsg) Model {
	switch {
	case msg.Button != tea.MouseLeft:
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
