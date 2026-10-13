package panel

import (
	"context"
	"slices"
	"strings"
	"testing"
	"time"
)

func ids(outcomes []Outcome) []string {
	out := make([]string, len(outcomes))
	for i, o := range outcomes {
		out[i] = o.ID
	}
	return out
}

func TestEveryPanelistRunsAndTheOutcomesComeBackInPanelOrder(t *testing.T) {
	starter := newFakeStarter(func(id string, _ int) act {
		if id == "codex" {
			// Slower than the rest, so it finishes last.
			time.Sleep(30 * time.Millisecond)
		}
		return answer(id + "-model")
	})
	r, out, log := testRun(t, starter)
	panel, cwds := panelOf("codex", "claude", "opencode")

	outcomes := r.fanOut(context.Background(), panel, cwds)

	if got := ids(outcomes); !slices.Equal(got, []string{"codex", "claude", "opencode"}) {
		t.Errorf("outcomes in order %q", got)
	}
	for _, o := range outcomes {
		if !o.Clean() || o.Model != o.ID+"-model" {
			t.Errorf("%+v", o)
		}
	}
	for _, want := range []string{"## codex / codex-model (exit 0)", "## opencode / opencode-model (exit 0)", "[LOW] a.rs:1 — nit"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("stdout lacks %q:\n%s", want, out.String())
		}
	}
	if !strings.Contains(log.String(), "panel: claude started (cwd=/wt/claude)\n") {
		t.Errorf("log:\n%s", log.String())
	}
	for _, c := range starter.commands() {
		if c.Stdin != r.promptPath || slices.Contains(c.Args, "the prompt") {
			t.Errorf("the prompt must arrive on stdin, never in argv: %+v", c)
		}
	}
}

func TestAPanelistThatReturnsNothingIsRetriedOnce(t *testing.T) {
	starter := newFakeStarter(func(id string, attempt int) act {
		if id == "codex" && attempt == 1 {
			return act{stderr: "transient blip\n", code: 1}
		}
		return answer(id + "-model")
	})
	r, out, log := testRun(t, starter)
	panel, cwds := panelOf("codex", "claude")

	outcomes := r.fanOut(context.Background(), panel, cwds)

	if !outcomes[0].Answered() || !outcomes[0].Retried {
		t.Errorf("codex: %+v", outcomes[0])
	}
	if !strings.Contains(log.String(), "panel: codex produced nothing (no output (exit 1): transient blip); trying once more\n") {
		t.Errorf("log:\n%s", log.String())
	}
	if strings.Count(out.String(), "## codex /") != 1 {
		t.Errorf("the failed first attempt was reported as a section:\n%s", out.String())
	}
}

func TestAPanelistThatNeverAnswersIsReportedAndTheRestStillRun(t *testing.T) {
	starter := newFakeStarter(func(id string, _ int) act {
		if id == "codex" {
			return act{stderr: "quota exceeded\n", code: 1}
		}
		return answer(id + "-model")
	})
	r, out, _ := testRun(t, starter)
	panel, cwds := panelOf("codex", "claude")

	outcomes := r.fanOut(context.Background(), panel, cwds)

	if outcomes[0].Answered() || outcomes[0].Failure != "no output (exit 1): quota exceeded" {
		t.Errorf("codex: %+v", outcomes[0])
	}
	if starter.attempts["codex"] != 2 {
		t.Errorf("codex ran %d times, want 2", starter.attempts["codex"])
	}
	if !strings.Contains(out.String(), "FAILED: no output (exit 1): quota exceeded") || !strings.Contains(out.String(), "## claude / claude-model") {
		t.Errorf("stdout:\n%s", out.String())
	}
}

func TestATimeoutIsNotRetried(t *testing.T) {
	starter := newFakeStarter(func(string, int) act { return act{code: exitTimeout} })
	r, _, _ := testRun(t, starter)
	panel, cwds := panelOf("codex")
	outcomes := r.fanOut(context.Background(), panel, cwds)
	if outcomes[0].Failure != "timed out" || starter.attempts["codex"] != 1 {
		t.Errorf("%+v after %d attempts", outcomes[0], starter.attempts["codex"])
	}
}

func TestAPanelistThatCannotStartCostsOneVoiceNotTheRun(t *testing.T) {
	starter := newFakeStarter(func(id string, _ int) act {
		if id == "codex" {
			return act{failStart: true}
		}
		return answer("m")
	})
	r, out, log := testRun(t, starter)
	panel, cwds := panelOf("codex", "claude")

	outcomes := r.fanOut(context.Background(), panel, cwds)

	if outcomes[0].Answered() || !strings.HasPrefix(outcomes[0].Failure, "could not start: ") || !outcomes[1].Answered() {
		t.Errorf("outcomes %+v", outcomes)
	}
	if !strings.Contains(log.String(), "panel: codex could not start: fork/exec dash-p: no such file or directory\n") {
		t.Errorf("log:\n%s", log.String())
	}
	if !strings.Contains(out.String(), "## codex / unknown (exit killed)") {
		t.Errorf("stdout:\n%s", out.String())
	}
}

func TestAWedgedPanelistIsStoppedAtItsDeadline(t *testing.T) {
	starter := newFakeStarter(func(id string, _ int) act {
		if id == "codex" {
			return act{hang: true}
		}
		return answer("m")
	})
	r, out, _ := testRun(t, starter)
	r.timeoutSecs = 0
	r.grace = 20 * time.Millisecond
	panel, cwds := panelOf("codex", "claude")

	outcomes := r.fanOut(context.Background(), panel, cwds)

	if outcomes[0].Failure != "timed out and had to be killed" || starter.attempts["codex"] != 1 {
		t.Errorf("codex: %+v after %d attempts", outcomes[0], starter.attempts["codex"])
	}
	if !outcomes[1].Answered() || !strings.Contains(out.String(), "## claude / m") {
		t.Errorf("the rest of the panel was not reported:\n%s", out.String())
	}
}

func TestAnInterruptStopsThePanelAndKeepsWhatFinished(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	starter := newFakeStarter(func(id string, _ int) act {
		if id == "codex" {
			return act{hang: true, stdout: "Model: half\n- [HIGH] a.go:1 — half a review"}
		}
		return answer("m")
	})
	r, out, log := testRun(t, starter)
	panel, cwds := panelOf("codex", "claude")
	time.AfterFunc(50*time.Millisecond, cancel)

	outcomes := r.fanOut(ctx, panel, cwds)

	if outcomes[0].Failure != "interrupted" || !outcomes[0].Answered() {
		t.Errorf("codex: %+v", outcomes[0])
	}
	if !outcomes[1].Clean() {
		t.Errorf("claude finished before the signal and must keep its own exit: %+v", outcomes[1])
	}
	if !strings.Contains(log.String(), "\npanel: interrupted; stopping 1 panelist\n") {
		t.Errorf("log:\n%s", log.String())
	}
	if !strings.Contains(out.String(), "half a review") {
		t.Errorf("a review stopped mid-run is still printed:\n%s", out.String())
	}
}

func TestASignalBeforeThePanelStartsStartsNobody(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	starter := newFakeStarter(func(string, int) act { return answer("m") })
	r, out, log := testRun(t, starter)
	panel, cwds := panelOf("codex", "claude")

	outcomes := r.fanOut(ctx, panel, cwds)

	if len(starter.commands()) != 0 {
		t.Errorf("started %d panelists after the signal", len(starter.commands()))
	}
	for _, o := range outcomes {
		if o.Failure != "interrupted before it started" {
			t.Errorf("%+v", o)
		}
	}
	if !strings.Contains(log.String(), "panel: interrupted before codex started\n") || strings.Count(out.String(), "FAILED: interrupted before it started") != 2 {
		t.Errorf("log:\n%s\nstdout:\n%s", log.String(), out.String())
	}
}
