package prs

// PR is one open pull request, already classified. Everything a view or the
// sweep says about it is here, because the GraphQL answer is the only place
// any of it exists and fetching again to draw a line would be absurd.
type PR struct {
	Number int
	Title  string
	// Author is the login that opened the PR, and "ghost" when the account
	// is gone, which is the name GitHub itself shows for it.
	Author string
	// Branch is the PR's own branch, and Base the branch it merges into.
	Branch string
	Base   string
	// Head is the SHA at the tip of the PR branch: the fingerprint that tells
	// a push from a comment. Empty when GitHub did not return one, which
	// reads as "no push anyone can prove".
	//
	// A SHA rather than the newest commit date, because a date is not unique:
	// a force-push that keeps committer dates, or two pushes inside the same
	// second, would read as no push at all.
	Head string
	// HeadDate is when the newest commit was made. A comment older than it
	// was written about an older head.
	HeadDate  string
	UpdatedAt string
	Draft     bool
	// CrossRepo says the head branch lives in a fork.
	CrossRepo bool
	// Conflicts says GitHub found the PR conflicts with its base.
	Conflicts bool
	// OthersCommitted says a person other than the author made a commit on
	// the branch. Dependabot stops rebasing a branch somebody else changed.
	OthersCommitted bool
	Decision        Decision
	CI              CI
	Engagement      Engagement
	// Parent is the open PR this one sits on top of, and nil when it sits on
	// none. It is worked out over every open PR, hidden ones included.
	Parent *Parent
	// Hidden is why the sweep leaves this PR out. Ready, Held and Stacked do
	// not read it: a caller ranges over Listing.Reviewable for the PRs the
	// sweep may act on.
	Hidden Hidden
}

// Gates is what the sweep consults before it reviews a PR. They are passed
// together so that a caller cannot answer one question while forgetting the
// other.
type Gates struct {
	// CI holds a PR whose checks have not passed.
	CI bool
	// Stack holds a PR sitting on top of another open PR.
	Stack bool
}

// Actionable says the PR is NEW or UPDATED, so something happened that the
// sweep has not answered, or UNDECIDED, so the sweep's last answer gave no
// verdict for a reason that can pass.
func (p PR) Actionable() bool {
	return p.Engagement == New || p.Engagement == Updated || p.Engagement == Undecided
}

// Ready is what the sweep reviews now: actionable, not sitting on another
// open PR, and, when the checks gate it, with its checks passing.
func (p PR) Ready(g Gates) bool {
	return !p.Stacked(g) && p.Actionable() && (!g.CI || p.CI.ready())
}

// Held is actionable, but the checks say not yet.
//
// A PR waiting on the PR underneath it is deliberately not here. Checks
// settle in minutes, so a loop waits for them; a PR merges when someone
// merges it, so a loop that treated the two the same would sit waiting on a
// person. Stacked names those separately.
func (p PR) Held(g Gates) bool {
	return !p.Stacked(g) && p.Actionable() && !p.Ready(g)
}

// Stacked says the PR is being left alone for the PR underneath it, when the
// sweep is gating on that at all. It is only ever said about a PR that would
// otherwise have been reviewed: a SEEN PR is not being held by anything.
func (p PR) Stacked(g Gates) bool {
	return p.Parent != nil && g.Stack && p.Actionable()
}

// Bot says a bot opened the PR. A view dims those rows, and with
// Options.Dependabot set nothing in Hidden would tell it which they are.
func (p PR) Bot() bool {
	return isBot(p.Author)
}
