package panel

import (
	"strings"
	"testing"
	"time"
)

func outcomeOf(exited bool, code int, stdout, stderr string) Outcome {
	return Outcome{
		ID: "claude", Model: "m", Exit: code, Exited: exited, Stdout: stdout,
		Failure: failureReason(exited, code, stdout, stderr), Elapsed: time.Second,
	}
}

func TestACleanPanelistIsNotAFailure(t *testing.T) {
	o := outcomeOf(true, 0, "Model: m\nGoal (clear): x\nNO_FINDINGS — checked", "")
	if !o.Clean() || !o.Answered() || worthRetrying(o) {
		t.Errorf("%+v", o)
	}
}

func TestAReportWithABadExitStillCountsAsAnswered(t *testing.T) {
	// The distinction that decides whether there is anything to
	// synthesize: this panelist did the work and stumbled on the way out.
	o := outcomeOf(true, 3, "Model: m\n- [HIGH] a.rs:1 — bug", "")
	if !o.Answered() {
		t.Error("a report is a report")
	}
	if o.Clean() || o.Failure != "exited 3 after producing output" {
		t.Errorf("the reader must still be told about the exit: %q", o.Failure)
	}
}

func TestEachWayOfContributingNothingSaysWhichItWas(t *testing.T) {
	cases := []struct {
		o    Outcome
		want string
	}{
		{outcomeOf(true, 20, "", ""), "timed out"},
		{outcomeOf(true, 1, "", "first\nquota exceeded\n\n"), "no output (exit 1): quota exceeded"},
		{outcomeOf(true, 1, "", ""), "no output (exit 1) and no error detail captured"},
		{outcomeOf(true, 0, "", "boom"), "exited 0 but produced no output: boom"},
		{outcomeOf(false, 0, "", ""), "killed before it finished"},
	}
	for _, c := range cases {
		if c.o.Failure != c.want || c.o.Answered() {
			t.Errorf("failure %q, answered %v; want %q, not answered", c.o.Failure, c.o.Answered(), c.want)
		}
	}
}

func TestOnlyAnEmptyTransientFailureIsRetried(t *testing.T) {
	cases := []struct {
		name string
		o    Outcome
		want bool
	}{
		{"a quota blip", outcomeOf(true, 1, "", "quota exceeded"), true},
		{"an empty success", outcomeOf(true, 0, "", ""), true},
		// A timeout would spend the same wall clock to fail the same way.
		{"a timeout", outcomeOf(true, 20, "", ""), false},
		// Something came back: that is the review, not a blip.
		{"a partial review", outcomeOf(true, 1, "Model: m\npartial", ""), false},
	}
	for _, c := range cases {
		if got := worthRetrying(c.o); got != c.want {
			t.Errorf("%s: worthRetrying = %v", c.name, got)
		}
	}
	second := outcomeOf(true, 1, "", "")
	second.Retried = true
	if worthRetrying(second) {
		t.Error("a second attempt was retried again")
	}
}

func TestAPanelistThatNeverStartedIsAnOutcomeNotAnError(t *testing.T) {
	o := neverRan(Panelist{ID: "codex", Backend: "codex"}, "could not start: no such file", 0)
	if o.Answered() || o.Clean() || o.ID != "codex" || o.Exited || o.Model != "unknown" || !strings.HasPrefix(o.Failure, "could not start") {
		t.Errorf("%+v", o)
	}
}

func TestASectionIsSanitizedAndItsSummaryGoesToTheLog(t *testing.T) {
	var out, log strings.Builder
	o := Outcome{
		ID: "codex", Model: "gpt\x1b[2J-5", Exit: 3, Exited: true,
		Stdout:  "Model: gpt-5\n- [LOW] a.go:1 — \u202Enit\n\tindented\n\n",
		Failure: "exited 3 after producing output\x1b]0;x\x07", Elapsed: 75 * time.Second,
	}
	writeSection(&out, &log, o)
	want := "## codex / gpt[2J-5 (exit 3)\n\nFAILED: exited 3 after producing output]0;x\n\nModel: gpt-5\n- [LOW] a.go:1 — nit\n\tindented\n\n"
	if out.String() != want {
		t.Errorf("section:\n%q\nwant:\n%q", out.String(), want)
	}
	if log.String() != "panel: codex (gpt[2J-5) done in 1m15s (exit 3)\n" {
		t.Errorf("log %q", log.String())
	}
	var killed strings.Builder
	writeSection(&killed, &log, neverRan(Panelist{ID: "claude"}, "interrupted before it started", 0))
	if killed.String() != "## claude / unknown (exit killed)\n\nFAILED: interrupted before it started\n\n" {
		t.Errorf("a panelist with no exit: %q", killed.String())
	}
}
