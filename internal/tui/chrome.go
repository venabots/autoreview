package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/venabots/autoreview/internal/engine"
	"github.com/venabots/autoreview/internal/sanitize"
	"github.com/venabots/autoreview/internal/words"
)

// headerLine names the run: the program, the repo, what the run does, and
// what the reviewers are told to look at.
func headerLine(s *engine.Snapshot) line {
	l := line{
		styled("autoreview", brandStyle),
		styled(" · ", dimStyle),
		styled(sanitize.Line(s.Repo.Slug()), boldStyle),
		styled(" · ", dimStyle),
		text(modeText(s)),
	}
	if s.Focus != "" {
		l = append(l, styled(" · ", dimStyle), styled("focus: "+sanitize.Line(s.Focus), yellowStyle))
	}
	return l
}

// modeText says what the run does by itself. A run with no directory of
// its own only reads the lists.
func modeText(s *engine.Snapshot) string {
	what := "one pass"
	if s.RunDir == "" {
		what = "read-only"
	}
	if s.Watch == 0 {
		return what
	}
	return what + " · lists every " + words.Interval(s.Watch)
}

// tabGap is the space between the two tab labels.
const tabGap = "  "

// tabLabels names both tabs, each with how many PRs it holds. A list that
// has not been read has no count: "0" would say there are none.
func tabLabels(s *engine.Snapshot) (review, mine string) {
	return tabLabel("Review", len(s.Reviews), s.Looked), tabLabel("My PRs", len(s.Mine), s.MineLooked)
}

func tabLabel(name string, n int, counted bool) string {
	if !counted {
		return " " + name + " "
	}
	return fmt.Sprintf(" %s %d ", name, n)
}

// tabLine draws both tabs. The one on screen is reversed.
func tabLine(shown tab, review, mine string) line {
	return line{tabSpan(review, shown == tabReview), text(tabGap), tabSpan(mine, shown == tabMine)}
}

func tabSpan(label string, shown bool) span {
	if shown {
		return styled(label, activeTab)
	}
	return styled(label, dimStyle)
}

// tabAt is the tab under column x of the tab bar. It measures the same
// labels tabLine draws, so a click lands on the tab a person sees whatever
// the counts are.
func tabAt(x int, review, mine string) (tab, bool) {
	reviewEnd := ansi.StringWidth(review)
	mineStart := reviewEnd + len(tabGap)
	switch {
	case x >= 0 && x < reviewEnd:
		return tabReview, true
	case x >= mineStart && x < mineStart+ansi.StringWidth(mine):
		return tabMine, true
	}
	return tabReview, false
}

// statusText is the left side of the footer: what the run holds, and what
// it is doing now. spin is the spinner's frame, shown while the run looks.
func statusText(s *engine.Snapshot, rows []reviewRow, now time.Time, spin string) word {
	if s.ListErr != "" {
		return word{"could not read the PR list: " + sanitize.Line(s.ListErr), redStyle}
	}
	if !s.Looked {
		return word{spin + " reading the PR list", dimStyle}
	}
	parts := []string{}
	if summary := tallyJobs(rows); summary != "" {
		parts = append(parts, summary)
	} else {
		parts = append(parts, "nothing to review")
	}
	switch {
	case s.Looking:
		parts = append(parts, spin+" checking the PR list")
	case !s.NextLookAt.IsZero():
		parts = append(parts, "next look in "+words.Duration(s.NextLookAt.Sub(now)))
	}
	return word{joinDots(parts), dimStyle}
}

// hintGap is the least space between the status and the hints.
const hintGap = 2

// footerLine puts the status on the left and the key hints on the right.
// It shows as many hints as fit beside the status, in order. The last hint
// is always shown: on a screen too narrow for both, the status is cut.
func footerLine(status word, hints []hint, width int) line {
	left := line{styled(status.text, status.style)}
	right := hintLine(fittingHints(hints, width-left.width()-hintGap))
	room := max(width-right.width()-hintGap, 0)
	return append(append(fit(left, room), text(strings.Repeat(" ", hintGap))), right...)
}

// fittingHints keeps the leading hints that fit in room columns along with
// the keys hint, which is kept whatever the room.
func fittingHints(hints []hint, room int) []hint {
	shown := []hint{}
	for _, h := range hints {
		candidate := append(append([]hint{}, shown...), h, keysHint)
		if hintLine(candidate).width() > room {
			break
		}
		shown = append(shown, h)
	}
	return append(shown, keysHint)
}

func hintLine(hints []hint) line {
	out := line{}
	for i, h := range hints {
		if i > 0 {
			out = append(out, text("  "))
		}
		out = append(out, styled(h.keys, cyanStyle), styled(" "+h.what, dimStyle))
	}
	return out
}

// keysPane is the key help, drawn in the right pane.
func keysPane() []line {
	lines := []line{{styled("KEYS", headingStyle)}, {}}
	for _, k := range helpLines {
		lines = append(lines, line{
			styled(fmt.Sprintf("%-6s", k.keys), numberStyle),
			text("  "),
			text(k.what),
		})
	}
	return lines
}
