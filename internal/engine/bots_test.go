package engine

import (
	"slices"
	"testing"
	"testing/synctest"
	"time"

	"github.com/venabots/autoreview/internal/mine"
	"github.com/venabots/autoreview/internal/prs"
)

// bump is a Dependabot PR whose build fails.
func bump(n int) mine.PR {
	return mine.PR{
		Number: n, Title: "chore(deps): bump left-pad", Branch: "dependabot/npm_and_yarn/left-pad-2.0.0", Head: "sha1",
		UpdatedAt: "2026-10-07T10:00:00Z", Review: mine.ReviewRequired, Merge: mine.MergeClean, CI: prs.CIFailing, Bot: true,
	}
}

func mineNumbers(list []mine.PR) []int {
	out := make([]int, len(list))
	for i, pr := range list {
		out[i] = pr.Number
	}
	return out
}

func TestTheMaintainTabHoldsNoBotsPRsUntilTheyAreIncluded(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		f, m := &trees{}, &mineList{}
		m.set(threaded(4))
		m.setBots(bump(12))
		b := newBench(t, held(setup{deps: m.deps(f), edit: watchedEvery}))
		synctest.Wait()

		snap := b.eng.Snapshot()
		if snap.MineBots || !slices.Equal(mineNumbers(snap.Mine), []int{4}) {
			t.Errorf("at the start: MineBots %v, Mine %v; want only your PR #4", snap.MineBots, mineNumbers(snap.Mine))
		}
		b.eng.Do(Quit{})
		<-b.results
	})
}

func TestIncludingBotsPRsReadsThemAtOnceAndLeavingThemOutDropsThem(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		f, m := &trees{}, &mineList{}
		m.set(threaded(4))
		m.setBots(bump(12))
		b := newBench(t, held(setup{deps: m.deps(f), edit: watchedEvery}))
		synctest.Wait()

		b.eng.Do(IncludeBots{On: true})
		synctest.Wait()
		snap := b.eng.Snapshot()
		if !snap.MineBots || !slices.Contains(mineNumbers(snap.Mine), 12) {
			t.Errorf("after including them: MineBots %v, Mine %v; want PR #12 listed at once", snap.MineBots, mineNumbers(snap.Mine))
		}

		b.eng.Do(IncludeBots{On: false})
		synctest.Wait()
		snap = b.eng.Snapshot()
		if snap.MineBots || slices.Contains(mineNumbers(snap.Mine), 12) {
			t.Errorf("after leaving them out: MineBots %v, Mine %v; want PR #12 gone", snap.MineBots, mineNumbers(snap.Mine))
		}
		b.eng.Do(Quit{})
		<-b.results
	})
}

func TestEveryLookAsksForBotsPRsWhileTheyAreIncluded(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		f, m := &trees{}, &mineList{}
		m.set(threaded(4))
		m.setBots(bump(12))
		b := newBench(t, held(setup{deps: m.deps(f), edit: watchedEvery}))
		synctest.Wait()
		b.eng.Do(IncludeBots{On: true})
		synctest.Wait()

		before := m.botReads()
		time.Sleep(time.Minute)
		synctest.Wait()
		if m.botReads() <= before {
			t.Error("the next look did not ask for the bots' PRs while they are included")
		}
		b.eng.Do(Quit{})
		<-b.results
	})
}
