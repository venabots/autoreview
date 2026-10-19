package prs

import "slices"

// Which open PRs sit on top of other open PRs.
//
// Three shapes, and each needs its own test. Only the last one holds a PR.
//
// A GitHub stack. GitHub names the stack and each PR's layer in it, so that
// is the answer, with nothing worked out (native.go).
//
// A declared stack. The PR's base is another open PR's branch, which is what
// every stacking tool produces. GitHub diffs it from that branch, and the
// review diffs it from that branch too, so its diff is its own layer and no
// other. It is reviewed at once: holding it until the PR below it merges
// would make each layer of a deep stack wait for every layer under it, which
// is the wait stacking exists to remove. It is found by the branch names, and
// shown so that a reader knows the layer below it.
//
// An undeclared stack. The branch was cut from another open PR's branch while
// it was in flight, and still says "base: main". GitHub then serves the diff
// from where the two branches parted, so that PR's commits are inside this
// one's diff and reviewing both reads the same code twice. Nothing in the
// base branch shows this. What shows it is the commits, which the PR list
// already fetches. This one is held until the PR below it lands.
//
// Every relation names one PR directly underneath, and rests on evidence
// about those two PRs alone. Relatedness is never passed along a chain: a PR
// that shares no work with the one below it would wait for a merge that
// changes nothing about it.

// Evidence is how a PR was found to sit on another one, strongest first.
type Evidence int

const (
	// Declared is a PR whose base is the other PR's branch: stacked on it by
	// design.
	Declared Evidence = iota
	// CarriesTip is a PR whose diff holds the other PR's tip commit, so it is
	// built on that PR's work.
	CarriesTip
	// Overlap is two diffs that share commits where neither holds the other's
	// tip: two branches cut from the same unmerged commit.
	Overlap
	// Native is a layer of a GitHub stack, on the layer below it.
	Native
)

func (e Evidence) String() string {
	switch e {
	case Declared:
		return "declared"
	case CarriesTip:
		return "carries tip"
	case Native:
		return "GitHub stack"
	default:
		return "overlap"
	}
}

// Parent is the open PR another PR is being left alone for, and why.
type Parent struct {
	Number   int
	Evidence Evidence
	// Shared is how many commits both diffs serve. It is zero for a declared
	// or GitHub stack, whose diffs GitHub already keeps apart, and at least
	// one otherwise: both commit tests need a commit in common.
	Shared int
}

// Holds says the PR waits for this parent to land. Only shared commits hold
// one: a layer's diff is its own, but a diff that carries another open PR's
// commits would have them reviewed twice.
func (p Parent) Holds() bool {
	return p.Evidence == CarriesTip || p.Evidence == Overlap
}

// The index that stands for "sits on no other PR".
const none = -1

// One PR reduced to what the three tests need.
type branch struct {
	number int
	// The branch this PR merges into. Named in the base repository, so it is
	// only comparable with the head branch of a PR from that same repository.
	baseRef string
	// The branch this PR is, and empty when it lives in a fork: a fork's
	// branch name means nothing to another PR's base, and two forks both
	// called `main` would otherwise read as a stack.
	headRef string
	// The tip. A PR whose tip is a commit inside another PR is the one
	// underneath it, which is what makes the relation directional.
	head string
	// The commits GitHub counts as this PR's own: base..head, so anything
	// already in the base branch is not in here. Two PRs sharing one is two
	// PRs whose diffs overlap.
	commits map[string]bool
}

// parents is every PR sitting on top of another open PR, and which PR that
// is. A layer of a GitHub stack takes GitHub's answer and no other: its
// branches and commits are not read for it.
//
// Every open PR counts, including drafts, approved ones, bots and the user's
// own. A PR is held by the shape of the branches, not by whether this tool
// would have reviewed the PR underneath it: a colleague's branch cut from
// your own unmerged work carries your commits in its diff whether or not
// anyone reviews yours.
//
// defaultBranch is the repository's own trunk, which is never a stack: see
// basedOn. Empty reads as "no branch is exempt", which only costs a hold that
// the second guard usually catches anyway.
func parents(nodes []node, defaultBranch string) map[int]Parent {
	branches := make([]branch, len(nodes))
	for i, n := range nodes {
		branches[i] = branchOf(n)
	}
	on := make([]int, len(branches))
	for i := range branches {
		on[i] = none
		if layerOf(nodes[i]) == nil {
			on[i] = below(branches, defaultBranch, i)
		}
	}
	found := nativeParents(nodes)
	for i, j := range breakCircles(branches, on) {
		if j != none {
			found[branches[i].number] = parentOf(branches, defaultBranch, i, j)
		}
	}
	return found
}

func branchOf(n node) branch {
	return branch{
		number:  n.Number,
		baseRef: n.BaseRefName,
		headRef: sameRepoHead(n),
		head:    n.HeadRefOid,
		commits: commitSet(n),
	}
}

func sameRepoHead(n node) string {
	if n.IsCrossRepository {
		return ""
	}
	return n.HeadRefName
}

// commitSet leaves out a commit GitHub gave no oid for. Two PRs that each
// have one would otherwise share it, and a PR with no tip would be carried by
// both.
func commitSet(n node) map[string]bool {
	set := map[string]bool{}
	for _, c := range n.Commits.Nodes {
		if c.Commit.Oid != "" {
			set[c.Commit.Oid] = true
		}
	}
	return set
}

// below is the one PR this one sits on, or none when it sits on nothing and
// is reviewed. Three tests, strongest evidence first.
func below(branches []branch, defaultBranch string, i int) int {
	lowerNumber := func(a, b int) bool { return branches[a].number < branches[b].number }

	// 1. The PR says so: its base is another open PR's branch. Two open PRs
	//    can share a head branch, so the lowest number wins rather than
	//    whichever the API listed first: the same repo must answer the same
	//    way twice.
	declared := pick(len(branches),
		func(j int) bool { return basedOn(branches, defaultBranch, i, j) },
		lowerNumber)
	if declared != none {
		return declared
	}

	// 2. It carries another PR's tip, so it is built on that PR's work. Of
	//    the PRs it is built on, the one with the most commits is the
	//    nearest, which makes a three-deep stack read as a chain.
	built := pick(len(branches),
		func(j int) bool { return carriesTip(branches, i, j) },
		func(a, b int) bool {
			if len(branches[a].commits) != len(branches[b].commits) {
				return len(branches[a].commits) > len(branches[b].commits)
			}
			return lowerNumber(a, b)
		})
	if built != none {
		return built
	}

	// 3. Neither is built on the other, but their diffs overlap: two branches
	//    cut from the same unmerged commit. The older PR is reviewed and this
	//    one waits, because this one came second. A PR that sits on this one
	//    is never the answer: the evidence there points the other way.
	return pick(len(branches),
		func(j int) bool {
			return branches[j].number < branches[i].number &&
				shared(branches, i, j) > 0 &&
				!carriesTip(branches, j, i) &&
				!basedOn(branches, defaultBranch, j, i)
		},
		func(a, b int) bool {
			if shared(branches, i, a) != shared(branches, i, b) {
				return shared(branches, i, a) > shared(branches, i, b)
			}
			return lowerNumber(a, b)
		})
}

// pick is the best of the indexes that pass `keep`, or none when none passes.
// `better` must be a strict order, so that the answer does not depend on the
// order GitHub listed the PRs in.
func pick(count int, keep func(j int) bool, better func(a, b int) bool) int {
	best := none
	for j := range count {
		if keep(j) && (best == none || better(j, best)) {
			best = j
		}
	}
	return best
}

// parentOf says how PR `above` was found to sit on PR `under`. The declared
// relation is preferred: it is what the author said, and a stack whose
// branches also share commits is still a stack.
func parentOf(branches []branch, defaultBranch string, above, under int) Parent {
	number := branches[under].number
	switch {
	case basedOn(branches, defaultBranch, above, under):
		return Parent{Number: number, Evidence: Declared}
	case carriesTip(branches, above, under):
		return Parent{Number: number, Evidence: CarriesTip, Shared: shared(branches, above, under)}
	default:
		return Parent{Number: number, Evidence: Overlap, Shared: shared(branches, above, under)}
	}
}

// shared is how many commits the two PRs serve in both diffs.
func shared(branches []branch, a, b int) int {
	count := 0
	for oid := range branches[a].commits {
		if branches[b].commits[oid] {
			count++
		}
	}
	return count
}

// basedOn says whether `above` merges into `under`'s branch, in a way that
// means a stack.
//
// The branch names alone are not enough, because a long-lived branch is a
// base too. Two guards keep an integration branch out:
//
//   - the repository's default branch is never a stack tip. A PR that merges
//     `main` back into a release branch would otherwise hold every PR that
//     merges into `main`.
//   - a stack parent creates the branch its children merge into, so no open
//     PR was already merging into that branch before the parent was opened.
//     On a git-flow repo the PRs merging into `develop` predate the PR that
//     merges `develop` onward, and none of them is stacked on it.
//
// The second guard asks about the branch, and every PR merging into it
// answers, the child under test included. Exempting the child would make one
// branch long-lived for one child and a stack tip for another, which is worse
// than either answer: two children of one parent would cancel each other
// out, and a git-flow PR older than the release PR would be held on it and
// never reviewed.
//
// The cost is a stack whose top PR was opened before its bottom PR, which
// this cannot tell from an integration branch and does not hold. That is the
// safe way to be wrong: the two PRs are both reviewed, which costs a second
// read of shared context. Holding the wrong PR costs a review nobody does.
func basedOn(branches []branch, defaultBranch string, above, under int) bool {
	head := branches[under].headRef
	if head == "" || above == under || branches[above].baseRef != head || defaultBranch == head {
		return false
	}
	return !slices.ContainsFunc(branches, func(b branch) bool {
		return b.baseRef == head && b.number < branches[under].number
	})
}

// carriesTip says whether `above` holds `under`'s tip commit, which puts it
// on top of `under`. A PR with no tip is carried by nobody: no commit set
// holds the empty oid.
func carriesTip(branches []branch, above, under int) bool {
	return above != under && branches[above].commits[branches[under].head]
}
