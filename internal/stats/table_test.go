package stats

import (
	"bytes"
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

func screenReport() report {
	failing := cohort{key: key{"opencode", "glm-5.3"}, runs: 6, answered: 1, failed: 5}
	flaky := cohort{key: key{"opencode", "grok-4.6"}, runs: 20, answered: 14, failed: 6}
	silent := cohort{key: key{"gemini", "unknown"}, runs: 2}
	proven := testCohort()
	proven.answered, proven.failed, proven.runs = 40, 0, 40
	return report{
		overview: overview{
			runs: 12, repos: 2, firstAt: ptr(int64(0)), lastAt: ptr(int64(86_400)),
			decisions: map[string]int{"commented": 8, "approved": 4},
		},
		cohorts:   []cohort{proven, flaky, failing, silent},
		attention: []string{"glm-5.3 on opencode answered 1 of 6 launches"},
		ledger:    "/l/ledger.jsonl",
	}
}

func TestTheScreenShowsTheSameCellsAsThePlainText(t *testing.T) {
	r := screenReport()
	drawn := ansi.Strip(screen(r, stylesFor(true), 0))
	lines := strings.Split(drawn, "\n")
	if lines[0] != headline(r) {
		t.Errorf("the first line is %q, want the headline %q", lines[0], headline(r))
	}
	if !strings.HasPrefix(lines[1], "╭") {
		t.Errorf("the table has no rounded corner: %q", lines[1])
	}
	rows := append([][]string{header}, row(r.cohorts[0]), row(r.cohorts[1]), row(r.cohorts[2]), row(r.cohorts[3]))
	for i, cells := range rows {
		// The header is under the top border, and a rule comes after it.
		line := lines[2+i]
		if i > 0 {
			line = lines[3+i]
		}
		if !strings.HasPrefix(line, "│ ") || !strings.HasSuffix(line, " │") {
			t.Errorf("table line %d has a cell against its border: %q", i, line)
		}
		var got []string
		for cell := range strings.SplitSeq(strings.Trim(line, "│"), "│") {
			got = append(got, strings.TrimSpace(cell))
		}
		if strings.Join(got, "|") != strings.Join(cells, "|") {
			t.Errorf("table line %d has the cells %q, want %q", i, got, cells)
		}
	}
	for _, line := range footer(r) {
		if !strings.Contains(drawn, line+"\n") {
			t.Errorf("the screen has no footer line %q:\n%s", line, drawn)
		}
	}
}

func TestTheScreenColorsWhatNeedsALook(t *testing.T) {
	s := stylesFor(true)
	drawn := screen(screenReport(), s, 0)
	cases := []struct {
		name string
		want string
	}{
		{"the headline is bold", s.bold.Render("12 reviews from 1970-01-01 to 1970-01-02 across 2 repos")},
		{"a model name is bold", s.bold.Render("gpt-5.5")},
		{"full availability is green", s.good.Render("100% (91-100)")},
		{"availability under nine in ten is yellow", s.warn.Render("70% (48-85)")},
		{"availability under a half is red", s.bad.Render("17% (3-56)")},
		{"no availability is dim", s.dim.Render("-")},
		{"a proven sample is green", s.good.Render("proven")},
		{"an emerging sample is yellow", s.warn.Render("emerging")},
		{"a thin sample is dim", s.dim.Render("thin")},
		{"the attention line is yellow", s.warn.Render("needs attention: glm-5.3 on opencode answered 1 of 6 launches")},
		{"the other footer lines are dim", s.dim.Render("ledger: /l/ledger.jsonl")},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if !strings.Contains(c.want, "\x1b[") {
				t.Fatalf("the style under test sends no escape sequence: %q", c.want)
			}
			if !strings.Contains(drawn, c.want) {
				t.Errorf("the screen has no %q:\n%q", c.want, drawn)
			}
		})
	}
}

func TestEachMarkIsItsOwnEscapeSequence(t *testing.T) {
	// The words can be right and the colors unreadable, so the sequences
	// themselves are checked.
	s := stylesFor(true)
	cases := []struct {
		name  string
		style lipgloss.Style
		want  string
	}{
		{"bold", s.bold, "\x1b[1m"},
		{"dim", s.dim, "\x1b[2m"},
		{"green", s.good, "\x1b[32m"},
		{"yellow", s.warn, "\x1b[33m"},
		{"red", s.bad, "\x1b[31m"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := c.style.Render("x"); !strings.HasPrefix(got, c.want+"x") {
				t.Errorf("%s renders as %q, want it to start with %q", c.name, got, c.want)
			}
		})
	}
	if got := s.normal.Render("x"); got != "x" {
		t.Errorf("an unmarked cell renders as %q, want x", got)
	}
}

func TestWithColorOffTheScreenSendsNoEscapeSequence(t *testing.T) {
	r := screenReport()
	drawn := screen(r, stylesFor(false), 0)
	if strings.Contains(drawn, "\x1b") {
		t.Errorf("the screen sends an escape sequence with color off:\n%q", drawn)
	}
	if drawn != ansi.Strip(screen(r, stylesFor(true), 0)) {
		t.Error("the screen with color off does not read the same as the screen with color on")
	}
}

func TestATableWiderThanTheTerminalIsSqueezedToFit(t *testing.T) {
	r := screenReport()
	natural := lipgloss.Width(cohortTable(r.cohorts, stylesFor(false), 0))
	cases := []struct {
		name  string
		width int
		want  int
	}{
		{"an unknown width leaves the table alone", 0, natural},
		{"a wider terminal leaves the table alone", natural + 40, natural},
		{"a terminal of the same width leaves the table alone", natural, natural},
		{"a narrower terminal squeezes it", natural - 20, natural - 20},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := lipgloss.Width(cohortTable(r.cohorts, stylesFor(false), c.width)); got != c.want {
				t.Errorf("the table is %d wide, want %d", got, c.want)
			}
		})
	}
}

func TestAScreenWithNoCohortsHasNoTable(t *testing.T) {
	r := report{overview: overview{runs: 1, firstAt: ptr(int64(0)), lastAt: ptr(int64(0))}, ledger: "/l"}
	want := "1 review on 1970-01-01\nledger: /l\n"
	if got := screen(r, stylesFor(false), 0); got != want {
		t.Errorf("screen = %q, want %q", got, want)
	}
}

func TestAWriterThatIsNotATerminalHasNoWidth(t *testing.T) {
	if got := widthOf(&bytes.Buffer{}); got != 0 {
		t.Errorf("widthOf(a buffer) = %d, want 0", got)
	}
}
