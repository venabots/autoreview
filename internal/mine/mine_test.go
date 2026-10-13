package mine

import (
	"context"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/venabots/autoreview/internal/prs"
)

func base() PR {
	return PR{
		Number:    4,
		Title:     "My own work",
		Branch:    "me/my-own-work",
		Head:      "sha4",
		UpdatedAt: "2026-10-07T10:00:00Z",
		Review:    ReviewRequired,
		Merge:     MergeClean,
		CI:        prs.CIPassing,
	}
}

func change(edit func(*PR)) PR {
	p := base()
	edit(&p)
	return p
}

func TestBlockerNamesWhatBlocksTheMergeFirst(t *testing.T) {
	cases := []struct {
		name string
		pr   PR
		want Blocker
	}{
		{"nothing is wrong", base(), BlockerAwaitingReview},
		{"approved", change(func(p *PR) { p.Review = ReviewApproved }), BlockerApproved},
		{"a draft", change(func(p *PR) { p.Draft = true }), BlockerDraft},
		{"checks still running", change(func(p *PR) { p.CI = prs.CIPending }), BlockerCIPending},
		{"a draft outranks pending checks", change(func(p *PR) { p.Draft = true; p.CI = prs.CIPending }), BlockerDraft},
		{"an open thread", change(func(p *PR) { p.ThreadIDs = []string{"T1"} }), BlockerThreads},
		{"a requested change outranks open threads", change(func(p *PR) {
			p.Review = ReviewChangesRequested
			p.ThreadIDs = []string{"T1", "T2"}
		}), BlockerChangesRequested},
		{"a red build outranks the reviewers", change(func(p *PR) {
			p.CI = prs.CIFailing
			p.Review = ReviewChangesRequested
		}), BlockerCIFailing},
		{"a conflict outranks a red build", change(func(p *PR) {
			p.Merge = MergeConflicts
			p.CI = prs.CIFailing
		}), BlockerConflicts},
		{"an approval outranks open threads", change(func(p *PR) {
			p.Review = ReviewApproved
			p.ThreadIDs = []string{"T1"}
		}), BlockerApproved},
		{"a conflict outranks an approval", change(func(p *PR) {
			p.Review = ReviewApproved
			p.Merge = MergeConflicts
		}), BlockerConflicts},
		{"pending checks outrank an approval", change(func(p *PR) {
			p.Review = ReviewApproved
			p.CI = prs.CIPending
		}), BlockerCIPending},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := c.pr.Blocker(); got != c.want {
				t.Errorf("Blocker() = %d, want %d", got, c.want)
			}
		})
	}
}

// A repo with no review rule reports no decision at all, so an approval
// there is read from the reviewers.
func TestApprovedIsTheDecisionOrAnApproverWhereNoRuleAsks(t *testing.T) {
	alice := Reviewer{Login: "alice", Review: ReviewApproved}
	bob := Reviewer{Login: "bob", Review: ReviewChangesRequested}
	cases := []struct {
		name      string
		review    Review
		reviewers []Reviewer
		want      bool
	}{
		{"the decision is approved", ReviewApproved, []Reviewer{alice}, true},
		{"the decision is approved and nobody is named", ReviewApproved, nil, true},
		{"the rules want another approval", ReviewRequired, []Reviewer{alice}, false},
		{"changes are requested", ReviewChangesRequested, []Reviewer{alice, bob}, false},
		{"no rule, and one approval", ReviewNone, []Reviewer{alice}, true},
		{"no rule, and one of two asks for changes", ReviewNone, []Reviewer{alice, bob}, false},
		{"no rule, and nobody decided", ReviewNone, nil, false},
	}
	for _, c := range cases {
		got := change(func(p *PR) { p.Review, p.Reviewers = c.review, c.reviewers }).Approved()
		if got != c.want {
			t.Errorf("%s: Approved() = %v, want %v", c.name, got, c.want)
		}
	}
}

func numbers(list []PR) []int {
	out := make([]int, len(list))
	for i, p := range list {
		out[i] = p.Number
	}
	return out
}

func TestRankedListsTheMostUrgentFirst(t *testing.T) {
	list := []PR{
		change(func(p *PR) { p.Number = 1; p.Review = ReviewApproved }),
		change(func(p *PR) { p.Number = 2; p.Merge = MergeConflicts }),
		change(func(p *PR) { p.Number = 3; p.ThreadIDs = []string{"A", "B"} }),
		change(func(p *PR) { p.Number = 5; p.ThreadIDs = []string{"C"} }),
	}
	before := numbers(list)

	got := numbers(Ranked(list))

	if want := []int{2, 5, 3, 1}; !slices.Equal(got, want) {
		t.Errorf("Ranked order = %v, want %v (newest first within the same state)", got, want)
	}
	if after := numbers(list); !slices.Equal(after, before) {
		t.Errorf("Ranked changed its argument: %v became %v", before, after)
	}
}

func TestRankedPutsAwaitingReviewAboveADraft(t *testing.T) {
	list := []PR{
		change(func(p *PR) { p.Number = 1; p.Draft = true }),
		change(func(p *PR) { p.Number = 2 }),
		change(func(p *PR) { p.Number = 3; p.CI = prs.CIPending }),
	}
	if got, want := numbers(Ranked(list)), []int{3, 2, 1}; !slices.Equal(got, want) {
		t.Errorf("Ranked order = %v, want %v", got, want)
	}
}

const searchResponse = `{"data":{"search":{"nodes":[
  {"number":4,"title":"My own work","isDraft":false,"headRefName":"me/my-own-work",
   "headRefOid":"sha4","updatedAt":"2026-10-07T10:00:00Z","isCrossRepository":false,
   "reviewDecision":"CHANGES_REQUESTED","mergeable":"MERGEABLE",
   "latestReviews":{"nodes":[{"author":{"login":"alice"},"state":"CHANGES_REQUESTED"},
                             {"author":{"login":"bob"},"state":"COMMENTED"},
                             {"author":null,"state":"APPROVED"}]},
   "reviewThreads":{"nodes":[{"id":"T1","isResolved":false},{"id":"T2","isResolved":true},{"id":"T3","isResolved":false}]},
   "headCommit":{"nodes":[{"commit":{"statusCheckRollup":{"state":"SUCCESS"}}}]}},
  {}
]}}}`

func TestParseTurnsASearchResponseIntoYourPRs(t *testing.T) {
	list, err := Parse([]byte(searchResponse))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("Parse gave %d PRs, want 1: a node that is not a PR is dropped", len(list))
	}
	want := PR{
		Number:    4,
		Title:     "My own work",
		Branch:    "me/my-own-work",
		Head:      "sha4",
		UpdatedAt: "2026-10-07T10:00:00Z",
		Review:    ReviewChangesRequested,
		Merge:     MergeClean,
		CI:        prs.CIPassing,
		ThreadIDs: []string{"T1", "T3"},
		Reviewers: []Reviewer{{Login: "alice", Review: ReviewChangesRequested}},
	}
	if !reflect.DeepEqual(list[0], want) {
		t.Errorf("Parse gave\n %+v\nwant\n %+v", list[0], want)
	}
	if list[0].OpenThreads() != 2 {
		t.Errorf("OpenThreads() = %d, want 2", list[0].OpenThreads())
	}
}

func TestParseReadsEveryOptionalFieldAsAbsent(t *testing.T) {
	list, err := Parse([]byte(`{"data":{"search":{"nodes":[{"number":7}]}}}`))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	want := PR{Number: 7, ThreadIDs: []string{}, Reviewers: []Reviewer{}}
	if !reflect.DeepEqual(list[0], want) {
		t.Errorf("Parse gave %+v, want %+v", list[0], want)
	}
}

func TestParseDoesNotReadAFailedSearchAsAnEmptyList(t *testing.T) {
	cases := []struct {
		name, raw, wantErr string
	}{
		{"an errors array", `{"errors":[{"message":"rate limited"}]}`, "rate limited"},
		{"an error with no message", `{"errors":[{}]}`, "unknown error"},
		{"no search in the data", `{"data":{}}`, "no search results"},
		{"no data at all", `{}`, "no search results"},
		{"not JSON", `<html>`, "parsing your PRs"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := Parse([]byte(c.raw))
			if err == nil || !strings.Contains(err.Error(), c.wantErr) {
				t.Errorf("Parse error = %v, want one containing %q", err, c.wantErr)
			}
		})
	}

	list, err := Parse([]byte(`{"data":{"search":{"nodes":[]}}}`))
	if err != nil || len(list) != 0 {
		t.Errorf("an empty search = %v, %v; want no PRs and no error", list, err)
	}
}

func TestSearchTermsAreThisRepoAndYourOpenPRs(t *testing.T) {
	if got, want := SearchTerms("acme", "widgets"), "repo:acme/widgets is:pr is:open author:@me"; got != want {
		t.Errorf("SearchTerms = %q, want %q", got, want)
	}
}

func TestListSendsTheSearchToGh(t *testing.T) {
	var sent []string
	run := func(_ context.Context, args ...string) ([]byte, error) {
		sent = args
		return []byte(searchResponse), nil
	}

	list, err := List(context.Background(), run, "acme", "widgets")

	if err != nil || len(list) != 1 {
		t.Fatalf("List = %v, %v", list, err)
	}
	want := []string{"api", "graphql", "-f", "q=repo:acme/widgets is:pr is:open author:@me", "-f", "query=" + Query}
	if !slices.Equal(sent, want) {
		t.Errorf("gh was sent %q, want %q", sent, want)
	}
}
