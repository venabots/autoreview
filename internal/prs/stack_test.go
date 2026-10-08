package prs

import (
	"fmt"
	"maps"
	"testing"
)

// pr is a PR with the commits named, the last of which is its tip, merging
// into `base` from a branch of its own named "pr<number>".
func pr(number int, base string, commits ...string) node {
	list := make([]commitNode, len(commits))
	for i, oid := range commits {
		list[i] = commitNode{Commit: commit{Oid: oid}}
	}
	head := ""
	if len(commits) > 0 {
		head = commits[len(commits)-1]
	}
	return node{
		Number:      number,
		Title:       "t",
		UpdatedAt:   "2026-08-10T10:00:00Z",
		HeadRefOid:  head,
		BaseRefName: base,
		HeadRefName: fmt.Sprintf("pr%d", number),
		Author:      actor{Login: "alice"},
		Commits:     commitList{Nodes: list},
	}
}

// from renames the PR's own branch.
func from(n node, branch string) node {
	n.HeadRefName = branch
	return n
}

// fork moves the PR's branch into somebody else's repository.
func fork(n node) node {
	n.IsCrossRepository = true
	return n
}

func TestParents(t *testing.T) {
	cases := []struct {
		name string
		prs  []node
		want map[int]Parent
	}{
		{
			// The declared stack. GitHub diffs #16 from #15's branch, so
			// their commits do not overlap at all: the branch names are the
			// only thing that says these two are one piece of work. The one
			// underneath is still reviewed.
			name: "a PR based on another open PR's branch is held on it",
			prs:  []node{pr(15, "main", "a", "b"), pr(16, "pr15", "c", "d")},
			want: map[int]Parent{16: {Number: 15, Evidence: Declared}},
		},
		{
			// The undeclared stack, which is the expensive one: both say
			// "base: main", and #18's diff is mostly #15's work.
			name: "a branch cut from another open PR's branch is held on it",
			prs:  []node{pr(15, "main", "a", "b", "c", "d"), pr(18, "main", "a", "b", "e")},
			want: map[int]Parent{18: {Number: 15, Evidence: Overlap, Shared: 2}},
		},
		{
			// Overlap alone says which two PRs are related but not which is
			// underneath. Carrying the other's tip says it outright, whatever
			// the PR numbers are: here the top of the stack was opened first.
			name: "a PR that carries another PR's tip is the one held",
			prs:  []node{pr(30, "main", "a", "b", "c"), pr(31, "main", "a", "b")},
			want: map[int]Parent{30: {Number: 31, Evidence: CarriesTip, Shared: 2}},
		},
		{
			name: "a three deep stack names the PR directly below each one",
			prs:  []node{pr(19, "main", "a", "b"), pr(20, "pr19", "c"), pr(21, "pr20", "d")},
			want: map[int]Parent{
				20: {Number: 19, Evidence: Declared},
				21: {Number: 20, Evidence: Declared},
			},
		},
		{
			name: "a three deep undeclared stack names the nearest too",
			prs: []node{
				pr(19, "main", "a", "b"),
				pr(20, "main", "a", "b", "c"),
				pr(21, "main", "a", "b", "c", "d"),
			},
			want: map[int]Parent{
				20: {Number: 19, Evidence: CarriesTip, Shared: 2},
				21: {Number: 20, Evidence: CarriesTip, Shared: 3},
			},
		},
		{
			name: "two PRs off one parent are both held and the parent is not",
			prs:  []node{pr(19, "main", "a", "b"), pr(20, "pr19", "c"), pr(21, "pr19", "d")},
			want: map[int]Parent{
				20: {Number: 19, Evidence: Declared},
				21: {Number: 19, Evidence: Declared},
			},
		},
		{
			// Different work, and the ordinary case of several PRs all
			// merging into main: a shared base branch is not a stack.
			name: "unrelated PRs are never held",
			prs:  []node{pr(9, "main", "a", "b"), pr(8, "main", "c"), pr(6, "develop", "d", "e")},
			want: map[int]Parent{},
		},
		{
			// No commits means no overlap anyone can prove, and two PRs with
			// no tip do not carry each other's.
			name: "a PR with no commit data is left alone",
			prs:  []node{pr(9, "main"), pr(8, "main"), pr(7, "main", "a")},
			want: map[int]Parent{},
		},
		{
			// A commit GitHub gave no oid for is not a commit two PRs have
			// in common, and a PR with no tip is not underneath anything.
			name: "a commit with no oid proves nothing",
			prs:  []node{pr(9, "main", ""), pr(8, "main", ""), pr(7, "main")},
			want: map[int]Parent{},
		},
		{
			// The declared relation is preferred: it is what the author
			// said, and a stack whose branches also share commits is still a
			// stack.
			name: "a declared stack that also shares commits is still declared",
			prs:  []node{pr(15, "main", "a", "b"), pr(16, "pr15", "a", "b", "c")},
			want: map[int]Parent{16: {Number: 15, Evidence: Declared}},
		},
		{
			// headRefName is a name in the head repository. Two contributors
			// working on their own fork's `patch-1`, both merging into main,
			// would otherwise each read as based on the other.
			name: "two forks with the same branch name are not a stack",
			prs:  []node{pr(9, "patch-1", "a"), fork(from(pr(8, "main", "b"), "patch-1"))},
			want: map[int]Parent{},
		},
		{
			name: "the same two branches in the base repository are a stack",
			prs:  []node{pr(9, "patch-1", "a"), from(pr(8, "main", "b"), "patch-1")},
			want: map[int]Parent{9: {Number: 8, Evidence: Declared}},
		},
		{
			// #18 merged both branches to fix a conflict, so it carries
			// #12's work and #15's. That does not relate #12 and #15 to each
			// other. Holding #15 until #12 lands would leave real work
			// unreviewed for a merge that changes nothing about it.
			name: "a PR is never held on one it shares no work with",
			prs: []node{
				pr(12, "main", "p", "q"),
				pr(15, "main", "a", "b"),
				pr(18, "main", "a", "b", "p", "q", "m", "t"),
			},
			want: map[int]Parent{18: {Number: 12, Evidence: CarriesTip, Shared: 2}},
		},
		{
			// #1 and #3 share nothing; only #2 touches both. Each hold names
			// the PR it actually overlaps, and #1 stays reviewable.
			name: "a chain of overlaps holds each PR on the one it overlaps",
			prs: []node{
				pr(1, "main", "x", "a"),
				pr(2, "main", "x", "y", "b"),
				pr(3, "main", "y", "c"),
			},
			want: map[int]Parent{
				2: {Number: 1, Evidence: Overlap, Shared: 1},
				3: {Number: 2, Evidence: Overlap, Shared: 1},
			},
		},
		{
			// v1: no_hold_ever_reports_nothing_in_common. The loop below
			// checks the count on every row; this is the row v1 checked.
			name: "a PR that shares nothing sits beside a chain untouched",
			prs: []node{
				pr(1, "main", "x", "a"),
				pr(2, "main", "x", "y", "b"),
				pr(3, "main", "y", "c"),
				pr(4, "main", "z"),
			},
			want: map[int]Parent{
				2: {Number: 1, Evidence: Overlap, Shared: 1},
				3: {Number: 2, Evidence: Overlap, Shared: 1},
			},
		},
		{
			// Two older PRs both overlap #3. The one it shares more with is
			// the one whose merge changes more of its diff.
			name: "of two overlaps the larger one is underneath",
			prs: []node{
				pr(1, "main", "x", "a"),
				pr(2, "main", "y", "z", "b"),
				pr(3, "main", "x", "y", "z", "c"),
			},
			want: map[int]Parent{3: {Number: 2, Evidence: Overlap, Shared: 2}},
		},
		{
			// Equal overlaps, and #2 listed first: the lowest number wins,
			// not the order of the answer.
			name: "of two equal overlaps the older PR is underneath",
			prs: []node{
				pr(2, "main", "y", "b"),
				pr(1, "main", "x", "a"),
				pr(3, "main", "x", "y", "c"),
			},
			want: map[int]Parent{3: {Number: 1, Evidence: Overlap, Shared: 1}},
		},
		{
			// git-flow: #99 merges `develop` onward, and four PRs merge into
			// `develop`. None of them is stacked on #99: their work is not in
			// its diff, and it lands by a route of its own.
			name: "a long-lived branch is not a stack",
			prs: []node{
				from(pr(99, "main", "r1"), "develop"),
				pr(1, "develop", "f1"),
				pr(2, "develop", "f2"),
				pr(3, "develop", "f3"),
				pr(4, "develop", "f4"),
			},
			want: map[int]Parent{},
		},
		{
			name: "a feature opened after the release PR is no different",
			prs: []node{
				from(pr(99, "main", "r1"), "develop"),
				pr(1, "develop", "f1"),
				pr(2, "develop", "f2"),
				pr(3, "develop", "f3"),
				pr(4, "develop", "f4"),
				pr(100, "develop", "f100"),
			},
			want: map[int]Parent{},
		},
		{
			// #9 merges into #12's branch, but #9 was opened first, which is
			// what a PR merging into a long-lived branch looks like. Nothing
			// in the data tells the two apart, so this is the deliberate
			// miss: both PRs are reviewed, and the cost is one second read of
			// shared context. The other way to be wrong holds a PR nobody
			// then reviews.
			name: "a stack opened from the top down is not held",
			prs:  []node{pr(9, "pr12", "a"), pr(12, "main", "b")},
			want: map[int]Parent{},
		},
		{
			// Whether a branch is long-lived is a fact about the branch.
			// Judging it per child would hold a git-flow PR opened before
			// the release PR while freeing one opened after it.
			name: "one branch gets one answer for an older and a newer child",
			prs: []node{
				from(pr(99, "main", "r1"), "develop"),
				pr(1, "develop", "f1"),
				pr(100, "develop", "f100"),
			},
			want: map[int]Parent{},
		},
		{
			// Two children of one parent, and the parent opened last. Both
			// get the same answer, and neither cancels the other's.
			name: "one branch gets one answer for two children",
			prs:  []node{pr(9, "pr12", "b"), pr(10, "pr12", "c"), pr(12, "main", "a")},
			want: map[int]Parent{},
		},
		{
			// Two open PRs can share a head branch, and the API lists PRs by
			// when they were last updated. The lowest number wins, so the
			// held line does not change under a repo nobody touched.
			name: "the PR underneath is the same one every run",
			prs: []node{
				pr(20, "shared", "a"),
				from(pr(8, "main", "b"), "shared"),
				from(pr(5, "main", "c"), "shared"),
			},
			want: map[int]Parent{20: {Number: 5, Evidence: Declared}},
		},
		{
			name: "the order GitHub lists them in does not decide it",
			prs: []node{
				from(pr(5, "main", "c"), "shared"),
				from(pr(8, "main", "b"), "shared"),
				pr(20, "shared", "a"),
			},
			want: map[int]Parent{20: {Number: 5, Evidence: Declared}},
		},
		{
			// #1 merges `main` into `develop`, so its head branch is `main`.
			// Every PR merging into `main` would otherwise wait on it.
			name: "a PR that merges the default branch onward is not a stack",
			prs:  []node{from(pr(1, "develop", "r1"), "main"), pr(9, "main", "a")},
			want: map[int]Parent{},
		},
		{
			// Two PRs on the same tip sit on each other, so neither is a
			// keeper by the ancestry rule. Holding both would leave work
			// nobody reviews. The later one waits.
			name: "a group always keeps one reviewable PR",
			prs:  []node{pr(9, "main", "a", "b"), pr(8, "main", "a", "b")},
			want: map[int]Parent{9: {Number: 8, Evidence: CarriesTip, Shared: 2}},
		},
		{
			// Two PRs each based on the other's branch. Nonsense, and GitHub
			// would refuse the second, but a rule that deadlocked on it
			// would silently stop reviewing both.
			name: "a base cycle still keeps one",
			prs:  []node{pr(9, "pr8", "a"), pr(8, "pr9", "b")},
			want: map[int]Parent{9: {Number: 8, Evidence: Declared}},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := parents(c.prs, "main")
			if !maps.Equal(got, c.want) {
				t.Errorf("parents = %v, want %v", got, c.want)
			}
			// A commit hold with nothing in common is a hold nothing
			// justifies. Both commit tests need a commit in common, so it
			// must be unreachable.
			for number, on := range got {
				if on.Evidence != Declared && on.Shared == 0 {
					t.Errorf("#%d is held on #%d with nothing in common", number, on.Number)
				}
				if on.Evidence == Declared && on.Shared != 0 {
					t.Errorf("#%d: a declared stack reports %d shared commits, want 0", number, on.Shared)
				}
			}
		})
	}
}

// Three PRs that each carry the next one's tip sit in one circle. The rule
// frees the lowest-numbered member and no other, whichever PR GitHub listed
// first.
func TestACircleFreesItsLowestNumberedMember(t *testing.T) {
	branches := []branch{{number: 12}, {number: 7}, {number: 30}}
	cases := []struct {
		name string
		on   []int
		want []int
	}{
		{"three in a circle", []int{1, 2, 0}, []int{1, none, 0}},
		{"two in a circle and one hanging off it", []int{2, 0, 0}, []int{none, 0, 0}},
		{"a chain is not a circle", []int{1, 2, none}, []int{1, 2, none}},
		{"nothing is held", []int{none, none, none}, []int{none, none, none}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			given := append([]int(nil), c.on...)
			got := breakCircles(branches, c.on)
			if fmt.Sprint(got) != fmt.Sprint(c.want) {
				t.Errorf("breakCircles(%v) = %v, want %v", given, got, c.want)
			}
			if fmt.Sprint(c.on) != fmt.Sprint(given) {
				t.Errorf("the argument was changed from %v to %v", given, c.on)
			}
		})
	}
}

// An answer with no defaultBranchRef exempts no branch, so the PR that merges
// `main` onward reads as a stack parent. That is the cost v1 accepts: one
// hold too many, and never a wrong name for the trunk.
func TestAMissingDefaultBranchExemptsNothing(t *testing.T) {
	prs := []node{from(pr(1, "develop", "r1"), "main"), pr(9, "main", "a")}
	got := parents(prs, "")
	want := map[int]Parent{9: {Number: 1, Evidence: Declared}}
	if !maps.Equal(got, want) {
		t.Errorf("parents = %v, want %v", got, want)
	}
}
