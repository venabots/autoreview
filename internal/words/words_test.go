package words

import (
	"testing"
	"time"

	"github.com/venabots/autoreview/internal/mine"
	"github.com/venabots/autoreview/internal/prs"
	"github.com/venabots/autoreview/internal/queue"
)

func TestCountReadsAsEnglish(t *testing.T) {
	cases := []struct {
		n    int
		want string
	}{{1, "1 PR"}, {0, "0 PRs"}, {3, "3 PRs"}}
	for _, c := range cases {
		if got := Count(c.n, "PR"); got != c.want {
			t.Errorf("Count(%d) = %q, want %q", c.n, got, c.want)
		}
	}
}

func TestDuration(t *testing.T) {
	cases := []struct {
		d    time.Duration
		want string
	}{
		{3 * time.Second, "3s"},
		{59 * time.Second, "59s"},
		{63 * time.Second, "1m03s"},
		{time.Hour + 5*time.Minute + 9*time.Second, "1h05m"},
		{0, "0s"},
		{-4 * time.Second, "0s"},
		{1500 * time.Millisecond, "1s"},
	}
	for _, c := range cases {
		if got := Duration(c.d); got != c.want {
			t.Errorf("Duration(%v) = %q, want %q", c.d, got, c.want)
		}
	}
}

func TestInterval(t *testing.T) {
	cases := []struct {
		d    time.Duration
		want string
	}{
		{2 * time.Minute, "2m"},
		{90 * time.Minute, "90m"},
		{time.Hour, "1h"},
		{48 * time.Hour, "48h"},
		{90 * time.Second, "1m30s"},
		{0, "0s"},
	}
	for _, c := range cases {
		if got := Interval(c.d); got != c.want {
			t.Errorf("Interval(%v) = %q, want %q", c.d, got, c.want)
		}
	}
}

func TestWait(t *testing.T) {
	cases := []struct {
		name string
		row  queue.Row
		want string
	}{
		{"ready", queue.Row{Wait: queue.WaitNext}, "ready"},
		{"checks running", queue.Row{Wait: queue.WaitChecks, PR: prs.PR{CI: prs.CIPending}}, "CI pending"},
		{"checks failed", queue.Row{Wait: queue.WaitChecks, PR: prs.PR{CI: prs.CIFailing}}, "CI failing"},
		{"sharing commits with an older PR", queue.Row{Wait: queue.WaitStacked, PR: prs.PR{Parent: &prs.Parent{Number: 15, Evidence: prs.Overlap, Shared: 3}}}, "shares commits with #15"},
		{"built on another PR's tip", queue.Row{Wait: queue.WaitStacked, PR: prs.PR{Parent: &prs.Parent{Number: 15, Evidence: prs.CarriesTip, Shared: 2}}}, "built on #15"},
		{"ready on another PR's branch", queue.Row{Wait: queue.WaitNext, PR: prs.PR{Parent: &prs.Parent{Number: 15, Evidence: prs.Declared}}}, "ready · layer on #15"},
		{"ready in a GitHub stack", queue.Row{Wait: queue.WaitNext, PR: prs.PR{
			Parent: &prs.Parent{Number: 15, Evidence: prs.Native},
			Layer:  &prs.Layer{Stack: 4, Position: 3, Size: 5},
		}}, "ready · layer 3 of 5"},
		{"stacked with no parent named", queue.Row{Wait: queue.WaitStacked}, "stacked"},
		{"seen", queue.Row{Wait: queue.WaitSeen}, "seen"},
		{"approved", queue.Row{Wait: queue.WaitApproved}, "approved"},
		{"capped", queue.Row{Wait: queue.WaitCapped}, "capped"},
		{"resting", queue.Row{Wait: queue.WaitResting, Until: time.Time{}.Add(12*time.Minute + 5*time.Second)}, "rest 12m05s"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := Wait(c.row, time.Time{}); got != c.want {
				t.Errorf("Wait = %q, want %q", got, c.want)
			}
		})
	}
}

func TestStackSaysHowAPRSitsOnAnotherOpenPR(t *testing.T) {
	cases := []struct {
		name string
		pr   prs.PR
		want string
	}{
		{"in no stack", prs.PR{}, ""},
		{"on another PR's branch", prs.PR{Parent: &prs.Parent{Number: 2784, Evidence: prs.Declared}},
			"on #2784 (its base is #2784's branch); reviewed by itself"},
		{"in a GitHub stack", prs.PR{
			Parent: &prs.Parent{Number: 2784, Evidence: prs.Native},
			Layer:  &prs.Layer{Stack: 12, Position: 3, Size: 5},
		}, "layer 3 of 5 in GitHub stack #12, on #2784; reviewed by itself"},
		{"at the bottom of a GitHub stack", prs.PR{Layer: &prs.Layer{Stack: 12, Position: 1, Size: 5}},
			"layer 1 of 5 in GitHub stack #12; reviewed by itself"},
		{"built on another PR's tip", prs.PR{Parent: &prs.Parent{Number: 2731, Evidence: prs.CarriesTip, Shared: 26}},
			"built on #2731 (26 commits also in #2731)"},
		{"sharing commits with an older PR", prs.PR{Parent: &prs.Parent{Number: 2731, Evidence: prs.Overlap, Shared: 1}},
			"shares 1 commit with #2731"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := Stack(c.pr); got != c.want {
				t.Errorf("Stack = %q, want %q", got, c.want)
			}
		})
	}
}

func TestBlocker(t *testing.T) {
	cases := []struct {
		name string
		pr   mine.PR
		want string
	}{
		{"waiting for a reviewer", mine.PR{}, "awaiting review"},
		{"approved", mine.PR{Review: mine.ReviewApproved}, "approved"},
		{"approved with a thread open", mine.PR{Review: mine.ReviewApproved, ThreadIDs: []string{"a"}}, "approved · 1 thread open"},
		{"approved with threads open", mine.PR{Review: mine.ReviewApproved, ThreadIDs: []string{"a", "b"}}, "approved · 2 threads open"},
		{"a draft", mine.PR{Draft: true}, "draft"},
		{"checks running", mine.PR{CI: prs.CIPending}, "CI pending"},
		{"one thread", mine.PR{ThreadIDs: []string{"a"}}, "1 thread"},
		{"two threads", mine.PR{ThreadIDs: []string{"a", "b"}}, "2 threads"},
		{"changes requested", mine.PR{Review: mine.ReviewChangesRequested}, "changes req"},
		{"checks failed", mine.PR{CI: prs.CIFailing}, "CI failing"},
		{"conflicts", mine.PR{Merge: mine.MergeConflicts}, "conflicts"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := Blocker(c.pr); got != c.want {
				t.Errorf("Blocker = %q, want %q", got, c.want)
			}
		})
	}
}

func TestTheWordsForEachField(t *testing.T) {
	cases := []struct {
		name, got, want string
	}{
		{"checks passing", CI(prs.CIPassing), "passing"},
		{"checks failing", CI(prs.CIFailing), "failing"},
		{"checks pending", CI(prs.CIPending), "pending"},
		{"no checks", CI(prs.CINone), "no checks"},
		{"approved", Decision(prs.Approved), "approved"},
		{"changes requested", Decision(prs.ChangesRequested), "changes requested"},
		{"review required", Decision(prs.ReviewRequired), "review required"},
		{"no decision", Decision(prs.DecisionNone), "no decision yet"},
		{"your PR approved", Review(mine.ReviewApproved), "approved"},
		{"your PR with changes requested", Review(mine.ReviewChangesRequested), "changes requested"},
		{"your PR needs a review", Review(mine.ReviewRequired), "review required"},
		{"your PR with no decision", Review(mine.ReviewNone), "no decision yet"},
		{"merges", Merge(mine.MergeClean), "merges cleanly"},
		{"conflicts", Merge(mine.MergeConflicts), "conflicts with its base"},
		{"merge unknown", Merge(mine.MergeUnknown), "not worked out yet"},
		{"new", Engagement(prs.New), "new to you"},
		{"updated", Engagement(prs.Updated), "changed since you last engaged"},
		{"seen", Engagement(prs.Seen), "nothing new since you last engaged"},
	}
	for _, c := range cases {
		if c.got != c.want {
			t.Errorf("%s: %q, want %q", c.name, c.got, c.want)
		}
	}
}
