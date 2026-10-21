package main

import (
	"cmp"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	tea "charm.land/bubbletea/v2"

	"github.com/venabots/autoreview/internal/cli"
	"github.com/venabots/autoreview/internal/config"
)

// editSettings is the view's e: the settings file in the person's editor,
// in the view's terminal, and checked when the editor closes. A mistake is
// then said while the person still has the file in mind, and not at the
// next start, where it stops the run. It is nil when the run has no
// settings file.
func editSettings(env func(string) string) func(tea.ExecCallback) tea.Cmd {
	path, ok := config.Path(env)
	if !ok {
		return nil
	}
	return func(done tea.ExecCallback) tea.Cmd {
		// The first-run form may not have written the file, and an editor
		// cannot save into a directory that is not there.
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			return func() tea.Msg { return done(err) }
		}
		return tea.ExecProcess(editorCommand(env, path), afterEdit(path, done))
	}
}

// editorCommand opens path in $VISUAL, else $EDITOR, else vi. It runs
// through sh, as git runs its editor: $EDITOR is often a command with
// flags, such as "code --wait", which is no program's name.
func editorCommand(env func(string) string, path string) *exec.Cmd {
	editor := cmp.Or(env("VISUAL"), env("EDITOR"), "vi")
	return exec.Command("sh", "-c", editor+` "$@"`, "sh", path)
}

// afterEdit reads the file the editor left as a run would read it.
func afterEdit(path string, done tea.ExecCallback) tea.ExecCallback {
	return func(err error) tea.Msg {
		if err != nil {
			return done(fmt.Errorf("the editor failed: %w", err))
		}
		file, err := config.Load(path)
		if err == nil {
			err = cli.CheckFile(file)
		}
		return done(err)
	}
}
