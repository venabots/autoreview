package engine

import (
	"context"
	"errors"
	"slices"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/venabots/autoreview/internal/babysit"
	"github.com/venabots/autoreview/internal/mine"
)

// A fix changes its PR. A row that kept the state from before the fix until
// the next look would say for minutes that the fix did nothing.
func TestATaskThatEndsReadsYourPRsAgain(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		f, m := &trees{}, &mineList{}
		m.set(threaded(4))
		b := newBench(t, held(setup{deps: m.deps(f), edit: watchedEvery}))
		b.eng.Do(fixOf(4))
		synctest.Wait()

		fixed := threaded(4)
		fixed.Head, fixed.ThreadIDs = "sha-fix", nil
		m.set(fixed)
		b.reviewer.finish(t, 4, 0, "done")
		synctest.Wait()

		snap := b.eng.Snapshot()
		if got := snap.Mine[0]; got.Head != "sha-fix" || got.OpenThreads() != 0 {
			t.Errorf("your PR after the fix: %+v, want what the fix left", got)
		}
		if snap.Looking {
			t.Error("the read after a task counts as a look that has not answered")
		}
		b.eng.Do(Quit{})
		<-b.results
	})
}

func TestAFailedReadAfterATaskKeepsTheListAndSaysNothing(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		f, m := &trees{}, &mineList{}
		m.set(threaded(4))
		b := newBench(t, held(setup{deps: m.deps(f), edit: watchedEvery}))
		b.eng.Do(fixOf(4))
		synctest.Wait()

		m.fail(errors.New("HTTP 502"))
		b.reviewer.finish(t, 4, 0, "done")
		synctest.Wait()

		snap := b.eng.Snapshot()
		if snap.MineErr != "" || len(snap.Mine) != 1 || snap.Mine[0].Head != "sha1" {
			t.Errorf("after a failed read: MineErr %q, Mine %+v; want the last list and no error", snap.MineErr, snap.Mine)
		}
		b.eng.Do(Quit{})
		<-b.results
	})
}

// slowMine answers the first read after it is armed only when the test says
// so, with the list from before. The reads after it answer at once.
type slowMine struct {
	mu      sync.Mutex
	list    []mine.PR
	armed   bool
	release chan struct{}
}

func (s *slowMine) read(context.Context, bool) ([]mine.PR, error) {
	s.mu.Lock()
	list, slow := slices.Clone(s.list), s.armed
	s.armed = false
	s.mu.Unlock()
	if slow {
		<-s.release
	}
	return list, nil
}

func (s *slowMine) set(armed bool, list ...mine.PR) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.list, s.armed = list, armed
}

// A look that began before the fix pushed can answer after the read that
// followed the fix. Its list is the older one, and must not go back on the
// screen.
func TestAnOlderAnswerDoesNotReplaceANewerOne(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		f, m := &trees{}, &slowMine{release: make(chan struct{})}
		m.set(false, threaded(4))
		b := newBench(t, held(setup{edit: watchedEvery, deps: func(d *Deps) {
			f.deps(d)
			d.Mine = m.read
		}}))
		b.eng.Do(fixOf(4))
		synctest.Wait()

		m.set(true, threaded(4))
		time.Sleep(time.Minute)
		synctest.Wait()
		if !b.eng.Snapshot().Looking {
			t.Fatal("the look whose read is slow has already answered")
		}

		fixed := threaded(4)
		fixed.Head, fixed.ThreadIDs = "sha-fix", nil
		m.set(false, fixed)
		b.reviewer.finish(t, 4, 0, "done")
		synctest.Wait()
		close(m.release)
		synctest.Wait()

		snap := b.eng.Snapshot()
		if got := snap.Mine[0]; got.Head != "sha-fix" {
			t.Errorf("your PR is %+v: the older answer replaced the newer one", got)
		}
		if snap.Looking {
			t.Error("the look whose answer was dropped never ended")
		}
		b.eng.Do(Quit{})
		<-b.results
	})
}

// The read after a task is seconds after a push: checks have not started
// and GitHub has not worked out the merge. Babysitting compares looks with
// looks, so b on a PR starts from the last look and not from that read.
func TestBabysittingDoesNotTakeTheReadAfterATaskAsALook(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		f, m := &trees{}, &mineList{}
		clean := threaded(4)
		clean.ThreadIDs = nil
		m.set(clean)
		b := newBench(t, held(setup{deps: m.deps(f), edit: watchedEvery}))
		b.eng.Do(fixOf(4))
		synctest.Wait()

		m.set(threaded(4))
		b.reviewer.finish(t, 4, 0, "done")
		synctest.Wait()
		b.eng.Do(Babysit{Change: babysit.One(4, true)})
		synctest.Wait()
		if got := b.reviewer.started(); !slices.Equal(got, []int{4}) {
			t.Fatalf("started %v: babysitting fixed from the read after the task", got)
		}

		time.Sleep(time.Minute)
		synctest.Wait()
		if got := b.reviewer.started(); !slices.Equal(got, []int{4, 4}) {
			t.Errorf("started %v: the next look did not fix the PR", got)
		}
		b.eng.Do(Quit{})
		<-b.results
	})
}
