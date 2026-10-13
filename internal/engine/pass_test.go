package engine

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/venabots/autoreview/internal/proc"
	"github.com/venabots/autoreview/internal/review"
	"github.com/venabots/autoreview/internal/session"
)

func TestAPassReviewsEveryReadyPRAndEndsClean(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		b := newBench(t, setup{prs: []int{9, 8}, jobs: 2})
		if got := b.eng.Snapshot(); got.Phase != Reviewing || got.Pass != 1 || got.RunDir != b.run.Root {
			t.Fatalf("after the look: phase %d, pass %d, run dir %q", got.Phase, got.Pass, got.RunDir)
		}
		if got := b.reviewer.started(); !slices.Equal(got, []int{9, 8}) {
			t.Fatalf("started %v, want both reviews", got)
		}

		time.Sleep(63 * time.Second)
		b.reviewer.finish(t, 9, 0, answer(9, "approved"))
		synctest.Wait()
		b.reviewer.finish(t, 8, 0, answer(8, "approved"))
		result := <-b.results

		if result.Code != ExitOK || result.Final.Phase != Done {
			t.Errorf("code %d and phase %d, want 0 and Done", result.Code, result.Final.Phase)
		}
		nine := b.job(t, 9)
		if nine.State != JobDone || nine.Verdict != "approved" || nine.Elapsed != 63*time.Second {
			t.Errorf("PR #9: %+v", nine)
		}
		if nine.Session != sessionOf9 || nine.Model != "claude-fable-5" || nine.Cost == nil || *nine.Cost != 0.42 {
			t.Errorf("PR #9 lost what the harness said: session %q, model %q, cost %v", nine.Session, nine.Model, nine.Cost)
		}
		if nine.Trailer == nil || nine.Trailer.Risk != "LOW" || len(nine.Trailer.Panel) != 2 {
			t.Errorf("PR #9's trailer: %+v", nine.Trailer)
		}
		if nine.PassDir != filepath.Join(b.run.Root, "pass-1") || nine.Title != "" || nine.Orchestrator != review.Claude() {
			t.Errorf("PR #9: pass dir %q, orchestrator %+v", nine.PassDir, nine.Orchestrator)
		}
		text, err := os.ReadFile(filepath.Join(b.run.Root, "pass-1", "pr-9.review.md"))
		if err != nil || string(text) != "Reviewed PR 9.\n" {
			t.Errorf("the review file holds %q, %v; want the review without its trailer", text, err)
		}
		if id, _ := os.ReadFile(b.run.SessionFile(9)); string(id) != sessionOf9 {
			t.Errorf("the session file holds %q", id)
		}
		want := []string{"looked", "pass 1: 2 at 2", "start 9", "start 8", "finish 9", "finish 8"}
		if got := b.journal(); !slices.Equal(got, want) {
			t.Errorf("journal = %v, want %v", got, want)
		}
		if len(b.recorded) != 2 || b.recorded[0].Job.PR != 9 || !b.recorded[0].HasText || b.recorded[0].Text != "Reviewed PR 9." {
			t.Errorf("recorded %+v", b.recorded)
		}
	})
}

func TestTheReviewerIsStartedTheWayV1StartedIt(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		b := newBench(t, setup{prs: []int{9}, edit: func(c *Config) {
			c.Focus = "the ledger migration"
			c.Budget = "2.50"
		}})
		c := b.reviewer.command(t, 9)
		b.reviewer.finish(t, 9, 0, answer(9, "approved"))
		<-b.results

		pass := filepath.Join(b.run.Root, "pass-1")
		args := strings.Join(c.Args, " ")
		for _, want := range []string{
			"-H claude --output-format json --meta-file " + filepath.Join(pass, "pr-9.meta.json") + " --timeout 3600 --dangerously-skip-permissions",
			"--session-id " + session.ID("/sandbox/repo", "acme", "widgets", 9),
			"--max-budget-usd=2.50",
			`-- /auto-review 9 --focus "the ledger migration"`,
		} {
			if !strings.Contains(args, want) {
				t.Errorf("the command line lacks %q:\n%s", want, args)
			}
		}
		if c.Path != "dash-p" || c.Dir != "/sandbox/repo" || c.Stdout != filepath.Join(pass, "pr-9.json") || c.Stderr != filepath.Join(pass, "pr-9.log") {
			t.Errorf("command = %+v", c)
		}
	})
}

func TestASlotIsFreeWhenAProcessEndsNotWhenItsResultIsRead(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		release := make(chan struct{})
		b := newBench(t, setup{prs: []int{9, 8}, jobs: 1, verdict: func(pr int) review.Readback {
			if pr == 9 {
				<-release
			}
			return review.Readback{Kind: review.ReadbackLanded, Word: "commented"}
		}})
		if got := b.reviewer.started(); !slices.Equal(got, []int{9}) {
			t.Fatalf("with one slot, started %v", got)
		}
		if b.job(t, 8).State != JobQueued {
			t.Errorf("PR #8 is %d, want queued", b.job(t, 8).State)
		}

		time.Sleep(10 * time.Second)
		b.reviewer.finish(t, 9, 0, answer(9, "commented"))
		synctest.Wait()

		nine := b.job(t, 9)
		if nine.State != JobFinishing || nine.Elapsed != 10*time.Second || nine.Session != sessionOf9 {
			t.Errorf("PR #9 while GitHub is asked: %+v, want finishing with its time and session known", nine)
		}
		if got := b.reviewer.started(); !slices.Equal(got, []int{9, 8}) {
			t.Errorf("started %v: PR #8 waited for PR #9's readback", got)
		}

		time.Sleep(time.Minute)
		close(release)
		b.reviewer.finish(t, 8, 0, answer(8, "commented"))
		result := <-b.results
		if got := b.job(t, 9).Elapsed; got != 10*time.Second {
			t.Errorf("PR #9 took %v, want the readback left out of its time", got)
		}
		if result.Code != ExitOK {
			t.Errorf("code %d", result.Code)
		}
	})
}

func TestAFailedReviewFailsTheRunAndSaysWhy(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		b := newBench(t, setup{prs: []int{9, 8}, jobs: 2})
		b.reviewer.finish(t, 9, 10, "You've hit your session limit\n· resets 12pm")
		b.reviewer.finish(t, 8, 0, answer(8, "approved"))
		result := <-b.results

		if result.Code != ExitFailed {
			t.Errorf("code %d, want %d", result.Code, ExitFailed)
		}
		nine := b.job(t, 9)
		if nine.State != JobFailed || nine.Outcome != "exit 10" || nine.Error != "You've hit your session limit · resets 12pm" {
			t.Errorf("PR #9: state %d, outcome %q, error %q", nine.State, nine.Outcome, nine.Error)
		}
		if nine.Verdict != "" || nine.Trailer != nil {
			t.Errorf("a failed review has a verdict %q or a trailer", nine.Verdict)
		}
		if b.job(t, 8).State != JobDone {
			t.Error("the other review did not carry on")
		}
		if slices.Contains(b.asked, 9) {
			t.Error("GitHub was asked about a review that failed")
		}
		if len(b.recorded) != 1 || b.recorded[0].Job.PR != 8 {
			t.Errorf("recorded %d reviews, want only the one that ran to its end", len(b.recorded))
		}
	})
}

func TestAReviewThatCannotStartFailsByItself(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		b := newBench(t, setup{prs: []int{9, 8}, jobs: 1, reviewer: &reviewer{refuse: []int{9}}})
		nine := b.job(t, 9)
		if nine.State != JobFailed || nine.Outcome != "exit 127" || nine.Error != "starting dash-p: executable file not found[2J" {
			t.Errorf("PR #9: state %d, outcome %q, error %q", nine.State, nine.Outcome, nine.Error)
		}
		b.reviewer.finish(t, 8, 0, answer(8, "approved"))
		if result := <-b.results; result.Code != ExitFailed || b.job(t, 8).State != JobDone {
			t.Errorf("code %d and PR #8 in state %d, want a failed run whose other review ran", result.Code, b.job(t, 8).State)
		}
	})
}

func TestAPassWhoseOnlyReviewCannotStartStillEnds(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		b := newBench(t, setup{prs: []int{9}, reviewer: &reviewer{refuse: []int{9}}})
		if result := <-b.results; result.Code != ExitFailed {
			t.Errorf("code %d, want %d", result.Code, ExitFailed)
		}
	})
}

func TestAReviewThatRunsPastItsTimeIsStoppedAndTimedOut(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		rev := &reviewer{onStop: proc.Exit{Code: 10}}
		b := newBench(t, setup{prs: []int{9}, reviewer: rev, edit: func(c *Config) { c.Timeout = 10 * time.Minute }})

		time.Sleep(10*time.Minute + 4*time.Second)
		synctest.Wait()
		if rev.procs[9].stops.Load() != 0 {
			t.Fatal("the guard fired before the harness had its five seconds of grace")
		}
		time.Sleep(time.Second)
		result := <-b.results

		nine := b.job(t, 9)
		if nine.State != JobTimedOut {
			t.Errorf("PR #9 is in state %d with outcome %q, want timed out: the stop produced exit 10, which must not read as a failed agent", nine.State, nine.Outcome)
		}
		if result.Code != ExitFailed {
			t.Errorf("code %d", result.Code)
		}
	})
}

func TestTheGuardDoesNotFireAfterTheReviewEnded(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		rev := &reviewer{}
		b := newBench(t, setup{prs: []int{9, 8}, jobs: 1, reviewer: rev, edit: func(c *Config) { c.Timeout = time.Minute }})
		b.reviewer.finish(t, 9, 0, answer(9, "approved"))
		synctest.Wait()

		time.Sleep(64 * time.Second)
		synctest.Wait()
		if rev.procs[9].stops.Load() != 0 {
			t.Error("a review that had ended was stopped by its guard")
		}
		b.reviewer.finish(t, 8, 0, answer(8, "approved"))
		<-b.results
	})
}

func TestQuitStopsTheRunningReviewsAndNeverStartsTheQueued(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		rev := &reviewer{onStop: proc.Exit{Code: 10}}
		b := newBench(t, setup{prs: []int{9, 8, 7}, jobs: 2, reviewer: rev})

		b.eng.Do(Quit{})
		result := <-b.results

		if result.Code != ExitInterrupted {
			t.Errorf("code %d, want %d", result.Code, ExitInterrupted)
		}
		if got := b.reviewer.started(); !slices.Equal(got, []int{9, 8}) {
			t.Errorf("started %v: a queued review started after the quit", got)
		}
		if len(result.Final.Jobs) != 2 {
			t.Fatalf("the final snapshot has %d jobs, want the two that ran", len(result.Final.Jobs))
		}
		for _, j := range result.Final.Jobs {
			if j.State != JobFailed || j.Outcome != "stopped" {
				t.Errorf("PR #%d: state %d, outcome %q; want a stopped review, not exit 10, which would read as an outage", j.PR, j.State, j.Outcome)
			}
		}
		if rev.procs[9].stops.Load() != 1 || rev.procs[8].stops.Load() != 1 {
			t.Errorf("stops: %d and %d, want one each", rev.procs[9].stops.Load(), rev.procs[8].stops.Load())
		}
		if len(b.asked) != 0 || len(b.recorded) != 0 {
			t.Error("a stopped review was read back or recorded")
		}
		journal := b.journal()
		if journal[len(journal)-3] != "interrupted" {
			t.Errorf("journal = %v, want the interrupt before the two results", journal)
		}
	})
}

func TestQuitKeepsTheResultOfAReviewThatHadAlreadyEnded(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		release := make(chan struct{})
		rev := &reviewer{onStop: proc.Exit{Signaled: true}}
		b := newBench(t, setup{prs: []int{9, 8}, jobs: 2, reviewer: rev, verdict: func(int) review.Readback {
			<-release
			return review.Readback{Kind: review.ReadbackLanded, Word: "approved"}
		}})
		b.reviewer.finish(t, 9, 0, answer(9, "approved"))
		synctest.Wait()

		b.eng.Do(Quit{})
		synctest.Wait()
		if got := b.eng.Snapshot().Phase; got != Stopping {
			t.Fatalf("phase %d, want Stopping while PR #9's result is read", got)
		}
		if rev.procs[9].stops.Load() != 0 {
			t.Error("a review whose process had ended was stopped")
		}
		close(release)
		result := <-b.results

		if nine := b.job(t, 9); nine.State != JobDone || nine.Verdict != "approved" {
			t.Errorf("PR #9: state %d, verdict %q; want its result kept", nine.State, nine.Verdict)
		}
		if eight := b.job(t, 8); eight.State != JobFailed || eight.Outcome != "stopped" {
			t.Errorf("PR #8: state %d, outcome %q", eight.State, eight.Outcome)
		}
		if result.Code != ExitInterrupted {
			t.Errorf("code %d", result.Code)
		}
	})
}
