// Package mine reads your own open PRs in a repo and says where each one
// stands: what the reviewers decided, which review threads are still open,
// whether it conflicts with its base, and what its checks say.
//
// The review list hides your PRs on purpose, because the sweep reviews other
// people's work. This is the other half, with its own query: a search by
// author finds every one of your PRs, where the review list's page of the
// 50 most recently updated would drop them in a busy repo.
package mine

import (
	"cmp"
	"slices"

	"github.com/venabots/autoreview/internal/prs"
)

// Review is what the reviewers decided, as GitHub reports it.
type Review int

const (
	// ReviewNone means no rule asks for a review and nobody has decided.
	ReviewNone Review = iota
	// ReviewRequired means the branch rules ask for a review and none has
	// approved yet.
	ReviewRequired
	ReviewApproved
	ReviewChangesRequested
)

// Merge is whether the PR merges into its base.
type Merge int

const (
	// MergeUnknown means GitHub has not worked it out yet. It does so
	// lazily after a push.
	MergeUnknown Merge = iota
	MergeClean
	MergeConflicts
)

// Reviewer is one reviewer's latest review that decided something.
type Reviewer struct {
	Login  string
	Review Review
}

// PR is one of your PRs, as the My PRs tab shows it.
type PR struct {
	Number int
	Title  string
	Draft  bool
	Branch string
	// Head and UpdatedAt are what babysitting compares to tell a PR that
	// changed from one that did not.
	Head      string
	UpdatedAt string
	// CrossRepo marks a PR from a fork. Its branch is not on origin, so no
	// worktree can be made for it here.
	CrossRepo bool
	Review    Review
	Merge     Merge
	CI        prs.CI
	// ThreadIDs are the open review threads. Babysitting tells a new thread
	// from an old one by id: a count goes down when one is resolved and
	// another is opened in the same minute.
	ThreadIDs []string
	Reviewers []Reviewer
}

// OpenThreads is how many review threads nobody has resolved.
func (p PR) OpenThreads() int {
	return len(p.ThreadIDs)
}

// Approved says the reviewers have approved the PR. A repo with no review
// rule reports no decision, so there it is one reviewer who approved and
// none who asks for changes.
//
// What is still open on an approved PR is optional: nothing but a conflict
// or a red build stops its merge.
func (p PR) Approved() bool {
	if p.Review != ReviewNone {
		return p.Review == ReviewApproved
	}
	approvals := 0
	for _, r := range p.Reviewers {
		switch r.Review {
		case ReviewChangesRequested:
			return false
		case ReviewApproved:
			approvals++
		}
	}
	return approvals > 0
}

// Blocker is what most needs doing about one of your PRs.
type Blocker int

// The blockers, in the order they are checked. A conflict and a red build
// stop a merge whatever the reviewers say, and a requested change outranks
// a comment that is merely open. An open thread blocks nothing once the PR
// is approved.
const (
	BlockerConflicts Blocker = iota
	BlockerCIFailing
	BlockerChangesRequested
	BlockerThreads
	BlockerDraft
	BlockerCIPending
	BlockerApproved
	BlockerAwaitingReview
)

// Blocker is the first thing that blocks this PR's merge. Approved and
// awaiting review are the two states with nothing left for you to do.
func (p PR) Blocker() Blocker {
	switch {
	case p.Merge == MergeConflicts:
		return BlockerConflicts
	case p.CI == prs.CIFailing:
		return BlockerCIFailing
	case p.Review == ReviewChangesRequested:
		return BlockerChangesRequested
	case p.OpenThreads() > 0 && !p.Approved():
		return BlockerThreads
	case p.Draft:
		return BlockerDraft
	case p.CI == prs.CIPending:
		return BlockerCIPending
	case p.Approved():
		return BlockerApproved
	}
	return BlockerAwaitingReview
}

// urgency orders the list. It differs from the order the blockers are
// checked in: a draft is checked before pending CI, but a PR that only
// waits for a review is more urgent than one you have not finished.
func urgency(b Blocker) int {
	switch b {
	case BlockerConflicts:
		return 0
	case BlockerCIFailing:
		return 1
	case BlockerChangesRequested:
		return 2
	case BlockerThreads:
		return 3
	case BlockerCIPending:
		return 4
	case BlockerAwaitingReview:
		return 5
	case BlockerDraft:
		return 6
	}
	return 7
}

// Ranked is your PRs with the most urgent first, and the newest first among
// PRs in the same state.
func Ranked(list []PR) []PR {
	ranked := slices.Clone(list)
	slices.SortStableFunc(ranked, func(a, b PR) int {
		return cmp.Or(
			cmp.Compare(urgency(a.Blocker()), urgency(b.Blocker())),
			cmp.Compare(b.Number, a.Number),
		)
	})
	return ranked
}
