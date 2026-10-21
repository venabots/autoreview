package babysit

import (
	"fmt"
	"slices"

	"github.com/venabots/autoreview/internal/mine"
	"github.com/venabots/autoreview/internal/prs"
	"github.com/venabots/autoreview/internal/task"
)

// OnApproved is what autofix still fixes on one of your PRs once it is
// approved. A fix pushes, and on some repos a push dismisses the approval,
// so how much to risk for what is left is the person's to choose. A review
// that approves can still leave polish in its threads, and a person may
// want it done.
type OnApproved int

const (
	// ApprovedConflicts fixes only a conflict: the PR cannot merge with
	// one, so a push that dismisses the approval loses nothing. It is the
	// default.
	ApprovedConflicts OnApproved = iota
	// ApprovedComments also fixes a PR with an open review thread. The fix
	// is the whole fix, so it mends a red build it finds there too; a red
	// build alone stays for a person.
	ApprovedComments
	// ApprovedAll fixes an approved PR as it fixes any other.
	ApprovedAll
)

var onApprovedWords = []string{"conflicts", "comments", "all"}

// ParseOnApproved reads the setting as the settings file writes it.
func ParseOnApproved(raw string) (OnApproved, error) {
	if i := slices.Index(onApprovedWords, raw); i >= 0 {
		return OnApproved(i), nil
	}
	return ApprovedConflicts, fmt.Errorf("expects conflicts, comments or all, got %q", raw)
}

func (o OnApproved) String() string {
	return onApprovedWords[o]
}

// Fixes names what autofix fixes on an approved PR, for a sentence that
// says why it fixes nothing else. It is empty under ApprovedAll, which
// leaves nothing.
func (o OnApproved) Fixes() string {
	switch o {
	case ApprovedConflicts:
		return "conflicts"
	case ApprovedComments:
		return "conflicts and review comments"
	}
	return ""
}

// Rules are the person's limits on autofix. They are an argument to Look
// and not fields of a State, because a change of babysitting makes a new
// State and must not take them back to their defaults.
type Rules struct {
	// Cap is the most fixes a PR gets in a row while nobody else acts.
	Cap int
	// OnApproved is what an approved PR still gets fixed.
	OnApproved OnApproved
}

// fixFor is the task a babysat PR that needs work may get. A PR nobody
// approved gets a full fix. An approved one gets what the setting allows,
// and a conflict always: it cannot merge with one, so a push that
// dismisses the approval loses nothing a person still has.
func fixFor(o OnApproved, approved bool, merge mine.Merge, threads int) (task.Kind, bool) {
	switch {
	case !approved || o == ApprovedAll || (o == ApprovedComments && threads > 0):
		return task.Fix, true
	case merge == mine.MergeConflicts:
		return task.Conflicts, true
	}
	return task.Review, false
}

// HoldsBack says autofix gives the PR less than a full fix: it is
// approved, and the setting does not allow one for what it needs.
func (o OnApproved) HoldsBack(pr mine.PR) bool {
	kind, _ := fixFor(o, pr.Approved(), pr.Merge, pr.OpenThreads())
	return kind != task.Fix
}

// LeftForAPerson says autofix leaves work on the PR for a person: it is
// approved, and something it needs is more than the setting lets autofix
// fix. Without this, turning autofix on for such a PR starts nothing and
// says nothing about why.
func LeftForAPerson(pr mine.PR, o OnApproved) bool {
	if !pr.Approved() || o == ApprovedAll {
		return false
	}
	red := pr.CI == prs.CIFailing || pr.Review == mine.ReviewChangesRequested
	if o == ApprovedComments {
		return red && pr.OpenThreads() == 0
	}
	return red || pr.OpenThreads() > 0
}
