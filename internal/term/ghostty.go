package term

import (
	"context"
	"fmt"
	"time"
)

// ghosttySettle is the wait after the script returns. The script returns
// before the new tab has taken the keystroke, and a second open that
// starts in that gap types into the same tab.
const ghosttySettle = 300 * time.Millisecond

// openInGhostty opens a tab with AppleScript and types the command into it.
// Ghostty has no way to name a tab, so the label is not used: a title set
// from here would be replaced by the command in seconds.
//
// The wait happens after a failure too. A failed attempt can have brought
// the window to the front, and the next open races it the same way.
func openInGhostty(ctx context.Context, run Runner, command string) error {
	_, err := run(ctx, "osascript", ghosttyArgs(command)...)
	settle(ctx, ghosttySettle)
	if err != nil {
		return fmt.Errorf("osascript could not open a Ghostty tab: %w", err)
	}
	return nil
}

// ghosttyArgs is the script, one `-e` for each line, then the command as
// the script's own argument after `--`.
//
// The command is never written into the script text. It reaches the script
// as `item 1 of argv`, so a quote or a backslash in it needs no escape and
// cannot end the string and add a statement.
func ghosttyArgs(command string) []string {
	script := []string{
		"on run argv",
		"  set theCmd to item 1 of argv",
		"  tell application \"Ghostty\" to activate",
		"  tell application \"System Events\" to keystroke \"t\" using command down",
		"  delay 0.4",
		"  tell application \"Ghostty\"",
		"    set newTab to selected tab of front window",
		"    set newTerminal to focused terminal of newTab",
		"    input text (theCmd & return) to newTerminal",
		"  end tell",
		"end run",
	}
	args := make([]string, 0, 2*len(script)+2)
	for _, line := range script {
		args = append(args, "-e", line)
	}
	return append(args, "--", command)
}

// settle waits, and stops early when the context ends.
func settle(ctx context.Context, wait time.Duration) {
	timer := time.NewTimer(wait)
	defer timer.Stop()
	select {
	case <-ctx.Done():
	case <-timer.C:
	}
}
