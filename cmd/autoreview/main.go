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
	"time"

	xterm "github.com/charmbracelet/x/term"

	"github.com/venabots/autoreview/internal/cli"
	"github.com/venabots/autoreview/internal/engine"
	"github.com/venabots/autoreview/internal/gh"
	"github.com/venabots/autoreview/internal/mine"
	"github.com/venabots/autoreview/internal/prs"
	"github.com/venabots/autoreview/internal/repo"
	"github.com/venabots/autoreview/internal/sanitize"
	"github.com/venabots/autoreview/internal/session"
	"github.com/venabots/autoreview/internal/stats"
	"github.com/venabots/autoreview/internal/term"
	"github.com/venabots/autoreview/internal/tui"
)

// version is set at build time with -ldflags "-X main.version=...". A
// source tarball has no VCS data to stamp, so the build has to say it.
var version = "dev"

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	// A subcommand is the first argument and nothing else. A PR number can
	// never be read as one, and a flag before it is an error cli.Parse names.
	if len(args) > 0 && args[0] == cli.Stats {
		return stats.Run(args[1:], os.Getenv, time.Now(), xterm.IsTerminal(os.Stdout.Fd()), stdout, stderr)
	}

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

	if !wantsScreen(cfg.View, stderr) {
		return runHeadless(cfg, rc, stdout, stderr)
	}
	return runScreen(cfg, rc, stdout, stderr)
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
	onTerminal := xterm.IsTerminal(os.Stdout.Fd()) && xterm.IsTerminal(os.Stdin.Fd())
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
// engineConfig is how the run loops.
//
//   - The view watches: it looks every --watch minutes, reviews what has
//     become ready, and rests a reviewed PR for the --babysit interval.
//     --once makes it one pass that holds.
//   - Off a terminal, --watch does the same and never ends by itself.
//   - Off a terminal, --babysit makes a pass every --babysit minutes and
//     ends when nothing is left.
//   - Otherwise a run makes one pass, after it waits for checks that are
//     still running.
func engineConfig(cfg cli.Config, rc repo.Context, screen bool) engine.Config {
	c := engine.Config{
		Repo:      rc,
		Gates:     prs.Gates{CI: !cfg.SkipCI, Stack: !cfg.Stacked},
		Only:      cfg.Only,
		MaxPasses: cfg.MaxPasses,
		MaxIdle:   cfg.MaxIdle,
	}
	switch {
	case screen || cfg.WatchGiven:
		c.Watch = cfg.Watch.Duration
		c.Loop = engine.LoopWatch
		c.Watching = !cfg.Once
		c.Rest = cfg.Babysit.Duration
	case cfg.BabysitGiven:
		c.Watch = cfg.Babysit.Duration
		c.Loop = engine.LoopBabysit
		c.Watching = true
	case !cfg.SkipCI:
		c.CIWait = cfg.CIWait.Duration
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

// actions is what the view can do outside itself: open a PR in the
// browser, and reopen a review in a new terminal tab.
func actions(rc repo.Context) tui.Actions {
	return tui.Actions{
		Open: func(ctx context.Context, pr int) error {
			// The repo is named, so the call does not depend on the
			// directory gh happens to run in.
			_, err := gh.Exec(ctx, "pr", "view", strconv.Itoa(pr), "--web", "--repo", rc.Slug())
			return err
		},
		OpenTab: func(ctx context.Context, label, command string) (string, error) {
			kind, err := term.Open(ctx, term.Exec, term.OSEnv(), label, command)
			return kind.Name(), err
		},
		ResumeCommand: term.ResumeCommand,
		Transcript:    session.StoreIn(session.ConfigDir(os.Getenv)).Transcript,
	}
}
