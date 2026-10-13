// Package queue decides, from one look at the PR list, what a run does about
// each PR and why. It is pure: the same list and the same settings give the
// same answer, so every rule here is tested without a clock or a process.
package queue

import (
	"cmp"
	"slices"
	"time"

	"github.com/venabots/autoreview/internal/prs"
)

// Wait is why a PR is not being reviewed right now.
type Wait int

// The waits, in the order the list shows them: what the run acts on next
// comes first, and what it leaves alone comes last.
const (
	// WaitNext means nothing holds the PR. It is reviewed next.
	WaitNext Wait = iota
	// WaitChecks means its checks are still running, or failed.
	WaitChecks
	// WaitStacked means it sits on another open PR, which is reviewed first.
	WaitStacked
	// WaitResting means the run reviewed it a short while ago, and gives
	// its author time to answer before it looks again.
	WaitResting
	// WaitCapped means it has had every review this run gives one PR.
	WaitCapped
	// WaitSeen means nothing has changed since you last acted on it.
	WaitSeen
	// WaitApproved means GitHub says it is approved.
	WaitApproved
)

// Row is one PR on the review list and where it stands.
type Row struct {
	PR   prs.PR
	Wait Wait
	// Until is when a resting PR may be reviewed again. It is zero for
	// every other wait.
	Until time.Time
}

// Rows is the review list. It shows every PR the sweep could review, and
// the approved ones, so a person can see that an approved PR was read and
// not lost. Drafts, your own PRs and bots are left out.
//
// only limits the list to the PRs a person named. A named PR is reviewed
// whatever its state: the person asked for it, so a gate that would hold it
// in a sweep does not apply. That holds for its first review in the run
// only. Once reviewed has a record of it, the PR is held like any other: a
// run that watches would otherwise review it again after each rest, with
// nothing new on it, and post each time.
func Rows(listing prs.Listing, gates prs.Gates, only []int, reviewed Track) []Row {
	rows := make([]Row, 0, len(listing.PRs))
	for _, pr := range listing.PRs {
		if !shown(pr) || !named(pr, only) {
			continue
		}
		_, done := reviewed[pr.Number]
		rows = append(rows, Row{PR: pr, Wait: waitFor(pr, gates, len(only) > 0 && !done)})
	}
	slices.SortStableFunc(rows, func(a, b Row) int { return cmp.Compare(a.Wait, b.Wait) })
	return rows
}

func shown(pr prs.PR) bool {
	return pr.Hidden == prs.NotHidden || pr.Hidden == prs.HiddenApproved
}

func named(pr prs.PR, only []int) bool {
	return len(only) == 0 || slices.Contains(only, pr.Number)
}

func waitFor(pr prs.PR, gates prs.Gates, asked bool) Wait {
	switch {
	case asked:
		return WaitNext
	case pr.Hidden == prs.HiddenApproved:
		return WaitApproved
	case !pr.Actionable() && pr.Decision == prs.Approved:
		return WaitApproved
	case !pr.Actionable():
		return WaitSeen
	case pr.Stacked(gates):
		return WaitStacked
	case pr.Held(gates):
		return WaitChecks
	}
	return WaitNext
}
