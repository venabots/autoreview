package tui

import (
	"cmp"
	"fmt"
	"slices"
	"time"

	"charm.land/lipgloss/v2"

	"github.com/venabots/autoreview/internal/engine"
	"github.com/venabots/autoreview/internal/queue"
	"github.com/venabots/autoreview/internal/report"
	"github.com/venabots/autoreview/internal/review"
)

// reviewRow is one PR on the Review tab: where it stands on GitHub, and
// the newest review this run made of it. One row for each PR, never one for
// each review.
type reviewRow struct {
	pr int
	// listed is the PR as the last look found it, or nil when it has left
	// the list since its review.
	listed *queue.Row
	// job is the newest review of the PR in this run, or nil.
	job *engine.Job
}

// section orders the list: what is happening now first, then what the run
// acts on next, then what it has done, then what it leaves alone.
type section int

const (
	sectionRunning section = iota
	sectionQueued
	sectionNext
	sectionChecks
	sectionStacked
	sectionFinished
	sectionSeen
	sectionApproved
)

func (r reviewRow) section() section {
	if r.job != nil {
		switch {
		case r.job.State == engine.JobQueued:
			return sectionQueued
		case !r.job.State.Settled():
			return sectionRunning
		}
		return sectionFinished
	}
	switch r.listed.Wait {
	case queue.WaitChecks:
		return sectionChecks
	case queue.WaitStacked:
		return sectionStacked
	case queue.WaitSeen:
		return sectionSeen
	case queue.WaitApproved:
		return sectionApproved
	}
	return sectionNext
}

// running says the review's process is alive, or what it left is being
// read.
func (r reviewRow) running() bool {
	return r.job != nil && (r.job.State == engine.JobRunning || r.job.State == engine.JobFinishing)
}

// finished is the review that has ended, if the row has one.
func (r reviewRow) finished() (engine.Job, bool) {
	if r.job == nil || !r.job.State.Settled() {
		return engine.Job{}, false
	}
	return *r.job, true
}

func (r reviewRow) title() string {
	if r.listed != nil {
		return r.listed.PR.Title
	}
	return r.job.Title
}

func (r reviewRow) author() string {
	if r.listed != nil {
		return r.listed.PR.Author
	}
	return r.job.Author
}

func (r reviewRow) branch() string {
	if r.listed != nil {
		return r.listed.PR.Branch
	}
	return ""
}

// reviewRowsOf builds the Review tab from a snapshot: every PR on the
// list, and every PR this run reviewed that has left it since.
func reviewRowsOf(s *engine.Snapshot) []reviewRow {
	rows := []reviewRow{}
	at := map[int]int{}
	add := func(pr int) int {
		if i, ok := at[pr]; ok {
			return i
		}
		at[pr] = len(rows)
		rows = append(rows, reviewRow{pr: pr})
		return len(rows) - 1
	}
	// The newest pass first: its review of a PR is newer than any before.
	for _, jobs := range [][]engine.Job{s.Jobs, s.Earlier} {
		for i := range jobs {
			if n := add(jobs[i].PR); rows[n].job == nil {
				rows[n].job = &jobs[i]
			}
		}
	}
	for i := range s.Reviews {
		rows[add(s.Reviews[i].PR.Number)].listed = &s.Reviews[i]
	}
	slices.SortStableFunc(rows, func(a, b reviewRow) int {
		return cmp.Or(cmp.Compare(a.section(), b.section()), newestFirst(a, b))
	})
	return rows
}

// newestFirst orders the finished reviews by when each ended. Rows of any
// other section keep the order they came in.
func newestFirst(a, b reviewRow) int {
	if a.section() != sectionFinished {
		return 0
	}
	return ended(*b.job).Compare(ended(*a.job))
}

func ended(job engine.Job) time.Time {
	return job.StartedAt.Add(job.Elapsed)
}

// reviewListRows draws the Review tab's rows. spin is the spinner's frame,
// which marks a review that is running.
func reviewListRows(rows []reviewRow, now time.Time, spin string) []listRow {
	out := make([]listRow, len(rows))
	for i, row := range rows {
		what := row.branch()
		if what != "" && row.author() != "" {
			what = "@" + row.author() + " " + what
		}
		out[i] = listRow{
			pr:   row.pr,
			head: headLine(rowIcon(row, spin), row.pr, rowWord(row, now)),
			sub:  subLine(what, row.title()),
		}
	}
	return out
}

// rowIcon is GitHub's own decision on the PR from the last look, or the
// spinner while this run reviews it. The word beside it is what this run
// did, and the two can differ until the next look.
func rowIcon(row reviewRow, spin string) span {
	switch {
	case row.running():
		return styled(spin, spinnerStyle)
	case row.job != nil && row.job.State == engine.JobQueued:
		return styled("·", dimStyle)
	case row.listed != nil:
		return decisionIcon(row.listed.PR.Decision)
	}
	return styled("○", dimStyle)
}

// rowWord is where the row stands, in a word or two and a color.
func rowWord(row reviewRow, now time.Time) word {
	if job, ok := row.finished(); ok {
		return word{report.Outcome(job), outcomeColor(job)}
	}
	if row.job != nil {
		text := report.Progress(*row.job, now)
		if row.job.State == engine.JobQueued {
			return word{text, dimStyle}
		}
		return word{text, spinnerStyle}
	}
	return waitWord(*row.listed)
}

func outcomeColor(job engine.Job) lipgloss.Style {
	switch job.State {
	case engine.JobTimedOut:
		return yellowStyle
	case engine.JobFailed:
		return redStyle
	}
	if _, short := review.CleanButShort(job.Verdict, job.Trailer); short {
		return greenStyle
	}
	switch job.Verdict {
	case "approved":
		return greenStyle
	case "changes requested":
		return yellowStyle
	case "commented":
		return cyanStyle
	case "":
		return dimStyle
	}
	return plainStyle
}

// tallyJobs counts the rows by what the run is doing with them, and names
// only the counts that are not zero.
func tallyJobs(rows []reviewRow) string {
	counts := map[section]int{}
	for _, row := range rows {
		switch s := row.section(); s {
		case sectionRunning, sectionQueued, sectionFinished:
			counts[s]++
		case sectionNext, sectionChecks, sectionStacked:
			counts[sectionNext]++
		}
	}
	parts := []string{}
	for _, part := range []struct {
		s    section
		what string
	}{{sectionRunning, "running"}, {sectionQueued, "queued"}, {sectionNext, "waiting"}, {sectionFinished, "finished"}} {
		if n := counts[part.s]; n > 0 {
			parts = append(parts, fmt.Sprintf("%d %s", n, part.what))
		}
	}
	if len(parts) == 0 {
		return ""
	}
	return joinDots(parts)
}
