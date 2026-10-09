// Package plain is the run in plain lines, for a pipe, cron or CI: the
// same facts the full-screen view shows, one line each, with no color and
// no cursor movement.
package plain

import (
	"fmt"
	"io"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/venabots/autoreview/internal/engine"
	"github.com/venabots/autoreview/internal/mine"
	"github.com/venabots/autoreview/internal/queue"
	"github.com/venabots/autoreview/internal/sanitize"
	"github.com/venabots/autoreview/internal/words"
)

// Lists prints both lists as the run last saw them. A list that could not
// be read is named on errs and not printed as empty: "no PRs" and "could
// not look" must never read the same.
func Lists(out, errs io.Writer, s *engine.Snapshot) error {
	if _, err := io.WriteString(out, listsText(s)); err != nil {
		return err
	}
	_, err := io.WriteString(errs, failures(s))
	return err
}

func listsText(s *engine.Snapshot) string {
	var b strings.Builder
	b.WriteString(sanitize.Line(s.Repo.Slug()) + "\n")
	if s.Looked {
		b.WriteString(reviewList(s.Reviews, s.Truncated, s.At))
	}
	if s.MineLooked {
		b.WriteString(mineList(s.Mine))
	}
	return b.String()
}

func failures(s *engine.Snapshot) string {
	var b strings.Builder
	if s.ListErr != "" {
		b.WriteString("error: could not read the PR list: " + sanitize.Line(s.ListErr) + "\n")
	}
	if s.MineErr != "" {
		b.WriteString("note: could not read your PRs: " + sanitize.Line(s.MineErr) + "\n")
	}
	return b.String()
}

func reviewList(rows []queue.Row, truncated bool, at time.Time) string {
	total := words.Count(len(rows), "PR")
	if truncated {
		total = fmt.Sprintf("%d+ PRs", len(rows))
	}
	cells := make([][]string, len(rows))
	for i, row := range rows {
		cells[i] = []string{
			fmt.Sprintf("#%d", row.PR.Number),
			words.Wait(row, at),
			"@" + sanitize.Line(row.PR.Author),
			sanitize.Line(row.PR.Branch),
			sanitize.Line(row.PR.Title),
		}
	}
	return "review list: " + total + "\n" + table(cells)
}

func mineList(list []mine.PR) string {
	cells := make([][]string, len(list))
	for i, pr := range list {
		cells[i] = []string{
			fmt.Sprintf("#%d", pr.Number),
			words.Blocker(pr),
			sanitize.Line(pr.Branch),
			sanitize.Line(pr.Title),
		}
	}
	return "your PRs: " + words.Count(len(list), "PR") + "\n" + table(cells)
}

// table lines the cells up in columns, two spaces in from the margin. The
// width is in bytes and not display columns, so a CJK title can push a
// later column over; the last column is the title, which has none after it.
func table(rows [][]string) string {
	var b strings.Builder
	tw := tabwriter.NewWriter(&b, 0, 0, 2, ' ', 0)
	for _, row := range rows {
		// A write to a strings.Builder cannot fail.
		_, _ = fmt.Fprintln(tw, "  "+strings.Join(row, "\t"))
	}
	_ = tw.Flush()
	return b.String()
}
