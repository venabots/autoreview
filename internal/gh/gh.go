// Package gh runs the GitHub CLI. Every other package reaches GitHub through
// a Runner, so a test hands in a function and no test needs a fake binary on
// PATH.
package gh

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"time"
)

// Runner runs one gh command and returns its stdout. The context carries the
// deadline: a gh call that hangs on the network must not hang the run.
type Runner func(ctx context.Context, args ...string) ([]byte, error)

// DefaultTimeout bounds a call whose caller set no deadline of its own.
const DefaultTimeout = 15 * time.Second

// killGrace is how long a killed gh gets to close its pipes before Wait
// gives up on them. A helper that inherited stdout would hold Wait forever.
const killGrace = 2 * time.Second

// Exec is the Runner that runs the real gh.
//
// gh runs in its own process group with no stdin, so it cannot read the
// terminal the view owns, and a deadline kills the credential helper it
// started as well as gh itself.
func Exec(ctx context.Context, args ...string) ([]byte, error) {
	ctx, cancel := withDefaultDeadline(ctx)
	defer cancel()

	var stdout, stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, "gh", args...)
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	cmd.Env = append(os.Environ(), "GH_PROMPT_DISABLED=1", "GH_NO_UPDATE_NOTIFIER=1")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	cmd.WaitDelay = killGrace

	if err := cmd.Run(); err != nil {
		return nil, describe(args, err, ctx.Err(), stderr.String())
	}
	return stdout.Bytes(), nil
}

func withDefaultDeadline(ctx context.Context) (context.Context, context.CancelFunc) {
	if _, ok := ctx.Deadline(); ok {
		return ctx, func() {}
	}
	return context.WithTimeout(ctx, DefaultTimeout)
}

// describe names the gh subcommand and gives gh's own first line of stderr,
// which is what says "not logged in" or "rate limited". The full argv is
// left out: a GraphQL query is hundreds of bytes of noise.
func describe(args []string, err, ctxErr error, stderr string) error {
	what := "gh " + strings.Join(firstWords(args, 2), " ")
	if errors.Is(ctxErr, context.DeadlineExceeded) {
		return fmt.Errorf("%s timed out", what)
	}
	if line := firstLine(stderr); line != "" {
		return fmt.Errorf("%s: %s", what, line)
	}
	return fmt.Errorf("%s: %w", what, err)
}

func firstWords(args []string, n int) []string {
	if len(args) < n {
		return args
	}
	return args[:n]
}

func firstLine(s string) string {
	line, _, _ := strings.Cut(strings.TrimSpace(s), "\n")
	return strings.TrimSpace(line)
}
