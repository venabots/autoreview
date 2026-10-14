package tui

import (
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// spot is one cell of the right pane's text. Its line counts from the top
// of the whole text, not of the screen, so a selection stays on its words
// when the pane scrolls.
type spot struct {
	line, col int
}

func (s spot) before(o spot) bool {
	return s.line < o.line || (s.line == o.line && s.col < o.col)
}

// selection is the text a drag in the right pane marks. The view captures
// the mouse, so a drag in the terminal would select across both panes and
// the footer; this one stays inside the pane and copies only its words.
type selection struct {
	anchor, head spot
	// dragging says the button is still down.
	dragging bool
	// on says the selection is drawn.
	on bool
}

// started is a selection that begins at one cell, with the button down.
func started(at spot) selection {
	return selection{anchor: at, head: at, dragging: true, on: true}
}

// empty says the drag never left its first cell: a click, not a selection.
func (s selection) empty() bool {
	return s.anchor == s.head
}

// bounds is the first and the last cell, in reading order, whichever way
// the drag went.
func (s selection) bounds() (spot, spot) {
	if s.head.before(s.anchor) {
		return s.head, s.anchor
	}
	return s.anchor, s.head
}

// span is the columns of line i that the selection covers, from inclusive
// to exclusive, in a line width cells wide. ok is false when it covers
// none.
func (s selection) span(i, width int) (from, to int, ok bool) {
	first, last := s.bounds()
	if !s.on || s.empty() || i < first.line || i > last.line {
		return 0, 0, false
	}
	from, to = 0, width
	if i == first.line {
		from = first.col
	}
	if i == last.line {
		to = min(last.col+1, width)
	}
	return from, to, from < to
}

// selectedText is the plain text the selection covers in lines, without
// the colors and without the spaces that pad each line to the pane's edge.
func selectedText(lines []string, s selection) string {
	first, last := s.bounds()
	out := []string{}
	for i := first.line; i <= last.line && i < len(lines); i++ {
		plain := ansi.Strip(lines[i])
		from, to, ok := s.span(i, ansi.StringWidth(plain))
		if !ok {
			out = append(out, "")
			continue
		}
		out = append(out, strings.TrimRight(ansi.Cut(plain, from, to), " "))
	}
	return strings.Join(out, "\n")
}

// highlighted is the pane's lines on screen with the selection drawn on
// them. offset is the line of the text at the top of the pane.
func highlighted(lines []string, offset int, s selection) []string {
	if !s.on || s.empty() {
		return lines
	}
	out := make([]string, len(lines))
	for i, l := range lines {
		from, to, ok := s.span(offset+i, ansi.StringWidth(l))
		if !ok {
			out[i] = l
			continue
		}
		out[i] = lipgloss.StyleRanges(l, lipgloss.NewRange(from, to, selectedStyle))
	}
	return out
}
