package tui

import (
	"fmt"
	"strings"
	"time"

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

// tabLine shows both tabs with how many PRs each holds. The one on screen
// is reversed.
func tabLine(shown tab, reviews, mine int) line {
	return line{
		tabLabel("Review", reviews, shown == tabReview),
		text("  "),
		tabLabel("My PRs", mine, shown == tabMine),
		styled("   tab switches", dimStyle),
	}
}

func tabLabel(name string, n int, shown bool) span {
	label := fmt.Sprintf(" %s %d ", name, n)
	if shown {
		return styled(label, activeTab)
	}
	return styled(label, dimStyle)
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
