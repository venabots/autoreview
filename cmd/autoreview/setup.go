package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	xterm "github.com/charmbracelet/x/term"

	"github.com/venabots/autoreview/internal/cli"
	"github.com/venabots/autoreview/internal/config"
	"github.com/venabots/autoreview/internal/doctor"
	"github.com/venabots/autoreview/internal/engine"
	"github.com/venabots/autoreview/internal/ledger"
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
		err = cli.CheckFile(file)
	}
	if err != nil {
		return setupEnded(withRepairHint(err, path), stderr)
	}
	_, raw, err := setUp(file)
	if err == nil {
		err = writeSettings(path, raw)
	}
	if err == nil {
		say(stdout, "wrote %s\n", path)
	}
	return setupEnded(err, stderr)
}

// firstRun asks for the settings before the first run in the view, and
// writes them. A file that cannot be written does not stop the run: it
// goes on with the answers, says so, and the next run asks again.
func firstRun(file config.File, stdout, stderr io.Writer) (config.File, error) {
	next, raw, err := setUp(file)
	if err != nil {
		return config.File{}, err
	}
	if err := writeSettings(file.Path, raw); err != nil {
		say(stderr, "note: %s; this run uses the answers, and the next one asks again\n", sanitize.Line(err.Error()))
		return next, nil
	}
	say(stdout, "wrote %s\n", file.Path)
	return next, nil
}

// setupEnded says how the form ended when it did not write, and is the
// exit status.
func setupEnded(err error, stderr io.Writer) int {
	switch {
	case errors.Is(err, setup.ErrAborted):
		say(stderr, "%s\n", setup.ErrAborted)
		return engine.ExitInterrupted
	case errors.Is(err, setup.ErrNotSaved):
		// The person chose this on the last step: it is no failure.
		say(stderr, "%s\n", setup.ErrNotSaved)
		return engine.ExitOK
	case err != nil:
		say(stderr, "error: %s\n", sanitize.Line(err.Error()))
		return engine.ExitFailed
	}
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

// setUp asks for the settings, starting from what the file says, and is
// the file the answers make and its text. Each answer is read back as
// CheckFile reads a file, so the caller never writes a bad value. The pair
// of orchestrator and fallback is the run's to check, with its own
// variables; the form refuses to choose an agent that is the file's
// fallback.
func setUp(file config.File) (config.File, []byte, error) {
	start, err := formStart(file, os.Getenv)
	if err != nil {
		return config.File{}, nil, err
	}
	answers, err := setup.Ask(start, setup.Terminal{In: os.Stdin, Out: os.Stdout})
	if err != nil {
		return config.File{}, nil, err
	}
	raw, err := config.Render(answers)
	if err != nil {
		return config.File{}, nil, err
	}
	next, err := config.Parse(raw, file.Path)
	if err == nil {
		err = cli.CheckFile(next)
	}
	if err != nil {
		return config.File{}, nil, err
	}
	return next, raw, nil
}

// formStart is what the form starts from: the file, the defaults of a run
// with no file, the shell, the PATH and the models the ledger has seated.
// A ledger that cannot be read offers no models, and stops nothing.
func formStart(file config.File, env func(string) string) (setup.Start, error) {
	current, err := valuesOf(file)
	if err != nil {
		return setup.Start{}, err
	}
	defaults, err := valuesOf(config.File{})
	if err != nil {
		return setup.Start{}, err
	}
	return setup.Start{
		Current: current, Defaults: defaults, Path: file.Path,
		Getenv: env, LookPath: exec.LookPath, Seen: seenModels(env),
	}, nil
}

func seenModels(env func(string) string) []setup.Seen {
	path, ok := ledger.Path(env)
	if !ok {
		return nil
	}
	records, err := ledger.Read(path)
	if err != nil {
		return nil
	}
	var out []setup.Seen
	for _, s := range doctor.SeenModels(records) {
		out = append(out, setup.Seen{Spec: s.Spec, Launched: s.Launched, Answered: s.Answered})
	}
	return out
}

func noEnv(string) string { return "" }

// valuesOf is every setting as the file and the built-in defaults give it,
// with no flag and no variable: what the form starts from, and what it
// writes back for the keys it does not ask.
//
// The fallback is read as the file writes it. Read against the default
// orchestrator, a fallback of claude would clash, though the person's runs
// move the orchestrator with a variable.
func valuesOf(file config.File) (config.Values, error) {
	if err := cli.CheckFile(file); err != nil {
		return config.Values{}, err
	}
	alone := file
	alone.Review.Fallback = nil
	cfg, err := cli.ParseWith(nil, noEnv, alone)
	if err != nil {
		return config.Values{}, err
	}
	fallback := "auto"
	if file.Review.Fallback != nil {
		fallback = *file.Review.Fallback
	}
	synth := "claude"
	if file.Panel.Synth != nil {
		synth = *file.Panel.Synth
	}
	// The focus goes back as the person wrote it. cfg.Focus is the form a
	// prompt carries, with its double quotes made single.
	focus := ""
	if file.Review.Focus != nil {
		focus = *file.Review.Focus
	}
	return config.Values{
		Orchestrator: cfg.Orchestrator.Label(), Fallback: fallback,
		Jobs: cfg.Jobs, Timeout: int(cfg.Timeout / time.Second), Budget: cfg.Budget, Focus: focus,
		Verdicts: cfg.Verdicts.String(),
		Watch:    cfg.Watch.Text, Rest: cfg.Babysit.Text, MaxPasses: cfg.MaxPasses, MaxIdle: cfg.MaxIdle, CIWait: cfg.CIWait.Text,
		Approved: cfg.All, Dependabot: cfg.Dependabot, Stacked: cfg.Stacked, SkipCI: cfg.SkipCI,
		Autofix: !cfg.NoAutofix, AutofixCap: cfg.AutofixCap, Panelists: file.Panel.Panelists, Synth: synth,
		Set: file.Keys(),
	}, nil
}

// writeSettings writes the file through a temporary one and a rename, so
// a run that reads it never sees half of it. A new file and its directory
// are the person's own. A file that is there keeps its mode, and a link to
// it, as a dotfiles repo makes, stays a link: the rename goes to the file
// the link points at.
func writeSettings(path string, raw []byte) error {
	target, mode := linkTarget(path), os.FileMode(0o600)
	info, err := os.Stat(target)
	existed := err == nil
	if existed {
		mode = info.Mode().Perm()
	}
	dir := filepath.Dir(target)
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
		err = os.Chmod(tmp.Name(), mode)
	}
	if err == nil {
		err = os.Rename(tmp.Name(), target)
	}
	if err != nil {
		_ = os.Remove(tmp.Name())
		return fmt.Errorf("writing %s: %w", path, err)
	}
	return readableThrough(path, target, existed)
}

// readableThrough checks that the file just written at target is the one a
// run reads at path. A link through a folder that does not exist leads
// nowhere: the file would sit where no run looks, and the next run would ask
// for the settings again. A file this write made is then removed.
func readableThrough(path, target string, existed bool) error {
	if _, err := os.Stat(path); err == nil {
		return nil
	}
	if !existed {
		_ = os.Remove(target)
	}
	return fmt.Errorf("%s does not lead to the file written at %s; make the folders that its links point at, or replace the link", path, target)
}

// linkTarget is the file a link at path points at, followed to its end,
// even when that file does not exist yet: a dotfiles install can make the
// link first. A path that is no link is itself.
func linkTarget(path string) string {
	target, _ := resolved(path, maxLinks)
	return target
}

// maxLinks is how many links one write may pass through, as the system
// limits it, so two links that point at each other end the walk.
const maxLinks = 40

// resolved is linkTarget with hops left to follow, and the hops left after.
func resolved(path string, hops int) (string, int) {
	if real, err := filepath.EvalSymlinks(path); err == nil {
		return real, hops
	}
	target := path
	for hops > 0 {
		info, err := os.Lstat(target)
		if err != nil || info.Mode()&os.ModeSymlink == 0 {
			return target, hops
		}
		text, err := os.Readlink(target)
		if err != nil {
			return target, hops
		}
		from := filepath.Dir(target)
		if filepath.IsAbs(text) {
			from = string(filepath.Separator)
		}
		target, hops = followed(from, text, hops-1)
	}
	return target, hops
}

// followed is the path that link text reaches from dir, read one part at a
// time as the system reads it: a folder that is a link is resolved before a
// later .. climbs out of it. Cleaned as text, alias/.. would vanish, and the
// write would go to another file.
func followed(dir, text string, hops int) (string, int) {
	at := dir
	if real, err := filepath.EvalSymlinks(at); err == nil {
		at = real
	}
	parts := strings.Split(filepath.ToSlash(text), "/")
	for i, part := range parts {
		switch part {
		case "", ".":
		case "..":
			at = filepath.Dir(at)
		default:
			at = filepath.Join(at, part)
			// A folder that is a link is followed now, also to a folder
			// that does not exist yet, so a later .. climbs out of where
			// it points.
			if i < len(parts)-1 {
				at, hops = resolved(at, hops)
			}
		}
	}
	return at, hops
}

// withRepairHint says how to get past a settings file that cannot be read:
// the form starts from what the file says, so it cannot start from that one.
func withRepairHint(err error, path string) error {
	return fmt.Errorf("%w; fix %s or delete it, then run autoreview setup again", err, path)
}
