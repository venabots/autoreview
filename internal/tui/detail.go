package tui

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"charm.land/lipgloss/v2"

	"github.com/venabots/autoreview/internal/engine"
	"github.com/venabots/autoreview/internal/mine"
	"github.com/venabots/autoreview/internal/prs"
	"github.com/venabots/autoreview/internal/queue"
	"github.com/venabots/autoreview/internal/report"
	"github.com/venabots/autoreview/internal/review"
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

// facts is what the right pane needs beside the row itself.
type facts struct {
	now time.Time
	// nextLook is how long until the run reads the lists again, or zero.
	nextLook time.Duration
	// root is the checkout, where a review is resumed.
	root string
	// resumeCommand writes the command that reopens a review.
	resumeCommand func(dir, reopen string) string
}

// reviewDetail is the right pane for one PR on the Review tab: what the PR
// is, what the run is doing about it now, and what its last review found.
// The review's own text goes under these lines. tail is how a running
// review is followed.
func reviewDetail(row reviewRow, f facts, tail followed) []line {
	lines := []line{
		titleLine(row.pr, row.title()),
		note(byline(row), dimStyle),
		{},
	}
	lines = append(lines, nowLines(row, f)...)
	if row.running() && row.job.State == engine.JobRunning {
		lines = append(lines, activityLines(*row.job, tail, f.now)...)
	}
	if job, ok := row.finished(); ok {
		lines = append(lines, lastReview(job, f)...)
	}
	if row.listed != nil {
		lines = append(lines, line{}, field("review", decisionWord(row.listed.PR.Decision)),
			field("checks", ciWord(row.listed.PR.CI)),
			field("activity", engagementWord(row.listed.PR.Engagement)),
			field("base", word{sanitize.Line(row.listed.PR.Base), plainStyle}))
		if stack := words.Stack(row.listed.PR); stack != "" {
			lines = append(lines, field("stack", word{stack, plainStyle}))
		}
	}
	return lines
}

func byline(row reviewRow) string {
	who := "@" + sanitize.Line(row.author())
	if branch := row.branch(); branch != "" {
		return who + " · " + sanitize.Line(branch)
	}
	return who
}

// nowLines says what the run is doing about the PR at this moment.
func nowLines(row reviewRow, f facts) []line {
	switch {
	case row.running():
		by := row.job.Orchestrator.Label()
		if first := row.job.First; first != nil {
			by += " after " + first.Orchestrator.Label() + " failed"
		}
		return []line{note(report.Progress(*row.job, f.now)+" · "+by, spinnerStyle), note("x x stops it", dimStyle)}
	case row.job != nil && row.job.State == engine.JobQueued && row.job.First != nil:
		return []line{note(fmt.Sprintf("queued · %s failed; waiting for a slot to retry with %s",
			row.job.First.Orchestrator.Label(), row.job.Orchestrator.Label()), yellowStyle)}
	case row.job != nil && row.job.State == engine.JobQueued:
		return []line{note("queued · starts when a review slot frees", dimStyle)}
	case row.job != nil && row.listed != nil && row.listed.Wait != queue.WaitNext:
		return []line{note(waitReason(*row.listed, f), waitStyle(row.listed.Wait)), note("R reviews it again now", dimStyle), {}}
	case row.job != nil:
		return []line{note("R reviews it again now", dimStyle), {}}
	}
	return []line{
		note(waitReason(*row.listed, f), waitStyle(row.listed.Wait)),
		note("R reviews it now", dimStyle),
	}
}

// lastReview is what the newest finished review of the PR found.
func lastReview(job engine.Job, f facts) []line {
	lines := []line{{styled("LAST REVIEW", headingStyle)}, field("result", resultWord(job))}
	if job.Error != "" {
		lines = append(lines, field("error", word{sanitize.Line(job.Error), redStyle}))
	}
	if job.First != nil {
		lines = append(lines, field("fallback", word{sanitize.Line(report.FirstFailure(job.First)) + "; retried with " + job.Orchestrator.Label(), yellowStyle}))
	}
	lines = append(lines,
		field("verdict", word{report.Verdict(job.Verdict), outcomeColor(job)}),
		field("risk", word{orDash(risk(job)), plainStyle}),
		field("findings", word{report.Findings(job.Trailer), plainStyle}),
		field("spent", word{spent(job), plainStyle}),
	)
	if job.Session != "" {
		lines = append(lines,
			field("session", word{job.Session, plainStyle}),
			field("resume", word{"r opens it in a new tab, or run:", dimStyle}),
			note(strings.Repeat(" ", fieldWidth)+f.resumeCommand(f.root, job.Orchestrator.ReopenCommand(job.Session)), plainStyle),
		)
	}
	if job.State != engine.JobDone {
		lines = append(lines, field("log", word{filepath.Join(job.PassDir, fmt.Sprintf("pr-%d.log", job.PR)), dimStyle}))
	}
	return append(lines, whyNot(job)...)
}

func resultWord(job engine.Job) word {
	text := report.Result(job)
	switch job.State {
	case engine.JobFailed:
		return word{text, redStyle}
	case engine.JobTimedOut:
		return word{text, yellowStyle}
	}
	return word{text, plainStyle}
}

func risk(job engine.Job) string {
	if job.Trailer == nil {
		return ""
	}
	return sanitize.Line(job.Trailer.Risk)
}

func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

// spent is the time, the cost and the model of a review, each where it is
// known.
func spent(job engine.Job) string {
	parts := []string{words.Duration(job.Elapsed)}
	if job.Cost != nil {
		parts = append(parts, report.Cost(job.Cost))
	}
	if job.Model != "" {
		parts = append(parts, sanitize.Line(job.Model))
	}
	return joinDots(parts)
}

// whyNot says why a finished review did not approve: too few of the panel
// answered a clean review, or the blockers it named.
func whyNot(job engine.Job) []line {
	lines := []line{}
	if short, ok := job.Shortfall(); ok {
		lines = append(lines, line{}, note(short.Line(), greenStyle),
			note("not approved: the gate needs more of the panel; R reviews it again", dimStyle))
	}
	reasons := review.Reasons(job.Verdict, job.Trailer)
	if len(reasons) == 0 {
		return lines
	}
	lines = append(lines, line{}, note(job.ReasonsHeader(), yellowStyle))
	for _, reason := range reasons {
		lines = append(lines, note(sanitize.Line(reason), plainStyle))
	}
	return lines
}

func joinDots(parts []string) string {
	return strings.Join(parts, " · ")
}

// waitReason says why a PR is not being reviewed, in a sentence.
func waitReason(row queue.Row, f facts) string {
	next := ""
	if f.nextLook > 0 {
		next = "; next look in " + words.Duration(f.nextLook)
	}
	switch row.Wait {
	case queue.WaitChecks:
		return "held until its checks pass (checks " + ciWord(row.PR.CI).text + ")" + next
	case queue.WaitStacked:
		return stackedReason(row.PR.Parent)
	case queue.WaitResting:
		return "resting after its review; it may be reviewed again in " + words.Duration(row.Until.Sub(f.now))
	case queue.WaitCapped:
		return "capped: it has had every review this run gives one PR (--max-passes); a push starts the count again"
	case queue.WaitSeen:
		return "nothing has happened on it since you last engaged"
	case queue.WaitApproved:
		return "approved already; the sweep leaves those alone"
	}
	return "ready for review"
}

// stackedReason is why a PR waits for the one under it: its diff carries
// that PR's commits. A layer of a stack never waits, so it has no reason
// here.
func stackedReason(parent *prs.Parent) string {
	if parent == nil {
		return "held until the PR under it lands"
	}
	return fmt.Sprintf("held until #%d lands (%s also in #%d)", parent.Number, words.Count(parent.Shared, "commit"), parent.Number)
}

func waitStyle(w queue.Wait) lipgloss.Style {
	switch w {
	case queue.WaitChecks, queue.WaitStacked:
		return yellowStyle
	case queue.WaitSeen, queue.WaitApproved, queue.WaitResting, queue.WaitCapped:
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

// threadsWord counts the open threads. Yellow is a reviewer waiting on
// you. On an approved PR nobody waits: the threads are yours to answer or
// to leave.
func threadsWord(pr mine.PR) word {
	open := pr.OpenThreads()
	switch {
	case open == 0:
		return word{"none open", plainStyle}
	case pr.Approved():
		return word{fmt.Sprintf("%d unresolved, optional now: f answers them", open), plainStyle}
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
