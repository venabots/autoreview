package tui

import "github.com/venabots/autoreview/internal/engine"

// lookingNow asks the run to read the PR lists from GitHub now. A look
// under way already will answer, so a second ask is said and not sent.
func (m Model) lookingNow() Model {
	if m.snap.Looking {
		return m.flashed("already reading the PR lists from GitHub")
	}
	m.eng.Do(engine.LookNow{})
	return m.flashed("reading the PR lists from GitHub now")
}
