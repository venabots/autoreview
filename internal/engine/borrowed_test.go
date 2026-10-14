package engine

import (
	"testing"
	"testing/synctest"
)

// A worktree of yours stays after the task, also when the task left
// nothing in it. The job must say so, or the view reads an empty Kept as
// a worktree that is gone, and r refuses to reopen the task there.
func TestATaskInYourOwnWorktreeSaysTheWorktreeIsYours(t *testing.T) {
	cases := []struct {
		name   string
		borrow bool
	}{
		{"a worktree the run made", false},
		{"a worktree of yours", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				f := &trees{borrow: c.borrow}
				b := newBench(t, held(setup{deps: f.deps}))
				b.eng.Do(fixOf(4))
				synctest.Wait()
				b.reviewer.finish(t, 4, 0, "done")
				synctest.Wait()

				job := taskOf(t, b, 4)
				if job.Yours != c.borrow || job.Kept != "" {
					t.Errorf("job = Yours %v, Kept %q; want Yours %v and nothing kept", job.Yours, job.Kept, c.borrow)
				}
				b.eng.Do(Quit{})
				<-b.results
			})
		})
	}
}
