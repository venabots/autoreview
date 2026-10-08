package prs

import (
	"encoding/json"
	"fmt"
	"slices"
	"testing"
)

// The fixture v1's unit tests use: 9 and 8 are NEW by others, 6 is SEEN (me
// commented last), 5 is a draft, 4 is mine, 3 is dependabot, 2 is approved.
// The numbering differs from testdata/prs.json, where 5 is the approved PR
// and 2 the draft.
const unitFixture = `[
  {"number":9,"title":"Add retry logic","isDraft":false,
   "updatedAt":"2026-08-10T10:00:00Z","reviewDecision":null,
   "author":{"login":"alice"},
   "comments":{"nodes":[]},"reviews":{"nodes":[]},
   "commits":{"nodes":[{"commit":{"committedDate":"2026-08-10T10:00:00Z","author":{"user":{"login":"alice"}}}}]}},
  {"number":8,"title":"Fix flaky test","isDraft":false,
   "updatedAt":"2026-08-09T10:00:00Z","reviewDecision":"CHANGES_REQUESTED",
   "author":{"login":"bob"},
   "comments":{"nodes":[]},"reviews":{"nodes":[]},
   "commits":{"nodes":[{"commit":{"committedDate":"2026-08-09T10:00:00Z","author":{"user":{"login":"bob"}}}}]}},
  {"number":6,"title":"Refactor helpers","isDraft":false,
   "updatedAt":"2026-08-07T10:00:00Z","reviewDecision":null,
   "author":{"login":"carol"},
   "comments":{"nodes":[{"author":{"login":"me"},"updatedAt":"2026-08-08T10:00:00Z"}]},
   "reviews":{"nodes":[]},
   "commits":{"nodes":[{"commit":{"committedDate":"2026-08-07T10:00:00Z","author":{"user":{"login":"carol"}}}}]}},
  {"number":5,"title":"WIP draft","isDraft":true,
   "updatedAt":"2026-08-06T10:00:00Z","reviewDecision":null,
   "author":{"login":"dave"},
   "comments":{"nodes":[]},"reviews":{"nodes":[]},"commits":{"nodes":[]}},
  {"number":4,"title":"My own PR","isDraft":false,
   "updatedAt":"2026-08-05T10:00:00Z","reviewDecision":null,
   "author":{"login":"me"},
   "comments":{"nodes":[]},"reviews":{"nodes":[]},"commits":{"nodes":[]}},
  {"number":3,"title":"Bump lodash","isDraft":false,
   "updatedAt":"2026-08-04T10:00:00Z","reviewDecision":null,
   "author":{"login":"dependabot[bot]"},
   "comments":{"nodes":[]},"reviews":{"nodes":[]},"commits":{"nodes":[]}},
  {"number":2,"title":"Approved already","isDraft":false,
   "updatedAt":"2026-08-03T10:00:00Z","reviewDecision":"APPROVED",
   "author":{"login":"erin"},
   "comments":{"nodes":[]},"reviews":{"nodes":[]},"commits":{"nodes":[]}}
]`

// Two open PRs, the second cut from the first's branch while it was in
// flight: both say "base: main", and #8 carries #9's two commits as well as
// its own, so its diff is mostly #9's work.
const stackedFixtureJSON = `[
  {"number":9,"title":"Vendor the skills","isDraft":false,
   "updatedAt":"2026-08-10T10:00:00Z","reviewDecision":null,
   "headRefOid":"c2","baseRefName":"main","headRefName":"vendor",
   "author":{"login":"alice"},
   "comments":{"nodes":[]},"reviews":{"nodes":[]},
   "commits":{"nodes":[{"commit":{"oid":"c1"}},{"commit":{"oid":"c2"}}]}},
  {"number":8,"title":"One decision line","isDraft":false,
   "updatedAt":"2026-08-09T10:00:00Z","reviewDecision":null,
   "headRefOid":"c3","baseRefName":"main","headRefName":"decision",
   "author":{"login":"bob"},
   "comments":{"nodes":[]},"reviews":{"nodes":[]},
   "commits":{"nodes":[{"commit":{"oid":"c1"}},{"commit":{"oid":"c2"}},{"commit":{"oid":"c3"}}]}}
]`

var (
	asMe         = Options{Me: "me"}
	allGates     = Gates{CI: true, Stack: true}
	ciGateOff    = Gates{CI: false, Stack: true}
	stackGateOff = Gates{CI: true, Stack: false}
)

func mustNodes(t *testing.T, raw string) []node {
	t.Helper()
	var nodes []node
	if err := json.Unmarshal([]byte(raw), &nodes); err != nil {
		t.Fatalf("the fixture does not decode: %v", err)
	}
	return nodes
}

func mustNode(t *testing.T, raw string) node {
	t.Helper()
	var n node
	if err := json.Unmarshal([]byte(raw), &n); err != nil {
		t.Fatalf("the fixture does not decode: %v", err)
	}
	return n
}

func fixture(t *testing.T) []node {
	t.Helper()
	return mustNodes(t, unitFixture)
}

func stackedFixture(t *testing.T) []node {
	t.Helper()
	return mustNodes(t, stackedFixtureJSON)
}

// change returns the nodes with one of them edited. The input is left as it
// was, so one fixture serves every row of a table.
func change(nodes []node, number int, edit func(node) node) []node {
	changed := make([]node, len(nodes))
	for i, n := range nodes {
		if n.Number == number {
			changed[i] = edit(n)
		} else {
			changed[i] = n
		}
	}
	return changed
}

// withCI returns the nodes with the head commit's checks set on the PRs
// named.
func withCI(nodes []node, states map[int]string) []node {
	changed := make([]node, len(nodes))
	for i, n := range nodes {
		changed[i] = n
		if state, ok := states[n.Number]; ok {
			changed[i].HeadCommit = headCommitList{Nodes: []headCommitNode{
				{Commit: headCommit{StatusCheckRollup: rollup{State: state}}},
			}}
		}
	}
	return changed
}

func commentBy(login, at string) func(node) node {
	return func(n node) node {
		n.Comments = commentList{Nodes: append(slices.Clone(n.Comments.Nodes), commentNode{
			Author:    actor{Login: login},
			UpdatedAt: at,
		})}
		return n
	}
}

func numbers(prs []PR) []int {
	out := make([]int, len(prs))
	for i, p := range prs {
		out[i] = p.Number
	}
	return out
}

// readyNumbers is the sweep's selection: the reviewable PRs that are ready.
// v1's select_auto returned the same numbers after it printed them.
func readyNumbers(l Listing, g Gates) []int {
	out := []int{}
	for _, p := range l.Reviewable() {
		if p.Ready(g) {
			out = append(out, p.Number)
		}
	}
	return out
}

// openCount is v1's "open" figure: every PR that is not a draft.
func openCount(l Listing) int {
	count := 0
	for _, p := range l.PRs {
		if p.Hidden != HiddenDraft {
			count++
		}
	}
	return count
}

func find(t *testing.T, prs []PR, number int) PR {
	t.Helper()
	for _, p := range prs {
		if p.Number == number {
			return p
		}
	}
	t.Fatalf("PR #%d is not in the list %v", number, numbers(prs))
	return PR{}
}

func describe(p *Parent) string {
	if p == nil {
		return "no parent"
	}
	return fmt.Sprintf("#%d (%s, %d shared)", p.Number, p.Evidence, p.Shared)
}

func sameParent(got *Parent, want *Parent) bool {
	if got == nil || want == nil {
		return got == nil && want == nil
	}
	return *got == *want
}
