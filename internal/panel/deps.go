package panel

import (
	"fmt"
	"io"
	"strings"

	"github.com/venabots/autoreview/internal/proc"
	"github.com/venabots/autoreview/internal/worktree"
)

// EnvDashp names a dash-p binary to use in place of the one on PATH. It is
// the name the other dash-p tooling reads.
const EnvDashp = "DASHP_BIN"

// Deps is everything a panel run reaches outside itself. cmd/autoreview
// hands in the real ones; a test hands in what it needs to watch.
type Deps struct {
	// Env looks a variable up. An empty value counts as unset.
	Env func(string) string
	// LookPath finds a program, as exec.LookPath does.
	LookPath func(string) (string, error)
	Git      worktree.Git
	Starter  proc.Starter
	// Stdout is the report. Stderr is progress and errors, so a report
	// piped to a file holds nothing else.
	Stdout io.Writer
	Stderr io.Writer
	// Live says Stderr is a terminal the spinner can redraw.
	Live bool
	// Cwd is where the person stands. A relative --tree path is read from
	// it.
	Cwd string
	// TempDir is where the run's directory goes when no --log-dir is given.
	TempDir string
	// PID keys the ledger record, as v1's does.
	PID     int
	Version string
}

// installHint is how to get a missing program.
func installHint(name string) string {
	if name == "dash-p" {
		return "brew install venabots/tap/dash-p, or set $" + EnvDashp
	}
	return "brew install " + name
}

// require says whether every program is installed, and names the first one
// that is not. It is asked before anything is spent: a synthesizer found
// missing after every panelist has been paid for is the worst moment to
// find out.
func (d Deps) require(names ...string) bool {
	for _, name := range names {
		if _, err := d.LookPath(name); err != nil {
			hint := installHint(name)
			if name == d.dashp() {
				hint = installHint("dash-p")
			}
			_, _ = fmt.Fprintf(d.Stderr, "error: missing required command: %s\n  install with: %s\n", name, hint)
			return false
		}
	}
	return true
}

// dashp is the dash-p to run: $DASHP_BIN, or dash-p from PATH.
func (d Deps) dashp() string {
	return orDefault(d.Env(EnvDashp), "dash-p")
}

// panelSpecs is the panel to seat: the one named, or every backend on PATH.
// A named backend that is not installed is a typo worth stopping for. An
// auto-detected one cannot be missing: that is how it was found.
func (d Deps) panelSpecs(cfg Config) ([]Spec, bool) {
	specs := cfg.Panelists
	if len(specs) == 0 {
		specs = Autodetect(func(name string) bool {
			_, err := d.LookPath(name)
			return err == nil
		})
	}
	if len(specs) == 0 {
		_, _ = fmt.Fprintf(d.Stderr, "error: no backend CLIs found on PATH (looked for %s)\n", strings.Join(Backends, ", "))
		return nil, false
	}
	for _, spec := range specs {
		if !d.require(spec.Backend) {
			return nil, false
		}
	}
	if cfg.Synthesize && !d.require(cfg.Synth.Backend) {
		return nil, false
	}
	return specs, true
}
