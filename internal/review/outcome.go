package review

import "fmt"

// State is how a finished job ended. The zero value is not a state, so a
// job that nobody classified cannot read as done.
type State int

const (
	// Done means the reviewer exited 0.
	Done State = iota + 1
	// Failed means it exited with an error, or was killed from outside.
	Failed
	// TimedOut means dash-p reported a timeout, or the guard killed the job.
	TimedOut
)

// Exit is how a process ended.
type Exit struct {
	// Code is the exit code. It means nothing when Signaled is true.
	Code int
	// Signaled says the process died of a signal and left no code.
	Signaled bool
}

// Classify says what a finished job's exit means.
//
// The guard outranks everything: a job the run had to kill timed out,
// whatever exit status the kill produced. Exit 20 is dash-p's code for a
// timeout, so from an override it is only a failure. A signal is a kill
// from outside, and there is no result to read.
func Classify(e Exit, guardTripped, override bool) State {
	switch {
	case guardTripped:
		return TimedOut
	case e.Signaled:
		return Failed
	case e.Code == 0:
		return Done
	case e.Code == 20 && !override:
		return TimedOut
	}
	return Failed
}

// OutcomeWord says how a job failed, in the words a reader needs: that a
// person stopped it, its exit code when there was one, and otherwise that
// it left no result at all, as an out-of-memory kill does.
//
// A stop outranks the exit code. Whatever the kill left behind, a review
// that a person stopped says so and is not shown as a crash.
func OutcomeWord(e Exit, stopped bool) string {
	switch {
	case stopped:
		return "stopped"
	case e.Signaled:
		return "no result"
	}
	return fmt.Sprintf("exit %d", e.Code)
}
