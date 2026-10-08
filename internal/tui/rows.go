package tui

import (
	"fmt"

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

func reviewRows(rows []queue.Row) []listRow {
	out := make([]listRow, len(rows))
	for i, row := range rows {
		out[i] = listRow{
			pr:   row.PR.Number,
			head: headLine(decisionIcon(row.PR.Decision), row.PR.Number, waitWord(row)),
			sub:  subLine(authorAndBranch(row.PR), row.PR.Title),
		}
	}
	return out
}

func mineRows(list []mine.PR) []listRow {
	out := make([]listRow, len(list))
	for i, pr := range list {
		out[i] = listRow{
			pr:   pr.Number,
			head: headLine(reviewIcon(pr.Review), pr.Number, blockerWord(pr)),
			sub:  subLine(pr.Branch, pr.Title),
		}
	}
	return out
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

func authorAndBranch(pr prs.PR) string {
	switch {
	case pr.Branch == "":
		return ""
	case pr.Author == "":
		return pr.Branch
	}
	return "@" + pr.Author + " " + pr.Branch
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

func reviewIcon(r mine.Review) span {
	switch r {
	case mine.ReviewApproved:
		return styled("✓", greenStyle)
	case mine.ReviewChangesRequested:
		return styled("✗", yellowStyle)
	}
	return styled("○", dimStyle)
}

func waitWord(row queue.Row) word {
	return word{words.Wait(row), waitColor(row)}
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
	case queue.WaitSeen:
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
