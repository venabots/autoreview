package setup

import (
	"io"
	"reflect"
	"strings"
	"testing"

	"github.com/venabots/autoreview/internal/config"
)

// lines gives one line for each read. The form makes a new scanner for
// each question, so a reader that handed over all the text at once would
// leave every question after the first with nothing to read.
type lines struct{ rest []string }

func (l *lines) Read(p []byte) (int, error) {
	if len(l.rest) == 0 {
		return 0, io.EOF
	}
	n := copy(p, l.rest[0]+"\n")
	l.rest = l.rest[1:]
	return n, nil
}

func current() config.Values {
	return config.Values{
		Orchestrator: "claude", Fallback: "auto", Jobs: 2, Timeout: 3600, Verdicts: "approve-and-request-changes",
		Watch: "2m", Rest: "30m", MaxPasses: 3, MaxIdle: 3, CIWait: "30m", AutofixCap: 3, Synth: "claude",
	}
}

func ask(t *testing.T, answers ...string) (config.Values, string, error) {
	t.Helper()
	var out strings.Builder
	got, err := Ask(current(), "/home/me/.config/autoreview/config.toml", Terminal{In: &lines{rest: answers}, Out: &out, Accessible: true})
	return got, out.String(), err
}

func TestTheFormReturnsTheAnswersAndKeepsWhatItDidNotAsk(t *testing.T) {
	got, out, err := ask(t, "codex:gpt-5.5", "claude:opus-4.8 codex", "3", "2", "1h", "5m", "5", "4")
	if err != nil {
		t.Fatalf("Ask: %v\n%s", err, out)
	}
	want := current()
	want.Orchestrator, want.Panelists, want.Verdicts = "codex:gpt-5.5", []string{"claude:opus-4.8", "codex"}, "comment-only"
	want.MaxPasses, want.Rest, want.Watch, want.AutofixCap, want.Jobs = 2, "1h", "5m", 5, 4
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Ask = %+v, want %+v", got, want)
	}
	if !strings.Contains(out, "These go to /home/me/.config/autoreview/config.toml") {
		t.Errorf("the form does not say where the settings go:\n%s", out)
	}
}

// A bad answer is asked again, with the reason, and never comes back.
func TestABadAnswerIsAskedAgain(t *testing.T) {
	got, out, err := ask(t, "gemini", "claude", "codex", "", "1", "0", "3", "soon", "30m", "2m", "3", "2")
	if err != nil {
		t.Fatalf("Ask: %v\n%s", err, out)
	}
	if got.Orchestrator != "claude" || len(got.Panelists) != 0 || got.MaxPasses != 3 || got.Rest != "30m" {
		t.Errorf("Ask = %+v", got)
	}
	for _, reason := range []string{"unknown orchestrator", "a panel of one never approves", "a whole number, 1 or more", "invalid wait interval"} {
		if !strings.Contains(out, reason) {
			t.Errorf("the form does not say %q:\n%s", reason, out)
		}
	}
}

func TestTheChecksMatchTheFlags(t *testing.T) {
	for _, c := range []struct {
		check func(string) error
		ok    []string
		bad   []string
	}{
		{orchestrator, []string{"claude", "codex:gpt-5.5"}, []string{"gemini", "codex:"}},
		{panelists, []string{"", "claude codex", "claude:opus-4.8 opencode:glm-5.2"}, []string{"claude", "claude gemini"}},
		{atLeastOne, []string{"1", " 12 "}, []string{"0", "-1", "two", ""}},
		{interval, []string{"30", "30m", "2h"}, []string{"0", "soon", ""}},
	} {
		for _, raw := range c.ok {
			if err := c.check(raw); err != nil {
				t.Errorf("%q refused: %v", raw, err)
			}
		}
		for _, raw := range c.bad {
			if c.check(raw) == nil {
				t.Errorf("%q passed", raw)
			}
		}
	}
}
