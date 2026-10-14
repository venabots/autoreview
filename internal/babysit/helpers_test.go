package babysit

import (
	"fmt"
	"slices"
	"testing"

	"github.com/venabots/autoreview/internal/mine"
	"github.com/venabots/autoreview/internal/prs"
)

// pr is one of your PRs with one open thread and nothing else wrong.
func pr(n int) mine.PR {
	return mine.PR{
		Number:    n,
		Title:     "My own work",
		Branch:    "me/my-own-work",
		Head:      "sha1",
		UpdatedAt: "2026-10-07T10:00:00Z",
		Review:    mine.ReviewRequired,
		Merge:     mine.MergeClean,
		CI:        prs.CIPassing,
		ThreadIDs: []string{"t1"},
	}
}

func with(p mine.PR, edit func(*mine.PR)) mine.PR {
	edit(&p)
	return p
}

func head(p mine.PR, sha string) mine.PR {
	return with(p, func(q *mine.PR) { q.Head = sha })
}

func shaN(i int) string {
	return fmt.Sprintf("sha%d", i+10)
}

// look is one look at list, with no job running on any PR, and the PRs
// it fixes.
func look(s State, list ...mine.PR) (State, []int) {
	next, due := lookDue(s, list...)
	prs := make([]int, 0, len(due))
	for _, d := range due {
		prs = append(prs, d.PR)
	}
	if len(prs) == 0 {
		return next, nil
	}
	return next, prs
}

// lookDue is one look at list, with no job running on any PR, and the fix
// each PR gets.
func lookDue(s State, list ...mine.PR) (State, []Due) {
	return s.Look(list, nil)
}

func babysitting(numbers ...int) State {
	s := State{}
	for _, n := range numbers {
		s, _ = s.Apply(One(n, true), nil)
	}
	return s
}

func everything() State {
	s, _ := State{}.Apply(All(true), nil)
	return s
}

func wantKinds(t *testing.T, got []Due, want []Due, why string) {
	t.Helper()
	if !slices.Equal(got, want) {
		t.Errorf("%s: due %v, want %v", why, got, want)
	}
}

func wantDue(t *testing.T, got []int, want []int, why string) {
	t.Helper()
	if !slices.Equal(got, want) {
		t.Errorf("%s: due %v, want %v", why, got, want)
	}
}
