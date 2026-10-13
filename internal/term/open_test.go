package term

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"testing/synctest"
	"time"
)

const (
	resumeLabel   = "PR 9 Resume"
	resumeCommand = "cd /src/app && claude --resume 7442b624-5cba-5d44-ae67-9c390cfe70a1"
)

// v1Script is the AppleScript that v1 hands to osascript, one line for
// each `-e`. It is written out again here so that a change to the script
// is a change to this test.
var v1Script = []string{
	"on run argv",
	"  set theCmd to item 1 of argv",
	`  tell application "Ghostty" to activate`,
	`  tell application "System Events" to keystroke "t" using command down`,
	"  delay 0.4",
	`  tell application "Ghostty"`,
	"    set newTab to selected tab of front window",
	"    set newTerminal to focused terminal of newTab",
	"    input text (theCmd & return) to newTerminal",
	"  end tell",
	"end run",
}

// osascriptCall is the call v1 makes for this command.
func osascriptCall(command string) call {
	args := []string{}
	for _, line := range v1Script {
		args = append(args, "-e", line)
	}
	return call{"osascript", append(args, "--", command)}
}

func withWorkspace(env Env, workspace string) Env {
	get := env.Get
	return Env{
		Get: func(key string) string {
			if key == "HERDR_WORKSPACE_ID" {
				return workspace
			}
			return get(key)
		},
		LookPath: env.LookPath,
		GOOS:     env.GOOS,
	}
}

func TestEachTerminalGetsV1Commands(t *testing.T) {
	cases := []struct {
		name string
		env  Env
		kind Kind
		want []call
	}{
		{
			"Herdr makes a tab that does not take focus, then runs the command in its pane",
			inHerdr(), Herdr,
			[]call{
				{"herdr", []string{"tab", "create", "--no-focus", "--label", resumeLabel}},
				{"herdr", []string{"pane", "run", "pane-7", resumeCommand}},
			},
		},
		{
			"Herdr puts the tab in the workspace it named",
			withWorkspace(inHerdr(), "ws-3"), Herdr,
			[]call{
				{"herdr", []string{"tab", "create", "--workspace", "ws-3", "--no-focus", "--label", resumeLabel}},
				{"herdr", []string{"pane", "run", "pane-7", resumeCommand}},
			},
		},
		{
			"cmux makes a surface, names it, types the command and presses Return",
			inCmux(), Cmux,
			[]call{
				{"cmux", []string{"new-surface"}},
				{"cmux", []string{"rename-tab", "--surface", "surface:1", resumeLabel}},
				{"cmux", []string{"send", "--surface", "surface:1", resumeCommand}},
				{"cmux", []string{"send-key", "--surface", "surface:1", "Return"}},
			},
		},
		{
			"Ghostty gets the script, then the command as the script's argument",
			inGhostty(), Ghostty,
			[]call{osascriptCall(resumeCommand)},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rec := answering()
			kind, err := open(t, rec.run, c.env, resumeLabel, resumeCommand)
			if err != nil || kind != c.kind {
				t.Fatalf("Open = %q, %v; want %q and no error", kind.Name(), err, c.kind.Name())
			}
			if len(rec.calls) != len(c.want) {
				t.Fatalf("Open ran %v, want %d calls", rec.steps(), len(c.want))
			}
			for i, want := range c.want {
				if got := rec.calls[i]; got.name != want.name || !slices.Equal(got.args, want.args) {
					t.Errorf("call %d = %s %q\nwant     %s %q", i, got.name, got.args, want.name, want.args)
				}
			}
		})
	}
}

func TestTextFromAPRReachesTheTerminalAsOneArgument(t *testing.T) {
	const (
		label   = `PR 9 "; rm -rf ~ $(x) --label`
		command = `cd '/a b' && echo "say \"hi\"" C:\dir\n $(id); 'x'` + "\n" + `end run`
	)
	cases := []struct {
		name string
		env  Env
		// labelled is false for Ghostty, which has no way to name a tab.
		labelled bool
	}{
		{"Herdr", inHerdr(), true},
		{"cmux", inCmux(), true},
		{"Ghostty", inGhostty(), false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rec := answering()
			if _, err := open(t, rec.run, c.env, label, command); err != nil {
				t.Fatalf("Open: %v", err)
			}
			if got := holding(rec.calls, command); got != 1 {
				t.Errorf("%d arguments hold the command, want it whole in one", got)
			}
			if got := whole(rec.calls, command); got != 1 {
				t.Errorf("%d arguments are the command byte for byte, want 1", got)
			}
			wantLabels := 0
			if c.labelled {
				wantLabels = 1
			}
			if got := holding(rec.calls, label); got != wantLabels || whole(rec.calls, label) != wantLabels {
				t.Errorf("%d arguments hold the label, want %d and the label whole", got, wantLabels)
			}
		})
	}
}

// holding counts the arguments that contain text.
func holding(calls []call, text string) int {
	n := 0
	for _, c := range calls {
		for _, arg := range c.args {
			if strings.Contains(arg, text) {
				n++
			}
		}
	}
	return n
}

// whole counts the arguments that are exactly text.
func whole(calls []call, text string) int {
	n := 0
	for _, c := range calls {
		for _, arg := range c.args {
			if arg == text {
				n++
			}
		}
	}
	return n
}

func TestAQuoteAndABackslashDoNotChangeTheGhosttyScript(t *testing.T) {
	const command = `echo "a\"b" \ 'c' \\`
	rec := answering()
	if _, err := open(t, rec.run, inGhostty(), resumeLabel, command); err != nil {
		t.Fatalf("Open: %v", err)
	}
	want := osascriptCall(command)
	got := rec.calls[0]
	if !slices.Equal(got.args, want.args) {
		t.Fatalf("osascript got %q\nwant         %q", got.args, want.args)
	}
	if last := got.args[len(got.args)-1]; last != command {
		t.Errorf("the script's argument = %q, want the command with no escape added: %q", last, command)
	}
	if before := got.args[len(got.args)-2]; before != "--" {
		t.Errorf("the command follows %q, want --, so that a command that starts with a dash is not an option", before)
	}
}

func TestAFailedStepIsNamedAndNothingRunsAfterIt(t *testing.T) {
	cases := []struct {
		name  string
		env   Env
		kind  Kind
		fails string
		want  string
		ran   []string
	}{
		{
			"Herdr cannot make the tab", inHerdr(), Herdr, "herdr tab",
			"herdr tab create: boom",
			[]string{"herdr tab"},
		},
		{
			"Herdr makes the tab and cannot run in it", inHerdr(), Herdr, "herdr pane",
			"herdr pane run failed, so the new tab is empty: boom",
			[]string{"herdr tab", "herdr pane"},
		},
		{
			"cmux cannot make the surface", inCmux(), Cmux, "cmux new-surface",
			"cmux new-surface: boom",
			[]string{"cmux new-surface"},
		},
		{
			"cmux cannot type the command", inCmux(), Cmux, "cmux send",
			"cmux send failed, so the new tab is empty: boom",
			[]string{"cmux new-surface", "cmux rename-tab", "cmux send"},
		},
		{
			"cmux cannot press Return", inCmux(), Cmux, "cmux send-key",
			"cmux send-key failed, so the command waits in the new tab for Return: boom",
			[]string{"cmux new-surface", "cmux rename-tab", "cmux send", "cmux send-key"},
		},
		{
			"osascript fails", inGhostty(), Ghostty, "osascript -e",
			"osascript could not open a Ghostty tab: boom",
			[]string{"osascript -e"},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rec := failing(c.fails)
			kind, err := open(t, rec.run, c.env, resumeLabel, resumeCommand)
			if kind != c.kind {
				t.Errorf("Open named %q, want %q: the message says which terminal failed", kind.Name(), c.kind.Name())
			}
			if err == nil || err.Error() != c.want {
				t.Errorf("error = %v, want %q", err, c.want)
			}
			if !errors.Is(err, errBoom) {
				t.Errorf("error %v does not wrap what the program said", err)
			}
			if got := rec.steps(); !slices.Equal(got, c.ran) {
				t.Errorf("Open ran %v, want %v", got, c.ran)
			}
		})
	}
}

func TestOutputWithNoIDStopsBeforeACommandIsSent(t *testing.T) {
	cases := []struct {
		name string
		env  Env
		step string
		out  string
		want string
	}{
		{
			"Herdr prints words and no JSON", inHerdr(), "herdr tab",
			"herdr: something went wrong\n",
			"herdr tab create: no pane id in its output: herdr: something went wrong",
		},
		{
			"Herdr prints JSON with no pane", inHerdr(), "herdr tab",
			`{"result":{}}` + "\n",
			`herdr tab create: no pane id in its output: {"result":{}}`,
		},
		{
			"cmux prints no surface", inCmux(), "cmux new-surface",
			"no surface here\n\n",
			"cmux new-surface: no surface in its output: no surface here",
		},
		{
			"cmux prints nothing", inCmux(), "cmux new-surface",
			"",
			"cmux new-surface: no surface in its output: ",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rec := answering()
			rec.replies[c.step] = reply{out: c.out}
			_, err := open(t, rec.run, c.env, resumeLabel, resumeCommand)
			if err == nil || err.Error() != c.want {
				t.Errorf("error = %v, want %q", err, c.want)
			}
			if got := rec.steps(); !slices.Equal(got, []string{c.step}) {
				t.Errorf("Open ran %v, want it to stop after %s", got, c.step)
			}
		})
	}
}

func TestACmuxTabThatCannotBeNamedStillRunsTheCommand(t *testing.T) {
	rec := failing("cmux rename-tab")
	kind, err := open(t, rec.run, inCmux(), resumeLabel, resumeCommand)
	if err != nil || kind != Cmux {
		t.Fatalf("Open = %q, %v; want cmux and no error", kind.Name(), err)
	}
	want := []string{"cmux new-surface", "cmux rename-tab", "cmux send", "cmux send-key"}
	if got := rec.steps(); !slices.Equal(got, want) {
		t.Errorf("Open ran %v, want %v", got, want)
	}
}

func TestGhosttyGetsTimeToTakeTheKeystroke(t *testing.T) {
	cases := []struct {
		name string
		rec  *recorder
	}{
		{"after a tab that opened", answering()},
		{"after a tab that did not", failing("osascript -e")},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				began := time.Now()
				_, _ = Open(context.Background(), c.rec.run, inGhostty(), resumeLabel, resumeCommand)
				if waited := time.Since(began); waited != ghosttySettle {
					t.Errorf("Open returned after %v, want %v", waited, ghosttySettle)
				}
			})
		})
	}
}

func TestACancelledOpenDoesNotWaitForGhostty(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		began := time.Now()
		if _, err := Open(ctx, answering().run, inGhostty(), resumeLabel, resumeCommand); err != nil {
			t.Errorf("Open: %v", err)
		}
		if waited := time.Since(began); waited != 0 {
			t.Errorf("Open waited %v after its context ended", waited)
		}
	})
}
