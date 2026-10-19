package engine

import (
	"slices"
	"testing"
	"testing/synctest"
	"time"

	"github.com/venabots/autoreview/internal/mine"
	"github.com/venabots/autoreview/internal/task"
)

func TestFixAllStartsOneFixOnEachPRThatNeedsWorkAndNeverAnother(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		f, m := &trees{}, &mineList{}
		clean := threaded(3)
		clean.ThreadIDs = nil
		approved := threaded(5)
		approved.Review = mine.ReviewApproved
		m.set(threaded(4), clean, approved)
		b := newBench(t, held(setup{deps: m.deps(f), edit: func(c *Config) { watchedEvery(c); c.Jobs = 2 }}))
		synctest.Wait()

		b.eng.Do(FixAll{})
		synctest.Wait()
		if job := taskOf(t, b, 4); job.Task != task.Fix {
			t.Errorf("the task on PR #4: %+v, want a fix", job)
		}
		if job := taskOf(t, b, 5); job.Task != task.Fix {
			t.Errorf("the task on the approved PR #5: %+v, want a fix: F is pressed by a person", job)
		}
		if got := b.reviewer.started(); len(got) != 2 || !slices.Contains(got, 4) || !slices.Contains(got, 5) {
			t.Errorf("started %v, want PRs #4 and #5", got)
		}
		if !slices.Contains(b.notes(), "fixing 2 PRs of yours") {
			t.Errorf("notes = %q", b.notes())
		}

		b.eng.Do(FixAll{})
		synctest.Wait()
		b.reviewer.finish(t, 4, 0, "done")
		b.reviewer.finish(t, 5, 0, "done")
		synctest.Wait()
		time.Sleep(3 * time.Minute)
		synctest.Wait()
		if got := b.reviewer.started(); len(got) != 2 {
			t.Errorf("started %v: F with a fix running, or a later look, started another", got)
		}
		b.eng.Do(Quit{})
		<-b.results
	})
}

func TestFixAllIsRefusedWhereAFixWouldBe(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		f, m := &trees{}, &mineList{}
		m.set(threaded(4))
		b := newBench(t, held(setup{deps: m.deps(f), edit: func(c *Config) { watchedEvery(c); c.NoPost = true }}))
		synctest.Wait()
		b.eng.Do(FixAll{})
		synctest.Wait()
		if got := b.reviewer.started(); len(got) != 0 {
			t.Errorf("started %v under --no-post", got)
		}
		if want := "not fixing your PRs: --no-post leaves every PR alone"; !slices.Contains(b.notes(), want) {
			t.Errorf("notes = %q, want %q", b.notes(), want)
		}
		b.eng.Do(Quit{})
		<-b.results
	})
}

// A fix a person asks for runs on an approved PR. Only autofix holds back
// there, because nobody asked it to push.
func TestAFixAskedForByHandRunsOnAnApprovedPR(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		f, m := &trees{}, &mineList{}
		approved := threaded(4)
		approved.Review = mine.ReviewApproved
		m.set(approved)
		b := newBench(t, held(setup{deps: m.deps(f), edit: watchedEvery}))
		synctest.Wait()

		b.eng.Do(fixOf(4))
		synctest.Wait()
		if job := taskOf(t, b, 4); job.Task != task.Fix || job.State != JobRunning {
			t.Errorf("the task on the approved PR #4: %+v, want a running fix", job)
		}
		b.reviewer.finish(t, 4, 0, "done")
		synctest.Wait()
		b.eng.Do(Quit{})
		<-b.results
	})
}
