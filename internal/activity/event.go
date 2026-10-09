// Package activity says what a running review is doing right now.
//
// dash-p says nothing while a review runs: its stderr is silent in json
// mode and its answer lands when the review ends. The one file written as
// the review goes is the session transcript Claude Code keeps, one JSON
// line per block: a tool call with its input, the text the reviewer wrote,
// the thinking in between. Following that file is how the view can say
// "Bash cargo test" instead of only "reviewing 1m47s".
//
// The file is read incrementally. Each poll is one stat, and only the bytes
// past the last read are parsed. The file may not exist for the first
// seconds of a review, and never exists for a reviewer that is not claude,
// so a tail knows how to have nothing to say.
package activity

import (
	"time"
	"unicode/utf8"
)

const (
	// keep is how many recent events a tail holds. The view shows as many
	// as its detail pane holds, and a review scrolled back through is worth
	// more than a few bytes a line.
	keep = 200
	// summaryWidth bounds what a pasted essay in a command costs to keep.
	// Every view cuts its lines to its own width as well.
	summaryWidth = 200
	// nameWidth bounds a tool's name. An MCP tool is named for its server
	// and its method both, which runs to fifty columns and leaves the
	// detail line no room for what the tool was given.
	nameWidth = 20
	// lookupEvery is how often to look for a transcript that has not
	// appeared yet. The lookup walks every Claude Code project directory,
	// which is not a cost to pay on every tick.
	lookupEvery = time.Second
)

// Event is one thing the reviewer did.
type Event struct {
	// At is when the transcript says it happened. It is zero when the line
	// had no timestamp, and always zero for a line of an override's stderr.
	At time.Time
	// Tool is what to call the tool on a row, and "" for text the reviewer
	// wrote. It is a name to draw, not a name to match on: it is cleaned of
	// anything a terminal would act on and cut to nameWidth.
	Tool string
	// Summary is one line: the command, the file, the first line of text.
	// It is already sanitized and cut to summaryWidth.
	Summary string
}

// cut shortens s to width characters, the last of them an ellipsis.
//
// A character is taken as one column. The bound only limits what a long
// line costs to keep, and the view cuts to its real width again.
func cut(s string, width int) string {
	if utf8.RuneCountInString(s) <= width {
		return s
	}
	runes := []rune(s)
	return string(runes[:width-1]) + "…"
}
