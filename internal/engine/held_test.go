package engine

import (
	"slices"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/venabots/autoreview/internal/proc"
	"github.com/venabots/autoreview/internal/prs"
)

// held is a run under the view: it stays open after its pass.
func held(s setup) setup {
	edit := s.edit
	s.edit = func(c *Config) {
		c.HoldOpen = true
		if edit != nil {
			edit(c)
		}
	}
	return s
}

func TestAHeldRunStaysOpenAfterItsPassAndEndsWithItsStatus(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		b := newBench(t, held(setup{prs: []int{9}}))
		b.reviewer.finish(t, 9, 10, "usage limit")
		synctest.Wait()

		s := b.eng.Snapshot()
		if s.Phase != Idle || s.Jobs[0].State != JobFailed {
			t.Fatalf("after the pass: phase %d and PR #9 in state %d; want an idle run that is still open", s.Phase, s.Jobs[0].State)
		}
		select {
		case <-b.results:
			t.Fatal("a held run ended by itself")
		default:
		}

		b.eng.Do(Quit{})
		if result := <-b.results; result.Code != ExitFailed {
			t.Errorf("code %d, want %d: quitting a run whose work is done is not an interrupt", result.Code, ExitFailed)
		}
	})
}

func TestAHeldRunWithNothingReadyStaysOpen(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		b := newBench(t, held(setup{}))
		if got := b.eng.Snapshot(); got.Phase != Idle || got.Pass != 0 {
			t.Fatalf("phase %d and pass %d", got.Phase, got.Pass)
		}
		b.eng.Do(Quit{})
		if result := <-b.results; result.Code != ExitOK {
			t.Errorf("code %d, want 0", result.Code)
		}
	})
}

func TestQuittingAHeldRunBeforeItsFirstLookIsAnInterrupt(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		b := newBench(t, held(setup{prs: []int{9}}))
		b.eng.Do(Quit{})
		if result := <-b.results; result.Code != ExitInterrupted {
			t.Errorf("code %d, want %d", result.Code, ExitInterrupted)
		}
	})
}

func TestReviewNowOnAnIdleRunStartsAPassForThatPR(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		b := newBench(t, held(setup{prs: []int{9, 8}, jobs: 2}))
		b.reviewer.finish(t, 9, 0, answer(9, "approved"))
		b.reviewer.finish(t, 8, 0, answer(8, "approved"))
		synctest.Wait()

		b.eng.Do(ReviewNow{PR: 8})
		synctest.Wait()
		s := b.eng.Snapshot()
		if s.Pass != 2 || s.Phase != Reviewing || len(s.Jobs) != 1 || s.Jobs[0].PR != 8 || s.Jobs[0].State != JobRunning {
			t.Fatalf("after R: pass %d, phase %d, jobs %+v; want a second pass with PR #8 alone", s.Pass, s.Phase, s.Jobs)
		}
		if !strings.HasSuffix(s.Jobs[0].PassDir, "pass-2") {
			t.Errorf("the second pass writes to %s", s.Jobs[0].PassDir)
		}
		if len(s.Earlier) != 2 || s.Earlier[0].PR != 9 || s.Earlier[1].PR != 8 {
			t.Errorf("Earlier = %+v, want the two reviews of the first pass", s.Earlier)
		}
		reviewed := s.Reviewed()
		if len(reviewed) != 2 || reviewed[0].PR != 9 || reviewed[1].State != JobRunning {
			t.Errorf("Reviewed = %+v, want PR #9's old review and PR #8's new one", reviewed)
		}

		b.reviewer.finish(t, 8, 0, answer(8, "commented"))
		synctest.Wait()
		b.eng.Do(Quit{})
		result := <-b.results
		if result.Code != ExitOK || len(result.Final.Earlier) != 2 {
			t.Errorf("code %d and %d earlier reviews", result.Code, len(result.Final.Earlier))
		}
	})
}

func TestReviewNowInAPassPutsAQueuedReviewFirst(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		b := newBench(t, held(setup{prs: []int{9, 8, 7, 6}, jobs: 1}))
		b.eng.Do(ReviewNow{PR: 6})
		synctest.Wait()
		order := []int{}
		for _, j := range b.eng.Snapshot().Jobs {
			order = append(order, j.PR)
		}
		if !slices.Equal(order, []int{9, 6, 8, 7}) {
			t.Fatalf("the pass is ordered %v, want PR #6 ahead of the other reviews that wait", order)
		}
		b.reviewer.finish(t, 9, 0, answer(9, "approved"))
		synctest.Wait()
		if got := b.reviewer.started(); !slices.Equal(got, []int{9, 6}) {
			t.Errorf("started %v, want PR #6 next", got)
		}
		b.eng.Do(Quit{})
		<-b.results
	})
}

// stackedOn is the list with pr held on the PR under it.
func stackedOn(l prs.Listing, pr, under int) prs.Listing {
	out := prs.Listing{PRs: slices.Clone(l.PRs)}
	for i := range out.PRs {
		if out.PRs[i].Number == pr {
			out.PRs[i].Parent = &prs.Parent{Number: under, Evidence: prs.Declared}
		}
	}
	return out
}

func TestReviewNowInAPassAddsAHeldPRToThatPassAheadOfTheReviewsThatWait(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		list := stackedOn(listing(9, 8, 7, 5), 5, 7)
		b := newBench(t, held(setup{jobs: 1, list: func() (prs.Listing, error) { return list, nil }}))
		if got := b.reviewer.started(); !slices.Equal(got, []int{9}) {
			t.Fatalf("started %v, want PR #9 alone: PR #5 sits on PR #7", got)
		}

		b.eng.Do(ReviewNow{PR: 5})
		synctest.Wait()
		s := b.eng.Snapshot()
		order := []int{}
		for _, j := range s.Jobs {
			order = append(order, j.PR)
		}
		if s.Pass != 1 || !slices.Equal(order, []int{9, 5, 8, 7}) {
			t.Fatalf("pass %d ordered %v, want PR #5 in pass 1, ahead of the other reviews that wait", s.Pass, order)
		}
		five := b.job(t, 5)
		if five.State != JobQueued || five.PassDir != s.Jobs[0].PassDir || five.Orchestrator != s.Jobs[0].Orchestrator {
			t.Errorf("PR #5's job is %+v, want a queued review of pass 1, made as the pass makes its reviews", five)
		}

		b.reviewer.finish(t, 9, 0, answer(9, "approved"))
		synctest.Wait()
		if got := b.reviewer.started(); !slices.Equal(got, []int{9, 5}) {
			t.Errorf("started %v, want PR #5 in the slot PR #9 freed", got)
		}
		b.eng.Do(Quit{})
		<-b.results
	})
}

func TestReviewNowLeavesARunningReviewAloneAndIgnoresAPRNotOnTheList(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		b := newBench(t, held(setup{prs: []int{9}, verdict: nil}))
		b.eng.Do(ReviewNow{PR: 9})
		b.eng.Do(ReviewNow{PR: 404})
		synctest.Wait()
		if got := b.reviewer.started(); !slices.Equal(got, []int{9}) {
			t.Fatalf("started %v: a PR that is being reviewed was started again", got)
		}

		b.reviewer.finish(t, 9, 0, answer(9, "approved"))
		synctest.Wait()
		b.eng.Do(ReviewNow{PR: 9})
		synctest.Wait()
		if s := b.eng.Snapshot(); s.Pass != 2 || s.Jobs[0].PR != 9 {
			t.Errorf("pass %d with jobs %+v, want a second pass for PR #9", s.Pass, s.Jobs)
		}
		b.eng.Do(Quit{})
		<-b.results
	})
}

func TestAPRAskedForDuringAPassIsReviewedWhenThePassEnds(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		b := newBench(t, held(setup{prs: []int{9, 8}, jobs: 2}))
		b.reviewer.finish(t, 9, 0, answer(9, "approved"))
		synctest.Wait()

		b.eng.Do(ReviewNow{PR: 9})
		b.eng.Do(ReviewNow{PR: 9})
		synctest.Wait()
		if s := b.eng.Snapshot(); s.Pass != 1 {
			t.Fatalf("a second pass started while the first was running")
		}
		b.reviewer.finish(t, 8, 0, answer(8, "approved"))
		synctest.Wait()

		s := b.eng.Snapshot()
		if s.Pass != 2 || len(s.Jobs) != 1 || s.Jobs[0].PR != 9 {
			t.Errorf("after the pass: pass %d, jobs %+v; want one new review of PR #9", s.Pass, s.Jobs)
		}
		b.eng.Do(Quit{})
		<-b.results
	})
}

func TestStopJobStopsOneReviewAndThePassGoesOn(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		rev := &reviewer{onStop: proc.Exit{Code: 10}}
		b := newBench(t, held(setup{prs: []int{9, 8}, jobs: 1, reviewer: rev}))

		b.eng.Do(StopJob{PR: 8})
		b.eng.Do(StopJob{PR: 404})
		b.eng.Do(StopJob{PR: 9})
		synctest.Wait()

		nine := b.job(t, 9)
		if nine.State != JobFailed || nine.Outcome != "stopped" {
			t.Errorf("PR #9: state %d, outcome %q; want stopped", nine.State, nine.Outcome)
		}
		if got := b.reviewer.started(); !slices.Equal(got, []int{9, 8}) {
			t.Errorf("started %v, want PR #8 to start in the slot the stop freed", got)
		}
		if !slices.Contains(b.notes(), "stopped the review of PR #9") {
			t.Errorf("notes = %q", b.notes())
		}
		b.reviewer.finish(t, 8, 0, answer(8, "approved"))
		synctest.Wait()
		b.eng.Do(Quit{})
		if result := <-b.results; result.Code != ExitFailed {
			t.Errorf("code %d, want %d: a review that was stopped did not run to its end", result.Code, ExitFailed)
		}
	})
}

func TestSetFocusReachesTheReviewsThatHaveNotStarted(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		b := newBench(t, held(setup{prs: []int{9, 8}, jobs: 1, edit: func(c *Config) { c.Focus = "the first focus" }}))
		if got := b.eng.Snapshot().Focus; got != "the first focus" {
			t.Fatalf("the snapshot's focus is %q", got)
		}
		b.eng.Do(SetFocus{Text: "the ledger migration"})
		synctest.Wait()
		b.reviewer.finish(t, 9, 0, answer(9, "approved"))
		synctest.Wait()

		first := strings.Join(b.reviewer.command(t, 9).Args, " ")
		second := strings.Join(b.reviewer.command(t, 8).Args, " ")
		if !strings.HasSuffix(first, `--focus "the first focus"`) || !strings.HasSuffix(second, `--focus "the ledger migration"`) {
			t.Errorf("the running review got %q and the next one %q", first[max(len(first)-40, 0):], second[max(len(second)-40, 0):])
		}
		if got := b.eng.Snapshot().Focus; got != "the ledger migration" {
			t.Errorf("the snapshot's focus is %q", got)
		}

		b.eng.Do(SetFocus{})
		b.reviewer.finish(t, 8, 0, answer(8, "approved"))
		synctest.Wait()
		b.eng.Do(ReviewNow{PR: 9})
		synctest.Wait()
		if third := strings.Join(b.reviewer.commands[2].Args, " "); strings.Contains(third, "--focus") {
			t.Errorf("after the focus was cleared a review got %q", third[max(len(third)-40, 0):])
		}
		b.eng.Do(Quit{})
		<-b.results
	})
}

func TestTheListsAreReadAgainWhileAPassRuns(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		b := newBench(t, held(setup{prs: []int{9}, edit: func(c *Config) { c.Watch = 2 * time.Minute }}))
		first := b.eng.Snapshot()
		if first.Phase != Reviewing || first.Looking || time.Until(first.NextLookAt) != 2*time.Minute {
			t.Fatalf("during the pass: phase %d, looking %v, next look in %v", first.Phase, first.Looking, time.Until(first.NextLookAt))
		}
		time.Sleep(2 * time.Minute)
		synctest.Wait()
		second := b.eng.Snapshot()
		if second.Phase != Reviewing || second.Jobs[0].State != JobRunning {
			t.Errorf("a look during the pass changed it: phase %d, PR #9 in state %d", second.Phase, second.Jobs[0].State)
		}
		if time.Until(second.NextLookAt) != 2*time.Minute {
			t.Errorf("the next look is in %v, want the lists read again every two minutes", time.Until(second.NextLookAt))
		}
		if second.Pass != 1 {
			t.Errorf("a later look started pass %d by itself", second.Pass)
		}
		b.eng.Do(Quit{})
		<-b.results
	})
}
