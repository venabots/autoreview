// Package report says what a review did, in words: the cells of the summary
// table and the lines under it. The plain printer and the view both take
// their text from here, so a result reads the same in a log and on screen.
package report

import (
	"fmt"
	"strings"

	"github.com/venabots/autoreview/internal/engine"
	"github.com/venabots/autoreview/internal/review"
)

// unknown fills a cell the review said nothing for.
const unknown = "-"

// Result is how the review itself went, apart from what it decided.
func Result(job engine.Job) string {
	switch job.State {
	case engine.JobDone:
		return "done"
	case engine.JobTimedOut:
		return "timed out"
	case engine.JobFailed:
		return fmt.Sprintf("failed (%s)", job.Outcome)
	case engine.JobFinishing:
		return "finishing"
	case engine.JobRunning:
		return "running"
	}
	return "queued"
}

// Verdict is what landed on the PR, or the fact that nothing did. A dash
// would read as a verdict of its own, a refusal to approve, when it only
// means that no review was posted.
func Verdict(verdict string) string {
	if verdict == "" {
		return "nothing posted"
	}
	return verdict
}

// Findings names the buckets that are not zero. It says "none" for a clean
// report, and a dash when the review never said.
func Findings(t *review.Trailer) string {
	if t == nil || t.Findings == nil {
		return unknown
	}
	buckets := []struct {
		n    *uint
		word string
	}{{t.Findings.MustFix, "must-fix"}, {t.Findings.ShouldFix, "should-fix"}, {t.Findings.Polish, "polish"}}

	parts := []string{}
	reported := 0
	for _, b := range buckets {
		if b.n == nil {
			continue
		}
		reported++
		if *b.n > 0 {
			parts = append(parts, fmt.Sprintf("%d %s", *b.n, b.word))
		}
	}
	switch {
	case len(parts) > 0:
		return strings.Join(parts, ", ")
	case reported == len(buckets):
		// "none" is a claim about all three buckets. A report that left
		// one out has not made it.
		return "none"
	}
	return unknown
}

// Cost is what a review spent, or a dash when the harness did not say.
func Cost(cost *float64) string {
	if cost == nil || *cost < 0 {
		return unknown
	}
	return fmt.Sprintf("$%.2f", *cost)
}

// orDash is a cell for text that may be missing.
func orDash(s string) string {
	if s == "" {
		return unknown
	}
	return s
}

// Panelist is one panelist in words: "codex (gpt-5.5) 3 findings, top
// MEDIUM".
func Panelist(p review.Panelist) string {
	name, model := p.Name, p.Model
	if name == "" {
		name = "?"
	}
	if model == "" {
		model = "unknown"
	}
	label := fmt.Sprintf("%s (%s)", name, model)
	if p.OK != nil && !*p.OK {
		return label + " failed"
	}
	if p.Findings == nil {
		return label
	}
	switch n := *p.Findings; n {
	case 0:
		return label + " clean"
	case 1:
		label += " 1 finding"
	default:
		label += fmt.Sprintf(" %d findings", n)
	}
	if p.Top != "" {
		label += ", top " + p.Top
	}
	return label
}
