package term

import (
	"context"
	"fmt"
	"strings"
)

// surfacePrefix starts the token that names a cmux surface.
const surfacePrefix = "surface:"

// openInCmux makes a surface, names it, types the command into it and
// presses Return.
//
// The name is set before the command is sent, so the tab carries its label
// from the first frame. A name that cannot be set does not stop the open:
// a tab with no label still holds the review.
func openInCmux(ctx context.Context, run Runner, label, command string) error {
	out, err := run(ctx, "cmux", "new-surface")
	if err != nil {
		return fmt.Errorf("cmux new-surface: %w", err)
	}
	surface, ok := parseSurface(string(out))
	if !ok {
		return fmt.Errorf("cmux new-surface: no surface in its output: %s", trimEnd(string(out)))
	}
	_, _ = run(ctx, "cmux", "rename-tab", "--surface", surface, label)
	if _, err := run(ctx, "cmux", "send", "--surface", surface, command); err != nil {
		return fmt.Errorf("cmux send failed, so the new tab is empty: %w", err)
	}
	if _, err := run(ctx, "cmux", "send-key", "--surface", surface, "Return"); err != nil {
		return fmt.Errorf("cmux send-key failed, so the command waits in the new tab for Return: %w", err)
	}
	return nil
}

// parseSurface finds the "surface:N" token in what `cmux new-surface`
// prints. That is also the form `--surface` takes. A token with no digits
// gives no surface: a command sent to "surface:" would go to no tab.
func parseSurface(out string) (string, bool) {
	_, rest, found := strings.Cut(out, surfacePrefix)
	if !found {
		return "", false
	}
	end := strings.IndexFunc(rest, notDigit)
	if end < 0 {
		end = len(rest)
	}
	if end == 0 {
		return "", false
	}
	return surfacePrefix + rest[:end], true
}

func notDigit(r rune) bool {
	return r < '0' || r > '9'
}
