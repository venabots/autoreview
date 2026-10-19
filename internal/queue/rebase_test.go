package queue

import (
	"testing"
	"time"

	"github.com/venabots/autoreview/internal/prs"
	"github.com/venabots/autoreview/internal/rebase"
)

func TestARebaseThatWaitsHoldsTheRowForItsHeadOnly(t *testing.T) {
	row := func(wait Wait) Row {
		return Row{PR: prs.PR{Number: 3, Author: "dependabot", Head: "sha3"}, Wait: wait}
	}
	cases := []struct {
		name  string
		row   Row
		asked Ask
		want  Wait
	}{
		{"asked about this head", row(WaitNext), Ask{Head: "sha3", State: rebase.Asked}, WaitRebase},
		{"still checking", row(WaitNext), Ask{Head: "sha3", State: rebase.Checking}, WaitRebase},
		{"held for its failing checks", row(WaitChecks), Ask{Head: "sha3", State: rebase.Asked}, WaitRebase},
		{"seen since you asked", row(WaitSeen), Ask{Head: "sha3", State: rebase.AskedBefore}, WaitRebase},
		{"a person committed to it", row(WaitNext), Ask{Head: "sha3", State: rebase.OthersCommitted}, WaitRebase},
		{"Dependabot pushed since", row(WaitNext), Ask{Head: "old", State: rebase.Asked}, WaitNext},
		{"its base has not moved", row(WaitChecks), Ask{Head: "sha3", State: rebase.NotBehind}, WaitChecks},
		{"GitHub could not be asked", row(WaitNext), Ask{Head: "sha3", State: rebase.Failed}, WaitNext},
		{"approved already", row(WaitApproved), Ask{Head: "sha3", State: rebase.Asked}, WaitApproved},
	}
	for _, c := range cases {
		got := Rebasing([]Row{c.row}, map[int]Ask{3: c.asked})
		if got[0].Wait != c.want {
			t.Errorf("%s: wait is %v, want %v", c.name, got[0].Wait, c.want)
		}
		if c.want == WaitRebase && got[0].Rebase != c.asked.State {
			t.Errorf("%s: the row's rebase is %v, want %v", c.name, got[0].Rebase, c.asked.State)
		}
	}
}

func TestRebasingDoesNotChangeTheRowsItIsGiven(t *testing.T) {
	rows := []Row{{PR: prs.PR{Number: 3, Head: "sha3"}, Wait: WaitNext}}
	_ = Rebasing(rows, map[int]Ask{3: {Head: "sha3", State: rebase.Asked}})
	if rows[0].Wait != WaitNext {
		t.Error("Rebasing changed the slice it was given")
	}
}

func TestARowThatWaitsOnDependabotIsNeverReviewedNext(t *testing.T) {
	rows := Rebasing([]Row{{PR: prs.PR{Number: 3, Head: "sha3"}, Wait: WaitNext}}, map[int]Ask{3: {Head: "sha3", State: rebase.Asked}})
	gated := Gated(rows, Track{}, Policy{}, time.Time{})
	if gated[0].Wait == WaitNext {
		t.Error("a PR that waits on Dependabot was left ready for review")
	}
}
