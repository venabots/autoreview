package report

import (
	"fmt"
	"slices"
	"strings"

	"github.com/charmbracelet/x/ansi"

	"github.com/venabots/autoreview/internal/engine"
	"github.com/venabots/autoreview/internal/review"
	"github.com/venabots/autoreview/internal/words"
)

var header = []string{"PR", "RESULT", "VERDICT", "RISK", "FINDINGS", "TIME", "COST", "MODEL", "SESSION"}

// Summary is the report of a run: one aligned table, then the lines that
// explain it. It is the same text on a terminal and in a log.
func Summary(jobs []engine.Job) string {
	rows := [][]string{header}
	for _, job := range jobs {
		rows = append(rows, []string{
			fmt.Sprintf("#%d", job.PR),
			Result(job),
			Verdict(job.Verdict),
			orDash(risk(job.Trailer)),
			Findings(job.Trailer),
			words.Duration(job.Elapsed),
			Cost(job.Cost),
			orDash(job.Model),
			orDash(job.Session),
		})
	}
	lines := []string{align(rows)}
	for _, job := range jobs {
		lines = append(lines, whyLines(job)...)
	}
	for _, job := range jobs {
		if job.Trailer != nil && len(job.Trailer.Panel) > 0 {
			lines = append(lines, fmt.Sprintf("panel #%d: %s", job.PR, panel(job.Trailer.Panel)))
		}
	}
	lines = append(lines, ErrorLines(jobs)...)
	return strings.Join(lines, "\n") + "\n"
}

func risk(t *review.Trailer) string {
	if t == nil {
		return ""
	}
	return t.Risk
}

func panel(panelists []review.Panelist) string {
	labels := make([]string, len(panelists))
	for i, p := range panelists {
		labels[i] = Panelist(p)
	}
	return strings.Join(labels, "; ")
}

// whyLines says why one PR was not approved. The PR number rides the first
// line: the table above holds every PR, not only this one.
//
// A clean review that too few of the panel answered comes first. It is the
// one unapproved PR a reader may only need to look over.
func whyLines(job engine.Job) []string {
	lines := []string{}
	if short, ok := review.CleanButShort(job.Verdict, job.Trailer); ok {
		lines = append(lines, fmt.Sprintf("#%d %s", job.PR, short.Line()))
	}
	reasons := review.Reasons(job.Verdict, job.Trailer)
	if len(reasons) == 0 {
		return lines
	}
	lines = append(lines, fmt.Sprintf("#%d %s", job.PR, review.ReasonsHeader))
	for _, reason := range reasons {
		lines = append(lines, "  "+reason)
	}
	return lines
}

// ErrorLines names the failed reviews and why the harness said they
// failed, one line for each distinct reason. The same reason is grouped: a
// usage limit hits every PR in a pass, and the same notice five times is
// noise.
func ErrorLines(jobs []engine.Job) []string {
	reasons := []string{}
	prs := map[string][]string{}
	for _, job := range jobs {
		if job.State != engine.JobFailed || job.Error == "" {
			continue
		}
		if !slices.Contains(reasons, job.Error) {
			reasons = append(reasons, job.Error)
		}
		prs[job.Error] = append(prs[job.Error], fmt.Sprintf("#%d", job.PR))
	}
	lines := make([]string, len(reasons))
	for i, reason := range reasons {
		lines[i] = fmt.Sprintf("error %s: %s", strings.Join(prs[reason], " "), reason)
	}
	return lines
}

// ReopenHints says how to open the reviews in a summary again, one line
// for each agent that ran one. A session id is printed against the CLI that
// can take it: a codex thread id handed to `claude --resume` opens nothing.
func ReopenHints(jobs []engine.Job) []string {
	seen := []review.Orchestrator{}
	for _, job := range jobs {
		known := slices.ContainsFunc(seen, func(o review.Orchestrator) bool { return o.Backend == job.Orchestrator.Backend })
		if job.Session != "" && !known {
			seen = append(seen, job.Orchestrator)
		}
	}
	if len(seen) == 0 {
		seen = []review.Orchestrator{review.Claude()}
	}
	hints := make([]string, len(seen))
	for i, o := range seen {
		if i == 0 {
			hints[i] = "reopen any review with: " + o.ReopenCommand("")
			continue
		}
		hints[i] = fmt.Sprintf("  or, for a %s review: %s", o.Backend, o.ReopenCommand(""))
	}
	return hints
}

// align pads each column to its widest cell, with two spaces between
// columns and nothing after the last. Widths are display widths, not byte
// counts: the verdict, risk and model cells carry text an agent wrote,
// which may be multibyte.
func align(rows [][]string) string {
	widths := []int{}
	for _, row := range rows {
		for i, cell := range row {
			if i == len(widths) {
				widths = append(widths, 0)
			}
			widths[i] = max(widths[i], ansi.StringWidth(cell))
		}
	}
	lines := make([]string, len(rows))
	for r, row := range rows {
		var b strings.Builder
		for i, cell := range row {
			b.WriteString(cell)
			if i < len(row)-1 {
				b.WriteString(strings.Repeat(" ", widths[i]-ansi.StringWidth(cell)+2))
			}
		}
		lines[r] = b.String()
	}
	return strings.Join(lines, "\n")
}
