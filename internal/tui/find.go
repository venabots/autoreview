package tui

import (
	"fmt"
	"strconv"
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"

	"github.com/venabots/autoreview/internal/sanitize"
)

// newestListed is how many open PRs a look reads, on each tab. A PR older
// than that is not on either tab, and the find line says so instead of
// "no match", which would read as a PR that does not exist.
const newestListed = 50

// finder is the find line: what was typed, and where the person was before
// it opened, so esc can put them back.
type finder struct {
	open  bool
	input textinput.Model
	// tab and cursors are the view as it was when the find line opened.
	tab     tab
	cursors [2]cursor
	// miss says why nothing matches what was typed, or is empty.
	miss string
}

// findOpened opens the find line with typed already in it. A digit opens
// it too, so a person told "look at 2734" can type the number at once.
func (m Model) findOpened(typed string) (tea.Model, tea.Cmd) {
	input := textinput.New()
	input.Prompt = ""
	input.SetValue(typed)
	input.CursorEnd()
	cmd := input.Focus()
	m.find = finder{open: true, input: input, tab: m.tab, cursors: m.cursors}
	m.flash = flash{}
	return m.found(), cmd
}

// findTyped takes one key while the find line is open. Every key that
// types goes into the line: a q or a j there is part of what is looked
// for, not a quit or a move.
func (m Model) findTyped(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		return m.findClosed().quit()
	case "esc":
		return m.findCancelled(), nil
	case "enter":
		return m.findClosed(), nil
	case "backspace":
		if m.find.input.Value() == "" {
			return m.findCancelled(), nil
		}
	}
	var cmd tea.Cmd
	m.find.input, cmd = m.find.input.Update(msg)
	return m.found(), cmd
}

// findPasted puts pasted text in the find line: a PR number copied from a
// message arrives as one paste, not as key presses.
func (m Model) findPasted(msg tea.PasteMsg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	m.find.input, cmd = m.find.input.Update(msg)
	return m.found(), cmd
}

// findClosed closes the find line and keeps what it selected.
func (m Model) findClosed() Model {
	m.find = finder{}
	return m
}

// findCancelled closes the find line and puts the tab and the selection
// back as they were before it opened.
func (m Model) findCancelled() Model {
	m.tab, m.cursors = m.find.tab, m.find.cursors
	return m.findClosed().synced()
}

// found selects the first PR that matches what is typed, on the shown tab
// first and then on the other one. An empty line goes back to where the
// person started.
func (m Model) found() Model {
	query := strings.TrimSpace(m.find.input.Value())
	m.find.miss = ""
	if query == "" {
		m.tab, m.cursors = m.find.tab, m.find.cursors
		return m.synced()
	}
	match := matchesText(query)
	number, isNumber := numberIn(query)
	if isNumber {
		match = func(c candidate) bool { return c.pr == number }
	}
	for _, t := range []tab{m.tab, 1 - m.tab} {
		if pr, ok := firstMatch(m.candidates(t), match); ok {
			m.tab = t
			return m.selectedPR(pr)
		}
	}
	switch {
	case isNumber && m.startsAPRNumber(query):
		// More digits may still name a PR: wait for them.
	case isNumber:
		m.find.miss = notListedText(number)
	default:
		m.find.miss = noMatchText(query)
	}
	return m
}

// selectedPR puts the cursor of the shown tab on pr.
func (m Model) selectedPR(pr int) Model {
	m.cursors[m.tab] = cursor{pr: pr, offset: m.cursors[m.tab].offset}
	return m.synced()
}

// candidate is one PR as the find line reads it: its number and the words
// a person may remember it by.
type candidate struct {
	pr    int
	words []string
}

// candidates is the PRs on a tab, in the order the list shows them.
func (m Model) candidates(t tab) []candidate {
	words := map[int][]string{}
	if t == tabMine {
		for _, row := range mineRowsOf(m.snap) {
			words[row.pr.Number] = []string{row.pr.Title, row.pr.Branch}
		}
	} else {
		for _, row := range m.reviews() {
			words[row.pr] = []string{row.title(), row.branch(), row.author()}
		}
	}
	rows := m.rows(t)
	out := make([]candidate, len(rows))
	for i, row := range rows {
		out[i] = candidate{pr: row.pr, words: words[row.pr]}
	}
	return out
}

func firstMatch(list []candidate, match func(candidate) bool) (int, bool) {
	for _, c := range list {
		if match(c) {
			return c.pr, true
		}
	}
	return 0, false
}

// matchesText is a match on any of a PR's words, whatever the case.
func matchesText(query string) func(candidate) bool {
	query = strings.ToLower(query)
	return func(c candidate) bool {
		for _, w := range c.words {
			if strings.Contains(strings.ToLower(w), query) {
				return true
			}
		}
		return false
	}
}

// numberIn reads a query of digits, with or without a "#" before them, as
// a PR number.
func numberIn(query string) (int, bool) {
	digits := strings.TrimPrefix(query, "#")
	if digits == "" || strings.Trim(digits, "0123456789") != "" {
		return 0, false
	}
	n, err := strconv.Atoi(digits)
	return n, err == nil
}

// startsAPRNumber says some PR on either tab has a number that begins
// with the digits typed so far.
func (m Model) startsAPRNumber(query string) bool {
	digits := strings.TrimPrefix(query, "#")
	for _, t := range []tab{tabReview, tabMine} {
		for _, row := range m.rows(t) {
			if strings.HasPrefix(strconv.Itoa(row.pr), digits) {
				return true
			}
		}
	}
	return false
}

func notListedText(pr int) string {
	return fmt.Sprintf("PR #%d is not on either tab: it may be closed, a draft or a bot's, or older than the %d newest open PRs", pr, newestListed)
}

func noMatchText(query string) string {
	return fmt.Sprintf("no PR on either tab matches %q", sanitize.Line(query))
}
