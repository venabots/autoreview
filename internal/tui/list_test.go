package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func rowsNumbered(numbers ...int) []listRow {
	rows := make([]listRow, len(numbers))
	for i, n := range numbers {
		rows[i] = listRow{pr: n, head: line{text("head")}, sub: line{text("sub")}}
	}
	return rows
}

func TestScrollToMovesTheLeastThatShowsTheWholeRow(t *testing.T) {
	cases := []struct {
		name                        string
		offset, index, rows, height int
		want                        int
	}{
		{"a row on screen does not scroll", 0, 1, 10, 6, 0},
		{"a row below scrolls until its second line shows", 0, 3, 10, 6, 2},
		{"a row above scrolls until its first line shows", 6, 1, 10, 6, 2},
		{"the last row stops at the end of the list", 0, 9, 10, 6, 14},
		{"a short list never scrolls", 4, 0, 2, 6, 0},
		{"an offset past the end is pulled back", 30, 9, 10, 6, 14},
		{"an odd height still shows both lines", 0, 3, 10, 5, 3},
		{"a one-line pane shows the line that names the PR", 0, 3, 10, 1, 6},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := scrollTo(c.offset, c.index, c.rows, c.height); got != c.want {
				t.Errorf("scrollTo = %d, want %d", got, c.want)
			}
		})
	}
}

func TestRowAtMapsAClickToARow(t *testing.T) {
	cases := []struct {
		name            string
		y, offset, rows int
		want            int
		ok              bool
	}{
		{"the first line of the first row", 0, 0, 3, 0, true},
		{"the second line of the first row", 1, 0, 3, 0, true},
		{"the second row", 2, 0, 3, 1, true},
		{"a scrolled list", 1, 4, 5, 2, true},
		{"a list scrolled by half a row", 0, 3, 5, 1, true},
		{"below the last row", 6, 0, 3, 0, false},
		{"above the pane", -1, 0, 3, 0, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, ok := rowAt(c.y, c.offset, c.rows)
			if got != c.want || ok != c.ok {
				t.Errorf("rowAt = %d, %v; want %d, %v", got, ok, c.want, c.ok)
			}
		})
	}
}

func TestTheSelectionFollowsThePRNotThePosition(t *testing.T) {
	c := placed(cursor{}, rowsNumbered(9, 8, 6), 1, 10)
	if c.pr != 8 {
		t.Fatalf("placed on row 1 selected #%d, want #8", c.pr)
	}
	reordered := rowsNumbered(6, 9, 8)
	if got := indexOf(reordered, c.pr); got != 2 {
		t.Errorf("after the rows moved the selection is on row %d, want 2", got)
	}
	gone := rowsNumbered(6, 9)
	if got := indexOf(gone, c.pr); got != 0 {
		t.Errorf("after the PR left, the selection is on row %d, want 0", got)
	}
}

func TestMovedStopsAtBothEnds(t *testing.T) {
	rows := rowsNumbered(9, 8, 6)
	top := moved(cursor{pr: 9}, rows, -1, 10)
	if top.pr != 9 {
		t.Errorf("moving up from the first row selected #%d, want #9", top.pr)
	}
	bottom := moved(cursor{pr: 6}, rows, 5, 10)
	if bottom.pr != 6 {
		t.Errorf("moving down from the last row selected #%d, want #6", bottom.pr)
	}
	if empty := moved(cursor{pr: 9, offset: 4}, nil, 1, 10); empty != (cursor{}) {
		t.Errorf("moving in an empty list gave %+v, want the zero cursor", empty)
	}
}

func TestListLinesFillThePaneExactly(t *testing.T) {
	for _, size := range []struct{ rows, width, height int }{{3, 30, 10}, {10, 30, 5}, {0, 30, 4}, {2, 12, 1}, {2, 30, 0}} {
		lines := listLines(rowsNumbered(make([]int, size.rows)...), cursor{}, size.width, size.height)
		if len(lines) != size.height {
			t.Errorf("%d rows in a pane %d high gave %d lines", size.rows, size.height, len(lines))
		}
		for i, l := range lines {
			if w := ansi.StringWidth(l); w != size.width {
				t.Errorf("line %d is %d columns wide, want %d", i, w, size.width)
			}
		}
	}
}

func TestListLinesDrawFromTheOffset(t *testing.T) {
	rows := []listRow{
		{pr: 9, head: line{text("nine")}, sub: line{text("alice")}},
		{pr: 8, head: line{text("eight")}, sub: line{text("bob")}},
	}
	lines := listLines(rows, cursor{pr: 8, offset: 1}, 10, 4)
	got := make([]string, len(lines))
	for i, l := range lines {
		got[i] = strings.TrimRight(ansi.Strip(l), " ")
	}
	if want := "alice|eight|bob|"; strings.Join(got, "|") != want {
		t.Errorf("listLines drew %q, want %q", strings.Join(got, "|"), want)
	}
}
