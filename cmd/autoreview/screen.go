package main

import (
	"errors"
	"io"
	"os"
	"runtime/debug"

	tea "charm.land/bubbletea/v2"

	"github.com/venabots/autoreview/internal/engine"
	"github.com/venabots/autoreview/internal/repo"
	"github.com/venabots/autoreview/internal/tui"
)

// exitCrashed is the status of a run that hit a bug in autoreview itself.
const exitCrashed = 2

// runScreen runs the engine under the full-screen view and returns the
// run's exit status.
//
// The engine decides when the run is over. The view closes on the engine's
// last snapshot, and a view that dies first still ends the run: the Quit
// after it is what guarantees nothing is left running behind a dead screen.
func runScreen(eng *engine.Engine, rc repo.Context, stderr io.Writer) int {
	program := tea.NewProgram(tui.New(eng, openInBrowser(rc)), tea.WithoutSignalHandler())

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
		results <- eng.Run()
	}()

	_, err := program.Run()
	eng.Do(engine.Quit{})
	result := <-results
	discardInput()
	if err != nil && !errors.Is(err, tea.ErrProgramKilled) {
		say(stderr, "error: the view stopped: %v\n", err)
		return engine.ExitFailed
	}
	return result.Code
}
