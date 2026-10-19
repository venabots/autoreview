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
		{"committedDate author { user { login } }", "engagement reads the commits"},
		{"stack { number size }", "a GitHub stack needs its number and size"},
		{"stackEntry { position }", "a GitHub stack needs each PR's layer"},
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

// sequence is a gh that answers each call with the next answer in turn.
type sequence struct {
	calls   [][]string
	answers []answer
}

type answer struct {
	out []byte
	err error
}

func (s *sequence) run(_ context.Context, args ...string) ([]byte, error) {
	s.calls = append(s.calls, slices.Clone(args))
	next := s.answers[min(len(s.calls), len(s.answers))-1]
	return next.out, next.err
}

func TestAServerWithNoGitHubStacksIsAskedAgainWithoutThem(t *testing.T) {
	cases := []struct {
		name    string
		first   error
		queries []string
		wantErr bool
	}{
		{
			name:    "the server has no stack field",
			first:   errors.New("gh api graphql: gh: Field 'stack' doesn't exist on type 'PullRequest'"),
			queries: []string{Query, QueryWithoutStacks},
		},
		{
			name:    "the server has no stackEntry field",
			first:   errors.New("gh api graphql: gh: Field 'stackEntry' doesn't exist on type 'PullRequest'"),
			queries: []string{Query, QueryWithoutStacks},
		},
		{
			name:    "any other failure is not asked again",
			first:   errors.New("gh api graphql: gh: API rate limit exceeded"),
			queries: []string{Query},
			wantErr: true,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			fake := &sequence{answers: []answer{{err: c.first}, {out: []byte(`{"data":{}}`)}}}
			_, err := Fetch(context.Background(), fake.run, "acme", "widgets")
			if (err != nil) != c.wantErr {
				t.Fatalf("Fetch error = %v, want an error: %v", err, c.wantErr)
			}
			got := make([]string, len(fake.calls))
			for i, call := range fake.calls {
				got[i] = strings.TrimPrefix(call[len(call)-1], "query=")
			}
			if !slices.Equal(got, c.queries) {
				t.Errorf("gh was asked %d queries, want %d", len(got), len(c.queries))
			}
		})
	}
}

func TestTheQueryWithoutStacksAsksForEverythingElse(t *testing.T) {
	if strings.Contains(QueryWithoutStacks, "stack") {
		t.Error("the fallback query still asks for a stack field")
	}
	if got := strings.Replace(Query, stackFields, "", 1); got != QueryWithoutStacks {
		t.Error("the two queries differ in more than the stack fields")
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
			Head: "sha9", UpdatedAt: "2026-08-10T10:00:00Z",
			Decision: DecisionNone, CI: CIPassing, Engagement: New, Hidden: NotHidden,
		},
		{
			Number: 8, Title: "Fix typo", Author: "bob", Branch: "bob/fix-typo",
			Head: "sha8", UpdatedAt: "2026-08-09T10:00:00Z",
			Decision: ChangesRequested, CI: CIPassing, Engagement: New, Hidden: NotHidden,
		},
		{
			Number: 5, Title: "Approved already", Author: "dave", Branch: "dave/approved-already",
			Head: "sha5", UpdatedAt: "2026-08-06T10:00:00Z",
			Decision: Approved, CI: CINone, Engagement: New, Hidden: HiddenApproved,
		},
		{
			Number: 3, Title: "Bump lodash", Author: "dependabot", Branch: "dependabot/bump-lodash",
			Head: "sha3", UpdatedAt: "2026-08-04T10:00:00Z",
			Decision: DecisionNone, CI: CINone, Engagement: New, Hidden: HiddenBot,
		},
		{
			Number: 2, Title: "Work in progress", Author: "erin", Branch: "erin/work-in-progress",
			Head: "sha2", UpdatedAt: "2026-08-03T10:00:00Z", Draft: true,
			Decision: DecisionNone, CI: CINone, Engagement: New, Hidden: HiddenDraft,
		},
		{
			// You commented last.
			Number: 6, Title: "Refactor client", Author: "carol", Branch: "carol/refactor-client",
			Head: "sha6", UpdatedAt: "2026-08-08T10:00:00Z",
			Decision: DecisionNone, CI: CINone, Engagement: Seen, Hidden: NotHidden,
		},
		{
			// The only activity on it is your own.
			Number: 4, Title: "My own work", Author: "me", Branch: "me/my-own-work",
			Head: "sha4", UpdatedAt: "2026-08-05T10:00:00Z",
			Decision: DecisionNone, CI: CINone, Engagement: Seen, Hidden: HiddenOwn,
		},
	}
	if !slices.Equal(numbers(found.PRs), numbers(want)) {
		t.Fatalf("the list is %v, want %v", numbers(found.PRs), numbers(want))
	}
	for i, p := range found.PRs {
		if p.Number == 2 {
			// The draft is the bottom layer of a GitHub stack of two.
			if p.Layer == nil || *p.Layer != (Layer{Stack: 4, Position: 1, Size: 2}) {
				t.Errorf("#2 has layer %v, want layer 1 of 2 in stack 4", p.Layer)
			}
			p.Layer = nil
		}
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
