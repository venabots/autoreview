package tui

import (
	"github.com/venabots/autoreview/internal/engine"
	"github.com/venabots/autoreview/internal/sanitize"
)

// noticing says in the footer what a new snapshot brought that nobody
// pressed a key for just now. A task is asked for with one key and ends
// minutes later, or never starts, and the person may be on another row or
// another tab by then.
//
// It compares the snapshot on screen with the next one, so the same news
// is said once.
func (m Model) noticing(next *engine.Snapshot) Model {
	for _, pr := range next.Mine {
		why := next.NotStarted[pr.Number]
		if why != "" && why != m.snap.NotStarted[pr.Number] {
			return m.flashed(sanitize.Line(why))
		}
	}
	return m
}
