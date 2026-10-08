package tui

import (
	"slices"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// span is a run of text in one style. Its text is already sanitized and
// holds no escape sequences, so its width can be measured as written.
type span struct {
	text  string
	style lipgloss.Style
}

// line is one row of the screen, as spans. A row stays as spans until it is
// drawn: a selected row is reversed span by span, which a string that
// already holds escape sequences does not allow.
type line []span

func text(s string) span {
	return span{text: s}
}

func styled(s string, style lipgloss.Style) span {
	return span{text: s, style: style}
}

func (l line) width() int {
	total := 0
	for _, s := range l {
		total += ansi.StringWidth(s.text)
	}
	return total
}

// fit makes a line exactly width columns: cut with an ellipsis when it is
// longer, padded with spaces when it is shorter. Width is in display
// columns, so a CJK title counts two a character.
//
// A list row must never wrap: a wrapped line would spill into the next PR's
// row and every row below it would be off by one.
func fit(l line, width int) line {
	if width <= 0 {
		return line{}
	}
	if total := l.width(); total <= width {
		return append(slices.Clone(l), text(strings.Repeat(" ", width-total)))
	}
	out := line{}
	room := width - 1
	for _, s := range l {
		w := ansi.StringWidth(s.text)
		if w <= room {
			out = append(out, s)
			room -= w
			continue
		}
		kept := ansi.Truncate(s.text, room, "")
		room -= ansi.StringWidth(kept)
		// A wide character that did not fit leaves a column over.
		return append(out, styled(kept+"…", s.style), text(strings.Repeat(" ", room)))
	}
	return out
}

// render draws the line. reverse draws every span reversed, which is how
// the selected row is marked across its whole width.
func (l line) render(reverse bool) string {
	var b strings.Builder
	for _, s := range l {
		if s.text == "" {
			continue
		}
		b.WriteString(s.style.Reverse(reverse).Render(s.text))
	}
	return b.String()
}
