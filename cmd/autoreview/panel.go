package main

import (
	"context"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"syscall"

	xterm "github.com/charmbracelet/x/term"

	"github.com/venabots/autoreview/internal/panel"
	"github.com/venabots/autoreview/internal/proc"
	"github.com/venabots/autoreview/internal/worktree"
)

// runPanel is `autoreview panel ...` with the real world handed in.
//
// INT, TERM and HUP all end the run's context, and the run does the same on
// each: it stops the panelists, then removes the worktrees. They are
// registered in the person's real repository, which is why leaving them is
// worse than an unfinished review. The handler stays installed until the
// run returns, so a second ctrl-C cannot cut the cleanup short.
func runPanel(args []string, stdout, stderr io.Writer) int {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM, syscall.SIGHUP)
	defer stop()
	// A closed pipe must end a write with an error, not the process: the
	// worktrees would stay behind.
	signal.Ignore(syscall.SIGPIPE)
	cwd, err := os.Getwd()
	if err != nil {
		say(stderr, "error: cannot read the current directory: %v\n", err)
		return panel.ExitFailed
	}
	return panel.Run(ctx, args, panel.Deps{
		Env:      os.Getenv,
		LookPath: exec.LookPath,
		Git:      worktree.Exec,
		Starter:  proc.OS{},
		Stdout:   stdout,
		Stderr:   stderr,
		Live:     isTerminal(stderr),
		Cwd:      cwd,
		TempDir:  os.TempDir(),
		PID:      os.Getpid(),
		Version:  version,
	})
}

// isTerminal says w is a terminal. The spinner asks stderr and not stdout:
// a run whose report is piped to a file still has a terminal to spin on.
func isTerminal(w io.Writer) bool {
	f, ok := w.(*os.File)
	return ok && xterm.IsTerminal(f.Fd())
}
