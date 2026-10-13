package stats

import (
	"io"
	"strings"

	"charm.land/lipgloss/v2"
	"charm.land/lipgloss/v2/table"
	"github.com/charmbracelet/x/term"
)

// styles is how the terminal rendering marks what matters.
type styles struct {
	bold   lipgloss.Style
	dim    lipgloss.Style
	good   lipgloss.Style
	warn   lipgloss.Style
	bad    lipgloss.Style
	normal lipgloss.Style
}

// stylesFor is the styles to draw with. With color off every style is the
// plain one, so the table reads the same and sends no escape sequence: a
// user who set NO_COLOR asked for that.
func stylesFor(color bool) styles {
	plain := lipgloss.NewStyle()
	if !color {
		return styles{bold: plain, dim: plain, good: plain, warn: plain, bad: plain, normal: plain}
	}
	return styles{
		bold:   plain.Bold(true),
		dim:    plain.Faint(true),
		good:   plain.Foreground(lipgloss.Green),
		warn:   plain.Foreground(lipgloss.Yellow),
		bad:    plain.Foreground(lipgloss.Red),
		normal: plain,
	}
}

// The availability under which a cohort is failing, and under which it is
// worth a second look.
const (
	failingBelow = 0.5
	healthyFrom  = 0.9
)

func (s styles) availability(r ratio) lipgloss.Style {
	switch {
	case !r.known:
		return s.dim
	case r.value < failingBelow:
		return s.bad
	case r.value < healthyFrom:
		return s.warn
	}
	return s.good
}

func (s styles) sample(word string) lipgloss.Style {
	switch word {
	case "proven":
		return s.good
	case "emerging":
		return s.warn
	}
	return s.dim
}

// cell is the style of one cell of a cohort's row.
func (s styles) cell(c cohort, col int) lipgloss.Style {
	switch col {
	case colModel:
		return s.bold
	case colAvail:
		return s.availability(c.availability())
	case colSample:
		return s.sample(c.sample())
	}
	return s.normal
}

// screen is the report for a terminal: the same cells as the plain text, in
// a table with rounded corners. width is the terminal's width, or zero when
// it is not known.
func screen(r report, s styles, width int) string {
	var out strings.Builder
	out.WriteString(s.bold.Render(headline(r)))
	out.WriteByte('\n')
	if len(r.cohorts) > 0 {
		out.WriteString(cohortTable(r.cohorts, s, width))
		out.WriteByte('\n')
	}
	for _, line := range footer(r) {
		style := s.dim
		if strings.HasPrefix(line, attentionPrefix) {
			style = s.warn
		}
		out.WriteString(style.Render(line))
		out.WriteByte('\n')
	}
	return out.String()
}

func cohortTable(cohorts []cohort, s styles, width int) string {
	t := table.New().
		Border(lipgloss.RoundedBorder()).
		BorderStyle(s.dim).
		Headers(header...).
		StyleFunc(func(rowIndex, col int) lipgloss.Style {
			style := s.normal
			if rowIndex >= 0 && rowIndex < len(cohorts) {
				style = s.cell(cohorts[rowIndex], col)
			}
			return style.Padding(0, 1)
		})
	for _, c := range cohorts {
		t = t.Row(row(c)...)
	}
	drawn := t.String()
	// A table wider than the terminal wraps every border line and reads as
	// noise, so it is squeezed to fit. A narrower one is left at its own
	// width: stretched to the edge, it is harder to read across.
	if width > 0 && lipgloss.Width(drawn) > width {
		return t.Width(width).String()
	}
	return drawn
}

// widthOf is the width of the terminal a writer is, or zero when the writer
// is not one or will not say.
func widthOf(w io.Writer) int {
	file, ok := w.(interface{ Fd() uintptr })
	if !ok {
		return 0
	}
	width, _, err := term.GetSize(file.Fd())
	if err != nil {
		return 0
	}
	return width
}
