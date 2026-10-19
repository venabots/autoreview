package engine

import (
	"context"
	"slices"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/venabots/autoreview/internal/prs"
	"github.com/venabots/autoreview/internal/queue"
	"github.com/venabots/autoreview/internal/rebase"
)

// staleBot is PR #9, a person's and ready, and PR #3, Dependabot's at head,
// which conflicts with main.
func staleBot(head string) prs.Listing {
	l := listing(9)
	return prs.Listing{PRs: append(l.PRs, prs.PR{
		Number: 3, Author: "dependabot", Base: "main", Head: head,
		Engagement: prs.New, CI: prs.CIPassing, Conflicts: true,
	})}
}

// asker is a fake Dependabot question. It answers with state after wait,
// and keeps the head of each PR it was asked about.
type asker struct {
	mu    sync.Mutex
	heads []string
	state rebase.State
	wait  time.Duration
}

func (a *asker) ask(_ context.Context, pr prs.PR) (rebase.State, error) {
	a.mu.Lock()
	a.heads = append(a.heads, pr.Head)
	a.mu.Unlock()
	time.Sleep(a.wait)
	return a.state, nil
}

func (a *asker) asked() []string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return slices.Clone(a.heads)
}

func rowOf(t *testing.T, s *Snapshot, pr int) queue.Row {
	t.Helper()
	for _, row := range s.Reviews {
		if row.PR.Number == pr {
			return row
		}
	}
	t.Fatalf("the review list has no PR #%d", pr)
	return queue.Row{}
}

func TestAStaleDependabotPRIsAskedToRebaseAndNotReviewed(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		a := &asker{state: rebase.Asked}
		b := newBench(t, setup{
			list: func() (prs.Listing, error) { return staleBot("sha3"), nil },
			deps: func(d *Deps) { d.Rebase = a.ask },
		})

		if got := b.reviewer.started(); !slices.Equal(got, []int{9}) {
			t.Errorf("reviews started: %v, want only #9", got)
		}
		row := rowOf(t, b.eng.Snapshot(), 3)
		if row.Wait != queue.WaitRebase || row.Rebase != rebase.Asked {
			t.Errorf("PR #3 waits %v with rebase %v, want WaitRebase and Asked", row.Wait, row.Rebase)
		}
		if !slices.Contains(b.notes(), "asked Dependabot to rebase PR #3: it conflicts with main") {
			t.Errorf("notes: %q", b.notes())
		}
		b.eng.Do(Quit{})
		<-b.results
	})
}

func TestTheFirstPassWaitsForTheRebaseQuestion(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		a := &asker{state: rebase.NotBehind, wait: 10 * time.Second}
		failing := staleBot("sha3")
		failing.PRs[1].Conflicts, failing.PRs[1].CI = false, prs.CIFailing
		b := newBench(t, setup{
			list: func() (prs.Listing, error) { return failing, nil },
			deps: func(d *Deps) { d.Rebase = a.ask },
		})
		if got := b.reviewer.started(); len(got) != 0 {
			t.Fatalf("reviews started %v before the question was answered", got)
		}

		time.Sleep(10 * time.Second)
		synctest.Wait()
		if got := b.reviewer.started(); !slices.Equal(got, []int{9}) {
			t.Errorf("reviews started: %v, want #9 once the look was answered", got)
		}
		if row := rowOf(t, b.eng.Snapshot(), 3); row.Wait != queue.WaitChecks {
			t.Errorf("PR #3 waits %v, want WaitChecks: its base has not moved, so it waits for its checks as before", row.Wait)
		}
		b.eng.Do(Quit{})
		<-b.results
	})
}

func TestARunAsksOnceForEachHeadOfADependabotPR(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		a := &asker{state: rebase.Asked}
		var mu sync.Mutex
		head := "sha3"
		b := newBench(t, setup{
			list: func() (prs.Listing, error) {
				mu.Lock()
				defer mu.Unlock()
				return staleBot(head), nil
			},
			edit: func(c *Config) {
				c.Watch, c.Loop, c.Watching = 2*time.Minute, LoopWatch, true
			},
			deps: func(d *Deps) { d.Rebase = a.ask },
		})

		time.Sleep(2 * time.Minute)
		synctest.Wait()
		if got := a.asked(); !slices.Equal(got, []string{"sha3"}) {
			t.Fatalf("asked about %v after two looks at one head, want [sha3]", got)
		}

		mu.Lock()
		head = "sha4"
		mu.Unlock()
		time.Sleep(2 * time.Minute)
		synctest.Wait()
		if got := a.asked(); !slices.Equal(got, []string{"sha3", "sha4"}) {
			t.Errorf("asked about %v, want [sha3 sha4]: Dependabot pushed a new head that still conflicts", got)
		}
		b.eng.Do(Quit{})
		<-b.results
	})
}

func TestARunWithNoRebaseQuestionReviewsADependabotPRAsBefore(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		b := newBench(t, setup{
			list: func() (prs.Listing, error) { return staleBot("sha3"), nil },
			jobs: 2,
		})
		if got := b.reviewer.started(); !slices.Equal(got, []int{9, 3}) {
			t.Errorf("reviews started: %v, want #9 and #3", got)
		}
		b.eng.Do(Quit{})
		<-b.results
	})
}
