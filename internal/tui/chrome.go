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

// modeText says what the run does by itself: watch for work, or hold
// after its pass. A run with no directory of its own only reads the lists.
func modeText(s *engine.Snapshot) string {
	switch {
	case s.RunDir == "":
		return "read-only"
	case s.Watching && s.Rest > 0:
		return "watching every " + words.Interval(s.Watch) + " · a reviewed PR rests " + words.Interval(s.Rest)
	case s.Watching:
		return "watching every " + words.Interval(s.Watch)
	}
	return "one pass · press w to watch"
}

// tabGap is the space between the two tab labels.
const tabGap = "  "

// tabLabels names both tabs, each with how many PRs it holds. A list that
// has not been read has no count: "0" would say there are none. The Review
// tab names the authors --author keeps.
func tabLabels(s *engine.Snapshot) (review, mine string) {
	mine = tabLabel("Maintain", len(s.Mine), s.MineLooked)
	if n := autofixing(s); n > 0 {
		mine = fmt.Sprintf("%s· %s ", mine, autofixingText(n))
	}
	review = tabLabel("Review", len(s.Reviews), s.Looked)
	if by := words.Authors(s.Authors); by != "" {
		review = fmt.Sprintf("%s· %s ", review, by)
	}
	return review, mine
}

// autofixing is how many of your PRs autofix watches. The tab label and
// the footer both count it, so they cannot disagree.
func autofixing(s *engine.Snapshot) int {
	return len(s.Babysit.Listed(s.Mine))
}

func autofixingText(n int) string {
	return fmt.Sprintf("autofixing %d", n)
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

// tally is what the footer says the shown tab holds. Each tab counts its
// own work: a count of reviews on Maintain reads as if your PRs waited.
type tally struct {
	looked  bool
	reading string
	summary string
}

func reviewTally(s *engine.Snapshot, rows []reviewRow) tally {
	summary := tallyJobs(rows)
	if summary == "" {
		summary = "nothing to review"
	}
	return tally{looked: s.Looked, reading: "reading the PR list", summary: summary}
}

func mineTally(s *engine.Snapshot) tally {
	parts := []string{}
	if tasks := tallyTasks(mineRowsOf(s)); tasks != "" {
		parts = append(parts, tasks)
	}
	if n := autofixing(s); n > 0 {
		parts = append(parts, autofixingText(n))
	}
	if len(parts) == 0 {
		parts = append(parts, "no task running")
	}
	return tally{looked: s.MineLooked, reading: "reading your PRs", summary: joinDots(parts)}
}

// statusText is the left side of the footer: what the shown tab holds, and
// what the run is doing now. spin is the spinner's frame, shown while the
// run looks.
func statusText(s *engine.Snapshot, t tally, now time.Time, spin string) word {
	if s.ListErr != "" {
		return word{"could not read the PR list: " + sanitize.Line(s.ListErr), redStyle}
	}
	if !t.looked {
		return word{spin + " " + t.reading, dimStyle}
	}
	parts := []string{t.summary}
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
