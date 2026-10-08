package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/venabots/autoreview/internal/engine"
	"github.com/venabots/autoreview/internal/queue"
	"github.com/venabots/autoreview/internal/sanitize"
	"github.com/venabots/autoreview/internal/words"
)

// headerLine names the run: the program, the repo, and what the run does.
func headerLine(s *engine.Snapshot) line {
	return line{
		styled("autoreview", brandStyle),
		styled(" · ", dimStyle),
		styled(sanitize.Line(s.Repo.Slug()), boldStyle),
		styled(" · ", dimStyle),
		text(modeText(s)),
	}
}

func modeText(s *engine.Snapshot) string {
	if s.Watch == 0 {
		return "read-only · one look"
	}
	return "read-only · looking every " + words.Interval(s.Watch)
}

// tabGap is the space between the two tab labels.
const tabGap = "  "

// tabLabels names both tabs, each with how many PRs it holds.
func tabLabels(s *engine.Snapshot) (review, mine string) {
	return tabLabel("Review", len(s.Reviews)), tabLabel("My PRs", len(s.Mine))
}

func tabLabel(name string, n int) string {
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
// it is doing now.
func statusText(s *engine.Snapshot, now time.Time) word {
	if s.ListErr != "" {
		return word{"could not read the PR list: " + sanitize.Line(s.ListErr), redStyle}
	}
	if !s.Looked {
		return word{"reading the PR list…", dimStyle}
	}
	parts := []string{}
	if summary := tally(s.Reviews); summary != "" {
		parts = append(parts, summary)
	} else {
		parts = append(parts, "nothing to review yet")
	}
	switch s.Phase {
	case engine.Looking:
		parts = append(parts, "checking the PR list…")
	case engine.Waiting:
		parts = append(parts, "next look in "+words.Duration(s.NextLookAt.Sub(now)))
	}
	return word{strings.Join(parts, " · "), dimStyle}
}

// tally counts the rows by what the run does with them, and names only the
// counts that are not zero.
func tally(rows []queue.Row) string {
	ready, held, left := 0, 0, 0
	for _, row := range rows {
		switch row.Wait {
		case queue.WaitNext:
			ready++
		case queue.WaitChecks, queue.WaitStacked:
			held++
		default:
			left++
		}
	}
	parts := []string{}
	for _, part := range []struct {
		n    int
		what string
	}{{ready, "ready"}, {held, "held"}, {left, "left alone"}} {
		if part.n > 0 {
			parts = append(parts, fmt.Sprintf("%d %s", part.n, part.what))
		}
	}
	return strings.Join(parts, " · ")
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
