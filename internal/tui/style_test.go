package tui

import (
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// The other tests strip the escape sequences and compare the text. These
// read the sequences: a row can say the right words in colors nobody can
// read.

var sgr = regexp.MustCompile(`\x1b\[([0-9;]*)m`)

// styles is the parameter list of each SGR sequence that sets a style. A
// sequence with no parameters is a reset and is left out.
func styles(raw string) [][]string {
	out := [][]string{}
	for _, m := range sgr.FindAllStringSubmatch(raw, -1) {
		if m[1] != "" {
			out = append(out, strings.Split(m[1], ";"))
		}
	}
	return out
}

func setsForeground(params []string) bool {
	return slices.ContainsFunc(params, func(p string) bool {
		return p == "38" || (len(p) == 2 && (p[0] == '3' || p[0] == '9') && p[1] >= '0' && p[1] <= '7')
	})
}

// wantSolidBar fails unless every styled run is reversed and none sets a
// color of its own.
func wantSolidBar(t *testing.T, what, raw string) {
	t.Helper()
	found := styles(raw)
	if len(found) == 0 {
		t.Errorf("%s carries no style at all: %q", what, raw)
	}
	for _, params := range found {
		if !slices.Contains(params, "7") {
			t.Errorf("%s has a run that is not reversed (%v): the bar has a gap", what, params)
		}
		if setsForeground(params) {
			t.Errorf("%s has a run with its own color (%v): reversed, that color is a second background", what, params)
		}
	}
}

func row() line {
	return line{
		styled("○", dimStyle), text(" "), styled("#9", numberStyle), styled(" · ", dimStyle),
		styled("conflicts", redStyle), text("    "),
	}
}

func TestASelectedLineIsOneSolidBar(t *testing.T) {
	got := row().render(true)

	wantSolidBar(t, "the selected line", got)
	if ansi.Strip(got) != "○ #9 · conflicts    " {
		t.Errorf("the selected line reads %q", ansi.Strip(got))
	}
	if !strings.Contains(got, "\x1b[1;7m#9") {
		t.Errorf("the PR number lost its bold on the selected line: %q", got)
	}
	if strings.Contains(got, "\x1b[1;7m○") {
		t.Errorf("a span that was not bold became bold on the selected line: %q", got)
	}
}

func TestAnUnselectedLineKeepsEachSpansStyle(t *testing.T) {
	l := row()
	want := ""
	for _, s := range l {
		want += s.style.Render(s.text)
	}
	if got := l.render(false); got != want {
		t.Errorf("render(false) = %q, want each span in its own style: %q", got, want)
	}
}

func TestAnUnselectedLineKeepsAReverseItsStyleAskedFor(t *testing.T) {
	got := line{styled(" Review 4 ", activeTab)}.render(false)
	if got != "\x1b[1;7m Review 4 \x1b[m" {
		t.Errorf("the active tab is drawn as %q, want bold and reversed", got)
	}
}

// listPart is the list pane's share of one raw frame row, with its styles:
// everything before the rule that divides the two panes.
func listPart(m Model, row int) string {
	raw := strings.Split(m.View().Content, "\n")[row]
	left, _, _ := strings.Cut(raw, dimStyle.Render("│"))
	return left
}

func TestTheSelectedRowIsOneSolidBarInTheFrame(t *testing.T) {
	m, _ := newModel(t, 120, 30)
	for _, tab := range []string{"review", "maintain"} {
		wantSolidBar(t, "the first line of the selected row on "+tab, listPart(m, 2))
		wantSolidBar(t, "the second line of the selected row on "+tab, listPart(m, 3))

		other := listPart(m, 4)
		if slices.ContainsFunc(styles(other), func(p []string) bool { return slices.Contains(p, "7") }) {
			t.Errorf("a row that is not selected is reversed on %s: %q", tab, other)
		}
		if !slices.ContainsFunc(styles(other), setsForeground) {
			t.Errorf("a row that is not selected lost its colors on %s: %q", tab, other)
		}
		m = press(t, m, "tab")
	}
}

func TestTheBarFollowsTheSelection(t *testing.T) {
	m, _ := newModel(t, 120, 30)
	m = press(t, m, "j")
	wantSolidBar(t, "the row moved to", listPart(m, 4))
	if slices.ContainsFunc(styles(listPart(m, 2)), func(p []string) bool { return slices.Contains(p, "7") }) {
		t.Error("the row moved from is still reversed")
	}
}

func TestTheShownTabIsHighlighted(t *testing.T) {
	m, _ := newModel(t, 120, 30)
	bar := func(m Model) string { return strings.Split(m.View().Content, "\n")[1] }

	if got := bar(m); !strings.Contains(got, "\x1b[1;7m Review 4 \x1b[m") || !strings.Contains(got, "\x1b[90m Maintain 2 \x1b[m") {
		t.Errorf("the tab bar is %q, want Review reversed and Maintain dim", got)
	}
	if got := bar(press(t, m, "tab")); !strings.Contains(got, "\x1b[1;7m Maintain 2 \x1b[m") || !strings.Contains(got, "\x1b[90m Review 4 \x1b[m") {
		t.Errorf("after tab the bar is %q, want Maintain reversed and Review dim", got)
	}
}
