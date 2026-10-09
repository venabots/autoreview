package panel

import (
	"strings"
	"testing"
	"time"
)

func TestOffATerminalEachStepIsOnePlainLineAndATickSaysNothing(t *testing.T) {
	var w syncBuffer
	s := NewStatus(&w, false)
	s.Step("reading the repo")
	s.Tick("2 panelists still reviewing, 3s")
	s.Say("panel: codex started (cwd=/x)")
	s.Suspend(func() { _, _ = w.Write([]byte("a section\n")) })
	s.Clear()
	s.Clear()
	want := "reading the repo\npanel: codex started (cwd=/x)\na section\n"
	if w.String() != want {
		t.Errorf("wrote %q, want %q", w.String(), want)
	}
}

func TestOnATerminalTheSpinnerStepsAsideAndLeavesNothingBehind(t *testing.T) {
	var w syncBuffer
	s := NewStatus(&w, true)
	s.Step("building the diff")
	time.Sleep(3 * spinnerEvery)
	s.Say("panel: codex started")
	s.Clear()
	got := w.String()
	if !strings.Contains(got, "building the diff") {
		t.Errorf("the step was never drawn: %q", got)
	}
	if !strings.Contains(got, clearLine+"panel: codex started\n") {
		t.Errorf("a line that stays was written over the spinner: %q", got)
	}
	if !strings.HasSuffix(got, clearLine) {
		t.Errorf("the spinner left a trace: %q", got)
	}
	before := w.String()
	time.Sleep(2 * spinnerEvery)
	if w.String() != before {
		t.Error("the spinner drew after Clear")
	}
}
