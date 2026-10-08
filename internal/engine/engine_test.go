package engine

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/venabots/autoreview/internal/mine"
	"github.com/venabots/autoreview/internal/prs"
	"github.com/venabots/autoreview/internal/repo"
)

var bothGates = prs.Gates{CI: true, Stack: true}

func listing(numbers ...int) prs.Listing {
	list := make([]prs.PR, len(numbers))
	for i, n := range numbers {
		list[i] = prs.PR{Number: n, Engagement: prs.New, CI: prs.CIPassing}
	}
	return prs.Listing{PRs: list}
}

// world is a fake GitHub. Each look takes the next answer, and the last
// answer repeats.
type world struct {
	lists atomic.Int32
	list  []func() (prs.Listing, error)
	mine  func() ([]mine.PR, error)
}

func (w *world) deps() Deps {
	return Deps{
		List: func(context.Context) (prs.Listing, error) {
			n := int(w.lists.Add(1)) - 1
			return w.list[min(n, len(w.list)-1)]()
		},
		Mine: func(context.Context) ([]mine.PR, error) {
			if w.mine == nil {
				return []mine.PR{{Number: 4}}, nil
			}
			return w.mine()
		},
	}
}

func answers(l prs.Listing) func() (prs.Listing, error) {
	return func() (prs.Listing, error) { return l, nil }
}

func fails(msg string) func() (prs.Listing, error) {
	return func() (prs.Listing, error) { return prs.Listing{}, errors.New(msg) }
}

func config(watch time.Duration) Config {
	return Config{Repo: repo.Context{Owner: "acme", Name: "widgets"}, Gates: bothGates, Watch: watch}
}

func reviewNumbers(s *Snapshot) []int {
	out := make([]int, len(s.Reviews))
	for i, row := range s.Reviews {
		out[i] = row.PR.Number
	}
	return out
}

func TestARunThatDoesNotWatchLooksOnceAndEnds(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		w := &world{list: []func() (prs.Listing, error){answers(listing(9, 8))}}
		result := New(config(0), w.deps()).Run()

		if result.Code != ExitOK {
			t.Errorf("Code = %d, want %d", result.Code, ExitOK)
		}
		s := result.Final
		if s.Phase != Done || !s.Looked || !s.MineLooked {
			t.Errorf("final snapshot = %+v, want Done with both lists read", s)
		}
		if got := reviewNumbers(s); len(got) != 2 || got[0] != 9 || got[1] != 8 {
			t.Errorf("Reviews = %v, want [9 8]", got)
		}
		if len(s.Mine) != 1 || s.Mine[0].Number != 4 {
			t.Errorf("Mine = %+v, want PR #4", s.Mine)
		}
		if w.lists.Load() != 1 {
			t.Errorf("the PR list was read %d times, want 1", w.lists.Load())
		}
	})
}

func TestARunThatLooksOnceFailsWhenTheListCannotBeRead(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		w := &world{list: []func() (prs.Listing, error){fails("rate limited")}}
		result := New(config(0), w.deps()).Run()

		if result.Code != ExitFailed {
			t.Errorf("Code = %d, want %d", result.Code, ExitFailed)
		}
		if result.Final.Looked || result.Final.ListErr != "rate limited" {
			t.Errorf("final snapshot = %+v, want the error and no list", result.Final)
		}
	})
}

func TestAFailedLookAtYourOwnPRsDoesNotFailTheRun(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		w := &world{
			list: []func() (prs.Listing, error){answers(listing(9))},
			mine: func() ([]mine.PR, error) { return nil, errors.New("search is down") },
		}
		result := New(config(0), w.deps()).Run()

		if result.Code != ExitOK {
			t.Errorf("Code = %d, want %d: the run was asked to review", result.Code, ExitOK)
		}
		if result.Final.MineLooked || result.Final.MineErr != "search is down" {
			t.Errorf("final snapshot = %+v, want the error and no list of your PRs", result.Final)
		}
	})
}

func TestAWatchingRunLooksAgainAfterTheInterval(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		w := &world{list: []func() (prs.Listing, error){answers(listing(9)), answers(listing(9, 10))}}
		eng := New(config(2*time.Minute), w.deps())
		results := make(chan Result, 1)
		go func() { results <- eng.Run() }()

		synctest.Wait()
		first := eng.Snapshot()
		if first.Phase != Idle || first.Looking || len(first.Reviews) != 1 {
			t.Fatalf("after the first look: %+v, want an idle run with one PR", first)
		}
		if got := time.Until(first.NextLookAt); got != 2*time.Minute {
			t.Errorf("the next look is in %v, want 2m", got)
		}

		time.Sleep(2*time.Minute - time.Second)
		synctest.Wait()
		if w.lists.Load() != 1 {
			t.Fatalf("the run looked again %v early", time.Second)
		}

		time.Sleep(time.Second)
		synctest.Wait()
		second := eng.Snapshot()
		if w.lists.Load() != 2 || len(second.Reviews) != 2 {
			t.Errorf("after the interval: %d looks and %d PRs, want 2 and 2", w.lists.Load(), len(second.Reviews))
		}
		if second.Seq <= first.Seq {
			t.Errorf("Seq went from %d to %d, want it to go up", first.Seq, second.Seq)
		}

		eng.Do(Quit{})
		if result := <-results; result.Code != ExitInterrupted || result.Final.Phase != Done {
			t.Errorf("after Quit: code %d and phase %d, want %d and Done", result.Code, result.Final.Phase, ExitInterrupted)
		}
	})
}

func TestAFailedLookKeepsWhatTheLastGoodOneFound(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		w := &world{list: []func() (prs.Listing, error){answers(listing(9, 8)), fails("timed out"), answers(listing(7))}}
		eng := New(config(time.Minute), w.deps())
		results := make(chan Result, 1)
		go func() { results <- eng.Run() }()

		time.Sleep(time.Minute)
		synctest.Wait()
		failed := eng.Snapshot()
		if failed.ListErr != "timed out" || len(failed.Reviews) != 2 || !failed.Looked {
			t.Errorf("after a failed look: %+v, want the error and the two PRs from before", failed)
		}
		if failed.Looking || failed.NextLookAt.IsZero() {
			t.Errorf("after a failed look: looking %v, next look at %v; want the next look set", failed.Looking, failed.NextLookAt)
		}

		time.Sleep(time.Minute)
		synctest.Wait()
		recovered := eng.Snapshot()
		if recovered.ListErr != "" || len(recovered.Reviews) != 1 {
			t.Errorf("after the next good look: %+v, want no error and one PR", recovered)
		}

		eng.Do(Quit{})
		<-results
	})
}

func TestQuitStopsALookThatHasNotAnswered(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		stopped := make(chan struct{})
		deps := Deps{
			List: func(ctx context.Context) (prs.Listing, error) {
				<-ctx.Done()
				close(stopped)
				return prs.Listing{}, ctx.Err()
			},
			Mine: func(context.Context) ([]mine.PR, error) { return nil, nil },
		}
		eng := New(config(time.Minute), deps)
		results := make(chan Result, 1)
		go func() { results <- eng.Run() }()

		synctest.Wait()
		if !eng.Snapshot().Looking {
			t.Fatal("the run is not looking while the list has not answered")
		}
		eng.Do(Quit{})
		result := <-results

		select {
		case <-stopped:
		default:
			t.Error("Run returned while its look was still running")
		}
		if result.Code != ExitInterrupted {
			t.Errorf("Code = %d, want %d", result.Code, ExitInterrupted)
		}
	})
}

func TestALookThatPanicsAnswersWithAnError(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		deps := Deps{
			List: func(context.Context) (prs.Listing, error) { panic("nil map") },
			Mine: func(context.Context) ([]mine.PR, error) { return nil, nil },
		}
		result := New(config(0), deps).Run()

		if result.Code != ExitFailed || result.Final.ListErr == "" {
			t.Errorf("code %d and error %q, want %d and a message", result.Code, result.Final.ListErr, ExitFailed)
		}
	})
}

func TestOnlyLimitsTheListToTheNamedPRs(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		w := &world{list: []func() (prs.Listing, error){answers(listing(9, 8, 7))}}
		cfg := config(0)
		cfg.Only = []int{8}
		result := New(cfg, w.deps()).Run()

		if got := reviewNumbers(result.Final); len(got) != 1 || got[0] != 8 {
			t.Errorf("Reviews = %v, want [8]", got)
		}
	})
}

func TestChangedHoldsOneTokenHoweverManySnapshots(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		w := &world{list: []func() (prs.Listing, error){answers(listing(9))}}
		eng := New(config(0), w.deps())
		eng.Run()

		<-eng.Changed()
		select {
		case <-eng.Changed():
			t.Error("Changed held a second token: a slow reader would be woken for a snapshot it already has")
		default:
		}
		if eng.Snapshot().Phase != Done {
			t.Error("the snapshot read after the token is not the newest")
		}
	})
}
