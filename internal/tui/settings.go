package tui

import (
	tea "charm.land/bubbletea/v2"

	"github.com/venabots/autoreview/internal/sanitize"
)

// settingsEditedMsg says the editor of the settings file closed, and err
// is why the editor or the file it left failed.
type settingsEditedMsg struct{ err error }

// editingSettings hands the terminal to the person's editor on the
// settings file. A run reads its settings once, when it starts, so the
// answer says when a change applies: a person who saw nothing change
// would edit the file again.
func (m Model) editingSettings() (tea.Model, tea.Cmd) {
	if m.act.EditSettings == nil {
		return m.flashed("this run has no settings file: $AUTOREVIEW_CONFIG is off, or there is no home directory"), nil
	}
	return m, m.act.EditSettings(func(err error) tea.Msg { return settingsEditedMsg{err: err} })
}

func settingsEditedText(msg settingsEditedMsg) string {
	if msg.err != nil {
		return "the settings file has a fault: " + sanitize.Line(msg.err.Error())
	}
	return "the settings file reads well; a change applies when autoreview starts again"
}
