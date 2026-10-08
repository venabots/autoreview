package tui

import (
	"fmt"
	"strings"
	"time"

	"charm.land/lipgloss/v2"

	"github.com/venabots/autoreview/internal/mine"
	"github.com/venabots/autoreview/internal/prs"
	"github.com/venabots/autoreview/internal/queue"
	"github.com/venabots/autoreview/internal/sanitize"
	"github.com/venabots/autoreview/internal/words"
)

// fieldWidth is the column the field values start in.
const fieldWidth = 10

func titleLine(number int, title string) line {
	return line{
		styled(fmt.Sprintf("#%d", number), numberStyle),
		text(" "),
		styled(sanitize.Line(title), boldStyle),
	}
}

func field(name string, value word) line {
	return line{
		styled(fmt.Sprintf("%-*s", fieldWidth, name), dimStyle),
		styled(value.text, value.style),
	}
}

func note(s string, style lipgloss.Style) line {
	return line{styled(s, style)}
}

// reviewDetail is the right pane for one PR on the review list.
func reviewDetail(row queue.Row, nextLook time.Duration) []line {
	pr := row.PR
	return []line{
		titleLine(pr.Number, pr.Title),
		note("@"+sanitize.Line(pr.Author)+" · "+sanitize.Line(pr.Branch), dimStyle),
		{},
		note(waitReason(row, nextLook), waitStyle(row.Wait)),
		{},
		field("review", decisionWord(pr.Decision)),
		field("checks", ciWord(pr.CI)),
		field("activity", engagementWord(pr.Engagement)),
		field("base", word{sanitize.Line(pr.Base), plainStyle}),
	}
}

// waitReason says why a PR is not being reviewed, in a sentence.
func waitReason(row queue.Row, nextLook time.Duration) string {
	next := ""
	if nextLook > 0 {
		next = "; next look in " + words.Duration(nextLook)
	}
	switch row.Wait {
	case queue.WaitChecks:
		return "held until its checks pass (checks " + ciWord(row.PR.CI).text + ")" + next
	case queue.WaitStacked:
		return stackedReason(row.PR.Parent)
	case queue.WaitSeen:
		return "nothing has happened on it since you last engaged"
	case queue.WaitApproved:
		return "approved already; the sweep leaves those alone"
	}
	return "ready for review; this build reads the list and starts no review"
}

func stackedReason(parent *prs.Parent) string {
	if parent == nil {
		return "held until the PR under it lands"
	}
	if parent.Evidence == prs.Declared {
		return fmt.Sprintf("held until #%d lands (its base is #%d's branch)", parent.Number, parent.Number)
	}
	return fmt.Sprintf("held until #%d lands (%s also in #%d)", parent.Number, words.Count(parent.Shared, "commit"), parent.Number)
}

func waitStyle(w queue.Wait) lipgloss.Style {
	switch w {
	case queue.WaitChecks, queue.WaitStacked:
		return yellowStyle
	case queue.WaitSeen, queue.WaitApproved:
		return dimStyle
	}
	return plainStyle
}

func decisionWord(d prs.Decision) word {
	text := words.Decision(d)
	switch d {
	case prs.Approved:
		return word{text, greenStyle}
	case prs.ChangesRequested:
		return word{text, yellowStyle}
	case prs.DecisionNone:
		return word{text, dimStyle}
	}
	return word{text, plainStyle}
}

func ciWord(ci prs.CI) word {
	text := words.CI(ci)
	switch ci {
	case prs.CIPassing:
		return word{text, greenStyle}
	case prs.CIFailing:
		return word{text, redStyle}
	case prs.CIPending:
		return word{text, yellowStyle}
	}
	return word{text, plainStyle}
}

func engagementWord(e prs.Engagement) word {
	if e == prs.Seen {
		return word{words.Engagement(e), dimStyle}
	}
	return word{words.Engagement(e), plainStyle}
}

// mineDetail is the right pane for one of your PRs.
func mineDetail(pr mine.PR) []line {
	lines := []line{
		titleLine(pr.Number, pr.Title),
		note(sanitize.Line(pr.Branch), dimStyle),
		{},
		field("review", reviewersWord(pr)),
		field("checks", ciWord(pr.CI)),
		field("threads", threadsWord(pr.OpenThreads())),
		field("merge", mergeWord(pr.Merge)),
	}
	if pr.Draft {
		lines = append(lines, field("draft", word{"yes", plainStyle}))
	}
	if pr.CrossRepo {
		lines = append(lines, field("from", word{"a fork", plainStyle}))
	}
	return lines
}

// reviewersWord names each reviewer who decided something. With none, it
// gives GitHub's overall decision.
func reviewersWord(pr mine.PR) word {
	if len(pr.Reviewers) == 0 {
		return mineReviewWord(pr.Review)
	}
	parts := make([]string, len(pr.Reviewers))
	for i, r := range pr.Reviewers {
		parts[i] = mineReviewWord(r.Review).text + " by @" + sanitize.Line(r.Login)
	}
	return word{strings.Join(parts, ", "), mineReviewWord(pr.Review).style}
}

func mineReviewWord(r mine.Review) word {
	text := words.Review(r)
	switch r {
	case mine.ReviewApproved:
		return word{text, greenStyle}
	case mine.ReviewChangesRequested:
		return word{text, yellowStyle}
	case mine.ReviewNone:
		return word{text, dimStyle}
	}
	return word{text, plainStyle}
}

func threadsWord(open int) word {
	if open == 0 {
		return word{"none open", plainStyle}
	}
	return word{fmt.Sprintf("%d unresolved", open), yellowStyle}
}

func mergeWord(m mine.Merge) word {
	text := words.Merge(m)
	switch m {
	case mine.MergeClean:
		return word{text, plainStyle}
	case mine.MergeConflicts:
		return word{text, redStyle}
	}
	return word{text, dimStyle}
}

// wrapped draws lines for a pane of the given width, breaking a long line
// between words. The detail pane is the one place that wraps.
func wrapped(lines []line, width int) string {
	out := make([]string, len(lines))
	for i, l := range lines {
		out[i] = lipgloss.Wrap(l.render(false), max(width, 1), "")
	}
	return strings.Join(out, "\n")
}
