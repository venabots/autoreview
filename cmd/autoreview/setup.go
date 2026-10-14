package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	xterm "github.com/charmbracelet/x/term"

	"github.com/venabots/autoreview/internal/cli"
	"github.com/venabots/autoreview/internal/config"
	"github.com/venabots/autoreview/internal/engine"
	"github.com/venabots/autoreview/internal/review"
	"github.com/venabots/autoreview/internal/sanitize"
	"github.com/venabots/autoreview/internal/setup"
)

// runSetup is `autoreview setup`: the settings form, and the file written
// from its answers.
func runSetup(args []string, stdout, stderr io.Writer) int {
	if len(args) > 0 {
		say(stderr, "error: autoreview setup takes no arguments\n")
		return engine.ExitFailed
	}
	path, ok := config.Path(os.Getenv)
	switch {
	case !ok:
		say(stderr, "error: there is no settings file to write: $%s is off, or there is no home directory\n", config.EnvConfig)
		return engine.ExitFailed
	case !onTerminal():
		say(stderr, "error: autoreview setup asks on a terminal; edit %s by hand\n", path)
		return engine.ExitFailed
	}
	file, err := config.Load(path)
	if err == nil {
		_, err = setUp(file)
	}
	return setupEnded(err, path, stdout, stderr)
}

// setupEnded says how the form ended, and is the exit status.
func setupEnded(err error, path string, stdout, stderr io.Writer) int {
	switch {
	case errors.Is(err, setup.ErrAborted):
		say(stderr, "%s\n", setup.ErrAborted)
		return engine.ExitInterrupted
	case err != nil:
		say(stderr, "error: %s\n", sanitize.Line(err.Error()))
		return engine.ExitFailed
	}
	say(stdout, "wrote %s\n", path)
	return engine.ExitOK
}

// needsSetup says the run is the first one: the settings are not off, and
// the file is not there yet.
func needsSetup(file config.File) bool {
	return file.Path != "" && !file.Found
}

func onTerminal() bool {
	return xterm.IsTerminal(os.Stdin.Fd()) && xterm.IsTerminal(os.Stdout.Fd())
}

// setUp asks for the settings, starting from what the file says, and
// writes them. The answers are read back the way a run reads them before
// the file is written: a file that the next run would refuse is never
// written.
func setUp(file config.File) (config.File, error) {
	current, err := valuesOf(file)
	if err != nil {
		return config.File{}, err
	}
	answers, err := setup.Ask(current, file.Path, setup.Terminal{In: os.Stdin, Out: os.Stdout})
	if err != nil {
		return config.File{}, err
	}
	raw, err := config.Render(answers)
	if err != nil {
		return config.File{}, err
	}
	next, err := config.Parse(raw, file.Path)
	if err == nil {
		_, err = cli.ParseWith(nil, noEnv, next)
	}
	if err != nil {
		return config.File{}, err
	}
	return next, writeSettings(file.Path, raw)
}

func noEnv(string) string { return "" }

// valuesOf is every setting as the file and the built-in defaults give it,
// with no flag and no variable: what the form starts from, and what it
// writes back for the keys it does not ask.
func valuesOf(file config.File) (config.Values, error) {
	cfg, err := cli.ParseWith(nil, noEnv, file)
	if err != nil {
		return config.Values{}, err
	}
	synth := "claude"
	if file.Panel.Synth != nil && *file.Panel.Synth != "" {
		synth = *file.Panel.Synth
	}
	return config.Values{
		Orchestrator: cfg.Orchestrator.Label(), Fallback: fallbackText(cfg.Fallback),
		Jobs: cfg.Jobs, Timeout: int(cfg.Timeout / time.Second), Budget: cfg.Budget, Focus: cfg.Focus,
		Verdicts: cfg.Verdicts.String(),
		Watch:    cfg.Watch.Text, Rest: cfg.Babysit.Text, MaxPasses: cfg.MaxPasses, MaxIdle: cfg.MaxIdle, CIWait: cfg.CIWait.Text,
		Approved: cfg.All, Dependabot: cfg.Dependabot, Stacked: cfg.Stacked, SkipCI: cfg.SkipCI,
		AutofixCap: cfg.AutofixCap, Panelists: file.Panel.Panelists, Synth: synth,
	}, nil
}

func fallbackText(f review.Fallback) string {
	switch f.Kind {
	case review.FallbackNone:
		return "none"
	case review.FallbackSpec:
		return f.Spec.Label()
	}
	return "auto"
}

// writeSettings writes the file through a temporary one and a rename, so
// a run that reads it never sees half of it. The directory and the file
// are the person's own.
func writeSettings(path string, raw []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("making %s: %w", dir, err)
	}
	tmp, err := os.CreateTemp(dir, ".config-*.toml")
	if err != nil {
		return fmt.Errorf("writing the settings: %w", err)
	}
	_, err = tmp.Write(raw)
	if closeErr := tmp.Close(); err == nil {
		err = closeErr
	}
	if err == nil {
		err = os.Rename(tmp.Name(), path)
	}
	if err != nil {
		_ = os.Remove(tmp.Name())
		return fmt.Errorf("writing %s: %w", path, err)
	}
	return nil
}
