package babysit

import (
	"slices"
	"testing"

	"github.com/venabots/autoreview/internal/mine"
	"github.com/venabots/autoreview/internal/prs"
	"github.com/venabots/autoreview/internal/task"
)

// F fixes your PRs by the same rules as autofix, once: an approved PR gets
// only its conflicts fixed, and a PR with nothing to fix gets nothing.
func TestFixAllFixesEachPRThatNeedsWorkByTheAutofixRules(t *testing.T) {
	cases := []struct {
		name string
		pr   mine.PR
		want []Due
	}{
		{"an open thread", pr(4), []Due{{PR: 4, Kind: task.Fix}}},
		{"a red build", with(pr(4), func(p *mine.PR) { p.ThreadIDs, p.CI = nil, prs.CIFailing }), []Due{{PR: 4, Kind: task.Fix}}},
		{"nothing to fix", with(pr(4), func(p *mine.PR) { p.ThreadIDs = nil }), nil},
		{"approved with conflicts", with(pr(4), func(p *mine.PR) { p.Review, p.Merge = mine.ReviewApproved, mine.MergeConflicts }), []Due{{PR: 4, Kind: task.Conflicts}}},
		{"approved with a thread", with(pr(4), func(p *mine.PR) { p.Review = mine.ReviewApproved }), nil},
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
