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

// render draws the line. An unselected line draws each span in its own
// style. The selected line is one solid bar across its whole width.
func (l line) render(selected bool) string {
	var b strings.Builder
	for _, s := range l {
		if s.text == "" {
			continue
		}
		style := s.style
		if selected {
			style = barStyle(s.style)
		}
		b.WriteString(style.Render(s.text))
	}
	return b.String()
}

// barStyle is a span's style on the selected line: reversed, with no color
// of its own, and bold where the span was bold.
//
// Reversing a span that has a color makes that color its background. A row
// of dim, cyan and plain spans then has three backgrounds, and the dim text
// sits on a block of its own grey. With no colors, the bar is the
// terminal's text color and the text is its background color: the one pair
// that is legible in every theme.
func barStyle(s lipgloss.Style) lipgloss.Style {
	return lipgloss.NewStyle().Reverse(true).Bold(s.GetBold())
}
