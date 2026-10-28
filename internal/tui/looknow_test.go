package tui

import (
	"testing"

	"github.com/venabots/autoreview/internal/engine"
)

// A person who just merged or pushed on GitHub must not wait out the watch
// interval to see it.
func TestCtrlRLooksAtGitHubNow(t *testing.T) {
	m, hs := newModel(t, 160, 30)
	m = press(t, m, "ctrl+r")
	if got := hs.eng.got; len(got) != 1 || got[0] != (engine.LookNow{}) {
		t.Errorf("ctrl+r sent %+v, want LookNow", got)
	}
	wantOnScreen(t, m, "reading the PR lists from GitHub now")

	hs.eng.snap.Looking = true
	m = send(m, snapshotMsg{snap: hs.eng.snap})
	m = press(t, m, "ctrl+r")
	if got := hs.eng.got; len(got) != 1 {
		t.Errorf("ctrl+r during a look sent %+v, want nothing more", got)
	}
	wantOnScreen(t, m, "already reading the PR lists from GitHub")
	wantOnScreen(t, press(t, m, "?"), "read the PR lists from GitHub now")
}
