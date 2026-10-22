package engine

import (
	"context"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/venabots/autoreview/internal/mine"
	"github.com/venabots/autoreview/internal/prs"
)

// A person who just merged or pushed on GitHub must not wait out the watch
// interval to see it. LookNow reads both lists at once, and the next look
// is a whole interval after it.
func TestLookNowReadsBothListsAtOnceAndPutsTheNextLookOff(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var lists, mines atomic.Int32
		count := func(d *Deps) {
			d.List = func(context.Context) (prs.Listing, error) { lists.Add(1); return listing(), nil }
			d.Mine = func(context.Context, bool) ([]mine.PR, error) { mines.Add(1); return nil, nil }
		}
		b := newBench(t, held(setup{deps: count, edit: func(c *Config) { c.Watch = 10 * time.Minute }}))
		time.Sleep(3 * time.Minute)
		synctest.Wait()
		if lists.Load() != 1 || mines.Load() != 1 {
			t.Fatalf("before LookNow: %d lists, %d reads of your PRs; want 1 each", lists.Load(), mines.Load())
		}

		b.eng.Do(LookNow{})
		synctest.Wait()
		if lists.Load() != 2 || mines.Load() != 2 {
			t.Errorf("after LookNow: %d lists, %d reads of your PRs; want 2 each", lists.Load(), mines.Load())
		}
		if next := b.eng.Snapshot().NextLookAt; !next.Equal(time.Now().Add(10 * time.Minute)) {
			t.Errorf("the next look is at %v, want a whole interval from now", next)
		}
		time.Sleep(9 * time.Minute)
		synctest.Wait()
		if lists.Load() != 2 {
			t.Errorf("the look the timer had set for 7 minutes on still ran: %d lists", lists.Load())
		}
		b.eng.Do(Quit{})
		<-b.results
	})
}

func TestLookNowDuringALookStartsNoSecondOne(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var lists atomic.Int32
		release := make(chan struct{})
		slow := func(d *Deps) {
			d.List = func(context.Context) (prs.Listing, error) {
				if lists.Add(1) == 2 {
					<-release
				}
				return listing(), nil
			}
		}
		b := newBench(t, held(setup{deps: slow, edit: func(c *Config) { c.Watch = 10 * time.Minute }}))
		b.eng.Do(LookNow{})
		synctest.Wait()
		b.eng.Do(LookNow{})
		synctest.Wait()
		close(release)
		synctest.Wait()
		if got := lists.Load(); got != 2 {
			t.Errorf("%d lists, want 2: a LookNow during a look started another", got)
		}
		b.eng.Do(Quit{})
		<-b.results
	})
}
