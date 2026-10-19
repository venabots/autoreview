package babysit

import (
	"slices"
	"testing"

	"github.com/venabots/autoreview/internal/mine"
	"github.com/venabots/autoreview/internal/prs"
	"github.com/venabots/autoreview/internal/task"
)

// F is a key a person presses, so it fixes each PR that needs work, an
// approved one too. Only autofix holds back on an approved PR. A PR with
// nothing to fix, or from a fork, gets nothing.
func TestFixAllFixesEachPRThatNeedsWorkEvenWhenItIsApproved(t *testing.T) {
	cases := []struct {
		name string
		pr   mine.PR
		want []Due
	}{
		{"an open thread", pr(4), []Due{{PR: 4, Kind: task.Fix}}},
		{"a red build", with(pr(4), func(p *mine.PR) { p.ThreadIDs, p.CI = nil, prs.CIFailing }), []Due{{PR: 4, Kind: task.Fix}}},
		{"nothing to fix", with(pr(4), func(p *mine.PR) { p.ThreadIDs = nil }), nil},
		{"approved with conflicts", with(pr(4), func(p *mine.PR) { p.Review, p.Merge = mine.ReviewApproved, mine.MergeConflicts }), []Due{{PR: 4, Kind: task.Fix}}},
		{"approved with a thread", with(pr(4), func(p *mine.PR) { p.Review = mine.ReviewApproved }), []Due{{PR: 4, Kind: task.Fix}}},
		{"approved with nothing to fix", with(pr(4), func(p *mine.PR) { p.Review, p.ThreadIDs = mine.ReviewApproved, nil }), nil},
		{"from a fork", with(pr(4), func(p *mine.PR) { p.CrossRepo = true }), nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := ToFix([]mine.PR{c.pr}, nil); !slices.Equal(got, c.want) {
				t.Errorf("ToFix = %+v, want %+v", got, c.want)
			}
		})
	}
}

func TestFixAllSkipsAPRWithAJobAndKeepsTheOrderOfTheList(t *testing.T) {
	busy := func(n int) bool { return n == 5 }
	got := ToFix([]mine.PR{pr(6), pr(5), pr(4)}, busy)
	want := []Due{{PR: 6, Kind: task.Fix}, {PR: 4, Kind: task.Fix}}
	if !slices.Equal(got, want) {
		t.Errorf("ToFix = %+v, want %+v", got, want)
	}
}

// Autofix leaves work on an approved PR for a person, except a conflict.
// The view says so, and names the key that fixes the rest.
func TestAutofixLeavesTheWorkOnAnApprovedPRForAPerson(t *testing.T) {
	approved := func(edit func(*mine.PR)) mine.PR {
		return with(pr(4), func(p *mine.PR) {
			p.Review, p.ThreadIDs = mine.ReviewApproved, nil
			edit(p)
		})
	}
	cases := []struct {
		name string
		pr   mine.PR
		want bool
	}{
		{"approved with an open thread", approved(func(p *mine.PR) { p.ThreadIDs = []string{"t1"} }), true},
		{"approved with a red build", approved(func(p *mine.PR) { p.CI = prs.CIFailing }), true},
		{"approved with only conflicts", approved(func(p *mine.PR) { p.Merge = mine.MergeConflicts }), false},
		{"approved with nothing to fix", approved(func(*mine.PR) {}), false},
		{"not approved, with an open thread", pr(4), false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := LeftForAPerson(c.pr); got != c.want {
				t.Errorf("LeftForAPerson = %v, want %v", got, c.want)
			}
		})
	}
}
