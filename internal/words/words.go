// Package words is the vocabulary a run uses for a PR's state. The view and
// the plain printer both take their words from here, so a state is never
// called one thing on screen and another in a log.
package words

import (
	"fmt"
	"time"

	"github.com/venabots/autoreview/internal/mine"
	"github.com/venabots/autoreview/internal/prs"
	"github.com/venabots/autoreview/internal/queue"
)

// Count reads as English: "1 thread", "2 threads". "1 PR(s)" is the shape
// to avoid.
func Count(n int, singular string) string {
	if n == 1 {
		return fmt.Sprintf("%d %s", n, singular)
	}
	return fmt.Sprintf("%d %ss", n, singular)
}

// Duration is short enough for a list row: "3s", "1m03s", "1h05m".
func Duration(d time.Duration) string {
	s := int(max(d, 0).Seconds())
	switch {
	case s >= 3600:
		return fmt.Sprintf("%dh%02dm", s/3600, (s%3600)/60)
	case s >= 60:
		return fmt.Sprintf("%dm%02ds", s/60, s%60)
	}
	return fmt.Sprintf("%ds", s)
}

// Interval is a wait a person chose, as they would write it: "2m", "1h".
// Duration would say "2m00s", which reads as a countdown.
func Interval(d time.Duration) string {
	switch {
	case d > 0 && d%time.Hour == 0:
		return fmt.Sprintf("%dh", d/time.Hour)
	case d > 0 && d%time.Minute == 0:
		return fmt.Sprintf("%dm", d/time.Minute)
	}
	return Duration(d)
}

// Wait is where a PR on the review list stands, in a word or two. now is
// what a rest is counted down from.
func Wait(row queue.Row, now time.Time) string {
	switch row.Wait {
	case queue.WaitChecks:
		if row.PR.CI == prs.CIFailing {
			return "CI failing"
		}
		return "CI pending"
	case queue.WaitStacked:
		if row.PR.Parent == nil {
			return "stacked"
		}
		return fmt.Sprintf("on #%d", row.PR.Parent.Number)
	case queue.WaitRebase:
		return Rebase(row.Rebase)
	case queue.WaitResting:
		return "rest " + Duration(row.Until.Sub(now))
	case queue.WaitCapped:
		return "capped"
	case queue.WaitSeen:
		return "seen"
	case queue.WaitApproved:
		return "approved"
	}
	return "ready"
}

// Blocker is what blocks one of your PRs, in a word or two.
func Blocker(pr mine.PR) string {
	switch pr.Blocker() {
	case mine.BlockerConflicts:
		return "conflicts"
	case mine.BlockerCIFailing:
		return "CI failing"
	case mine.BlockerChangesRequested:
		return "changes req"
	case mine.BlockerThreads:
		return Count(pr.OpenThreads(), "thread")
	case mine.BlockerDraft:
		return "draft"
	case mine.BlockerCIPending:
		return "CI pending"
	case mine.BlockerApproved:
		if pr.OpenThreads() > 0 {
			return "approved · " + Count(pr.OpenThreads(), "thread") + " open"
		}
		return "approved"
	}
	return "awaiting review"
}

// CI is what a PR's checks say.
func CI(ci prs.CI) string {
	switch ci {
	case prs.CIPassing:
		return "passing"
	case prs.CIFailing:
		return "failing"
	case prs.CIPending:
		return "pending"
	}
	return "no checks"
}

// Decision is what GitHub says the reviewers decided on somebody's PR.
func Decision(d prs.Decision) string {
	switch d {
	case prs.Approved:
		return "approved"
	case prs.ChangesRequested:
		return "changes requested"
	case prs.ReviewRequired:
		return "review required"
	}
	return "no decision yet"
}

// Review is what GitHub says the reviewers decided on one of your PRs.
func Review(r mine.Review) string {
	switch r {
	case mine.ReviewApproved:
		return "approved"
	case mine.ReviewChangesRequested:
		return "changes requested"
	case mine.ReviewRequired:
		return "review required"
	}
	return "no decision yet"
}

// Merge is whether one of your PRs merges into its base.
func Merge(m mine.Merge) string {
	switch m {
	case mine.MergeClean:
		return "merges cleanly"
	case mine.MergeConflicts:
		return "conflicts with its base"
	}
	return "not worked out yet"
}

// Engagement is whether anything on a PR is new to you.
func Engagement(e prs.Engagement) string {
	switch e {
	case prs.New:
		return "new to you"
	case prs.Updated:
		return "changed since you last engaged"
	}
	return "nothing new since you last engaged"
}
