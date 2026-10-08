package queue

import (
	"slices"
	"testing"

	"github.com/venabots/autoreview/internal/prs"
)

var bothGates = prs.Gates{CI: true, Stack: true}

func pr(number int, edit func(*prs.PR)) prs.PR {
	p := prs.PR{Number: number, Engagement: prs.New, CI: prs.CIPassing}
	if edit != nil {
		edit(&p)
	}
	return p
}

func TestWaitSaysWhyAPRIsNotReviewedNow(t *testing.T) {
	cases := []struct {
		name  string
		pr    prs.PR
		gates prs.Gates
		want  Wait
	}{
		{"new with passing checks", pr(9, nil), bothGates, WaitNext},
		{"updated with no checks", pr(9, func(p *prs.PR) { p.Engagement = prs.Updated; p.CI = prs.CINone }), bothGates, WaitNext},
		{"checks still running", pr(9, func(p *prs.PR) { p.CI = prs.CIPending }), bothGates, WaitChecks},
		{"checks failed", pr(9, func(p *prs.PR) { p.CI = prs.CIFailing }), bothGates, WaitChecks},
		{"checks failed with the gate off", pr(9, func(p *prs.PR) { p.CI = prs.CIFailing }), prs.Gates{Stack: true}, WaitNext},
		{"on another PR", pr(9, func(p *prs.PR) { p.Parent = &prs.Parent{Number: 8} }), bothGates, WaitStacked},
		{"on another PR with the gate off", pr(9, func(p *prs.PR) { p.Parent = &prs.Parent{Number: 8} }), prs.Gates{CI: true}, WaitNext},
		{"stacked outranks failing checks", pr(9, func(p *prs.PR) {
			p.Parent = &prs.Parent{Number: 8}
			p.CI = prs.CIFailing
		}), bothGates, WaitStacked},
		{"nothing new", pr(9, func(p *prs.PR) { p.Engagement = prs.Seen }), bothGates, WaitSeen},
		{"nothing new on a stacked PR", pr(9, func(p *prs.PR) {
			p.Engagement = prs.Seen
			p.Parent = &prs.Parent{Number: 8}
		}), bothGates, WaitSeen},
		{"approved and hidden from the sweep", pr(9, func(p *prs.PR) { p.Hidden = prs.HiddenApproved }), bothGates, WaitApproved},
		{"approved, shown, and nothing new", pr(9, func(p *prs.PR) {
			p.Decision = prs.Approved
			p.Engagement = prs.Seen
		}), bothGates, WaitApproved},
		{"approved, shown, and changed since", pr(9, func(p *prs.PR) { p.Decision = prs.Approved }), bothGates, WaitNext},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rows := Rows(prs.Listing{PRs: []prs.PR{c.pr}}, c.gates, nil)
			if len(rows) != 1 || rows[0].Wait != c.want {
				t.Errorf("Rows = %+v, want one row with wait %d", rows, c.want)
			}
		})
	}
}

func numbers(rows []Row) []int {
	out := make([]int, len(rows))
	for i, row := range rows {
		out[i] = row.PR.Number
	}
	return out
}

func TestRowsLeaveOutWhatTheSweepNeverReviews(t *testing.T) {
	listing := prs.Listing{PRs: []prs.PR{
		pr(9, nil),
		pr(5, func(p *prs.PR) { p.Hidden = prs.HiddenApproved }),
		pr(4, func(p *prs.PR) { p.Hidden = prs.HiddenOwn }),
		pr(3, func(p *prs.PR) { p.Hidden = prs.HiddenBot }),
		pr(2, func(p *prs.PR) { p.Hidden = prs.HiddenDraft }),
	}}
	if got, want := numbers(Rows(listing, bothGates, nil)), []int{9, 5}; !slices.Equal(got, want) {
		t.Errorf("Rows = %v, want %v: approved PRs are shown, drafts, your own and bots are not", got, want)
	}
}

func TestRowsPutWhatTheRunActsOnFirst(t *testing.T) {
	listing := prs.Listing{PRs: []prs.PR{
		pr(1, func(p *prs.PR) { p.Hidden = prs.HiddenApproved }),
		pr(2, func(p *prs.PR) { p.Engagement = prs.Seen }),
		pr(3, func(p *prs.PR) { p.Parent = &prs.Parent{Number: 4} }),
		pr(4, func(p *prs.PR) { p.CI = prs.CIPending }),
		pr(5, nil),
		pr(6, nil),
	}}
	before := numbers(Rows(listing, bothGates, nil))
	if want := []int{5, 6, 4, 3, 2, 1}; !slices.Equal(before, want) {
		t.Errorf("Rows = %v, want %v: ready, held by checks, stacked, seen, approved", before, want)
	}
	if got, want := listing.PRs[0].Number, 1; got != want {
		t.Errorf("Rows reordered its argument: the first PR is now #%d", got)
	}
}

func TestRowsKeepTheSweepOrderWithinOneState(t *testing.T) {
	listing := prs.Listing{PRs: []prs.PR{pr(7, nil), pr(9, nil), pr(8, nil)}}
	if got, want := numbers(Rows(listing, bothGates, nil)), []int{7, 9, 8}; !slices.Equal(got, want) {
		t.Errorf("Rows = %v, want %v", got, want)
	}
}

func TestANamedPRIsReviewedWhateverItsState(t *testing.T) {
	listing := prs.Listing{PRs: []prs.PR{
		pr(9, nil),
		pr(8, func(p *prs.PR) { p.Engagement = prs.Seen }),
		pr(7, func(p *prs.PR) { p.CI = prs.CIFailing }),
		pr(5, func(p *prs.PR) { p.Hidden = prs.HiddenApproved }),
		pr(4, func(p *prs.PR) { p.Hidden = prs.HiddenOwn }),
	}}
	rows := Rows(listing, bothGates, []int{8, 7, 5, 4})

	if got, want := numbers(rows), []int{8, 7, 5}; !slices.Equal(got, want) {
		t.Fatalf("Rows = %v, want %v: only the named PRs, and never your own", got, want)
	}
	for _, row := range rows {
		if row.Wait != WaitNext {
			t.Errorf("PR #%d has wait %d, want WaitNext: a person asked for it", row.PR.Number, row.Wait)
		}
	}
}
