// Package prs lists a repository's open PRs and decides which ones need a
// review. Every front-end reads this one list, so they cannot disagree about
// what is actionable.
package prs

import (
	"context"
	"slices"
	"strings"

	"github.com/venabots/autoreview/internal/gh"
)

// Options is whose sweep this is and how wide it looks.
type Options struct {
	// Me is the login running the sweep. Engagement is measured against it,
	// and its own PRs are hidden.
	Me string
	// All keeps approved PRs in the sweep.
	All bool
	// Dependabot keeps bot PRs in the sweep.
	Dependabot bool
	// Authors keeps only the PRs these logins opened. Empty keeps every
	// author.
	Authors []string
}

// Listing is what one fetch found.
type Listing struct {
	// DefaultBranch is the repository's trunk, and empty when GitHub did not
	// name one.
	DefaultBranch string
	// PRs is every open PR GitHub returned, most actionable first. A PR the
	// sweep leaves out is still here, with the reason in its Hidden field.
	PRs []PR
	// Truncated says the query came back full, so there are more PRs than
	// this saw. It is taken from the raw count, before anything is hidden: a
	// full page with two drafts in it would otherwise report 48 as a fact.
	Truncated bool
}

// Reviewable is the PRs the sweep may act on, in the same order.
func (l Listing) Reviewable() []PR {
	kept := make([]PR, 0, len(l.PRs))
	for _, p := range l.PRs {
		if p.Hidden == NotHidden {
			kept = append(kept, p)
		}
	}
	return kept
}

// List fetches the open PRs and classifies them.
func List(ctx context.Context, run gh.Runner, owner, name string, opts Options) (Listing, error) {
	raw, err := Fetch(ctx, run, owner, name)
	if err != nil {
		return Listing{}, err
	}
	return Parse(raw, opts)
}

// classify turns the raw nodes into the listing. It is split from the
// decoding and pure so the tests exercise the real chain: a test that listed
// the same four filters again would agree with itself while the code
// drifted.
//
// The stack is worked out over every open PR, before a single filter runs. A
// branch stacked on a draft, on a bot's PR, on an approved one or on the
// user's own is stacked just the same, and a stack computed over the visible
// half would miss exactly those.
func classify(nodes []node, defaultBranch string, opts Options) Listing {
	stacked := parents(nodes, defaultBranch)
	found := make([]PR, len(nodes))
	for i, n := range nodes {
		found[i] = toPR(n, stacked, opts)
	}
	return Listing{
		DefaultBranch: defaultBranch,
		PRs:           sweepOrder(found),
		Truncated:     len(nodes) >= queryLimit,
	}
}

func toPR(n node, stacked map[int]Parent, opts Options) PR {
	return PR{
		Number:     n.Number,
		Title:      n.Title,
		Author:     displayAuthor(n.Author.Login),
		Branch:     n.HeadRefName,
		Base:       n.BaseRefName,
		Head:       n.HeadRefOid,
		UpdatedAt:  n.UpdatedAt,
		Draft:      n.IsDraft,
		CrossRepo:  n.IsCrossRepository,
		Decision:   decisionFromRaw(n.ReviewDecision),
		CI:         CIFromState(n.rollupState()),
		Engagement: engagement(n, opts.Me),
		Parent:     parentFor(stacked, n.Number),
		Hidden:     hiddenReason(n, opts),
	}
}

// displayAuthor names a deleted account the way GitHub does. Only the name
// shown changes: the own, author and bot filters read the login GitHub
// returned.
func displayAuthor(login string) string {
	if login == "" {
		return "ghost"
	}
	return login
}

func parentFor(stacked map[int]Parent, number int) *Parent {
	parent, ok := stacked[number]
	if !ok {
		return nil
	}
	return &parent
}

// sweepOrder is most actionable first: NEW, then UPDATED, then SEEN, and
// within each the most recently updated first, the ISO timestamps compared as
// strings. The sort is stable, so two PRs updated in the same second keep the
// order GitHub gave them and the list does not change under a repo nobody
// touched.
func sweepOrder(found []PR) []PR {
	ordered := slices.Clone(found)
	slices.SortStableFunc(ordered, func(a, b PR) int {
		if a.Engagement != b.Engagement {
			return int(a.Engagement) - int(b.Engagement)
		}
		return strings.Compare(b.UpdatedAt, a.UpdatedAt)
	})
	return ordered
}
