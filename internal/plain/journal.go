package plain

import (
	"fmt"
	"strings"

	"github.com/venabots/autoreview/internal/engine"
	"github.com/venabots/autoreview/internal/report"
	"github.com/venabots/autoreview/internal/review"
	"github.com/venabots/autoreview/internal/sanitize"
	"github.com/venabots/autoreview/internal/words"
)

// indent lines a detail up under the eight-column label that opens every
// result line.
const indent = "        "

// text is what one event says: the report on one stream, and what a person
// should notice on the other. A run from cron keeps the report and mails
// the rest.
type text struct {
	out  string
	errs string
}

// say turns one event into lines.
func say(ev engine.Event) text {
	switch ev := ev.(type) {
	case engine.Looked:
		return text{out: listsText(ev.Snapshot), errs: failures(ev.Snapshot)}
	case engine.NothingToReview:
		return text{out: "no PR is ready for review\n"}
	case engine.PassStarted:
		return text{out: passHeadline(ev) + "\nlogs: " + ev.Dir + "\n\n"}
	case engine.JobStarted:
		return text{out: startLine(ev.Job) + "\n"}
	case engine.JobFinished:
		return text{out: strings.Join(resultLines(ev.Job), "\n") + "\n"}
	case engine.Noted:
		return text{errs: "note: " + sanitize.Line(ev.Text) + "\n"}
	case engine.Interrupted:
		return text{errs: "\ninterrupted; stopping running reviews\n"}
	}
	return text{}
}

// passHeadline names the limit only when it holds reviews back. "1 PR, 2
// at a time" describes nothing.
func passHeadline(ev engine.PassStarted) string {
	headline := "reviewing " + words.Count(ev.Reviews, "PR")
	if ev.Limit < ev.Reviews {
		headline += fmt.Sprintf(", %d at a time", ev.Limit)
	}
	return headline
}

func startLine(job engine.Job) string {
	verb := "reviewing"
	if job.Resumed {
		verb = "rechecking"
	}
	who := ""
	if job.Author != "" {
		who = " @" + sanitize.Line(job.Author)
	}
	return fmt.Sprintf("start   #%d%s (%s)", job.PR, who, verb)
}

// resultLines is the line for a finished review, and under a review that
// did not approve, why.
func resultLines(job engine.Job) []string {
	took := words.Duration(job.Elapsed)
	switch job.State {
	case engine.JobTimedOut:
		return []string{fmt.Sprintf("TIMEOUT #%d (%s)", job.PR, took)}
	case engine.JobDone:
		return append([]string{fmt.Sprintf("done    #%d (%s)", job.PR, took)}, whyLines(job)...)
	}
	line := fmt.Sprintf("FAILED  #%d (%s, %s)", job.PR, job.Outcome, took)
	if job.Error != "" {
		line += ": " + job.Error
	}
	return []string{line}
}

func whyLines(job engine.Job) []string {
	lines := []string{}
	if short, ok := review.CleanButShort(job.Verdict, job.Trailer); ok {
		lines = append(lines, indent+short.Line())
	}
	reasons := review.Reasons(job.Verdict, job.Trailer)
	if len(reasons) == 0 {
		return lines
	}
	lines = append(lines, indent+review.ReasonsHeader)
	for _, reason := range reasons {
		lines = append(lines, indent+reason)
	}
	return lines
}

// Final is what a run that reviewed says at its end: the summary, where
// the files are, and how to open a review again. A run that reviewed
// nothing says nothing more.
func Final(result engine.Result, noPost bool) (out, errs string) {
	jobs := result.Final.Reviewed()
	if len(jobs) == 0 {
		return "", ""
	}
	where := filesOf(result.Final)
	lines := []string{"", strings.TrimSuffix(report.Summary(jobs), "\n"), "", "logs: " + where}
	lines = append(lines, report.ReopenHints(jobs)...)
	if noPost {
		lines = append(lines, "nothing was posted to any PR; the reviews are in "+where)
	}
	failed := 0
	for _, job := range jobs {
		if job.State != engine.JobDone {
			failed++
		}
	}
	if failed > 0 {
		errs = fmt.Sprintf("error: %d of %s failed\n", failed, words.Count(len(jobs), "review"))
	}
	return strings.Join(lines, "\n") + "\n", errs
}

// filesOf is where the run's reviews wrote their files: the directory of
// the pass when there was one pass, and the run's own directory when there
// were several.
func filesOf(s *engine.Snapshot) string {
	jobs := s.Reviewed()
	for _, job := range jobs {
		if job.PassDir != jobs[0].PassDir {
			return s.RunDir
		}
	}
	return jobs[0].PassDir
}
