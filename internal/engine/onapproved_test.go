package engine

import (
	"slices"
	"testing"
	"testing/synctest"
	"time"

	"github.com/venabots/autoreview/internal/babysit"
	"github.com/venabots/autoreview/internal/mine"
)

// A review can approve and leave polish in threads. With the setting at
// comments, autofix fixes those threads, and the view hears the setting.
func TestTheSettingLetsAutofixFixTheThreadsOnAnApprovedPR(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		f, m := &trees{}, &mineList{}
		approved := threaded(4)
		approved.Review = mine.ReviewApproved
		m.set(approved)
		comments := func(c *Config) {
			watchedEvery(c)
			c.AutofixApproved = babysit.ApprovedComments
		}
		b := newBench(t, held(setup{deps: m.deps(f), edit: comments}))
		b.eng.Do(Babysit{Change: babysit.One(4, true)})
		synctest.Wait()

		if got := b.reviewer.started(); !slices.Equal(got, []int{4}) {
			t.Errorf("started %v: the threads on an approved PR were not fixed", got)
		}
		if got := b.eng.Snapshot().AutofixApproved; got != babysit.ApprovedComments {
			t.Errorf("the snapshot says %s, want comments", got)
		}
		if notes := b.notes(); !slices.Contains(notes, "autofix: fixing PR #4, which changed and needs work") {
			t.Errorf("the fix was not noted: %q", notes)
		}
		time.Sleep(time.Minute)
		b.eng.Do(Quit{})
		<-b.results
	})
}
