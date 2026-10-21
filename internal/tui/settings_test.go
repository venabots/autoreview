package tui

import (
	"errors"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// editingWith is the view with an editor that closes at once with err, and
// a count of the times it was opened.
func editingWith(t *testing.T, err error) (Model, *int) {
	t.Helper()
	m, _ := newModel(t, 250, 30)
	opened := 0
	m.act.EditSettings = func(done tea.ExecCallback) tea.Cmd {
		return func() tea.Msg {
			opened++
			return done(err)
		}
	}
	return m, &opened
}

// pressRunning presses a key and sends back what its command says.
func pressRunning(t *testing.T, m Model, name string) Model {
	t.Helper()
	next, cmd := m.Update(keys[name])
	m = next.(Model)
	if cmd == nil {
		t.Fatalf("%s started nothing", name)
	}
	return send(m, cmd())
}

func TestEOpensTheSettingsFileAndSaysWhenTheChangeApplies(t *testing.T) {
	for _, shown := range []string{"Review", "Maintain"} {
		m, opened := editingWith(t, nil)
		if shown == "Maintain" {
			m = press(t, m, "tab")
		}
		m = pressRunning(t, m, "e")
		if *opened != 1 {
			t.Errorf("the editor opened %d times, want once", *opened)
		}
		wantOnScreen(t, m, "the settings file reads well; a change applies when autoreview starts again")
	}
}

func TestASettingsFileThatDoesNotReadIsSaid(t *testing.T) {
	m, _ := editingWith(t, errors.New("config.toml: loop.max_pases is not a setting"))
	m = pressRunning(t, m, "e")
	wantOnScreen(t, m, "the settings file has a fault: config.toml: loop.max_pases is not a setting")
}

func TestARunWithNoSettingsFileSaysSoAndOpensNothing(t *testing.T) {
	m, _ := newModel(t, 250, 30)
	next, cmd := m.Update(keys["e"])
	if cmd != nil {
		t.Error("e started a command with no settings file to open")
	}
	wantOnScreen(t, next.(Model), "this run has no settings file: $AUTOREVIEW_CONFIG is off, or there is no home directory")
}

func TestTheKeyHelpNamesTheSettingsKey(t *testing.T) {
	m, _ := newModel(t, 250, 40)
	wantOnScreen(t, press(t, m, "?"), "edit the settings file")
}
