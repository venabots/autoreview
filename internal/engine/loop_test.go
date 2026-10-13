package engine

import (
	"errors"
	"slices"
	"strings"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/venabots/autoreview/internal/prs"
	"github.com/venabots/autoreview/internal/queue"
	"github.com/venabots/autoreview/internal/review"
)

// github is a PR list that a test changes between looks.
type github struct {
	mu   sync.Mutex
	prs  map[int]prs.PR
	fail bool
}

func newGithub(numbers ...int) *github {
	g := &github{prs: map[int]prs.PR{}}
	for _, n := range numbers {
		g.prs[n] = prs.PR{Number: n, Engagement: prs.New, CI: prs.CIPassing, Head: "sha1"}
	}
	return g
}

func (g *github) list() (prs.Listing, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.fail {
		return prs.Listing{}, errors.New("rate limited")
	}
	numbers := make([]int, 0, len(g.prs))
	for n := range g.prs {
		numbers = append(numbers, n)
	}
	slices.SortFunc(numbers, func(a, b int) int { return b - a })
	out := prs.Listing{}
	for _, n := range numbers {
		out.PRs = append(out.PRs, g.prs[n])
	}
	return out, nil
}

// set changes one PR.
func (g *github) set(n int, edit func(*prs.PR)) {
	g.mu.Lock()
	defer g.mu.Unlock()
	p := g.prs[n]
	edit(&p)
	g.prs[n] = p
}

func (g *github) close(n int) {
	g.mu.Lock()
	defer g.mu.Unlock()
	delete(g.prs, n)
}

func (g *github) failing(on bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.fail = on
}

func seen(p *prs.PR)    { p.Engagement = prs.Seen }
func updated(p *prs.PR) { p.Engagement = prs.Updated }

// ownWord is a GitHub that cannot be asked, so the verdict is the
// reviewer's own claim: a test chooses it with the answer it gives.
func ownWord(int) review.Readback { return review.Readback{Kind: review.ReadbackFailed} }

func watchRun(g *github, edit func(*Config)) setup {
	return setup{list: g.list, jobs: 2, verdict: ownWord, edit: func(c *Config) {
		c.Watch = 2 * time.Minute
		c.Loop = LoopWatch
		c.Watching = true
		c.Rest = 30 * time.Minute
		c.MaxPasses = 3
		if edit != nil {
			edit(c)
		}
	}}
}

func wait(t *testing.T, b *bench, pr int) queue.Wait {
	t.Helper()
	for _, row := range b.eng.Snapshot().Reviews {
		if row.PR.Number == pr {
			return row.Wait
		}
	}
	t.Fatalf("PR #%d is not on the list", pr)
	return 0
}

func TestAWatchingRunReviewsWhatBecomesReadyAtEachLook(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		g := newGithub(9)
		b := newBench(t, watchRun(g, nil))
		g.set(9, seen)
		b.reviewer.finish(t, 9, 0, answer(9, "commented"))
		synctest.Wait()
		if s := b.eng.Snapshot(); s.Phase != Idle || s.Pass != 1 {
			t.Fatalf("after the first pass: phase %d, pass %d", s.Phase, s.Pass)
		}

		g.set(8, func(p *prs.PR) { *p = prs.PR{Number: 8, Engagement: prs.New, CI: prs.CIPassing} })
		time.Sleep(2 * time.Minute)
		synctest.Wait()
		if got := b.reviewer.started(); !slices.Equal(got, []int{9, 8}) {
			t.Errorf("started %v, want the PR that opened since", got)
		}
		b.eng.Do(Quit{})
		if result := <-b.results; result.Code != ExitInterrupted {
			t.Errorf("code %d, want %d: a run that watches would have gone on", result.Code, ExitInterrupted)
		}
	})
}

func TestAReviewedPRRestsAndIsThenRecheckedInItsSession(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		g := newGithub(9)
		b := newBench(t, watchRun(g, nil))
		b.reviewer.finish(t, 9, 0, answer(9, "commented"))
		synctest.Wait()

		// The author answers at once. The PR is new to the run again, and
		// rests.
		g.set(9, updated)
		time.Sleep(2 * time.Minute)
		synctest.Wait()
		if got := wait(t, b, 9); got != queue.WaitResting {
			t.Fatalf("two minutes after its review PR #9 waits %d, want resting", got)
		}
		if len(b.reviewer.commandsFor(9)) != 1 {
			t.Fatal("a resting PR was reviewed again")
		}

		time.Sleep(28 * time.Minute)
		synctest.Wait()
		calls := b.reviewer.commandsFor(9)
		if len(calls) != 2 {
			t.Fatalf("after the rest PR #9 was reviewed %d times, want 2", len(calls))
		}
		if !strings.Contains(calls[1], "--resume "+sessionOf9) || !strings.HasSuffix(calls[1], "-- /recheck-pr 9") {
			t.Errorf("the second review is %q, want a re-check in the first review's session", calls[1][max(len(calls[1])-80, 0):])
		}
		b.eng.Do(Quit{})
		<-b.results
	})
}

// A person who names a PR asks for one review of it. A run under the view
// watches, and it must not review the PR again at the end of each rest when
// nothing on the PR changed: each review posts.
func TestANamedPRIsNotReviewedAgainWhenNothingChanged(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		g := newGithub(9)
		b := newBench(t, watchRun(g, func(c *Config) { c.Only = []int{9} }))
		g.set(9, seen)
		b.reviewer.finish(t, 9, 0, answer(9, "commented"))
		synctest.Wait()

		time.Sleep(2 * time.Hour)
		synctest.Wait()
		if got := len(b.reviewer.commandsFor(9)); got != 1 {
			t.Errorf("PR #9 was reviewed %d times in two hours with nothing new on it, want 1", got)
		}

		g.set(9, updated)
		time.Sleep(2 * time.Minute)
		synctest.Wait()
		if got := len(b.reviewer.commandsFor(9)); got != 2 {
			t.Errorf("after its author answered PR #9 was reviewed %d times, want 2", got)
		}
		b.eng.Do(Quit{})
		<-b.results
	})
}

func TestAPRIsReviewedAtMostItsCapUntilItsAuthorPushes(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		g := newGithub(9)
		b := newBench(t, watchRun(g, func(c *Config) { c.Rest = 0; c.MaxPasses = 2 }))
		for range 2 {
			b.reviewer.finish(t, 9, 0, answer(9, "commented"))
			synctest.Wait()
			g.set(9, updated)
			time.Sleep(2 * time.Minute)
			synctest.Wait()
		}
		if got := len(b.reviewer.commandsFor(9)); got != 2 {
			t.Fatalf("PR #9 was reviewed %d times, want its cap of 2", got)
		}
		if got := wait(t, b, 9); got != queue.WaitCapped {
			t.Errorf("PR #9 waits %d, want capped", got)
		}

		g.set(9, func(p *prs.PR) { p.Head = "sha2" })
		time.Sleep(2 * time.Minute)
		synctest.Wait()
		if got := len(b.reviewer.commandsFor(9)); got != 3 {
			t.Errorf("after a push PR #9 was reviewed %d times, want 3", got)
		}
		b.eng.Do(Quit{})
		<-b.results
	})
}

func TestAPRTheRunApprovedIsNotReviewedAgain(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		g := newGithub(9)
		b := newBench(t, watchRun(g, func(c *Config) { c.Rest = 0 }))
		b.reviewer.finish(t, 9, 0, answer(9, "approved"))
		synctest.Wait()

		// The list has not caught up with the approval, and someone
		// commented after it.
		g.set(9, func(p *prs.PR) { p.Engagement = prs.Updated; p.Head = "sha2" })
		time.Sleep(2 * time.Minute)
		synctest.Wait()
		if got := wait(t, b, 9); got != queue.WaitApproved {
			t.Errorf("PR #9 waits %d, want approved", got)
		}
		if got := len(b.reviewer.commandsFor(9)); got != 1 {
			t.Errorf("PR #9 was reviewed %d times, want once", got)
		}
		b.eng.Do(Quit{})
		<-b.results
	})
}

func TestAReviewAfterAFailedOneStartsFresh(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		g := newGithub(9)
		b := newBench(t, watchRun(g, func(c *Config) { c.Rest = 0 }))
		b.reviewer.finish(t, 9, 10, "usage limit")
		synctest.Wait()
		time.Sleep(2 * time.Minute)
		synctest.Wait()

		calls := b.reviewer.commandsFor(9)
		if len(calls) != 2 || strings.Contains(calls[1], "--resume") || !strings.HasSuffix(calls[1], "-- /auto-review 9") {
			t.Errorf("reviews of PR #9: %q, want a fresh second review: a failed one leaves nothing to re-check", calls)
		}
		b.eng.Do(Quit{})
		<-b.results
	})
}

func TestAWatchingRunKeepsLookingThroughFailedLooks(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		g := newGithub()
		b := newBench(t, watchRun(g, nil))
		g.failing(true)
		for range 5 {
			time.Sleep(2 * time.Minute)
			synctest.Wait()
		}
		if s := b.eng.Snapshot(); s.Phase != Idle || s.ListErr == "" {
			t.Fatalf("after five failed looks: phase %d, error %q; want a run that goes on", s.Phase, s.ListErr)
		}
		g.failing(false)
		g.set(9, func(p *prs.PR) { *p = prs.PR{Number: 9, Engagement: prs.New, CI: prs.CIPassing} })
		time.Sleep(2 * time.Minute)
		synctest.Wait()
		if got := b.reviewer.started(); !slices.Equal(got, []int{9}) {
			t.Errorf("started %v after the list came back", got)
		}
		b.eng.Do(Quit{})
		<-b.results
	})
}

func babysitRun(g *github, edit func(*Config)) setup {
	return setup{list: g.list, jobs: 2, verdict: ownWord, edit: func(c *Config) {
		c.Watch = 30 * time.Minute
		c.Loop = LoopBabysit
		c.Watching = true
		c.MaxPasses = 3
		c.MaxIdle = 3
		if edit != nil {
			edit(c)
		}
	}}
}

func TestABabysitRunEndsWhenEveryPRItReviewedIsDone(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		g := newGithub(9, 8)
		b := newBench(t, babysitRun(g, nil))
		g.set(9, seen)
		g.set(8, seen)
		b.reviewer.finish(t, 9, 0, answer(9, "approved"))
		b.reviewer.finish(t, 8, 0, answer(8, "commented"))
		synctest.Wait()

		g.close(8)
		time.Sleep(30 * time.Minute)
		result := <-b.results
		if result.Code != ExitOK {
			t.Errorf("code %d, want the last pass's", result.Code)
		}
		if !slices.Contains(b.notes(), "nothing left to babysit") {
			t.Errorf("notes = %q", b.notes())
		}
	})
}

func TestABabysitRunWaitsAWholeIntervalAfterItsPass(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		g := newGithub(9)
		b := newBench(t, babysitRun(g, nil))
		time.Sleep(25 * time.Minute)
		g.set(9, seen)
		b.reviewer.finish(t, 9, 0, answer(9, "commented"))
		synctest.Wait()
		if got := time.Until(b.eng.Snapshot().NextLookAt); got != 30*time.Minute {
			t.Errorf("the next look is in %v after the pass, want the whole interval", got)
		}
		b.eng.Do(Quit{})
		<-b.results
	})
}

func TestABabysitRunEndsAfterTooManyIdleLooks(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		g := newGithub(9)
		b := newBench(t, babysitRun(g, nil))
		g.set(9, seen)
		b.reviewer.finish(t, 9, 0, answer(9, "commented"))
		synctest.Wait()

		// The look straight after the pass is not idle; three more are.
		for range 3 {
			time.Sleep(30 * time.Minute)
			synctest.Wait()
			if s := b.eng.Snapshot(); s.Phase == Done {
				t.Fatal("the run ended before three idle looks")
			}
		}
		time.Sleep(30 * time.Minute)
		<-b.results
		if !slices.Contains(b.notes(), "nothing has changed in 3 idle checks since the last review") {
			t.Errorf("notes = %q", b.notes())
		}
	})
}

func TestABabysitRunWithNothingToDoEndsAtOnce(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		g := newGithub(9)
		g.set(9, seen)
		b := newBench(t, babysitRun(g, nil))
		if result := <-b.results; result.Code != ExitOK || result.Final.Pass != 0 {
			t.Errorf("code %d, pass %d", result.Code, result.Final.Pass)
		}
	})
}

func TestABabysitRunStaysForChecksThatAreRunning(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		g := newGithub(9)
		g.set(9, func(p *prs.PR) { p.CI = prs.CIPending })
		b := newBench(t, babysitRun(g, nil))
		if s := b.eng.Snapshot(); s.Phase != Idle {
			t.Fatalf("phase %d, want a run that waits for the checks", s.Phase)
		}
		g.set(9, func(p *prs.PR) { p.CI = prs.CIPassing })
		time.Sleep(30 * time.Minute)
		synctest.Wait()
		if got := b.reviewer.started(); !slices.Equal(got, []int{9}) {
			t.Errorf("started %v, want PR #9 once its checks passed", got)
		}
		b.eng.Do(Quit{})
		<-b.results
	})
}

func TestABabysitRunGivesUpAfterThreeFailedLooks(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		g := newGithub(9)
		b := newBench(t, babysitRun(g, nil))
		g.set(9, seen)
		b.reviewer.finish(t, 9, 0, answer(9, "commented"))
		synctest.Wait()
		g.failing(true)
		for range 2 {
			time.Sleep(30 * time.Minute)
			synctest.Wait()
		}
		if b.eng.Snapshot().Phase == Done {
			t.Fatal("the run gave up after two failed looks")
		}
		time.Sleep(30 * time.Minute)
		if result := <-b.results; result.Code != ExitFailed {
			t.Errorf("code %d, want %d", result.Code, ExitFailed)
		}
		if !slices.Contains(b.notes(), "the PR list failed to refresh 3 times in a row; giving up") {
			t.Errorf("notes = %q", b.notes())
		}
	})
}

func oneShot(g *github) setup {
	return setup{list: g.list, jobs: 2, verdict: ownWord, edit: func(c *Config) { c.CIWait = 10 * time.Minute }}
}

func TestARunThatLooksOnceWaitsForChecksThatAreRunning(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		g := newGithub(9, 8)
		g.set(8, func(p *prs.PR) { p.CI = prs.CIPending })
		b := newBench(t, oneShot(g))
		if len(b.reviewer.started()) != 0 {
			t.Fatal("the run started its pass before the checks it waits for")
		}
		if notes := b.notes(); len(notes) == 0 || !strings.HasPrefix(notes[0], "waiting up to 10m for the checks of #8") {
			t.Errorf("notes = %q", notes)
		}
		time.Sleep(30 * time.Second)
		synctest.Wait()
		if len(b.reviewer.started()) != 0 {
			t.Fatal("the run stopped waiting while the checks still ran")
		}

		g.set(8, func(p *prs.PR) { p.CI = prs.CIPassing })
		time.Sleep(30 * time.Second)
		synctest.Wait()
		if got := b.reviewer.started(); !slices.Equal(got, []int{9, 8}) {
			t.Errorf("started %v, want both once the checks passed", got)
		}
		b.reviewer.finish(t, 9, 0, answer(9, "commented"))
		b.reviewer.finish(t, 8, 0, answer(8, "commented"))
		if result := <-b.results; result.Code != ExitOK {
			t.Errorf("code %d", result.Code)
		}
	})
}

func TestTheWaitForChecksEnds(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		g := newGithub(9, 8)
		g.set(8, func(p *prs.PR) { p.CI = prs.CIPending })
		b := newBench(t, oneShot(g))
		time.Sleep(10 * time.Minute)
		synctest.Wait()
		if got := b.reviewer.started(); !slices.Equal(got, []int{9}) {
			t.Errorf("started %v, want the ready PR without the one whose checks never finished", got)
		}
		b.reviewer.finish(t, 9, 0, answer(9, "commented"))
		<-b.results
	})
}

func TestFailingChecksAreNotWaitedFor(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		g := newGithub(9, 8)
		g.set(8, func(p *prs.PR) { p.CI = prs.CIFailing })
		b := newBench(t, oneShot(g))
		if got := b.reviewer.started(); !slices.Equal(got, []int{9}) {
			t.Errorf("started %v, want PR #9 at once", got)
		}
		b.reviewer.finish(t, 9, 0, answer(9, "commented"))
		<-b.results
	})
}

func TestWatchTurnedOnInTheViewReviewsWhatIsReady(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		g := newGithub(9)
		s := watchRun(g, func(c *Config) { c.HoldOpen = true; c.Watching = false })
		b := newBench(t, s)
		g.set(9, seen)
		b.reviewer.finish(t, 9, 0, answer(9, "commented"))
		synctest.Wait()

		g.set(8, func(p *prs.PR) { *p = prs.PR{Number: 8, Engagement: prs.New, CI: prs.CIPassing} })
		time.Sleep(2 * time.Minute)
		synctest.Wait()
		if got := b.reviewer.started(); !slices.Equal(got, []int{9}) {
			t.Fatalf("a run that is not watching started %v", got)
		}

		b.eng.Do(SetWatch{On: true})
		synctest.Wait()
		if got := b.reviewer.started(); !slices.Equal(got, []int{9, 8}) || !b.eng.Snapshot().Watching {
			t.Errorf("after w: started %v, watching %v", got, b.eng.Snapshot().Watching)
		}
		b.reviewer.finish(t, 8, 0, answer(8, "commented"))
		synctest.Wait()
		b.eng.Do(SetWatch{On: false})
		synctest.Wait()
		b.eng.Do(Quit{})
		if result := <-b.results; result.Code != ExitOK {
			t.Errorf("code %d: a held run that no longer watches ends with its pass's status", result.Code)
		}
		if notes := b.notes(); !slices.Contains(notes, "watching: looking for work every 2m") || !slices.Contains(notes, "no longer looking for work; R still reviews a PR") {
			t.Errorf("notes = %q", notes)
		}
	})
}

func TestQuittingAWatchingViewIsAnInterrupt(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		g := newGithub(9)
		b := newBench(t, watchRun(g, func(c *Config) { c.HoldOpen = true }))
		b.reviewer.finish(t, 9, 0, answer(9, "approved"))
		synctest.Wait()
		b.eng.Do(Quit{})
		if result := <-b.results; result.Code != ExitInterrupted {
			t.Errorf("code %d, want %d", result.Code, ExitInterrupted)
		}
	})
}

func TestTheSnapshotSaysHowTheRunLoops(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		g := newGithub()
		b := newBench(t, watchRun(g, nil))
		s := b.eng.Snapshot()
		if !s.Watching || s.Rest != 30*time.Minute || s.At.IsZero() {
			t.Errorf("snapshot: watching %v, rest %v, at %v", s.Watching, s.Rest, s.At)
		}
		b.eng.Do(Quit{})
		<-b.results
	})
}

func TestTheRunLooksAgainWhenARestEnds(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		g := newGithub(9)
		b := newBench(t, watchRun(g, func(c *Config) { c.Rest = 3 * time.Minute; c.Watch = 2 * time.Minute }))
		b.reviewer.finish(t, 9, 0, answer(9, "commented"))
		synctest.Wait()
		g.set(9, updated)

		time.Sleep(2 * time.Minute)
		synctest.Wait()
		if got := time.Until(b.eng.Snapshot().NextLookAt); got != time.Minute {
			t.Fatalf("the next look is in %v, want when the rest ends, in 1m", got)
		}
		time.Sleep(time.Minute)
		synctest.Wait()
		if got := len(b.reviewer.commandsFor(9)); got != 2 {
			t.Errorf("PR #9 was reviewed %d times when its rest ended, want 2", got)
		}
		b.eng.Do(Quit{})
		<-b.results
	})
}
