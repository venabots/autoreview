package panel

import (
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/venabots/autoreview/internal/sanitize"
	"github.com/venabots/autoreview/internal/words"
)

// exitTimeout is dash-p's exit code for a run that hit its own timeout.
const exitTimeout = 20

// Outcome is what one panelist contributed.
type Outcome struct {
	ID string
	// Model is what the panelist said it ran on, or "unknown".
	Model string
	// Exit is the exit code. It means nothing when Exited is false: the
	// process was killed by a signal, or never started.
	Exit   int
	Exited bool
	Stdout string
	// Failure says why the run was not clean, or is "" when it was. That
	// is not the question of whether it produced a review: a panelist can
	// write a whole report and still exit non-zero on the way out.
	Failure string
	Elapsed time.Duration
	Retried bool
}

// Answered says the panelist reviewed anything. This, not Failure, decides
// whether there is something to synthesize: a report that arrived with a
// bad exit status is still a report, and dropping it would throw away the
// work and the money both.
func (o Outcome) Answered() bool {
	return strings.TrimSpace(o.Stdout) != ""
}

// Clean says the run ended with no failure.
func (o Outcome) Clean() bool {
	return o.Failure == ""
}

// exitWord is the exit code, or "killed" for a process that left none.
func (o Outcome) exitWord() string {
	if !o.Exited {
		return "killed"
	}
	return strconv.Itoa(o.Exit)
}

// failureReason is why a finished panelist's run was not clean, in the words
// a reader needs, or "" when it was clean.
func failureReason(exited bool, code int, stdout, stderr string) string {
	empty := strings.TrimSpace(stdout) == ""
	switch {
	case !exited:
		return "killed before it finished"
	case code == exitTimeout:
		return "timed out"
	case code == 0 && empty:
		return "exited 0 but produced no output" + detail(stderr)
	case code == 0:
		return ""
	case empty:
		return fmt.Sprintf("no output (exit %d)%s", code, detail(stderr))
	}
	// Output and a bad status: the words are still worth reading, and the
	// reader should know which half to trust.
	return fmt.Sprintf("exited %d after producing output", code)
}

// detail is the last line of stderr that says anything.
func detail(stderr string) string {
	lines := strings.Split(stderr, "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		if line := strings.TrimSpace(lines[i]); line != "" {
			return ": " + line
		}
	}
	return " and no error detail captured"
}

// worthRetrying says one more try is likely to help. A transient refusal (a
// quota blip, an auth hiccup, a backend that returned nothing) usually
// succeeds the second time. A timeout does not: it would spend the same
// wall clock again and time out again.
func worthRetrying(o Outcome) bool {
	return !o.Retried && !o.Answered() && !o.timedOut()
}

// neverRan is a panelist that did not start. It is an outcome and not an
// error: an early return would abandon the panelists already running, and
// their worktrees would be removed underneath them on the way out.
func neverRan(p Panelist, why string, elapsed time.Duration) Outcome {
	return Outcome{ID: p.ID, Model: unknownModel, Failure: why, Elapsed: elapsed}
}

// writeSection prints one panelist's report to out and its one-line summary
// to log. Everything in it but the id is process output, so all of it is
// sanitized: an escape sequence or a bidi mark in it could repaint or
// reorder the report around it.
func writeSection(out, log io.Writer, o Outcome) {
	model := sanitize.Line(o.Model)
	_, _ = fmt.Fprintf(out, "## %s / %s (exit %s)\n\n", o.ID, model, o.exitWord())
	if !o.Clean() {
		_, _ = fmt.Fprintf(out, "FAILED: %s\n\n", sanitize.Line(o.Failure))
	}
	if o.Answered() {
		_, _ = fmt.Fprintf(out, "%s\n\n", sanitize.Block(strings.TrimRightFunc(o.Stdout, unicode.IsSpace)))
	}
	// To the log, not the report: progress must not land in the middle of
	// a report someone is piping to a file.
	_, _ = fmt.Fprintf(log, "panel: %s (%s) done in %s (exit %s)\n", o.ID, model, words.Duration(o.Elapsed), o.exitWord())
}

// timedOut says dash-p stopped the panelist at its own timeout.
func (o Outcome) timedOut() bool {
	return o.Exited && o.Exit == exitTimeout
}
