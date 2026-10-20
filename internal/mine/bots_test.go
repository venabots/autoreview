package mine

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
)

const botResponse = `{"data":{"search":{"nodes":[
  {"number":12,"title":"chore(deps): bump left-pad","headRefName":"dependabot/npm_and_yarn/left-pad-2.0.0","headRefOid":"sha12",
   "reviewDecision":"REVIEW_REQUIRED","mergeable":"MERGEABLE",
   "commits":{"nodes":[{"commit":{"author":{"name":"dependabot[bot]"}}}]},
   "headCommit":{"nodes":[{"commit":{"statusCheckRollup":{"state":"FAILURE"}}}]}},
  {"number":13,"title":"chore(deps): bump right-pad","headRefName":"dependabot/npm_and_yarn/right-pad-3.0.0","headRefOid":"sha13",
   "commits":{"nodes":[{"commit":{"author":{"name":"dependabot[bot]"}}},{"commit":{"author":{"name":"Matt Venables"}}}]}},
  {}
]}}}`

func TestBotTermsAreThisRepoAndDependabotsOpenPRs(t *testing.T) {
	if got, want := BotTerms("acme", "widgets"), "repo:acme/widgets is:pr is:open author:app/dependabot"; got != want {
		t.Errorf("BotTerms = %q, want %q", got, want)
	}
}

func TestTheBotSearchAsksForEveryFieldOfYourSearchAndTheCommitAuthors(t *testing.T) {
	for _, field := range []string{"reviewThreads(first:100)", "statusCheckRollup", "mergeable", "commits(last:50)", "author { name }"} {
		if !strings.Contains(BotQuery, field) {
			t.Errorf("BotQuery does not ask for %s", field)
		}
	}
	if strings.Contains(Query, "commits(last:50)") {
		t.Error("the search for your own PRs asks for commit authors; it must keep v1's fields")
	}
}

func TestADependabotPRIsMarkedAsABot(t *testing.T) {
	list, err := ParseBots([]byte(botResponse))
	if err != nil {
		t.Fatalf("ParseBots: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("ParseBots gave %d PRs, want 2", len(list))
	}
	for _, pr := range list {
		if !pr.Bot {
			t.Errorf("PR #%d is not marked as a bot's PR", pr.Number)
		}
	}
}

func TestACommitFromSomebodyElseMeansDependabotNoLongerRebasesThePR(t *testing.T) {
	list, err := ParseBots([]byte(botResponse))
	if err != nil {
		t.Fatalf("ParseBots: %v", err)
	}
	cases := []struct {
		number int
		want   bool
	}{
		{12, false},
		{13, true},
	}
	for _, c := range cases {
		i := slices.IndexFunc(list, func(p PR) bool { return p.Number == c.number })
		if i < 0 {
			t.Fatalf("PR #%d is missing", c.number)
		}
		if got := list[i].OthersPushed; got != c.want {
			t.Errorf("PR #%d OthersPushed = %v, want %v", c.number, got, c.want)
		}
	}
}

func TestYourOwnPRsAreNeverMarkedAsABots(t *testing.T) {
	list, err := Parse([]byte(searchResponse))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if list[0].Bot || list[0].OthersPushed {
		t.Errorf("your own PR reads as a bot's: %+v", list[0])
	}
}

func TestListWithBotsAsksForBothSearchesAndRanksThemTogether(t *testing.T) {
	var sent [][]string
	run := func(_ context.Context, args ...string) ([]byte, error) {
		sent = append(sent, slices.Clone(args))
		if strings.Contains(strings.Join(args, " "), "app/dependabot") {
			return []byte(botResponse), nil
		}
		return []byte(searchResponse), nil
	}

	list, err := ListWith(context.Background(), run, "acme", "widgets", true)

	if err != nil {
		t.Fatalf("ListWith: %v", err)
	}
	if len(sent) != 2 {
		t.Fatalf("gh was called %d times, want 2", len(sent))
	}
	wantBots := []string{"api", "graphql", "-f", "q=" + BotTerms("acme", "widgets"), "-f", "query=" + BotQuery}
	if !slices.Equal(sent[1], wantBots) {
		t.Errorf("the bot search sent %q, want %q", sent[1], wantBots)
	}
	got := make([]int, len(list))
	for i, pr := range list {
		got[i] = pr.Number
	}
	// #12 fails its checks, which outranks #4's requested change and #13,
	// which only waits for a review.
	if want := []int{12, 4, 13}; !slices.Equal(got, want) {
		t.Errorf("ListWith ranked %v, want %v", got, want)
	}
}

func TestListWithoutBotsAsksOnlyForYourPRs(t *testing.T) {
	calls := 0
	run := func(_ context.Context, _ ...string) ([]byte, error) {
		calls++
		return []byte(searchResponse), nil
	}
	list, err := ListWith(context.Background(), run, "acme", "widgets", false)
	if err != nil || len(list) != 1 || calls != 1 {
		t.Errorf("ListWith = %v, %v after %d calls; want your one PR after one call", list, err, calls)
	}
}

func TestAFailedBotSearchFailsTheRead(t *testing.T) {
	run := func(_ context.Context, args ...string) ([]byte, error) {
		if strings.Contains(strings.Join(args, " "), "app/dependabot") {
			return nil, errors.New("rate limited")
		}
		return []byte(searchResponse), nil
	}
	_, err := ListWith(context.Background(), run, "acme", "widgets", true)
	if err == nil || !strings.Contains(err.Error(), "rate limited") {
		t.Errorf("ListWith error = %v, want the bot search's error: a list without the bots would read as if they were all merged", err)
	}
}
