package queue

import (
	"testing"
	"time"

	"github.com/venabots/autoreview/internal/prs"
)

// A review with no verdict leaves the PR due, so a provider that stays down
// must not get it reviewed at every look. It rests and it is capped as any
// other PR this run reviewed.
func TestAnUndecidedPRIsDueButRestsAndIsCappedLikeAnyOther(t *testing.T) {
	pr := prs.PR{Number: 9, Head: "sha1", Engagement: prs.Undecided}
	if w := waitFor(pr, prs.Gates{}, false); w != WaitNext {
		t.Fatalf("an undecided PR waits as %d, want next", w)
	}
	row := Row{PR: pr, Wait: WaitNext}
	track := Track{}.Reviewed(9, "sha1", t0, false, watching)
	if got := Gated([]Row{row}, track, watching, t0.Add(10*time.Minute)); got[0].Wait != WaitResting {
		t.Errorf("ten minutes after its review the wait is %d, want resting", got[0].Wait)
	}
	for range watching.MaxPasses - 1 {
		track = track.Reviewed(9, "sha1", t0, false, watching)
	}
	if got := Gated([]Row{row}, track, watching, t0.Add(time.Hour)); got[0].Wait != WaitCapped {
		t.Errorf("after %d reviews the wait is %d, want capped", watching.MaxPasses, got[0].Wait)
	}
}
