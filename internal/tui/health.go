package tui

import (
	"fmt"

	"github.com/venabots/autoreview/internal/engine"
	"github.com/venabots/autoreview/internal/words"
)

// healthMark is the footer's first word: what `autoreview doctor` would
// say, as the count of panelists that answer, in green when all is clear,
// yellow when a review can still approve, and red when reviews cannot.
// It is empty before the doctor's first answer. The log says what each
// problem is.
func healthMark(h *engine.Health) line {
	if h == nil {
		return nil
	}
	label := fmt.Sprintf("panel %d/%d", h.Answering, h.Seated)
	switch {
	case h.Seated == 0:
		label = "no panel"
	case h.Answering == h.Seated && len(h.Problems) > 0:
		label += " · " + words.Count(len(h.Problems), "problem")
	}
	style := greenStyle
	switch {
	case h.Blocking:
		style = redStyle
	case len(h.Problems) > 0:
		style = yellowStyle
	}
	return line{styled("● "+label, style), styled(" · ", dimStyle)}
}
