package tui

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/venabots/autoreview/internal/engine"
	"github.com/venabots/autoreview/internal/sanitize"
)

// View draws the screen. It asks for the alternate screen and the mouse
// here, per frame: turning the mouse off is a field of the next frame, not
// a call that could be missed on the way out.
func (m Model) View() tea.View {
	v := tea.NewView(m.frame())
	v.AltScreen = true
	v.WindowTitle = "autoreview · " + sanitize.Line(m.snap.Repo.Slug())
	if m.mouse && !m.quitting {
		v.MouseMode = tea.MouseModeCellMotion
	}
	return v
}

// frame is the whole screen as text: one string per row, each exactly as
// wide as the terminal.
func (m Model) frame() string {
	w := m.lay.width
	rows := []string{}
	if m.lay.header.h > 0 {
		rows = append(rows, fit(headerLine(m.snap), w).render(false))
	}
	if m.lay.tabs.h > 0 {
		review, mine := tabLabels(m.snap)
		rows = append(rows, fit(tabLine(m.tab, review, mine), w).render(false))
	}
	rows = append(rows, m.body()...)
	if m.lay.footer.h > 0 {
		rows = append(rows, m.footer())
	}
	if m.modal.kind != modalNone && len(rows) > 0 {
		rows = m.overlaid(rows)
	}
	return strings.Join(rows, "\n")
}

// footer is the last row: the status and the keys, or the focus editor
// while it is open.
func (m Model) footer() string {
	w := m.lay.width
	if !m.editing {
		return fit(footerLine(m.status(), footerHints(m.tab), w), w).render(false)
	}
	const label, help = "focus ", "   enter to apply · esc to leave it"
	// The help is never cut: it is the only place that says how to get
	// out. A long focus scrolls inside the room that is left.
	room := max(w-len(label)-lipgloss.Width(help)-1, 1)
	input := m.input
	input.SetWidth(room)
	left := ansi.Truncate(yellowStyle.Render(label)+input.View(), max(w-lipgloss.Width(help), 0), "")
	gap := max(w-lipgloss.Width(left)-lipgloss.Width(help), 0)
	return ansi.Truncate(left+strings.Repeat(" ", gap)+dimStyle.Render(help), w, "")
}

// body is the list and the right pane: side by side with a rule between
// them on a wide screen, stacked with a rule between them on a narrow one.
func (m Model) body() []string {
	list := m.listPane()
	pane := exactly(highlighted(strings.Split(m.pane.View(), "\n"), m.pane.YOffset(), m.sel), m.lay.detail.h)
	if m.lay.wide {
		rule := dimStyle.Render("│") + " "
		out := make([]string, len(list))
		for i := range list {
			out[i] = list[i] + rule + pane[i]
		}
		return out
	}
	out := list
	if m.lay.body.h > m.lay.list.h {
		out = append(out, dimStyle.Render(strings.Repeat("─", m.lay.width)))
	}
	return append(out, pane...)
}

// listPane is the list on the shown tab. Until that list has been read,
// its first row is a spinner and what it waits for, where the PRs will be.
func (m Model) listPane() []string {
	lines := listLines(m.rows(m.tab), m.cursors[m.tab], m.lay.list.w, m.lay.list.h)
	what := m.reading()
	if what == "" || len(lines) == 0 {
		return lines
	}
	first := line{styled(m.spin.View(), spinnerStyle), text(" " + what)}
	return append([]string{fit(first, m.lay.list.w).render(false)}, lines[1:]...)
}

// reading names the list the shown tab is still waiting for, or is empty
// when that list has arrived or has failed.
func (m Model) reading() string {
	switch {
	case m.tab == tabReview && !m.snap.Looked && m.snap.ListErr == "":
		return "reading the PR list"
	case m.tab == tabMine && !m.snap.MineLooked && m.snap.MineErr == "":
		return "reading your PRs"
	}
	return ""
}

// exactly makes a block n lines tall: cut when it is taller, padded with
// blank lines when it is shorter.
func exactly(lines []string, n int) []string {
	out := make([]string, n)
	copy(out, lines)
	return out
}

func (m Model) status() word {
	if m.flash.text != "" && m.now.Before(m.flash.until) {
		return word{m.flash.text, yellowStyle}
	}
	if m.quitting {
		return word{"stopping…", dimStyle}
	}
	if m.tab == tabMine && m.snap.MineLooked && m.snap.MineErr != "" {
		// The rows are the last good list. Without this, a PR that was
		// merged an hour ago would look as if it still waited on you.
		return word{"could not read your PRs: " + sanitize.Line(m.snap.MineErr) + "; this list is from the last look", redStyle}
	}
	if m.tab == tabMine {
		return statusText(m.snap, mineTally(m.snap), m.now, m.spin.View())
	}
	return statusText(m.snap, reviewTally(m.snap, m.reviews()), m.now, m.spin.View())
}

// paneContent is what the right pane shows, and a key that names it.
func (m Model) paneContent() (string, string) {
	w := m.lay.detail.w
	switch {
	case m.side == sideLog:
		heading := wrapped([]line{{styled("run log · "+logPath(m.snap), headingStyle)}, {}}, w)
		return "log", heading + "\n" + lipgloss.Wrap(m.log.text, max(w, 1), "")
	case m.tab == tabMine:
		key, lines := m.mineContent()
		return key, wrapped(lines, w)
	}
	return m.reviewContent()
}

// logPath is the run's own log, or empty for a run that keeps none.
func logPath(s *engine.Snapshot) string {
	if s.RunDir == "" {
		return ""
	}
	return filepath.Join(s.RunDir, "autoreview.log")
}

func (m Model) reviewContent() (string, string) {
	w := m.lay.detail.w
	row, ok := m.selectedReview()
	if !ok {
		return "review/none", wrapped(emptyPane(m.noReviews()), w)
	}
	f := facts{now: m.now, nextLook: m.untilNextLook(), root: m.snap.Repo.Root, resumeCommand: m.act.ResumeCommand}
	content := wrapped(reviewDetail(row, f, m.tails[row.pr]), w)
	if job, done := row.finished(); done && job.State == engine.JobDone {
		content += "\n\n" + headingStyle.Render("REVIEW") + "\n" + m.reviewBody(job)
	}
	return fmt.Sprintf("review/%d", row.pr), content
}

// reviewBody is the review's own text, or where it would be.
func (m Model) reviewBody(job engine.Job) string {
	path := reviewPath(job)
	if m.text.path != path || !m.text.found {
		return dimStyle.Render("no review text at " + path)
	}
	return m.text.rendered
}

func (m Model) noReviews() string {
	switch {
	case m.snap.ListErr != "":
		return "the PR list could not be read; the next look tries again"
	case !m.snap.Looked:
		return ""
	}
	return "no PRs to show"
}

func (m Model) mineContent() (string, []line) {
	pr, ok := m.selected()
	if !ok {
		return "mine/none", emptyPane(m.noMine())
	}
	for _, row := range mineRowsOf(m.snap) {
		if row.pr.Number == pr {
			return fmt.Sprintf("mine/%d", pr), mineDetail(row, m.snap.Babysit, m.now)
		}
	}
	return "mine/none", nil
}

func (m Model) noMine() string {
	switch {
	case m.snap.MineErr != "":
		return "your PRs could not be read: " + sanitize.Line(m.snap.MineErr)
	case !m.snap.MineLooked:
		return ""
	}
	return "you have no open PRs in this repo"
}

// emptyPane is the right pane when no PR is selected: one line that says
// why, or nothing while the list is still being read. The spinner in the
// list already says that.
func emptyPane(why string) []line {
	if why == "" {
		return nil
	}
	return []line{note(why, dimStyle)}
}

// untilNextLook is how long until the run reads the PR list again, or zero
// when it is reading it now.
func (m Model) untilNextLook() time.Duration {
	if m.snap.NextLookAt.IsZero() {
		return 0
	}
	return max(m.snap.NextLookAt.Sub(m.now), 0)
}
