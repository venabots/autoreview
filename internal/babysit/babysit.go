// Package babysit decides when one of your babysat PRs gets another fix.
//
// A fix runs the babysit-pr skill once. Babysitting decides, at each look at
// your PRs, which babysat PR gets another one. Three rules keep that from
// becoming a loop that spends for nothing, or that undoes a reviewer's work:
//
//   - A fix only follows a change. A PR that needs work and has not changed
//     since its last fix is left alone: a second fix of the same state would
//     find what the first one could not fix. The push a fix makes is not a
//     change: the first look after the fix whose checks and merge state are
//     known takes the PR's new state as the one to compare with. A build or
//     a conflict the fix itself caused is the exception, because that is
//     work.
//   - An approved PR gets only a fix of its conflicts. A fix pushes, and a
//     push can dismiss the approval. A PR with conflicts cannot merge, so
//     that push loses nothing. Anything else open on an approved PR is for
//     a person to decide.
//   - A cap. At most MaxStreak fixes run in a row while nobody else acts. A
//     new review thread, or a review decision that changed other than a stale
//     approval the push dismissed, is somebody else acting and starts the
//     count again. So does a settled look that finds the PR clean.
//
// A PR with a job running is skipped at a look, and a PR from a fork is never
// babysat: its branch is not on origin to fix.
//
// The package decides and runs nothing. The engine calls Look at each look,
// starts the fix it names on each PR it returns, and calls Finished when a
// task on a PR ends.
package babysit

import (
	"github.com/venabots/autoreview/internal/mine"
	"github.com/venabots/autoreview/internal/prs"
)

// MaxStreak is the most fixes a babysat PR gets in a row while nobody else
// acts on it. Without it, a fix that keeps changing the PR without making it
// clean would run for as long as the run does.
const MaxStreak = 3

// SettleLooks is how many looks after a fix wait for its checks and merge
// state to be known. A check that never finishes, such as one that waits for
// an approval, must not stop babysitting that PR for good.
const SettleLooks = 5

// NeedsWork says a PR has something a fix can work on. A PR that only waits
// for a review or for its checks gets no fix: there is nothing to change.
func NeedsWork(pr mine.PR) bool {
	return pr.Merge == mine.MergeConflicts ||
		pr.CI == prs.CIFailing ||
		pr.Review == mine.ReviewChangesRequested ||
		pr.OpenThreads() > 0
}

// State is which of your PRs are babysat, and what each has had in this run.
// A State is never changed: each method returns a new one. The zero value
// babysits nothing.
type State struct {
	// all babysits every PR of yours, PRs opened later included.
	all     bool
	prs     map[int]bool
	watches map[int]watch
}

// Entry is one babysat PR as the view shows it.
type Entry struct {
	PR     int
	Fixes  int
	Streak int
}

// All says every PR of yours is babysat, PRs opened later included.
func (s State) All() bool {
	return s.all
}

// Babysat says the PR is babysat. A fork can read as babysat while all are
// on, but Look never fixes it and Listed never shows it.
func (s State) Babysat(pr int) bool {
	return s.all || s.prs[pr]
}

// Any says something is babysat. The run must keep looking while it is, or
// no fix would ever come.
func (s State) Any() bool {
	return s.all || len(s.prs) > 0
}

// Fixes is how many fixes babysitting has started on the PR in this run.
func (s State) Fixes(pr int) int {
	return s.watches[pr].fixes
}

// Streak is how many fixes the PR has had in a row while nobody else acted.
// At MaxStreak it gets no more until somebody acts or it comes clean.
func (s State) Streak(pr int) int {
	return s.watches[pr].streak
}

// OnHold says the last look found the PR approved, so until that changes
// it gets only a fix of its conflicts. A push can dismiss an approval, and
// only a conflict stops the merge that the approval allows: a person starts
// any other fix.
func (s State) OnHold(pr int) bool {
	return s.Babysat(pr) && s.watches[pr].approved
}

// Listed is the babysat PRs among list, in its order, for the view. A PR from
// a fork is never fixed, so it is not shown as babysat either.
func (s State) Listed(list []mine.PR) []Entry {
	var out []Entry
	for _, pr := range list {
		if s.Babysat(pr.Number) && !pr.CrossRepo {
			out = append(out, Entry{PR: pr.Number, Fixes: s.Fixes(pr.Number), Streak: s.Streak(pr.Number)})
		}
	}
	return out
}
