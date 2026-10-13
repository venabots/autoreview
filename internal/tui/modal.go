package tui

import (
	"fmt"

	tea "charm.land/bubbletea/v2"

	"github.com/venabots/autoreview/internal/engine"
)

type modalKind int

const (
	modalNone modalKind = iota
	modalKeys
	modalQuit
	modalStop
)

// modal is the box over the screen: the key help, or a question before a
// costly key acts. Stopping a review throws away what it has spent, and
// quitting stops every review, so one stray key must not do either: both
// ask first, and act on the same key pressed again.
//
// While a box is open it has the keys. A stop names its PR: the rows move
// as reviews finish, and the answer must stop the review the question was
// about, not the one that is under the cursor by then.
type modal struct {
	kind modalKind
	pr   int
	// what is the job a stop ends: "review" or "task".
	what string
	// top is the first row of the key help on screen, when the help is
	// taller than its box.
	top int
}

func (m Model) asking(q modal) Model {
	m.modal = q
	return m
}

func (m Model) closed() Model {
	m.modal = modal{}
	return m
}

// modalPressed takes one key while a box is open. esc closes every box,
// and ctrl-C quits from every one of them: neither may depend on what the
// box asks.
func (m Model) modalPressed(in intent) (tea.Model, tea.Cmd) {
	switch {
	case in == intentInterrupt:
		return m.closed().quit()
	case in == intentBack:
		return m.closed(), nil
	case m.modal.kind == modalKeys:
		return m.keysPressed(in), nil
	case m.modal.kind == modalQuit && in == intentQuit:
		return m.closed().quit()
	case m.modal.kind == modalStop && in == intentStop:
		return m.stopConfirmed(), nil
	}
	return m, nil
}

// keysPressed scrolls the key help, or closes it on the key that opened it
// and on q.
func (m Model) keysPressed(in intent) Model {
	s := m.modalShape()
	switch in {
	case intentKeys, intentQuit:
		return m.closed()
	case intentDown:
		return m.keysScrolled(1)
	case intentUp:
		return m.keysScrolled(-1)
	case intentPageDown:
		return m.keysScrolled(max(s.room/2, 1))
	case intentPageUp:
		return m.keysScrolled(-max(s.room/2, 1))
	case intentTop:
		return m.keysScrolled(-len(s.body))
	case intentBottom:
		return m.keysScrolled(len(s.body))
	}
	return m
}

func (m Model) keysScrolled(delta int) Model {
	m.modal.top = min(max(m.modal.top+delta, 0), m.modalShape().lastTop())
	return m
}

func (m Model) stopConfirmed() Model {
	pr, what := m.modal.pr, m.modal.what
	m.eng.Do(engine.StopJob{PR: pr})
	return m.closed().flashed(fmt.Sprintf("stopping PR #%d's %s", pr, what))
}

// modalChecked closes a stop question whose job has ended. The question
// would otherwise ask about a review that is no longer there to stop.
func (m Model) modalChecked() Model {
	if m.modal.kind != modalStop || m.stoppable(m.modal.pr, m.modal.what) {
		return m
	}
	pr, what := m.modal.pr, m.modal.what
	return m.closed().flashed(fmt.Sprintf("PR #%d's %s ended before it was stopped", pr, what))
}

// stoppable says the PR has a job of that kind with a process to stop.
func (m Model) stoppable(pr int, what string) bool {
	jobs := m.snap.Jobs
	if what == "task" {
		jobs = m.snap.Tasks
	}
	for _, job := range jobs {
		if job.PR == pr && job.State == engine.JobRunning {
			return true
		}
	}
	return false
}

// modalText is what a box says: its title, the lines under it, and the
// keys that answer it.
type modalText struct {
	title string
	body  []said
	hint  []hint
}

// said is one line of a box: a key or nothing, then what it says.
type said struct {
	lead string
	text string
}

func (m Model) modalText() modalText {
	switch m.modal.kind {
	case modalQuit:
		t := modalText{title: "Quit autoreview?", hint: []hint{{"q", "quit"}, {"esc", "stay"}}}
		if m.alive() > 0 {
			t.body = []said{{text: "A quit stops " + m.aliveWords() + "."}}
		}
		return t
	case modalStop:
		return modalText{
			title: fmt.Sprintf("Stop PR #%d's %s?", m.modal.pr, m.modal.what),
			hint:  []hint{{"x", "stop"}, {"esc", "keep it"}},
		}
	}
	keys := make([]said, len(helpLines))
	for i, k := range helpLines {
		keys[i] = said{lead: k.keys, text: k.what}
	}
	return modalText{title: "KEYS", body: keys, hint: []hint{{"esc", "closes"}}}
}
