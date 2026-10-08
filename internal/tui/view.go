package tui

import (
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

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
		rows = append(rows, fit(footerLine(m.status(), footerHints(m.tab), w), w).render(false))
	}
	return strings.Join(rows, "\n")
}

// body is the list and the right pane: side by side with a rule between
// them on a wide screen, stacked with a rule between them on a narrow one.
func (m Model) body() []string {
	list := m.listPane()
	pane := exactly(strings.Split(m.pane.View(), "\n"), m.lay.detail.h)
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
	return statusText(m.snap, m.now, m.spin.View())
}

// paneContent is what the right pane shows, and a key that names it.
func (m Model) paneContent() (string, []line) {
	if m.side == sideKeys {
		return "keys", keysPane()
	}
	if m.tab == tabMine {
		return m.mineContent()
	}
	return m.reviewContent()
}

func (m Model) reviewContent() (string, []line) {
	pr, ok := m.selected()
	if !ok {
		return "review/none", emptyPane(m.noReviews())
	}
	for _, row := range m.snap.Reviews {
		if row.PR.Number == pr {
			return fmt.Sprintf("review/%d", pr), reviewDetail(row, m.untilNextLook())
		}
	}
	return "review/none", nil
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
	for _, mine := range m.snap.Mine {
		if mine.Number == pr {
			return fmt.Sprintf("mine/%d", pr), mineDetail(mine)
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
	if m.snap.Phase != engine.Waiting {
		return 0
	}
	return max(m.snap.NextLookAt.Sub(m.now), 0)
}
