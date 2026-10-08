// Command autoreview is one place to review your team's PRs and handle
// your own.
package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"strconv"
	"syscall"

	"github.com/charmbracelet/x/term"

	"github.com/venabots/autoreview/internal/cli"
	"github.com/venabots/autoreview/internal/engine"
	"github.com/venabots/autoreview/internal/gh"
	"github.com/venabots/autoreview/internal/mine"
	"github.com/venabots/autoreview/internal/plain"
	"github.com/venabots/autoreview/internal/prs"
	"github.com/venabots/autoreview/internal/repo"
	"github.com/venabots/autoreview/internal/sanitize"
)

// version is set at build time with -ldflags "-X main.version=...". A
// source tarball has no VCS data to stamp, so the build has to say it.
var version = "dev"

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	cfg, err := cli.Parse(args, os.Getenv)
	if err != nil {
		say(stderr, "error: %v\nsee autoreview --help\n", err)
		return engine.ExitFailed
	}
	if cfg.Help {
		say(stdout, "%s", cli.Help)
		return engine.ExitOK
	}
	if cfg.Version {
		say(stdout, "autoreview %s\n", version)
		return engine.ExitOK
	}

	// A closed pipe must end a write with an error, not end the process:
	// `autoreview --headless | head` would otherwise die with work running.
	signal.Ignore(syscall.SIGPIPE)

	rc, err := repo.Load(context.Background(), gh.Exec, git)
	if err != nil {
		// The message can carry a line gh printed, which can carry text
		// GitHub sent. It is drawn on a terminal like any other.
		say(stderr, "error: %s\n", sanitize.Line(err.Error()))
		return engine.ExitFailed
	}

	screen := wantsScreen(cfg.View, stderr)
	eng := engine.New(engineConfig(cfg, rc, screen), deps(cfg, rc))
	stopOnSignal(eng)

	if screen {
		return runScreen(eng, rc, stderr)
	}
	result := eng.Run()
	if err := plain.Lists(stdout, stderr, result.Final); err != nil {
		return engine.ExitFailed
	}
	return result.Code
}

// say writes a message for a person. A failed write has nowhere better to
// be reported than the stream that just failed.
func say(w io.Writer, format string, args ...any) {
	_, _ = fmt.Fprintf(w, format, args...)
}

// wantsScreen decides between the view and plain lines. The view needs a
// terminal to draw on and one to read keys from. Asked for without one, the
// run says so and prints plain lines, as a run without the flag would.
func wantsScreen(view cli.View, stderr io.Writer) bool {
	onTerminal := term.IsTerminal(os.Stdout.Fd()) && term.IsTerminal(os.Stdin.Fd())
	switch view {
	case cli.ViewPlain:
		return false
	case cli.ViewScreen:
		if !onTerminal {
			say(stderr, "note: --tui needs a terminal; printing plain lines\n")
		}
	}
	return onTerminal
}

// engineConfig watches only under the view. A run off a terminal looks
// once: nobody is there to press q.
func engineConfig(cfg cli.Config, rc repo.Context, screen bool) engine.Config {
	c := engine.Config{
		Repo:  rc,
		Gates: prs.Gates{CI: !cfg.SkipCI, Stack: !cfg.Stacked},
		Only:  cfg.Only,
	}
	if screen {
		c.Watch = cfg.Watch.Duration
	}
	return c
}

func deps(cfg cli.Config, rc repo.Context) engine.Deps {
	opts := prs.Options{Me: rc.Me, All: cfg.All, Dependabot: cfg.Dependabot}
	return engine.Deps{
		List: func(ctx context.Context) (prs.Listing, error) {
			return prs.List(ctx, gh.Exec, rc.Owner, rc.Name, opts)
		},
		Mine: func(ctx context.Context) ([]mine.PR, error) {
			return mine.List(ctx, gh.Exec, rc.Owner, rc.Name)
		},
	}
}

// stopOnSignal turns the three signals that end a session into one Quit.
// A hangup is handled like an interrupt: a closed terminal must stop the
// run as surely as ctrl-C does.
func stopOnSignal(eng *engine.Engine) {
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)
	go func() {
		<-signals
		eng.Do(engine.Quit{})
	}()
}

func git(ctx context.Context, args ...string) ([]byte, error) {
	return exec.CommandContext(ctx, "git", args...).Output()
}

// openInBrowser opens one PR's page. The repo is named, so the call does
// not depend on the directory gh happens to run in.
func openInBrowser(rc repo.Context) func(ctx context.Context, pr int) error {
	return func(ctx context.Context, pr int) error {
		_, err := gh.Exec(ctx, "pr", "view", strconv.Itoa(pr), "--web", "--repo", rc.Slug())
		return err
	}
}
