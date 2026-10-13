package tui

import (
	tea "charm.land/bubbletea/v2"

	"github.com/venabots/autoreview/internal/cli"
	"github.com/venabots/autoreview/internal/engine"
)

// focusOpened opens the one-line editor for the focus in the footer, with
// the focus as it is now. While it is open, every key goes to the editor.
func (m Model) focusOpened() (tea.Model, tea.Cmd) {
	m.editing = true
	m.input.SetValue(m.snap.Focus)
	m.input.CursorEnd()
	return m, m.input.Focus()
}

// typed takes one key while the focus is being edited.
func (m Model) typed(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc", "ctrl+c":
		return m.focusClosed().flashed("the focus is unchanged"), nil
	case "enter":
		return m.focusApplied(), nil
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

func (m Model) focusClosed() Model {
	m.editing = false
	m.input.Blur()
	return m
}

// focusApplied sends the new focus to the engine. A blank line clears it.
// Text a prompt cannot carry is refused, and the editor stays open with
// what was typed: the same rule as the flag, so a sentence the flag would
// refuse does not reach a prompt by another door.
func (m Model) focusApplied() Model {
	typed := m.input.Value()
	if isBlank(typed) {
		m.eng.Do(engine.SetFocus{})
		return m.focusClosed().flashed("the reviewers are told nothing in particular")
	}
	focus, err := cli.ParseFocus(typed)
	if err != nil {
		return m.flashed(err.Error())
	}
	m.eng.Do(engine.SetFocus{Text: focus})
	return m.focusClosed().flashed("the reviewers are now told: " + focus)
}

func isBlank(s string) bool {
	for _, r := range s {
		if r != ' ' && r != '\t' {
			return false
		}
	}
	return true
}
