package review

import "testing"

func TestClassifySaysWhatAnExitMeans(t *testing.T) {
	killed := Exit{Signaled: true}
	cases := []struct {
		name     string
		exit     Exit
		guard    bool
		override bool
		want     State
	}{
		{"exit 0 is done", Exit{Code: 0}, false, false, Done},
		{"exit 20 is dash-p's timeout", Exit{Code: 20}, false, false, TimedOut},
		{"exit 10 is an agent error", Exit{Code: 10}, false, false, Failed},
		{"any other code is a failure", Exit{Code: 2}, false, false, Failed},
		// Killed from outside: there is no result to read.
		{"a signal is a failure", killed, false, false, Failed},
		// 20 is dash-p's code. From an override it is only a failure.
		{"an override that exits 20 failed", Exit{Code: 20}, false, true, Failed},
		{"an override that exits 0 is done", Exit{Code: 0}, false, true, Done},
		// The guard outranks whatever the kill produced.
		{"the guard outranks a signal", killed, true, false, TimedOut},
		{"the guard outranks exit 0", Exit{Code: 0}, true, false, TimedOut},
		{"the guard outranks exit 10", Exit{Code: 10}, true, false, TimedOut},
		{"the guard outranks an override's exit", Exit{Code: 1}, true, true, TimedOut},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := Classify(c.exit, c.guard, c.override); got != c.want {
				t.Errorf("Classify = %d, want %d", got, c.want)
			}
		})
	}
}

func TestAJobNobodyClassifiedIsNotDone(t *testing.T) {
	var unset State
	for _, state := range []State{Done, Failed, TimedOut} {
		if state == unset {
			t.Errorf("state %d is the zero value, so a job with no state reads as it", state)
		}
	}
}

func TestOutcomeWordSaysHowAJobFailed(t *testing.T) {
	cases := []struct {
		name    string
		exit    Exit
		stopped bool
		want    string
	}{
		{"an exit code", Exit{Code: 10}, false, "exit 10"},
		{"a timeout from dash-p", Exit{Code: 20}, false, "exit 20"},
		{"a kill from outside left nothing", Exit{Signaled: true}, false, "no result"},
		// Whatever the kill left behind, a stopped review says it was
		// stopped.
		{"a stop outranks the exit code", Exit{Code: 10}, true, "stopped"},
		{"a stop outranks the signal that did it", Exit{Signaled: true}, true, "stopped"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := OutcomeWord(c.exit, c.stopped); got != c.want {
				t.Errorf("OutcomeWord = %q, want %q", got, c.want)
			}
		})
	}
}
