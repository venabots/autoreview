// Package stats is `autoreview stats`: how each panelist model has done,
// across every review the ledger holds.
//
// A cohort is one model on one backend. The ledger can say how often it came
// back with a review at all, how much it reported, and how much of that the
// synthesis kept after it checked the finding against the code. That check
// is the only verification step in the pipeline, so "kept" is the closest
// the data has to "right". Every ratio is shown with its numerator, its
// denominator and a Wilson interval.
//
// The numbers are v1's. v1 and v2 read the same file, and a model must not
// score differently because of which version was asked.
package stats

import (
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/venabots/autoreview/internal/ledger"
	"github.com/venabots/autoreview/internal/words"
)

// The exit statuses of the subcommand.
const (
	exitOK  = 0
	exitBad = 1
)

// Run is `autoreview stats ...`. args are the arguments after the word
// "stats". It returns the exit status; everything it has to say is written
// to stdout or stderr by then.
//
// now is passed in so that `--since 7d` gives the same answer twice in a
// test. onTerminal chooses the table over the aligned text. `--json` is the
// same in both, because the reader of it is a program.
func Run(args []string, env func(string) string, now time.Time, onTerminal bool, stdout, stderr io.Writer) int {
	opts, err := parse(args, now.Unix())
	if err != nil {
		say(stderr, "error: %v\n", err)
		if errors.Is(err, errUnknownArg) {
			say(stderr, "%s", Help)
		}
		return exitBad
	}
	if opts.help {
		say(stdout, "%s", Help)
		return exitOK
	}
	path, ok := ledgerPath(opts, env)
	if !ok {
		say(stderr, "error: the ledger is off ($AUTOREVIEW_LEDGER=off); pass --ledger PATH to read one anyway\n")
		return exitBad
	}
	runs, err := ledger.Read(path)
	if err != nil {
		say(stderr, "error: could not read the ledger at %s: %v\n", path, err)
		return exitBad
	}
	selected := selectRuns(runs, opts.since, opts.repo)
	// An empty result under --json is still JSON. A tool that reads the
	// output must not get a sentence where it expected an object.
	if len(selected) == 0 && !opts.json {
		say(stdout, "%s", nothingToReport(len(runs), path))
		return exitOK
	}
	out, err := render(reportOf(selected, path, opts.since), opts.json, now, onTerminal, env, stdout)
	if err != nil {
		say(stderr, "error: %v\n", err)
		return exitBad
	}
	say(stdout, "%s", out)
	return exitOK
}

func ledgerPath(opts options, env func(string) string) (string, bool) {
	if opts.ledger != "" {
		return opts.ledger, true
	}
	return ledger.Path(env)
}

// nothingToReport says why there is no table: nothing is recorded yet, or
// the filters left nothing.
func nothingToReport(recorded int, path string) string {
	if recorded == 0 {
		return "no reviews recorded yet in " + path + "\n" +
			"every autoreview pass and panel run is recorded from now on\n"
	}
	return fmt.Sprintf("no reviews match (%s recorded in %s)\n", words.Count(recorded, "review"), path)
}

func reportOf(selected []ledger.Record, path string, since *int64) report {
	f := fold(selected)
	return report{
		overview:   overviewOf(selected),
		cohorts:    f.cohorts,
		attention:  attention(f.cohorts),
		unresolved: f.unresolved,
		ledger:     path,
		since:      since,
	}
}

// render is the report in the form that was asked for.
func render(r report, asJSON bool, now time.Time, onTerminal bool, env func(string) string, stdout io.Writer) (string, error) {
	switch {
	case asJSON:
		doc, err := reportJSON(r, now.Unix())
		return doc + "\n", err
	case onTerminal:
		return screen(r, stylesFor(env("NO_COLOR") == ""), widthOf(stdout)), nil
	}
	return plain(r), nil
}

// say writes to the writer the caller handed in. A write that fails has
// nowhere better to be reported: the reader went away.
func say(w io.Writer, format string, args ...any) {
	_, _ = fmt.Fprintf(w, format, args...)
}
