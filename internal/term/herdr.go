package term

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"unicode"
)

// openInHerdr makes a tab, then runs the command in the tab's first pane.
//
// A failed run is reported as an empty tab and not as a failed open. The
// tab exists and carries the label by then, and a bare "failed" would hide
// what was left behind.
func openInHerdr(ctx context.Context, run Runner, workspace, label, command string) error {
	out, err := run(ctx, "herdr", herdrCreateArgs(workspace, label)...)
	if err != nil {
		return fmt.Errorf("herdr tab create: %w", err)
	}
	pane, ok := parsePaneID(string(out))
	if !ok {
		return fmt.Errorf("herdr tab create: no pane id in its output: %s", trimEnd(string(out)))
	}
	if _, err := run(ctx, "herdr", "pane", "run", pane, command); err != nil {
		return fmt.Errorf("herdr pane run failed, so the new tab is empty: %w", err)
	}
	return nil
}

// herdrCreateArgs names the workspace when Herdr gave one. Without it Herdr
// can put the tab in the workspace that has focus, which is not always the
// one the view is in.
func herdrCreateArgs(workspace, label string) []string {
	args := []string{"tab", "create"}
	if workspace != "" {
		args = append(args, "--workspace", workspace)
	}
	return append(args, "--no-focus", "--label", label)
}

// parsePaneID reads the new pane's id out of the JSON that `herdr tab
// create` prints, at .result.root_pane.pane_id. The id is a string or a
// number. Text that is not JSON gives no id: an error that Herdr printed
// must not become the target of the next command.
func parsePaneID(out string) (string, bool) {
	var reply struct {
		Result struct {
			RootPane struct {
				PaneID json.RawMessage `json:"pane_id"`
			} `json:"root_pane"`
		} `json:"result"`
	}
	if err := json.Unmarshal([]byte(out), &reply); err != nil {
		return "", false
	}
	raw := reply.Result.RootPane.PaneID
	if len(raw) == 0 {
		return "", false
	}
	var text string
	if err := json.Unmarshal(raw, &text); err == nil {
		return text, text != ""
	}
	var number json.Number
	if err := json.Unmarshal(raw, &number); err == nil {
		return number.String(), true
	}
	return "", false
}

func trimEnd(s string) string {
	return strings.TrimRightFunc(s, unicode.IsSpace)
}
