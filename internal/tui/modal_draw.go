package tui

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

const (
	// modalMaxText is the widest a box's text gets. Past it, a line of
	// help is harder to read than the same line wrapped.
	modalMaxText = 76
	// modalMinText is the narrowest text a border is drawn around.
	modalMinText = 16
	// modalEdge is the columns a box adds to its text: a border and a
	// space on each side.
	modalEdge = 4
	// modalFrame is the rows a box adds to its body: the border, the
	// title, the hint and the blank line above the hint.
	modalFrame = 5
)

var modalBorder = lipgloss.NewStyle().
	Border(lipgloss.RoundedBorder()).
	BorderForeground(lipgloss.BrightBlack).
	Padding(0, 1)

// shape is the open box laid out for one screen size.
type shape struct {
	// boxed is false on a screen too small for a border. The text then
	// takes the screen from its top, so the question is still asked.
	boxed bool
	// width is the columns the text has.
	width int
	title string
	// body is every row under the title, wrapped to width. room is how
	// many of them fit, and top is the first one shown.
	body []string
	room int
	top  int
	hint string
}

// lastTop is the furthest the body scrolls: its last row on the box's
// last line.
func (s shape) lastTop() int {
	return max(len(s.body)-s.room, 0)
}

func (m Model) modalShape() shape {
	w, h := m.lay.width, m.lay.height
	t := m.modalText()
	frame := modalFrame
	if len(t.body) > 0 {
		// A body has a blank line between it and the title.
		frame++
	}
	s := shape{boxed: w-modalEdge >= modalMinText && h >= frame+min(len(t.body), 1), width: w}
	room := h - 2
	if s.boxed {
		s.width = min(max(widest(t), modalMinText), modalMaxText, w-modalEdge)
		room = h - frame
	}
	s.body = saidRows(t.body, s.width)
	s.room = min(max(room, 0), len(s.body))
	s.top = min(m.modal.top, s.lastTop())
	hints := t.hint
	if s.lastTop() > 0 {
		hints = append(hints[:len(hints):len(hints)], hint{"j/k", "scrolls"})
	}
	s.title = boldStyle.Render(t.title)
	s.hint = hintLine(hints).render(false)
	return s
}

// widest is the columns a box's text wants before any wrapping.
func widest(t modalText) int {
	w := max(ansi.StringWidth(t.title), hintLine(t.hint).width())
	lead := leadColumn(t.body)
	for _, b := range t.body {
		w = max(w, lead+ansi.StringWidth(b.text))
	}
	return w
}

// leadGap is the space between the column of keys and what each one does.
const leadGap = 2

// leadColumn is how wide the column of keys is, or zero for a body that
// names no key.
func leadColumn(body []said) int {
	col := 0
	for _, b := range body {
		if b.lead != "" {
			col = max(col, ansi.StringWidth(b.lead)+leadGap)
		}
	}
	return col
}

// saidRows is the body wrapped to width. A long line goes on under its
// own words, so the column of keys stays clear to read down.
func saidRows(body []said, width int) []string {
	col := leadColumn(body)
	rows := []string{}
	for _, b := range body {
		for i, part := range strings.Split(lipgloss.Wrap(b.text, max(width-col, 1), ""), "\n") {
			lead := ""
			if i == 0 {
				lead = b.lead
			}
			rows = append(rows, line{styled(fmt.Sprintf("%-*s", col, lead), numberStyle), text(part)}.render(false))
		}
	}
	return rows
}

// rows is the text of the box, top to bottom, each row exactly as wide as
// the text column.
func (s shape) rows() []string {
	rows := []string{s.title}
	if s.boxed && len(s.body) > 0 {
		rows = append(rows, "")
	}
	rows = append(rows, s.body[s.top:s.top+s.room]...)
	if s.boxed {
		rows = append(rows, "")
	}
	rows = append(rows, s.hint)
	for i, row := range rows {
		rows[i] = padded(row, s.width)
	}
	return rows
}

// overlaid draws the open box over a frame. Every row stays exactly as
// wide as the screen: a row one column short would wrap in the terminal
// and push every row under it down.
func (m Model) overlaid(frame []string) []string {
	s := m.modalShape()
	if !s.boxed {
		out := append([]string{}, frame...)
		copy(out, s.rows())
		return out
	}
	w, h := m.lay.width, len(frame)
	box := modalBorder.Render(strings.Join(s.rows(), "\n"))
	x, y := (w-lipgloss.Width(box))/2, (h-lipgloss.Height(box))/2
	layers := lipgloss.NewCompositor(
		lipgloss.NewLayer(strings.Join(frame, "\n")),
		lipgloss.NewLayer(box).X(max(x, 0)).Y(max(y, 0)).Z(1),
	)
	// The canvas trims the spaces at the end of a row, and the rows at
	// the end of the screen that are blank.
	drawn := exactly(strings.Split(lipgloss.NewCanvas(w, h).Compose(layers).Render(), "\n"), h)
	for i, row := range drawn {
		drawn[i] = padded(row, w)
	}
	return drawn
}

// padded makes a drawn row exactly width columns: cut when it is longer,
// filled with spaces when it is shorter.
func padded(row string, width int) string {
	row = ansi.Truncate(row, width, "")
	return row + strings.Repeat(" ", max(width-ansi.StringWidth(row), 0))
}
