// Package rebase asks Dependabot to rebase its own PR instead of reviewing
// one that cannot merge.
//
// A Dependabot PR that conflicts with its base, or whose checks fail after
// its base moved on, is stale: a review of it reads a diff that Dependabot
// is about to replace. Dependabot rebases its PR when asked in a comment,
// so asking costs one comment and gets a fresh PR, which is then new and is
// reviewed as usual.
package rebase

import (
	"github.com/venabots/autoreview/internal/prs"
)

// Comment is what asks Dependabot to rebase.
const Comment = "@dependabot rebase"

// State is where the question stands for one head of one PR.
type State int

const (
	// None means nothing has been asked about this head.
	None State = iota
	// Checking means GitHub is being asked whether to post.
	Checking
	// Asked means this run posted the comment.
	Asked
	// AskedBefore means a comment of yours already asks, about this head.
	AskedBefore
	// WouldAsk means the run would post, but --no-post leaves the PR alone.
	WouldAsk
	// NotBehind means the checks fail on a PR whose base has not moved: a
	// rebase changes nothing, so the PR is held for its checks as before.
	NotBehind
	// OthersCommitted means a person committed to Dependabot's branch.
	// Dependabot will not rebase it, so it needs that person.
	OthersCommitted
	// Failed means GitHub could not be asked.
	Failed
)

func (s State) String() string {
	switch s {
	case Checking:
		return "checking"
	case Asked:
		return "asked"
	case AskedBefore:
		return "asked before"
	case WouldAsk:
		return "would ask"
	case NotBehind:
		return "not behind"
	case OthersCommitted:
		return "others committed"
	case Failed:
		return "failed"
	}
	return "none"
}

// Holds says the PR waits on Dependabot, or on a person, and is not
// reviewed until its head changes. A failed check holds nothing: the PR is
// treated as it would be without this package.
func (s State) Holds() bool {
	switch s {
	case Checking, Asked, AskedBefore, WouldAsk, OthersCommitted:
		return true
	}
	return false
}

// Needs says a PR on the review list may need a rebase: Dependabot's, from
// this repository, with something new on it, and either conflicting or
// with its checks failing. A failing PR still needs Check to say whether
// its base moved on.
func Needs(pr prs.PR) bool {
	return pr.Bot() && !pr.CrossRepo && pr.Actionable() && (pr.Conflicts || pr.CI == prs.CIFailing)
}
