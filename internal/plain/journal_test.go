package plain

import (
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/venabots/autoreview/internal/engine"
	"github.com/venabots/autoreview/internal/review"
)

func ptr[T any](v T) *T { return &v }

func blocked() *review.Trailer {
	return &review.Trailer{
		Findings: &review.Findings{MustFix: ptr(uint(1)), ShouldFix: ptr(uint(0)), Polish: ptr(uint(0))},
		Blockers: []review.Blocker{{Severity: "HIGH", Domain: "data", Reversible: ptr(true), Location: "db/migrate.go:12", Gist: "the migration drops a column that is still read"}},
	}
}

func TestSay(t *testing.T) {
	cases := []struct {
		name      string
		ev        engine.Event
		out, errs string
	}{
		{"nothing to review", engine.NothingToReview{}, "no PR is ready for review\n", ""},
		{"a pass under its limit", engine.PassStarted{Pass: 1, Reviews: 1, Limit: 2, Dir: "/logs/run-1/pass-1"},
			"reviewing 1 PR\nlogs: /logs/run-1/pass-1\n\n", ""},
		{"a pass the limit holds back", engine.PassStarted{Pass: 1, Reviews: 5, Limit: 2, Dir: "/logs/run-1/pass-1"},
			"reviewing 5 PRs, 2 at a time\nlogs: /logs/run-1/pass-1\n\n", ""},
		{"a review starts", engine.JobStarted{Job: engine.Job{PR: 9, Author: "alice"}}, "start   #9 @alice (reviewing)\n", ""},
		{"a re-check starts", engine.JobStarted{Job: engine.Job{PR: 9, Author: "alice", Resumed: true}}, "start   #9 @alice (rechecking)\n", ""},
		{"a review with no author", engine.JobStarted{Job: engine.Job{PR: 9}}, "start   #9 (reviewing)\n", ""},
		{"an author with an escape", engine.JobStarted{Job: engine.Job{PR: 9, Author: "al\x1bice"}}, "start   #9 @alice (reviewing)\n", ""},
		{"a review that approved", engine.JobFinished{Job: engine.Job{PR: 9, State: engine.JobDone, Verdict: "approved", Elapsed: 63 * time.Second, Trailer: blocked()}},
			"done    #9 (1m03s)\n", ""},
		{"a review that did not approve", engine.JobFinished{Job: engine.Job{PR: 9, State: engine.JobDone, Verdict: "changes requested", Elapsed: 63 * time.Second, Trailer: blocked()}},
			"done    #9 (1m03s)\n        not approved yet because:\n        - [HIGH] (data, reversible) db/migrate.go:12 — the migration drops a column that is still read\n", ""},
		{"a review that failed", engine.JobFinished{Job: engine.Job{PR: 9, State: engine.JobFailed, Outcome: "exit 10", Error: "usage limit", Elapsed: 3 * time.Second}},
			"FAILED  #9 (exit 10, 3s): usage limit\n", ""},
		{"a review that failed without a reason", engine.JobFinished{Job: engine.Job{PR: 9, State: engine.JobFailed, Outcome: "no result", Elapsed: 3 * time.Second}},
			"FAILED  #9 (no result, 3s)\n", ""},
		{"a review that timed out", engine.JobFinished{Job: engine.Job{PR: 9, State: engine.JobTimedOut, Elapsed: time.Hour}}, "TIMEOUT #9 (1h00m)\n", ""},
		{"a note", engine.Noted{Text: "PR #9 has a review session open\x1b[2J elsewhere"}, "", "note: PR #9 has a review session open[2J elsewhere\n"},
		{"an interrupt", engine.Interrupted{}, "", "\ninterrupted; stopping running reviews\n"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := say(c.ev)
			if got.out != c.out || got.errs != c.errs {
				t.Errorf("say = %q and %q, want %q and %q", got.out, got.errs, c.out, c.errs)
			}
		})
	}
}

func TestSayPrintsTheListsAfterTheFirstLook(t *testing.T) {
	s := snapshot()
	s.MineErr = "search is down"
	got := say(engine.Looked{Snapshot: s})
	if !strings.HasPrefix(got.out, "acme/widgets\nreview list: 2 PRs\n") || got.errs != "note: could not read your PRs: search is down\n" {
		t.Errorf("say = %q and %q", got.out, got.errs)
	}
}

func TestACleanReviewThatTooFewAnsweredSaysSo(t *testing.T) {
	job := engine.Job{PR: 9, State: engine.JobDone, Verdict: "commented", Trailer: &review.Trailer{
		Findings: &review.Findings{MustFix: ptr(uint(0)), ShouldFix: ptr(uint(0)), Polish: ptr(uint(0))},
		Panel:    []review.Panelist{{Name: "a", OK: ptr(true)}, {Name: "b", OK: ptr(false)}, {Name: "c", OK: ptr(false)}},
	}}
	want := "done    #9 (0s)\n        looks clean, but only 1 of 3 reviewers answered; approval needs 3\n"
	if got := say(engine.JobFinished{Job: job}).out; got != want {
		t.Errorf("say = %q, want %q", got, want)
	}
}

func finalOf(jobs ...engine.Job) engine.Result {
	return engine.Result{Final: &engine.Snapshot{Jobs: jobs}}
}

func TestFinal(t *testing.T) {
	done := engine.Job{PR: 9, State: engine.JobDone, Verdict: "approved", PassDir: "/logs/run-1/pass-1", Session: "s", Orchestrator: review.Claude()}
	failed := engine.Job{PR: 8, State: engine.JobFailed, Outcome: "exit 10", PassDir: "/logs/run-1/pass-1"}
	timedOut := engine.Job{PR: 7, State: engine.JobTimedOut, PassDir: "/logs/run-1/pass-1"}

	out, errs := Final(finalOf(done), false)
	want := "\nPR  RESULT  VERDICT   RISK  FINDINGS  TIME  COST  MODEL  SESSION\n#9  done    approved  -     -         0s    -     -      s\n\nlogs: /logs/run-1/pass-1\nreopen any review with: claude --resume <SESSION>\n"
	if out != want || errs != "" {
		t.Errorf("Final = %q and %q, want %q and nothing", out, errs, want)
	}

	out, errs = Final(finalOf(done, failed, timedOut), false)
	if errs != "error: 2 of 3 reviews failed\n" || !strings.Contains(out, "#8  failed (exit 10)") {
		t.Errorf("Final = %q and %q", out, errs)
	}

	_, errs = Final(finalOf(failed), false)
	if errs != "error: 1 of 1 review failed\n" {
		t.Errorf("Final errs = %q", errs)
	}

	out, _ = Final(finalOf(done), true)
	if !strings.HasSuffix(out, "nothing was posted to any PR; the reviews are in /logs/run-1/pass-1\n") {
		t.Errorf("Final under --no-post = %q", out)
	}

	if out, errs := Final(finalOf(), false); out != "" || errs != "" {
		t.Errorf("Final of a run that reviewed nothing = %q and %q", out, errs)
	}
}

// slowWriter blocks every write until it is released, like a pager that
// has stopped reading.
type slowWriter struct {
	release chan struct{}
	mu      sync.Mutex
	got     strings.Builder
}

func (w *slowWriter) Write(p []byte) (int, error) {
	<-w.release
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.got.Write(p)
}

func TestThePrinterNeverMakesTheEngineWait(t *testing.T) {
	out := &slowWriter{release: make(chan struct{})}
	var errs, log strings.Builder
	p := NewPrinter(out, &errs, &log)

	emitted := make(chan struct{})
	go func() {
		for pr := range 50 {
			p.Emit(engine.JobStarted{Job: engine.Job{PR: pr + 1}})
		}
		close(emitted)
	}()
	select {
	case <-emitted:
	case <-time.After(5 * time.Second):
		t.Fatal("Emit blocked on a writer that had stopped reading")
	}

	close(out.release)
	p.Close()
	lines := strings.Split(strings.TrimSuffix(out.got.String(), "\n"), "\n")
	if len(lines) != 50 || lines[0] != "start   #1 (reviewing)" || lines[49] != "start   #50 (reviewing)" {
		t.Errorf("the printer wrote %d lines, first %q, last %q; want all 50 in order", len(lines), lines[0], lines[len(lines)-1])
	}
	if log.String() != out.got.String() {
		t.Error("the run log does not hold what was printed")
	}
}

func TestThePrinterSendsNotesToStderrAndBothToTheLog(t *testing.T) {
	var out, errs, log strings.Builder
	p := NewPrinter(&out, &errs, &log)
	p.Emit(engine.JobStarted{Job: engine.Job{PR: 9}})
	p.Emit(engine.Noted{Text: "careful"})
	p.Close()
	p.Emit(engine.Noted{Text: "after the close"})
	p.Finish(finalOf(engine.Job{PR: 9, State: engine.JobFailed, Outcome: "exit 10", PassDir: "/p"}), false)

	if out.String() != "start   #9 (reviewing)\n\nPR  RESULT            VERDICT         RISK  FINDINGS  TIME  COST  MODEL  SESSION\n#9  failed (exit 10)  nothing posted  -     -         0s    -     -      -\n\nlogs: /p\nreopen any review with: claude --resume <SESSION>\n" {
		t.Errorf("stdout = %q", out.String())
	}
	if errs.String() != "note: careful\nerror: 1 of 1 review failed\n" {
		t.Errorf("stderr = %q", errs.String())
	}
	if !strings.Contains(log.String(), "start   #9") || !strings.Contains(log.String(), "note: careful\n") || !strings.Contains(log.String(), "error: 1 of 1 review failed\n") {
		t.Errorf("the log = %q, want both streams", log.String())
	}
}

func TestAPrinterWithNoLogStillPrints(t *testing.T) {
	var out, errs strings.Builder
	p := NewPrinter(&out, &errs, nil)
	p.Emit(engine.NothingToReview{})
	p.Close()
	if out.String() != "no PR is ready for review\n" {
		t.Errorf("stdout = %q", out.String())
	}
}

func TestAfterViewIsOneLine(t *testing.T) {
	result := engine.Result{Final: &engine.Snapshot{RunDir: "/logs/run-1", Jobs: []engine.Job{
		{PR: 9, State: engine.JobDone, Verdict: "approved"},
		{PR: 8, State: engine.JobDone, Verdict: "commented"},
		{PR: 7, State: engine.JobFailed, Outcome: "stopped"},
	}, Earlier: []engine.Job{{PR: 6, State: engine.JobDone, Verdict: "commented"}}}}

	out, errs := AfterView(result)
	if want := "4 reviews: 2 commented, 1 approved, 1 stopped · summary in /logs/run-1/autoreview.log\n"; out != want {
		t.Errorf("out = %q, want %q", out, want)
	}
	if errs != "error: 1 of 4 reviews failed\n" {
		t.Errorf("errs = %q", errs)
	}
	if out, errs := AfterView(engine.Result{Final: &engine.Snapshot{}}); out != "" || errs != "" {
		t.Errorf("a run that reviewed nothing left %q and %q", out, errs)
	}
}

func TestARetryIsOneLine(t *testing.T) {
	job := engine.Job{PR: 9, Author: "alice", Orchestrator: review.Orchestrator{Backend: "codex"},
		First: &engine.Attempt{Orchestrator: review.Claude(), Outcome: "exit 10", Error: "usage limit"}}
	if got := say(engine.JobRetrying{Job: job}).out; got != "RETRY   #9 (claude exit 10: usage limit · retrying with codex)\n" {
		t.Errorf("say = %q", got)
	}
	if got := say(engine.JobStarted{Job: job}).out; got != "start   #9 @alice (reviewing with codex)\n" {
		t.Errorf("say = %q", got)
	}
}
