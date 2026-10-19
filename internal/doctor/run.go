// Package doctor is `autoreview doctor`: is the panel answering, what did
// the last day of reviews cost, and which PRs got no verdict because a
// panelist was missing.
//
// A review that loses a panelist still finishes. It posts a comment
// instead of a verdict, and the run counts the PR as reviewed. Nothing on
// the screen is red. The ledger is where that shows, and only as a
// pattern over many reviews, so this reads it and asks the panel directly.
package doctor

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/venabots/autoreview/internal/cli"
	"github.com/venabots/autoreview/internal/ledger"
	"github.com/venabots/autoreview/internal/panel"
	"github.com/venabots/autoreview/internal/sanitize"
)

// The exit statuses of the subcommand.
const (
	ExitOK = 0
	// ExitProblem is a doctor that found something, or could not look.
	ExitProblem = 1
)

// Deps is the world the doctor reads. The caller hands it in, so a test
// runs no process and reads no real ledger.
type Deps struct {
	Env func(string) string
	Now time.Time
	// Panelists is the panel the settings file names, or nil.
	Panelists []string
	// Has says a program is on PATH.
	Has func(string) bool
	// Probe asks each panelist now.
	Probe func(context.Context, []panel.Spec) []Check
	// Version is the first line of a program's --version, and false when
	// it is not installed.
	Version func(string) (string, bool)
	Stdout  io.Writer
	Stderr  io.Writer
}

// Run is `autoreview doctor ...`. args are the arguments after the word
// "doctor". It returns the exit status.
func Run(ctx context.Context, args []string, d Deps) int {
	opts, err := parse(args, d.Now.Unix())
	if err != nil {
		say(d.Stderr, "error: %s\n", sanitize.Line(err.Error()))
		if errors.Is(err, errUnknownArg) {
			say(d.Stderr, "%s", Help)
		}
		return ExitProblem
	}
	if opts.help {
		say(d.Stdout, "%s", Help)
		return ExitOK
	}
	seated, err := panelOf(d.Env(cli.EnvPanelists), d.Panelists, d.Has)
	if err != nil {
		say(d.Stderr, "error: %s\n", sanitize.Line(err.Error()))
		return ExitProblem
	}
	r := report{now: d.Now, seated: seated, probed: !opts.noProbe, window: opts.window, tools: toolsOf(d.Version)}
	r = withLedger(r, opts, d.Env)
	if r.probed {
		r.checks = d.Probe(ctx, seated.Specs)
	}
	say(d.Stdout, "%s", render(r))
	if len(r.problems()) > 0 {
		return ExitProblem
	}
	return ExitOK
}

// withLedger is the report with the window's history, or with why there
// is none. A ledger that cannot be read must not hide the probe.
func withLedger(r report, opts options, env func(string) string) report {
	path, ok := opts.ledger, opts.ledger != ""
	if !ok {
		path, ok = ledger.Path(env)
	}
	if !ok {
		r.ledgerErr = "the ledger is off ($AUTOREVIEW_LEDGER=off)"
		return r
	}
	records, err := ledger.Read(path)
	if err != nil {
		r.ledgerErr = fmt.Sprintf("could not read %s: %v", path, err)
		return r
	}
	r.ledger = path
	r.history = historyOf(records, opts.since)
	return r
}

func say(w io.Writer, format string, args ...any) {
	_, _ = fmt.Fprintf(w, format, args...)
}
