package queue

import (
	"testing"
	"time"

	"github.com/venabots/autoreview/internal/prs"
)

var t0 = time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)

func ready(number int, head string) Row {
	return Row{PR: prs.PR{Number: number, Head: head, Engagement: prs.Updated}}
}

var watching = Policy{MaxPasses: 3, Rest: 30 * time.Minute, PushResets: true}

func TestARunReviewsAPRAtMostItsCap(t *testing.T) {
	track := Track{}
	for range 3 {
		track = track.Reviewed(9, "sha1", t0, false, watching)
	}
	got := Gated([]Row{ready(9, "sha1")}, track, watching, t0.Add(time.Hour))
	if got[0].Wait != WaitCapped {
		t.Errorf("after three reviews the wait is %d, want capped", got[0].Wait)
	}
	twice := Track{}.Reviewed(9, "sha1", t0, false, watching).Reviewed(9, "sha1", t0, false, watching)
	if got := Gated([]Row{ready(9, "sha1")}, twice, watching, t0.Add(time.Hour)); got[0].Wait != WaitNext {
		t.Errorf("after two reviews the wait is %d, want ready", got[0].Wait)
	}
}

func TestOnlyANewHeadStartsTheCountAgain(t *testing.T) {
	track := Track{}
	for range 3 {
		track = track.Reviewed(9, "sha1", t0, false, watching)
	}
	later := t0.Add(time.Hour)
	if got := Gated([]Row{ready(9, "sha2")}, track, watching, later); got[0].Wait != WaitNext {
		t.Errorf("after a push the wait is %d, want ready", got[0].Wait)
	}
	if got := Gated([]Row{ready(9, "")}, track, watching, later); got[0].Wait != WaitCapped {
		t.Errorf("an unknown head reset the cap: wait %d", got[0].Wait)
	}
	noReset := Policy{MaxPasses: 3}
	if got := Gated([]Row{ready(9, "sha2")}, track, noReset, later); got[0].Wait != WaitCapped {
		t.Errorf("a push reset the cap in a run that does not reset on a push: wait %d", got[0].Wait)
	}
	if rec := track.Reviewed(9, "sha2", later, false, watching)[9]; rec.Passes != 1 {
		t.Errorf("a review after a push counts %d passes, want 1", rec.Passes)
	}
}

func TestAReviewedPRRestsBeforeItIsReviewedAgain(t *testing.T) {
	track := Track{}.Reviewed(9, "sha1", t0, false, watching)
	got := Gated([]Row{ready(9, "sha1")}, track, watching, t0.Add(10*time.Minute))
	if got[0].Wait != WaitResting || !got[0].Until.Equal(t0.Add(30*time.Minute)) {
		t.Errorf("ten minutes after its review: %+v, want resting until %v", got[0], t0.Add(30*time.Minute))
	}
	if got := Gated([]Row{ready(9, "sha1")}, track, watching, t0.Add(30*time.Minute)); got[0].Wait != WaitNext {
		t.Errorf("when the rest is over the wait is %d, want ready", got[0].Wait)
	}
}

func TestAPRThisRunApprovedStaysFinished(t *testing.T) {
	track := Track{}.Reviewed(9, "sha1", t0, true, watching).Reviewed(9, "sha2", t0, false, watching)
	if got := Gated([]Row{ready(9, "sha3")}, track, watching, t0.Add(time.Hour)); got[0].Wait != WaitApproved {
		t.Errorf("the wait is %d, want approved: the list lags the approval", got[0].Wait)
	}
}

func TestGatedLeavesAPRThatWaitsForSomethingElse(t *testing.T) {
	track := Track{}.Reviewed(9, "sha1", t0, false, watching)
	seen := Row{PR: prs.PR{Number: 9, Head: "sha1", Engagement: prs.Seen}, Wait: WaitSeen}
	if got := Gated([]Row{seen}, track, watching, t0); got[0].Wait != WaitSeen {
		t.Errorf("a seen PR became %d", got[0].Wait)
	}
}

func TestGatedSortsAndDoesNotChangeItsArgument(t *testing.T) {
	track := Track{}.Reviewed(9, "sha1", t0, false, watching)
	rows := []Row{ready(9, "sha1"), ready(8, "sha8")}
	got := Gated(rows, track, watching, t0)
	if got[0].PR.Number != 8 || got[1].Wait != WaitResting {
		t.Errorf("Gated = %+v, want the ready PR first", got)
	}
	if rows[0].Wait != WaitNext {
		t.Error("Gated changed its argument")
	}
	before := Track{}
	before.Reviewed(9, "sha1", t0, false, watching)
	if len(before) != 0 {
		t.Error("Reviewed changed the track it was called on")
	}
}

func TestWatchedIsAReviewedPRTheRunStillOwes(t *testing.T) {
	track := Track{}.Reviewed(9, "sha1", t0, false, watching)
	cases := []struct {
		name string
		rows []Row
		want bool
	}{
		{"reviewed and seen", []Row{{PR: prs.PR{Number: 9}, Wait: WaitSeen}}, true},
		{"reviewed and resting", []Row{{PR: prs.PR{Number: 9}, Wait: WaitResting}}, true},
		{"reviewed and approved", []Row{{PR: prs.PR{Number: 9}, Wait: WaitApproved}}, false},
		{"reviewed and capped", []Row{{PR: prs.PR{Number: 9}, Wait: WaitCapped}}, false},
		{"closed: gone from the list", []Row{{PR: prs.PR{Number: 8}, Wait: WaitNext}}, false},
	}
	for _, c := range cases {
		if got := Watched(c.rows, track); got != c.want {
			t.Errorf("%s: Watched = %v, want %v", c.name, got, c.want)
		}
	}
}
