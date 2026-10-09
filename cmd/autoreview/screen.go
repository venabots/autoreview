package main

import (
	"errors"
	"io"
	"os"
	"runtime/debug"

	tea "charm.land/bubbletea/v2"

	"github.com/venabots/autoreview/internal/cli"
	"github.com/venabots/autoreview/internal/engine"
	"github.com/venabots/autoreview/internal/plain"
	"github.com/venabots/autoreview/internal/repo"
	"github.com/venabots/autoreview/internal/sanitize"
	"github.com/venabots/autoreview/internal/tui"
)

// exitCrashed is the status of a run that hit a bug in autoreview itself.
const exitCrashed = 2

// runScreen runs a held run under the full-screen view and returns the
// run's exit status. The summary is printed when the view has closed, on
// the normal screen, where it stays.
//
// The engine decides when the run is over. The view closes on the engine's
// last snapshot, and a view that dies first still ends the run: the Quit
// after it is what guarantees nothing is left running behind a dead screen.
func runScreen(cfg cli.Config, rc repo.Context, stdout, stderr io.Writer) int {
	run, err := start(cfg, rc, true, stdout, stderr)
	if err != nil {
		say(stderr, "error: %s\n", sanitize.Line(err.Error()))
		return engine.ExitFailed
	}
	defer run.close()
	program := tea.NewProgram(tui.New(run.engine, actions(rc)), tea.WithoutSignalHandler())

	results := make(chan engine.Result, 1)
	go func() {
		// A panic on this goroutine would end the process with the terminal
		// still in raw mode on the alternate screen. Give it back first.
		defer func() {
			if p := recover(); p != nil {
				program.Kill()
				say(stderr, "autoreview crashed: %v\n%s", p, debug.Stack())
				os.Exit(exitCrashed)
			}
		}()
		results <- run.engine.Run()
	}()

	_, viewErr := program.Run()
	run.engine.Do(engine.Quit{})
	result := <-results
	discardInput()
	run.printer.Close()

	// The full summary goes to the log, and one line to the screen.
	summary, failures := plain.Final(result, cfg.NoPost)
	say(run.log, "%s%s", summary, failures)
	out, errs := plain.AfterView(result)
	say(stdout, "%s", out)
	say(stderr, "%s", errs)
	if viewErr != nil && !errors.Is(viewErr, tea.ErrProgramKilled) {
		say(stderr, "error: the view stopped: %v\n", viewErr)
		return engine.ExitFailed
	}
	return result.Code
}
