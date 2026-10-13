package stats

import (
	"strings"
	"unicode"

	"github.com/charmbracelet/x/ansi"
)

// plain is the report as aligned text, for a pipe or a file. The words and
// the columns are v1's: a script may grep them.
func plain(r report) string {
	var out strings.Builder
	out.WriteString(headline(r))
	out.WriteByte('\n')
	if len(r.cohorts) > 0 {
		rows := [][]string{header}
		for _, c := range r.cohorts {
			rows = append(rows, row(c))
		}
		out.WriteByte('\n')
		out.WriteString(align(rows))
	}
	out.WriteByte('\n')
	for _, line := range footer(r) {
		out.WriteString(line)
		out.WriteByte('\n')
	}
	return out.String()
}

// align sets rows in columns two spaces apart. A column is as wide as its
// widest cell on the screen, which is not its length in bytes: a name in
// Chinese is twice as wide as its count of characters.
func align(rows [][]string) string {
	widths := columnWidths(rows)
	var out strings.Builder
	for _, cells := range rows {
		var line strings.Builder
		for i, cell := range cells {
			line.WriteString(cell)
			if i+1 < len(cells) {
				line.WriteString(strings.Repeat(" ", widths[i]-ansi.StringWidth(cell)+2))
			}
		}
		out.WriteString(strings.TrimRightFunc(line.String(), unicode.IsSpace))
		out.WriteByte('\n')
	}
	return out.String()
}

func columnWidths(rows [][]string) []int {
	var widths []int
	for _, cells := range rows {
		for i, cell := range cells {
			if i == len(widths) {
				widths = append(widths, 0)
			}
			widths[i] = max(widths[i], ansi.StringWidth(cell))
		}
	}
	return widths
}
