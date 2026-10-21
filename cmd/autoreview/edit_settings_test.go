package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// envOf is an environment with only the variables named.
func envOf(vars map[string]string) func(string) string {
	return func(name string) string { return vars[name] }
}

// writtenBy is a file that holds text, to be copied by an "editor".
func writtenBy(t *testing.T, text string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "edited.toml")
	if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// $EDITOR is often a command with flags, such as "code --wait". Run as one
// program name, it was not found.
func TestTheEditorIsVisualThenEditorThenViAndMayHaveFlags(t *testing.T) {
	visual, editor := writtenBy(t, "visual"), writtenBy(t, "editor")
	cases := []struct {
		name string
		env  map[string]string
		want string
	}{
		{"both set", map[string]string{"VISUAL": "cp " + visual, "EDITOR": "cp " + editor}, "visual"},
		{"only EDITOR", map[string]string{"EDITOR": "cp " + editor}, "editor"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.toml")
			if out, err := editorCommand(envOf(c.env), path).CombinedOutput(); err != nil {
				t.Fatalf("the editor failed: %v: %s", err, out)
			}
			if got, err := os.ReadFile(path); err != nil || string(got) != c.want {
				t.Errorf("the file holds %q, %v; want %q", got, err, c.want)
			}
		})
	}
	if args := editorCommand(envOf(nil), "/x/config.toml").Args; !strings.HasPrefix(args[2], "vi ") || args[len(args)-1] != "/x/config.toml" {
		t.Errorf("with neither set the command is %q, want vi on the file", args)
	}
}

func TestTheFileTheEditorLeftIsCheckedAsARunReadsIt(t *testing.T) {
	cases := []struct {
		name    string
		text    string
		editErr error
		wantErr string
	}{
		{"a file that reads", "[review]\njobs = 2\n", nil, ""},
		{"an unknown key", "[loop]\nmax_pases = 2\n", nil, "loop.max_pases, which is not a setting"},
		{"a bad value", "[review]\njobs = 0\n", nil, "--jobs expects an integer >= 1"},
		{"an editor that failed", "", errors.New("exit status 1"), "the editor failed: exit status 1"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			path := writtenBy(t, c.text)
			var heard error
			afterEdit(path, func(err error) tea.Msg { heard = err; return nil })(c.editErr)
			switch {
			case c.wantErr == "" && heard != nil:
				t.Errorf("the check said %v, want nothing", heard)
			case c.wantErr != "" && (heard == nil || !strings.Contains(heard.Error(), c.wantErr)):
				t.Errorf("the check said %v, want %q", heard, c.wantErr)
			}
		})
	}
}

func TestARunWithItsSettingsOffHasNoEditor(t *testing.T) {
	if editSettings(envOf(map[string]string{"AUTOREVIEW_CONFIG": "off"})) != nil {
		t.Error("a run with no settings file offers to edit one")
	}
}
