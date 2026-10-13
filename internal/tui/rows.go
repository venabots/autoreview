package tui

import (
	"fmt"
	"time"

	"charm.land/lipgloss/v2"

	"github.com/venabots/autoreview/internal/mine"
	"github.com/venabots/autoreview/internal/prs"
	"github.com/venabots/autoreview/internal/queue"
	"github.com/venabots/autoreview/internal/sanitize"
	"github.com/venabots/autoreview/internal/words"
)

// word is a state in a word or two, and the color that says how urgent.
type word struct {
	text  string
	style lipgloss.Style
}

func headLine(icon span, number int, state word) line {
	return line{
		icon,
		text(" "),
		styled(fmt.Sprintf("#%d", number), numberStyle),
		styled(" · ", dimStyle),
		styled(state.text, state.style),
	}
}

// subLine is the second line of a row: who and which branch, or the title
// when GitHub named no branch.
func subLine(what, title string) line {
	if what == "" {
		what = title
	}
	return line{text("  "), styled(sanitize.Line(what), dimStyle)}
}

// decisionIcon is GitHub's own decision on the PR, from the last look. The
// word beside it is what this run makes of the PR, and the two can differ.
func decisionIcon(d prs.Decision) span {
	switch d {
	case prs.Approved:
		return styled("✓", greenStyle)
	case prs.ChangesRequested:
		return styled("✗", yellowStyle)
	}
	return styled("○", dimStyle)
}

func reviewIcon(pr mine.PR) span {
	switch {
	case pr.Approved():
		return styled("✓", greenStyle)
	case pr.Review == mine.ReviewChangesRequested:
		return styled("✗", yellowStyle)
	}
	return styled("○", dimStyle)
}

func waitWord(row queue.Row, now time.Time) word {
	return word{words.Wait(row, now), waitColor(row)}
}

func waitColor(row queue.Row) lipgloss.Style {
	switch row.Wait {
	case queue.WaitChecks:
		if row.PR.CI == prs.CIFailing {
			return redStyle
		}
		return yellowStyle
	case queue.WaitStacked:
		return yellowStyle
	case queue.WaitSeen, queue.WaitResting, queue.WaitCapped:
		return dimStyle
	case queue.WaitApproved:
		return greenStyle
	}
	return plainStyle
}

// blockerWord names what blocks one of your PRs. Red stops a merge
// whatever the reviewers say, and yellow is a reviewer waiting on you.
func blockerWord(pr mine.PR) word {
	return word{words.Blocker(pr), blockerColor(pr.Blocker())}
}

func blockerColor(b mine.Blocker) lipgloss.Style {
	switch b {
	case mine.BlockerConflicts, mine.BlockerCIFailing:
		return redStyle
	case mine.BlockerChangesRequested, mine.BlockerThreads:
		return yellowStyle
	case mine.BlockerApproved:
		return greenStyle
	}
	return dimStyle
}
