package prs

import (
	"context"
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/venabots/autoreview/internal/gh"
)

func TestTheQueryAsksForWhatThePackageReads(t *testing.T) {
	cases := []struct {
		want string
		why  string
	}{
		// Two values that would otherwise drift, and the "50+" label would
		// be wrong the moment they did.
		{fmt.Sprintf("first:%d", queryLimit), "the query and queryLimit disagree"},
		{"headRefOid", "the push detection needs the head SHA"},
		{"statusCheckRollup", "the CI hold needs the rollup"},
		{"headCommit: commits(last:1)", "the rollup is wanted on the head commit alone"},
		{"commit { oid", "the stack needs the commit ids"},
		{"baseRefName", "the stack needs the branch a PR merges into"},
		{"headRefName", "the stack needs the branch a PR is"},
		{"isCrossRepository", "the stack needs to know a fork's branch"},
		{"defaultBranchRef { name }", "the stack needs the trunk's name"},
		{"states:OPEN", "the list is of open PRs"},
		{"reviewDecision", "the approved filter needs the decision"},
		{"comments(last:100) { nodes { author { login } updatedAt } }", "engagement reads the comments"},
		{"reviews(last:100)  { nodes { author { login } submittedAt } }", "engagement reads the reviews"},
		{"lastReview: reviews(last:1) { nodes { author { login } submittedAt body } }", "a review with no verdict is read from the last review's body"},
		{"committedDate author { user { login } }", "engagement reads the commits"},
	}
	for _, c := range cases {
		if !strings.Contains(Query, c.want) {
			t.Errorf("the query has no %q: %s", c.want, c.why)
		}
	}
}

// recorder is a gh that answers every call the same way and keeps the argv.
type recorder struct {
	calls [][]string
	out   []byte
	err   error
}

func (r *recorder) run(_ context.Context, args ...string) ([]byte, error) {
	r.calls = append(r.calls, slices.Clone(args))
	return r.out, r.err
}

func TestFetchCallsGhWithV1Argv(t *testing.T) {
	fake := &recorder{out: []byte(`{"data":{}}`)}
	raw, err := Fetch(context.Background(), fake.run, "acme", "widgets")
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if string(raw) != `{"data":{}}` {
		t.Errorf("Fetch returned %q, want the answer unread", raw)
	}
	want := []string{"api", "graphql", "-F", "owner=acme", "-F", "name=widgets", "-f", "query=" + Query}
	if len(fake.calls) != 1 || !slices.Equal(fake.calls[0], want) {
		t.Errorf("gh was called with %q, want one call with %q", fake.calls, want)
	}
}

func TestAFailedFetchIsAnErrorAndNotAnEmptyList(t *testing.T) {
	down := errors.New("gh api graphql: the PR list is unavailable")
	fake := &recorder{err: down}
	found, err := List(context.Background(), fake.run, "acme", "widgets", asMe)
	if !errors.Is(err, down) {
		t.Errorf("List error = %v, want %v", err, down)
	}
	if len(found.PRs) != 0 {
		t.Errorf("List returned %d PRs beside an error", len(found.PRs))
	}
}

// The seven PRs tests/helpers.sh seeds for v1's bash suite: three open PRs by
// other people, one approved, one of yours, one Dependabot and one draft.
func TestListReadsTheSevenPRFixture(t *testing.T) {
	raw, err := os.ReadFile("testdata/prs.json")
	if err != nil {
		t.Fatal(err)
	}
	var run gh.Runner = (&recorder{out: raw}).run
	found, err := List(context.Background(), run, "acme", "widgets", asMe)
	if err != nil {
		t.Fatalf("List: %v", err)
	}

	want := []PR{
		{
			Number: 9, Title: "Add retry logic", Author: "alice", Branch: "alice/add-retry-logic",
			Head: "sha9", HeadDate: "2026-08-10T10:00:00Z", UpdatedAt: "2026-08-10T10:00:00Z",
			Decision: DecisionNone, CI: CIPassing, Engagement: New, Hidden: NotHidden,
		},
		{
			Number: 8, Title: "Fix typo", Author: "bob", Branch: "bob/fix-typo",
			Head: "sha8", HeadDate: "2026-08-09T10:00:00Z", UpdatedAt: "2026-08-09T10:00:00Z",
			Decision: ChangesRequested, CI: CIPassing, Engagement: New, Hidden: NotHidden,
		},
		{
			Number: 5, Title: "Approved already", Author: "dave", Branch: "dave/approved-already",
			Head: "sha5", HeadDate: "2026-08-06T10:00:00Z", UpdatedAt: "2026-08-06T10:00:00Z",
			Decision: Approved, CI: CINone, Engagement: New, Hidden: HiddenApproved,
		},
		{
			Number: 3, Title: "Bump lodash", Author: "dependabot", Branch: "dependabot/bump-lodash",
			Head: "sha3", HeadDate: "2026-08-04T10:00:00Z", UpdatedAt: "2026-08-04T10:00:00Z",
			Decision: DecisionNone, CI: CINone, Engagement: New, Hidden: HiddenBot,
		},
		{
			Number: 2, Title: "Work in progress", Author: "erin", Branch: "erin/work-in-progress",
			Head: "sha2", HeadDate: "2026-08-03T10:00:00Z", UpdatedAt: "2026-08-03T10:00:00Z", Draft: true,
			Decision: DecisionNone, CI: CINone, Engagement: New, Hidden: HiddenDraft,
		},
		{
			// You commented last.
			Number: 6, Title: "Refactor client", Author: "carol", Branch: "carol/refactor-client",
			Head: "sha6", HeadDate: "2026-08-07T10:00:00Z", UpdatedAt: "2026-08-08T10:00:00Z",
			Decision: DecisionNone, CI: CINone, Engagement: Seen, Hidden: NotHidden,
		},
		{
			// The only activity on it is your own.
			Number: 4, Title: "My own work", Author: "me", Branch: "me/my-own-work",
			Head: "sha4", HeadDate: "2026-08-05T10:00:00Z", UpdatedAt: "2026-08-05T10:00:00Z",
			Decision: DecisionNone, CI: CINone, Engagement: Seen, Hidden: HiddenOwn,
		},
	}
	if !slices.Equal(numbers(found.PRs), numbers(want)) {
		t.Fatalf("the list is %v, want %v", numbers(found.PRs), numbers(want))
	}
	for i, p := range found.PRs {
		if p != want[i] {
			t.Errorf("#%d:\n got %+v\nwant %+v", p.Number, p, want[i])
		}
	}

	if found.DefaultBranch != "main" {
		t.Errorf("default branch = %q, want main", found.DefaultBranch)
	}
	if found.Truncated {
		t.Error("seven PRs is not a full page")
	}
	if got, wantNumbers := numbers(found.Reviewable()), []int{9, 8, 6}; !slices.Equal(got, wantNumbers) {
		t.Errorf("reviewable = %v, want %v", got, wantNumbers)
	}
	// 9 and 8 have passing checks; the rest have no checks at all, which the
	// sweep never holds a PR for.
	if got, wantNumbers := readyNumbers(found, allGates), []int{9, 8}; !slices.Equal(got, wantNumbers) {
		t.Errorf("ready = %v, want %v", got, wantNumbers)
	}
}
