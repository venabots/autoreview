package engine

import (
	"slices"
	"testing"
	"testing/synctest"

	"github.com/venabots/autoreview/internal/task"
)

func autofixedEvery(c *Config) {
	watchedEvery(c)
	c.Autofix = true
}

func TestARunThatAutofixesFixesAPRThatNeedsWorkWithNoKey(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		f, m := &trees{}, &mineList{}
		m.set(threaded(4))
		b := newBench(t, held(setup{deps: m.deps(f), edit: autofixedEvery}))
		synctest.Wait()

		if !b.eng.Snapshot().Babysit.All() {
			t.Error("the snapshot does not say all your PRs are autofixed")
		}
		if job := taskOf(t, b, 4); job.Task != task.Fix || job.State != JobRunning {
			t.Fatalf("the fix: %+v", job)
		}
		notes := b.notes()
		for _, want := range []string{
			"autofixing all your PRs, and any you open: each is fixed when it changes and needs work",
			"autofix: fixing PR #4, which changed and needs work",
		} {
			if !slices.Contains(notes, want) {
				t.Errorf("notes = %q, want %q", notes, want)
			}
		}
		b.eng.Do(Quit{})
		<-b.results
	})
}

func TestARunThatAutofixesWhereAFixIsRefusedSaysWhyAndFixesNothing(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		m := &mineList{}
		m.set(threaded(4))
		noPost := func(c *Config) {
			autofixedEvery(c)
			c.NoPost = true
		}
		b := newBench(t, held(setup{deps: m.deps(&trees{}), edit: noPost}))
		synctest.Wait()

		if b.eng.Snapshot().Babysit.Any() || len(b.eng.Snapshot().Tasks) != 0 {
			t.Error("a refused autofix babysat something")
		}
		if want := "not autofixing your PRs: --no-post leaves every PR alone"; !slices.Contains(b.notes(), want) {
			t.Errorf("notes = %q, want %q", b.notes(), want)
		}
		b.eng.Do(Quit{})
		<-b.results
	})
}

func TestARunThatDoesNotAutofixFixesNothingByItself(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		m := &mineList{}
		m.set(threaded(4))
		b := newBench(t, held(setup{deps: m.deps(&trees{}), edit: watchedEvery}))
		synctest.Wait()

		if b.eng.Snapshot().Babysit.Any() || len(b.eng.Snapshot().Tasks) != 0 {
			t.Error("a run without autofix fixed a PR by itself")
		}
		b.eng.Do(Quit{})
		<-b.results
	})
}
