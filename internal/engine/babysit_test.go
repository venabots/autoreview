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
	"github.com/venabots/autoreview/internal/prs"
	"github.com/venabots/autoreview/internal/task"
)

// mineList is your PRs as each look finds them, changed by a test between
// looks.
type mineList struct {
	mu   sync.Mutex
	list []mine.PR
	err  error
}

func (m *mineList) fail(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.err = err
}

func (m *mineList) set(list ...mine.PR) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.list = list
}

func (m *mineList) deps(f *trees) func(*Deps) {
	return func(d *Deps) {
		f.deps(d)
		d.Mine = func(context.Context) ([]mine.PR, error) {
			m.mu.Lock()
			defer m.mu.Unlock()
			if m.err != nil {
				return nil, m.err
			}
			return slices.Clone(m.list), nil
		}
	}
}

// threaded is one of your PRs with one open review thread.
func threaded(n int) mine.PR {
	return mine.PR{
		Number: n, Title: "My own work", Branch: "me/work", Head: "sha1", UpdatedAt: "2026-10-07T10:00:00Z",
		Review: mine.ReviewRequired, Merge: mine.MergeClean, CI: prs.CIPassing, ThreadIDs: []string{"t1"},
	}
}

func watchedEvery(c *Config) { c.Watch = time.Minute }

func TestBabysittingFixesAPRThatNeedsWorkAtOnceAndNotAgainUntilItChanges(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		f, m := &trees{}, &mineList{}
		m.set(threaded(4))
		b := newBench(t, held(setup{deps: m.deps(f), edit: watchedEvery}))
		b.eng.Do(Babysit{Change: babysit.One(4, true)})
		synctest.Wait()

		if !b.eng.Snapshot().Babysit.Babysat(4) {
			t.Error("the snapshot does not say PR #4 is babysat")
		}
		if job := taskOf(t, b, 4); job.Task != task.Fix || job.State != JobRunning {
			t.Fatalf("the fix: %+v", job)
		}
		notes := b.notes()
		for _, want := range []string{
			"babysitting PR #4: it is fixed when it changes and needs work",
			"babysitting: fixing PR #4, which changed and needs work",
		} {
			if !slices.Contains(notes, want) {
				t.Errorf("notes = %q, want %q", notes, want)
			}
		}

		b.reviewer.finish(t, 4, 0, "done")
		synctest.Wait()
		m.set(threaded(4))
		time.Sleep(3 * time.Minute)
		synctest.Wait()
		if got := b.reviewer.started(); !slices.Equal(got, []int{4}) {
			t.Errorf("started %v: a PR that did not change was fixed again", got)
		}

		changed := threaded(4)
		changed.Head, changed.ThreadIDs = "sha2", []string{"t1", "t2"}
		m.set(changed)
		time.Sleep(time.Minute)
		synctest.Wait()
		if got := b.reviewer.started(); !slices.Equal(got, []int{4, 4}) {
			t.Errorf("started %v: a PR with a new thread was not fixed", got)
		}
		b.eng.Do(Quit{})
		<-b.results
	})
}

func TestBabysittingLeavesAnApprovedPRAloneAndSaysSoOnce(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		f, m := &trees{}, &mineList{}
		approved := threaded(4)
		approved.Review = mine.ReviewApproved
		m.set(approved)
		b := newBench(t, held(setup{deps: m.deps(f), edit: watchedEvery}))
		b.eng.Do(Babysit{Change: babysit.One(4, true)})
		synctest.Wait()
		time.Sleep(3 * time.Minute)
		synctest.Wait()

		if got := b.reviewer.started(); len(got) != 0 {
			t.Fatalf("started %v: an approved PR was fixed", got)
		}
		if !b.eng.Snapshot().Babysit.OnHold(4) {
			t.Error("the snapshot does not say PR #4 is on hold")
		}
		const said = "babysitting: PR #4 is approved, so it gets no fix until that changes"
		if n := len(slices.DeleteFunc(b.notes(), func(note string) bool { return note != said })); n != 1 {
			t.Errorf("the hold was noted %d times in %q, want once", n, b.notes())
		}

		asked := threaded(4)
		asked.Review = mine.ReviewChangesRequested
		m.set(asked)
		time.Sleep(time.Minute)
		synctest.Wait()
		if got := b.reviewer.started(); !slices.Equal(got, []int{4}) {
			t.Errorf("started %v: a PR that lost its approval was not fixed", got)
		}
		b.eng.Do(Quit{})
		<-b.results
	})
}

func TestThePushOfACommentFixByHandIsNotAChangeOnABabysatPR(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		f, m := &trees{}, &mineList{}
		m.set(threaded(4))
		b := newBench(t, held(setup{deps: m.deps(f), edit: watchedEvery}))
		b.eng.Do(Babysit{Change: babysit.One(4, true)})
		synctest.Wait()
		b.reviewer.finish(t, 4, 0, "done")
		synctest.Wait()

		b.eng.Do(RunTask{Request: task.Request{PR: 4, Kind: task.Comments, Branch: "me/work"}})
		synctest.Wait()
		tasks := b.eng.Snapshot().Tasks
		if job := tasks[len(tasks)-1]; job.Task != task.Comments || job.State != JobRunning {
			t.Fatalf("the comment fix: %+v", job)
		}
		b.reviewer.finish(t, 4, 0, "done")
		synctest.Wait()
		answered := threaded(4)
		answered.Head = "sha-comments"
		m.set(answered)
		time.Sleep(3 * time.Minute)
		synctest.Wait()
		if got := b.reviewer.started(); !slices.Equal(got, []int{4, 4}) {
			t.Errorf("started %v: the comment fix's own push was fixed again", got)
		}
		b.eng.Do(Quit{})
		<-b.results
	})
}

func TestBabysittingIsRefusedWhereAFixWouldBe(t *testing.T) {
	fork := threaded(4)
	fork.CrossRepo = true
	cases := []struct {
		name string
		edit func(*Config)
		f    *trees
		list []mine.PR
		want string
	}{
		{"under --no-post", func(c *Config) { c.NoPost = true }, &trees{}, []mine.PR{threaded(4)},
			"not babysitting PR #4: --no-post leaves every PR alone"},
		{"when the skill is missing", nil, &trees{missing: "babysit-pr is not installed where claude looks (~/.claude/skills)"}, []mine.PR{threaded(4)},
			"not babysitting PR #4: babysit-pr is not installed where claude looks (~/.claude/skills)"},
		{"for a fork", nil, &trees{}, []mine.PR{fork},
			"PR #4 is from a fork; its branch is not on origin, so nothing can fix it here"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				m := &mineList{}
				m.set(c.list...)
				b := newBench(t, held(setup{deps: m.deps(c.f), edit: c.edit}))
				b.eng.Do(Babysit{Change: babysit.One(4, true)})
				synctest.Wait()
				if b.eng.Snapshot().Babysit.Any() || len(b.eng.Snapshot().Tasks) != 0 {
					t.Error("a refused change babysat something")
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

func TestBabysittingAllRefusedNamesNoPR(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		m := &mineList{}
		b := newBench(t, held(setup{deps: m.deps(&trees{}), edit: func(c *Config) { c.NoPost = true }}))
		b.eng.Do(Babysit{Change: babysit.All(true)})
		synctest.Wait()
		if !slices.Contains(b.notes(), "not babysitting your PRs: --no-post leaves every PR alone") {
			t.Errorf("notes = %q", b.notes())
		}
		b.eng.Do(Quit{})
		<-b.results
	})
}

func TestStoppingBabysittingLeavesARunningFixAlone(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		f, m := &trees{}, &mineList{}
		m.set(threaded(4))
		b := newBench(t, held(setup{deps: m.deps(f), edit: watchedEvery}))
		b.eng.Do(Babysit{Change: babysit.All(true)})
		synctest.Wait()
		b.eng.Do(Babysit{Change: babysit.All(false)})
		synctest.Wait()
		if b.eng.Snapshot().Babysit.Any() {
			t.Error("B off left something babysat")
		}
		if job := taskOf(t, b, 4); job.State != JobRunning {
			t.Errorf("the fix is in state %d; stopping babysitting stopped it", job.State)
		}
		if !slices.Contains(b.notes(), "no longer babysitting your PRs") {
			t.Errorf("notes = %q", b.notes())
		}
		b.eng.Do(Quit{})
		<-b.results
	})
}

func TestAFailedReadOfYourPRsIsANoteAndKeepsTheLastList(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		m := &mineList{}
		m.set(threaded(4))
		b := newBench(t, held(setup{deps: m.deps(&trees{}), edit: watchedEvery}))
		m.fail(errors.New("gh timed out"))
		time.Sleep(time.Minute)
		synctest.Wait()
		snap := b.eng.Snapshot()
		if len(snap.Mine) != 1 || snap.MineErr != "gh timed out" {
			t.Errorf("after a failed read: %d PRs, error %q", len(snap.Mine), snap.MineErr)
		}
		if want := "could not read your PRs (gh timed out); the My PRs tab keeps its last list"; !slices.Contains(b.notes(), want) {
			t.Errorf("notes = %q, want %q", b.notes(), want)
		}
		b.eng.Do(Quit{})
		<-b.results
	})
}
