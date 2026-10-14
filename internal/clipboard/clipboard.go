// Package clipboard puts text on the system clipboard with the program the
// system has for it.
//
// The view also asks the terminal to do it, with OSC 52. That alone is not
// enough: a multiplexer can drop the sequence, and some terminals ignore it
// unless a setting allows it. The person then sees "copied" and pastes
// nothing.
package clipboard

import (
	"bytes"
	"context"
	"errors"
	"os/exec"
	"strings"
	"syscall"
)

// Runner runs one program with text on its stdin. Copy reaches the
// clipboard through one, so a test hands in a function.
type Runner func(ctx context.Context, text, name string, args ...string) error

// program is one clipboard program, and the variable that says its display
// is there. A program with no display to talk to fails, and that failure
// would hide a copy that OSC 52 made.
type program struct {
	name    string
	args    []string
	display string
}

var programs = []program{
	{name: "pbcopy"},
	{name: "wl-copy", display: "WAYLAND_DISPLAY"},
	{name: "xclip", args: []string{"-selection", "clipboard"}, display: "DISPLAY"},
	{name: "xsel", args: []string{"--clipboard", "--input"}, display: "DISPLAY"},
}

// Copy puts text on the clipboard with the first program that is there.
// With none, it does nothing and returns nil: the terminal was asked as
// well, and there is nothing the person can fix here.
func Copy(ctx context.Context, look func(string) (string, error), getenv func(string) string, run Runner, text string) error {
	for _, p := range programs {
		if p.display != "" && getenv(p.display) == "" {
			continue
		}
		if path, err := look(p.name); err == nil {
			return run(ctx, text, path, p.args...)
		}
	}
	return nil
}

// Exec is the Runner that runs the real program, in its own process group
// so that a deadline also kills what it started. Its error is the first
// line of its stderr when it wrote one.
func Exec(ctx context.Context, text, name string, args ...string) error {
	var stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Stdin = strings.NewReader(text)
	cmd.Stderr = &stderr
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	err := cmd.Run()
	if err == nil {
		return nil
	}
	if line, _, _ := strings.Cut(strings.TrimSpace(stderr.String()), "\n"); line != "" {
		return errors.New(line)
	}
	return err
}
