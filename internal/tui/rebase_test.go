package tui

import (
	"slices"
	"testing"

	"github.com/venabots/autoreview/internal/prs"
	"github.com/venabots/autoreview/internal/queue"
	"github.com/venabots/autoreview/internal/rebase"
)

func rebaseRow(state rebase.State) queue.Row {
	return queue.Row{
		PR:     prs.PR{Number: 3, Title: "Bump lodash", Author: "dependabot", Branch: "dependabot/npm/lodash", Base: "main", Conflicts: true},
		Wait:   queue.WaitRebase,
		Rebase: state,
	}
}

func TestADependabotPRThatWaitsOnARebaseSaysWhoItWaitsForAndWhy(t *testing.T) {
	cases := []struct {
		state  rebase.State
		row    string
		reason string
	}{
		{rebase.Asked, "#3 · asked Dependabot to rebase", "waits for Dependabot to rebase it: it conflicts with main. Its next push is reviewed as usual"},
		{rebase.AskedBefore, "#3 · asked Dependabot to rebase", "waits for Dependabot to rebase it: it conflicts with main. Its next push is reviewed as usual"},
		{rebase.WouldAsk, "#3 · would ask Dependabot to rebase", "a run that posts would ask Dependabot to rebase it: it conflicts with main (--no-post)"},
		{rebase.Checking, "#3 · checking for a rebase", "checking whether Dependabot should rebase it"},
		{rebase.OthersCommitted, "#3 · needs a person", "needs a person: somebody other than Dependabot committed to it, so Dependabot will not rebase it"},
	}
	for _, c := range cases {
		m, hs := newModel(t, 200, 30)
		snap := snapshot()
		snap.Reviews = []queue.Row{rebaseRow(c.state)}
		hs.eng.snap = snap
		m = send(m, snapshotMsg{snap: snap})
		wantOnScreen(t, m, c.row, c.reason)
	}
}

func TestARowThatWaitsOnARebaseSitsAfterTheStackedRowsAndBeforeTheRestingOnes(t *testing.T) {
	snap := snapshot()
	snap.Reviews = []queue.Row{
		{PR: prs.PR{Number: 9}, Wait: queue.WaitResting},
		rebaseRow(rebase.Asked),
		{PR: prs.PR{Number: 7, Parent: &prs.Parent{Number: 8}}, Wait: queue.WaitStacked},
	}
	if got := listOrder(snap); !slices.Equal(got, []int{7, 3, 9}) {
		t.Errorf("the list is %v, want [7 3 9]", got)
	}
}

func TestOnlyTheRowThatNeedsAPersonIsRed(t *testing.T) {
	cases := []struct {
		state rebase.State
		want  string
	}{
		{rebase.OthersCommitted, redStyle.Render("x")},
		{rebase.Asked, yellowStyle.Render("x")},
		{rebase.Checking, yellowStyle.Render("x")},
	}
	for _, c := range cases {
		if got := waitColor(rebaseRow(c.state)).Render("x"); got != c.want {
			t.Errorf("%v: the word is drawn as %q, want %q", c.state, got, c.want)
		}
	}
}
