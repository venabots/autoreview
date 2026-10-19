package rebase

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/venabots/autoreview/internal/prs"
)

func botPR() prs.PR {
	return prs.PR{
		Number: 3, Author: "dependabot", Base: "main", Head: "sha3", HeadDate: "2026-08-04T10:00:00Z",
		Engagement: prs.New, CI: prs.CIPassing, Conflicts: true,
	}
}

func TestOnlyAnActionableDependabotPRThatConflictsOrFailsItsChecksNeedsAsking(t *testing.T) {
	cases := []struct {
		name string
		edit func(prs.PR) prs.PR
		want bool
	}{
		{"conflicts", func(p prs.PR) prs.PR { return p }, true},
		{"its checks fail", func(p prs.PR) prs.PR { p.Conflicts, p.CI = false, prs.CIFailing; return p }, true},
		{"clean and passing", func(p prs.PR) prs.PR { p.Conflicts = false; return p }, false},
		{"its checks are still running", func(p prs.PR) prs.PR { p.Conflicts, p.CI = false, prs.CIPending; return p }, false},
		{"a person's PR", func(p prs.PR) prs.PR { p.Author = "alice"; return p }, false},
		{"from a fork", func(p prs.PR) prs.PR { p.CrossRepo = true; return p }, false},
		{"nothing new on it", func(p prs.PR) prs.PR { p.Engagement = prs.Seen; return p }, false},
	}
	for _, c := range cases {
		if got := Needs(c.edit(botPR())); got != c.want {
			t.Errorf("%s: Needs is %v, want %v", c.name, got, c.want)
		}
	}
}

// fakeGh answers each call by the first rule whose prefix matches its argv,
// and keeps every argv.
type fakeGh struct {
	calls   [][]string
	answers map[string]string
	fail    map[string]error
}

func (f *fakeGh) run(_ context.Context, args ...string) ([]byte, error) {
	f.calls = append(f.calls, slices.Clone(args))
	joined := strings.Join(args, " ")
	for prefix, err := range f.fail {
		if strings.HasPrefix(joined, prefix) {
			return nil, err
		}
	}
	for prefix, out := range f.answers {
		if strings.HasPrefix(joined, prefix) {
			return []byte(out), nil
		}
	}
	return nil, nil
}

func (f *fakeGh) posted() bool {
	return slices.ContainsFunc(f.calls, func(c []string) bool { return slices.Contains(c, "POST") })
}

const (
	compareCall  = "api repos/acme/widgets/compare/main...sha3"
	commentsCall = "api --paginate repos/acme/widgets/issues/3/comments"
	postCall     = "api --method POST repos/acme/widgets/issues/3/comments"
)

func TestCheckAsksDependabotToRebaseAPRThatConflicts(t *testing.T) {
	gh := &fakeGh{}
	state, err := Check(context.Background(), gh.run, "acme/widgets", "me", botPR(), true)
	if err != nil || state != Asked {
		t.Fatalf("Check = %v, %v; want Asked", state, err)
	}
	last := gh.calls[len(gh.calls)-1]
	want := []string{"api", "--method", "POST", "repos/acme/widgets/issues/3/comments", "-f", "body=@dependabot rebase", "--silent"}
	if !slices.Equal(last, want) {
		t.Errorf("the last call is %q, want %q", last, want)
	}
	if slices.ContainsFunc(gh.calls, func(c []string) bool { return strings.HasPrefix(strings.Join(c, " "), compareCall) }) {
		t.Error("a PR that conflicts was compared with its base: the conflict alone is the reason")
	}
}

func TestCheckNeverAsksTwiceForTheSameHead(t *testing.T) {
	cases := []struct {
		name     string
		comments string
		want     State
	}{
		{"you asked after the head commit", "me\t2026-08-04T11:00:00Z\n", AskedBefore},
		{"you asked about an older head", "me\t2026-08-03T11:00:00Z\n", Asked},
		{"somebody else asked", "alice\t2026-08-04T11:00:00Z\n", Asked},
	}
	for _, c := range cases {
		gh := &fakeGh{answers: map[string]string{commentsCall: c.comments}}
		state, err := Check(context.Background(), gh.run, "acme/widgets", "me", botPR(), true)
		if err != nil || state != c.want {
			t.Errorf("%s: Check = %v, %v; want %v", c.name, state, err, c.want)
		}
		if gh.posted() != (c.want == Asked) {
			t.Errorf("%s: posted is %v, want %v", c.name, gh.posted(), c.want == Asked)
		}
	}
}

func TestCheckAsksForAFailingPROnlyWhenItsBaseMovedOn(t *testing.T) {
	failing := botPR()
	failing.Conflicts, failing.CI = false, prs.CIFailing
	cases := []struct {
		behind string
		want   State
	}{
		{"4\n", Asked},
		{"0\n", NotBehind},
	}
	for _, c := range cases {
		gh := &fakeGh{answers: map[string]string{compareCall: c.behind}}
		state, err := Check(context.Background(), gh.run, "acme/widgets", "me", failing, true)
		if err != nil || state != c.want {
			t.Errorf("behind by %q: Check = %v, %v; want %v", c.behind, state, err, c.want)
		}
	}
}

func TestCheckDoesNotAskWhenAPersonCommittedToTheBranch(t *testing.T) {
	pr := botPR()
	pr.OthersCommitted = true
	gh := &fakeGh{}
	state, err := Check(context.Background(), gh.run, "acme/widgets", "me", pr, true)
	if err != nil || state != OthersCommitted || gh.posted() {
		t.Errorf("Check = %v, %v, posted %v; want OthersCommitted and nothing posted", state, err, gh.posted())
	}
}

func TestCheckPostsNothingUnderNoPost(t *testing.T) {
	gh := &fakeGh{}
	state, err := Check(context.Background(), gh.run, "acme/widgets", "me", botPR(), false)
	if err != nil || state != WouldAsk || gh.posted() {
		t.Errorf("Check = %v, %v, posted %v; want WouldAsk and nothing posted", state, err, gh.posted())
	}
}

func TestCheckFailsWhenGitHubCannotBeAsked(t *testing.T) {
	for _, call := range []string{commentsCall, postCall} {
		gh := &fakeGh{fail: map[string]error{call: errors.New("HTTP 502")}}
		state, err := Check(context.Background(), gh.run, "acme/widgets", "me", botPR(), true)
		if err == nil || state != Failed {
			t.Errorf("%s fails: Check = %v, %v; want Failed with the error", call, state, err)
		}
	}
}

func TestOnlyTheStatesThatWaitOnDependabotOrAPersonHoldTheReview(t *testing.T) {
	holds := map[State]bool{
		None: false, Checking: true, Asked: true, AskedBefore: true, WouldAsk: true,
		NotBehind: false, OthersCommitted: true, Failed: false,
	}
	for state, want := range holds {
		if state.Holds() != want {
			t.Errorf("%v.Holds() is %v, want %v", state, state.Holds(), want)
		}
	}
}
