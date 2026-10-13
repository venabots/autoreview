// Package term opens a new terminal tab and runs a command in it. The view
// uses it to reopen a finished review while it stays up.
//
// It knows three terminals and finds the one it is in from the environment:
// Herdr, cmux, and Ghostty on macOS. Ghostty is driven through AppleScript,
// which is why it is macOS only.
//
// Every program runs through a Runner, with no stdin and with its output
// captured. The view owns the terminal, and a child that read it would take
// the keys meant for the view.
package term

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"runtime"
)

// Kind is a terminal that can open a tab.
type Kind int

// The terminals, in the order detection prefers them.
const (
	None Kind = iota
	Herdr
	Cmux
	Ghostty
)

// Name is the word a message uses for the terminal, as in "opened PR #9's
// review in a new cmux tab". None has no name.
func (k Kind) Name() string {
	switch k {
	case Herdr:
		return "Herdr"
	case Cmux:
		return "cmux"
	case Ghostty:
		return "Ghostty"
	default:
		return ""
	}
}

// Env is what detection reads. It is a value so that a test passes its own
// and no test depends on the terminal it happens to run in.
type Env struct {
	// Get reads an environment variable, as os.Getenv does.
	Get func(string) string
	// LookPath finds a program on PATH, as exec.LookPath does.
	LookPath func(string) (string, error)
	// GOOS is the operating system, as runtime.GOOS names it.
	GOOS string
}

// OSEnv is the Env of this process.
func OSEnv() Env {
	return Env{Get: os.Getenv, LookPath: exec.LookPath, GOOS: runtime.GOOS}
}

// get reads a variable. An Env with no Get has nothing set: a caller that
// forgot a field must get "no terminal", not a crash under the view.
func (e Env) get(key string) string {
	if e.Get == nil {
		return ""
	}
	return e.Get(key)
}

// has says the program is on PATH. An Env with no LookPath has none.
func (e Env) has(program string) bool {
	if e.LookPath == nil {
		return false
	}
	_, err := e.LookPath(program)
	return err == nil
}

// Detect picks the terminal this process is in, or None.
//
// The order is v1's. Herdr and cmux each need their CLI on PATH as well as
// their variable: a variable that a parent shell left behind must fall
// through to the next terminal and not name one that cannot be driven.
// A variable that is set to nothing counts as not set.
func Detect(env Env) Kind {
	switch {
	case env.get("HERDR_ENV") == "1" && env.has("herdr"):
		return Herdr
	case env.get("CMUX_SURFACE_ID") != "" && env.has("cmux"):
		return Cmux
	case env.get("TERM_PROGRAM") == "ghostty" && env.GOOS == "darwin":
		return Ghostty
	default:
		return None
	}
}

// Open opens a new tab labelled label and runs command in it. It returns
// the terminal it used and, on a failure, an error that names the step
// that failed.
//
// With no terminal it returns None and an error that names the three it
// knows. Without that, the key that reopens a review would do nothing and
// say nothing.
//
// label and command can hold text that came from a PR. Each one is passed
// as one argument and is never put into a shell line here. The errors can
// hold a program's own words, so pass them through sanitize before they
// reach the terminal.
func Open(ctx context.Context, run Runner, env Env, label, command string) (Kind, error) {
	kind := Detect(env)
	switch kind {
	case Herdr:
		return kind, openInHerdr(ctx, run, env.get("HERDR_WORKSPACE_ID"), label, command)
	case Cmux:
		return kind, openInCmux(ctx, run, label, command)
	case Ghostty:
		return kind, openInGhostty(ctx, run, command)
	default:
		return None, noTerminal(env)
	}
}

// noTerminal says what was looked for and what was found, on one line. A
// person who pressed the key in Terminal.app must learn that the key works
// elsewhere, and a person in Ghostty on Linux must learn why it does not
// work there.
func noTerminal(env Env) error {
	if env.get("TERM_PROGRAM") == "ghostty" {
		return errors.New("no terminal that can open a tab: Ghostty opens one on macOS only (AppleScript), and neither Herdr nor cmux was found")
	}
	return fmt.Errorf(
		"no terminal that can open a tab: this needs Herdr, cmux, or Ghostty on macOS (HERDR_ENV=%s CMUX_SURFACE_ID=%s TERM_PROGRAM=%s)",
		shown(env.get("HERDR_ENV")), shown(env.get("CMUX_SURFACE_ID")), shown(env.get("TERM_PROGRAM")),
	)
}

func shown(value string) string {
	if value == "" {
		return "<unset>"
	}
	return value
}
