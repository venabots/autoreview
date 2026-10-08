package tui

// rowLines is how many screen lines one PR takes in a list.
const rowLines = 2

// listRow is one PR in a list: what it is and where it stands on the first
// line, who and which branch on the second.
type listRow struct {
	pr   int
	head line
	sub  line
}

// cursor is where a person is in one list. The selection is the PR number,
// not the position: the rows move as PRs change state, and the selection
// must stay on the PR the person chose.
type cursor struct {
	pr int
	// offset is the first list line on screen.
	offset int
}

// indexOf is the position of the selected PR. When that PR has left the
// list, the selection goes to the first row.
func indexOf(rows []listRow, pr int) int {
	for i, row := range rows {
		if row.pr == pr {
			return i
		}
	}
	return 0
}

// moved is the cursor after the selection moves by delta rows, stopping at
// both ends.
func moved(c cursor, rows []listRow, delta, height int) cursor {
	return placed(c, rows, indexOf(rows, c.pr)+delta, height)
}

// placed is the cursor on the row at index, clamped to the list, scrolled
// by the least that keeps both lines of that row on screen.
func placed(c cursor, rows []listRow, index, height int) cursor {
	if len(rows) == 0 {
		return cursor{}
	}
	index = min(max(index, 0), len(rows)-1)
	return cursor{pr: rows[index].pr, offset: scrollTo(c.offset, index, len(rows), height)}
}

// scrollTo is the offset that shows the row at index, moved as little as
// possible from the current one.
func scrollTo(offset, index, rows, height int) int {
	top := index * rowLines
	bottom := top + rowLines - 1
	// A pane too short for one whole row shows the line that names the PR.
	if height < rowLines {
		return top
	}
	if top < offset {
		offset = top
	}
	if bottom >= offset+height {
		offset = bottom - height + 1
	}
	last := max(rows*rowLines-height, 0)
	return min(max(offset, 0), last)
}

// rowAt is the row under a click on list line y, where y is 0 at the top
// of the list pane. A click below the last row selects nothing.
func rowAt(y, offset, rows int) (int, bool) {
	index := (y + offset) / rowLines
	if y < 0 || index >= rows {
		return 0, false
	}
	return index, true
}

// listLines draws the list into exactly height lines of exactly width
// columns. Both lines of the selected row are reversed across the pane.
func listLines(rows []listRow, c cursor, width, height int) []string {
	selected := indexOf(rows, c.pr)
	out := make([]string, 0, height)
	for at := c.offset; len(out) < height; at++ {
		index := at / rowLines
		if index >= len(rows) {
			out = append(out, fit(line{}, width).render(false))
			continue
		}
		row := rows[index]
		content := row.head
		if at%rowLines == 1 {
			content = row.sub
		}
		out = append(out, fit(content, width).render(index == selected))
	}
	return out
}
