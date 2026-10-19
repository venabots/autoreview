package engine

import (
	"errors"
	"slices"
	"testing"
	"testing/synctest"
)

// endedTask runs a fix on PR #4 to its end. Its worktree is then given
// back, as a task that pushed everything leaves it.
func endedTask(t *testing.T, f *trees) *bench {
	t.Helper()
	b := newBench(t, held(setup{deps: f.deps}))
	b.eng.Do(fixOf(4))
	synctest.Wait()
	b.reviewer.finish(t, 4, 0, "done")
	synctest.Wait()
	return b
}

func (f *trees) reopens() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.reopened)
}

func TestAnEndedTaskGetsItsWorktreeMadeAgainAndKept(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		f := &trees{}
		b := endedTask(t, f)
		b.eng.Do(ReopenTask{PR: 4})
		synctest.Wait()

		job := taskOf(t, b, 4)
		if job.Reopening || job.Reopens != 1 || job.ReopenError != "" {
			t.Errorf("after the reopen: reopening %v, reopens %d, error %q", job.Reopening, job.Reopens, job.ReopenError)
		}
		if job.Worktree != "/run/worktrees/pr-4" || job.Kept != KeptToResume {
			t.Errorf("worktree %q kept %q, want /run/worktrees/pr-4 kept %q", job.Worktree, job.Kept, KeptToResume)
		}
		if f.reopens() != 1 || f.reopened[0].Branch != "me/work" {
			t.Errorf("Reopen had %+v", f.reopened)
		}
		if want := "made PR #4's worktree again at /run/worktrees/pr-4, to resume its session"; !slices.Contains(b.notes(), want) {
			t.Errorf("notes = %q, want %q", b.notes(), want)
		}

		b.eng.Do(Quit{})
		if result := <-b.results; result.Code != ExitOK {
			t.Errorf("code %d", result.Code)
		}
		if _, released := f.state(); len(released) != 1 {
			t.Errorf("worktrees given back: %v; the one made again is the person's", released)
		}
	})
}

func TestASecondReopenWhileOneIsUnderWayStartsNothing(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		f := &trees{}
		b := endedTask(t, f)
		hold := make(chan struct{})
		f.mu.Lock()
		f.reopenHold = hold
		f.mu.Unlock()
		b.eng.Do(ReopenTask{PR: 4})
		synctest.Wait()
		if job := taskOf(t, b, 4); !job.Reopening {
			t.Errorf("the task does not say its worktree is being made: %+v", job)
		}
		b.eng.Do(ReopenTask{PR: 4})
		synctest.Wait()
		close(hold)
		synctest.Wait()

		if n := f.reopens(); n != 1 {
			t.Errorf("Reopen ran %d times, want 1", n)
		}
		b.eng.Do(Quit{})
		<-b.results
	})
}

func TestAWorktreeThatCannotBeMadeAgainSaysWhy(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		why := "PR #4's branch me/work is checked out at /home/me/work, so its worktree cannot be made again at /run/worktrees/pr-4"
		f := &trees{reopenErr: errors.New(why)}
		b := endedTask(t, f)
		b.eng.Do(ReopenTask{PR: 4})
		synctest.Wait()

		job := taskOf(t, b, 4)
		if job.Reopening || job.Reopens != 1 || job.ReopenError != why || job.Kept != "" {
			t.Errorf("after the failed reopen: %+v", job)
		}
		if want := "not resuming PR #4's task: " + why; !slices.Contains(b.notes(), want) {
			t.Errorf("notes = %q, want %q", b.notes(), want)
		}
		b.eng.Do(Quit{})
		<-b.results
	})
}

func TestReopenIsRefusedWhenThereIsNothingToMakeAgain(t *testing.T) {
	cases := []struct {
		name string
		kept string
		ask  func(t *testing.T, f *trees) *bench
		want string
	}{
		{"a PR with no task", "", func(t *testing.T, f *trees) *bench {
			return newBench(t, held(setup{deps: f.deps}))
		}, "not making PR #4's worktree again: it has had no task in this run"},
		{"a task still running", "", func(t *testing.T, f *trees) *bench {
			b := newBench(t, held(setup{deps: f.deps}))
			b.eng.Do(fixOf(4))
			synctest.Wait()
			return b
		}, "not making PR #4's worktree again: its task is still running"},
		{"a worktree that was kept", "uncommitted changes", endedTask,
			"not making PR #4's worktree again: it is still there"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				f := &trees{kept: c.kept}
				b := c.ask(t, f)
				b.eng.Do(ReopenTask{PR: 4})
				synctest.Wait()
				if n := f.reopens(); n != 0 {
					t.Errorf("Reopen ran %d times", n)
				}
				if !slices.Contains(b.notes(), c.want) {
					t.Errorf("notes = %q, want %q", b.notes(), c.want)
				}
				b.eng.Do(Quit{})
				<-b.results
			})
		})
	}
}

func TestATaskIsRefusedWhileItsWorktreeIsBeingMadeAgain(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		f := &trees{}
		b := endedTask(t, f)
		hold := make(chan struct{})
		f.mu.Lock()
		f.reopenHold = hold
		f.mu.Unlock()
		b.eng.Do(ReopenTask{PR: 4})
		synctest.Wait()
		b.eng.Do(fixOf(4))
		synctest.Wait()

		if made, _ := f.state(); len(made) != 1 {
			t.Errorf("worktrees made: %v; a task started while its worktree was being made again", made)
		}
		if want := "not fixing PR #4: its worktree is being made again"; !slices.Contains(b.notes(), want) {
			t.Errorf("notes = %q, want %q", b.notes(), want)
		}
		close(hold)
		synctest.Wait()
		b.eng.Do(Quit{})
		<-b.results
	})
}

func TestATaskAfterAReopenKeepsTheWorktreeMadeAgain(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		f := &trees{}
		b := endedTask(t, f)
		b.eng.Do(ReopenTask{PR: 4})
		synctest.Wait()
		b.eng.Do(fixOf(4))
		synctest.Wait()

		f.mu.Lock()
		asked := slices.Clone(f.asked)
		f.mu.Unlock()
		if len(asked) != 2 || asked[0].KeepWorktree || !asked[1].KeepWorktree {
			t.Errorf("Prepare had %+v; want only the task after the reopen to keep the worktree", asked)
		}
		b.reviewer.finish(t, 4, 0, "done")
		synctest.Wait()
		b.eng.Do(Quit{})
		<-b.results
	})
}
