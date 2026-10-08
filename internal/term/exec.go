package term

import (
	"bytes"
	"context"
	"errors"
	"os/exec"
	"strings"
	"syscall"
	"time"
)

// Runner runs one program and returns its stdout. Open reaches every
// terminal through one, so a test hands in a function and no test opens a
// real tab.
type Runner func(ctx context.Context, name string, args ...string) ([]byte, error)

// DefaultTimeout bounds a call whose caller set no deadline of its own. A
// terminal that does not answer must not leave the key waiting forever.
const DefaultTimeout = 15 * time.Second

// killGrace is how long a killed program gets to close its pipes before
// Wait gives up on them. A helper that inherited stdout would hold Wait
// forever.
const killGrace = 2 * time.Second

// Exec is the Runner that runs the real program.
//
// The program runs in its own process group with no stdin, and both of its
// outputs are captured. It cannot read the keys meant for the view or draw
// over it, and a deadline kills whatever it started as well as itself.
//
// The error is the cause alone: the first line of stderr, or "timed out".
// Open adds the name of the step.
func Exec(ctx context.Context, name string, args ...string) ([]byte, error) {
	ctx, cancel := withDefaultDeadline(ctx)
	defer cancel()

	var stdout, stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	cmd.WaitDelay = killGrace

	if err := cmd.Run(); err != nil {
		return nil, cause(err, ctx.Err(), stderr.String())
	}
	return stdout.Bytes(), nil
}

func withDefaultDeadline(ctx context.Context) (context.Context, context.CancelFunc) {
	if _, ok := ctx.Deadline(); ok {
		return ctx, func() {}
	}
	return context.WithTimeout(ctx, DefaultTimeout)
}

// cause picks the words for a failed run. A killed program says only
// "signal: killed", so a deadline and a cancel are named for what they
// are. After those, the program's own first line of stderr says more than
// its exit status does.
func cause(err, ctxErr error, stderr string) error {
	if errors.Is(ctxErr, context.DeadlineExceeded) {
		return errors.New("timed out")
	}
	if ctxErr != nil {
		return ctxErr
	}
	if line := firstLine(stderr); line != "" {
		return errors.New(line)
	}
	return err
}

func firstLine(s string) string {
	line, _, _ := strings.Cut(strings.TrimSpace(s), "\n")
	return strings.TrimSpace(line)
}
