package worktree

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

// killGrace is how long a killed git gets to close its pipes before Wait
// gives up on them. An ssh it started could hold stderr open for ever.
const killGrace = 2 * time.Second

// Exec is the Git that runs the real git, as `git -C dir args...`.
//
// Git never asks for anything: the view owns the terminal, and a prompt
// there would draw over it and wait for keys nobody types. Git's own
// prompts are off and it has no stdin. It also runs in a session of its
// own, with no controlling terminal: ssh asks for a passphrase or a host
// key on /dev/tty, not on stdin, and without a terminal it fails at once.
// The session is a process group, so a git past its deadline is killed with
// everything it started.
//
// A failure says what git said, not "exit status 128": with prompts off, a
// failed login is the usual cause, and only git's words say so.
func Exec(ctx context.Context, dir string, args ...string) ([]byte, error) {
	var stdout, stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", dir}, args...)...)
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	cmd.WaitDelay = killGrace

	if err := cmd.Run(); err != nil {
		return nil, describe(args, err, ctx.Err(), stderr.String())
	}
	return stdout.Bytes(), nil
}

// describe turns a failed git into the words a person reads. An ended
// context is wrapped, so a caller can tell a deadline from a refusal.
func describe(args []string, err, ctxErr error, stderr string) error {
	what := strings.TrimSpace("git " + subcommand(args))
	if ctxErr != nil {
		return fmt.Errorf("%s stopped: %w", what, ctxErr)
	}
	var exited *exec.ExitError
	if !errors.As(err, &exited) {
		return fmt.Errorf("could not run git: %w", err)
	}
	if line := reason(stderr); line != "" {
		return errors.New(line)
	}
	return fmt.Errorf("%s failed (%w)", what, err)
}

// subcommand is the first word of args that is not an option to git
// itself, so an error names "git fetch" and not "git -c".
func subcommand(args []string) string {
	for at := 0; at < len(args); at++ {
		switch {
		case args[at] == "-c" || args[at] == "-C":
			at++
		case !strings.HasPrefix(args[at], "-"):
			return args[at]
		}
	}
	return ""
}

// reason is the line of git's stderr that says what went wrong: its fatal:
// line, which comes before the hints it adds, else its first line. The line
// before a fatal: line is often the cause, an ssh "Permission denied", so
// it is kept in front.
func reason(stderr string) string {
	lines := nonEmptyLines(stderr)
	for at, line := range lines {
		if !strings.HasPrefix(line, "fatal:") {
			continue
		}
		if at > 0 {
			return lines[at-1] + " " + line
		}
		return line
	}
	if len(lines) == 0 {
		return ""
	}
	return lines[0]
}

func nonEmptyLines(s string) []string {
	var lines []string
	for line := range strings.Lines(s) {
		if trimmed := strings.TrimSpace(line); trimmed != "" {
			lines = append(lines, trimmed)
		}
	}
	return lines
}
