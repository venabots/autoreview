package riskcheck

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"syscall"
	"time"
)

// killGrace is how long a killed script gets to close its pipes. A child
// that inherited stdout would hold Wait forever.
const killGrace = 2 * time.Second

// Exec runs a risk script for real. It runs in its own process group with
// no terminal, so it cannot read the keys the view owns, and the context's
// deadline kills it and every process it started: a script that hangs
// must not hold the look that waits for it. Its environment is s.Env and
// nothing else. A nil Env would hand it autoreview's whole environment, so
// an empty one is never left nil.
func Exec(ctx context.Context, s Script) ([]byte, error) {
	var stdout, stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, s.Path)
	cmd.Dir = s.Dir
	cmd.Env = append([]string{}, s.Env...)
	cmd.Stdin = bytes.NewReader(s.Stdin)
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	cmd.WaitDelay = killGrace
	err := cmd.Run()
	switch {
	case errors.Is(ctx.Err(), context.DeadlineExceeded):
		return nil, errors.New("it timed out")
	case err == nil:
		return stdout.Bytes(), nil
	}
	if line, _, _ := strings.Cut(strings.TrimSpace(stderr.String()), "\n"); line != "" {
		return nil, fmt.Errorf("%w: %s", err, line)
	}
	return nil, err
}
