// Package cli turns the command line and the environment into one Config.
// Bad input is an error with a message that names the flag, and the caller
// exits 1 on it: a run started by cron must not go ahead on a guess.
package cli

import (
	"errors"
	"fmt"
	"io"
	"strconv"
	"time"

	"github.com/spf13/pflag"

	"github.com/venabots/autoreview/internal/review"
)

// View is which front-end a run uses.
type View int

const (
	// ViewAuto is the full-screen view on a terminal and plain lines
	// anywhere else.
	ViewAuto View = iota
	ViewScreen
	ViewPlain
)

// Config is what one run was asked to do.
type Config struct {
	// Only limits the run to these PRs. Empty means every PR.
	Only []int
	// All includes PRs that are already approved.
	All bool
	// Dependabot includes Dependabot's PRs.
	Dependabot bool
	// Stacked reviews a PR that sits on another open PR.
	Stacked bool
	// SkipCI reviews a PR whose checks have not passed.
	SkipCI bool
	// Watch is the wait between two looks at the PR list.
	Watch Interval
	View  View

	// Once makes one pass and stops looking for work. Under the view, the
	// view then holds for a person.
	Once bool
	// WatchGiven and BabysitGiven say the flags were given. A run off a
	// terminal loops only when asked.
	WatchGiven   bool
	BabysitGiven bool
	// Babysit is the wait between passes of a run that babysits. Under
	// --watch, and in the view, it is how long a reviewed PR rests.
	Babysit Interval
	// CIWait is how long a run that looks once waits for checks that are
	// still running.
	CIWait Interval
	// MaxIdle is how many idle looks in a row end a run that babysits.
	// MaxPasses is how many reviews one PR gets in a run.
	MaxIdle   int
	MaxPasses int
	// NoPost reviews and leaves the PR alone.
	NoPost bool
	// Continue resumes a PR's earlier review session.
	Continue bool
	// Jobs is how many reviews run at once.
	Jobs int
	// Timeout is the limit for one review. Zero means none.
	Timeout time.Duration
	// Budget is the cost cap for one review, as written, or empty.
	Budget string
	// LogDir is where the run directory is made, or empty for a temporary
	// directory.
	LogDir string
	// Skills is the --skills value, not yet checked against the disk.
	Skills string
	// Focus is what the reviewers are told to look at, or empty.
	Focus        string
	Orchestrator review.Orchestrator
	// Fallback is what retries a review whose orchestrator failed: auto,
	// none, or a named orchestrator. It is resolved against what is
	// installed when the run starts.
	Fallback review.Fallback
	// Override is a shell command that replaces the built-in reviewer.
	Override string

	Help    bool
	Version bool
}

// Stats is the subcommand that reports how each model has done. It is
// read as a subcommand only as the first argument.
const Stats = "stats"

// DefaultWatch is the wait between looks when nobody chose one.
const DefaultWatch = "2"

// EnvWatch names the variable that sets the wait between looks.
const EnvWatch = "AUTOREVIEW_WATCH_INTERVAL"

// valued names the flags that take a value, in the order their values are
// checked. The first one has a short form.
var valued = []string{"jobs", "timeout", "budget", "log-dir", "skills", "focus", "orchestrator", "fallback", "max-idle", "max-passes"}

// noValue marks a --watch given with no value. No interval can contain it.
const noValue = "\x00"

// Parse reads the arguments after the program name. env looks a variable
// up, and an empty value counts as unset.
func Parse(args []string, env func(string) string) (Config, error) {
	if err := refuseSpacedValue(args); err != nil {
		return Config{}, err
	}

	var cfg Config
	var watch, babysit string
	var screen, plain bool
	raw := map[string]*string{}
	for _, name := range valued {
		raw[name] = new(string)
	}
	fs := pflag.NewFlagSet("autoreview", pflag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.Usage = func() {}
	fs.BoolVarP(&cfg.All, "all", "a", false, "")
	fs.BoolVarP(&cfg.Dependabot, "dependabot", "d", false, "")
	fs.BoolVarP(&cfg.Stacked, "stacked", "s", false, "")
	fs.BoolVar(&cfg.SkipCI, "skip-wait-for-ci", false, "")
	fs.StringVarP(&watch, "watch", "w", "", "")
	fs.Lookup("watch").NoOptDefVal = noValue
	fs.StringVarP(&babysit, "babysit", "b", "", "")
	fs.Lookup("babysit").NoOptDefVal = noValue
	fs.BoolVar(&cfg.Once, "once", false, "")
	fs.BoolVarP(&cfg.NoPost, "no-post", "n", false, "")
	fs.BoolVarP(&cfg.Continue, "continue", "C", false, "")
	fs.StringVarP(raw["jobs"], "jobs", "j", "", "")
	for _, name := range valued[1:] {
		fs.StringVar(raw[name], name, "", "")
	}
	fs.BoolVar(&screen, "tui", false, "")
	fs.BoolVar(&plain, "headless", false, "")
	fs.BoolVarP(&cfg.Help, "help", "h", false, "")
	fs.BoolVarP(&cfg.Version, "version", "V", false, "")
	if err := fs.Parse(args); err != nil {
		return Config{}, err
	}
	if cfg.Help || cfg.Version {
		return cfg, nil
	}

	view, err := viewFrom(screen, plain)
	if err != nil {
		return Config{}, err
	}
	only, err := prNumbers(fs.Args())
	if err != nil {
		return Config{}, err
	}
	interval, err := ParseInterval(watchValue(watch, env), "watch")
	if err != nil {
		return Config{}, err
	}
	cfg.View, cfg.Only, cfg.Watch = view, only, interval
	given := func(name string) bool { return fs.Changed(name) }
	cfg, err = withReviewFlags(cfg, raw, given, env)
	if err != nil {
		return Config{}, err
	}
	return withLoopFlags(cfg, raw, babysit, given, env)
}

// withReviewFlags checks the flags that say how a review runs.
func withReviewFlags(cfg Config, raw map[string]*string, given func(string) bool, env func(string) string) (Config, error) {
	for _, name := range valued {
		if err := refuseFlagAsValue(name, *raw[name]); err != nil {
			return Config{}, err
		}
	}
	var err error
	if cfg.Jobs, err = count("jobs", chosen(*raw["jobs"], given("jobs"), env(EnvJobs), defaultJobs), 1, maxJobs); err != nil {
		return Config{}, err
	}
	timeout, err := count("timeout", chosen(*raw["timeout"], given("timeout"), env(EnvTimeout), defaultTimeout), 0, maxTimeout)
	if err != nil {
		return Config{}, err
	}
	cfg.Timeout = seconds(timeout)
	if value := chosen(*raw["budget"], given("budget"), env(EnvBudget), ""); value != "" || given("budget") {
		if cfg.Budget, err = budget(value); err != nil {
			return Config{}, err
		}
	}
	if given("focus") {
		if cfg.Focus, err = ParseFocus(*raw["focus"]); err != nil {
			return Config{}, err
		}
	}
	if cfg.Orchestrator, err = review.ParseOrchestrator(chosen(*raw["orchestrator"], given("orchestrator"), env(EnvOrchestrator), "claude")); err != nil {
		return Config{}, err
	}
	if cfg.Fallback, err = review.ParseFallback(chosen(*raw["fallback"], given("fallback"), env(EnvFallback), "auto")); err != nil {
		return Config{}, err
	}
	if cfg.Fallback.Kind == review.FallbackSpec && cfg.Fallback.Spec == cfg.Orchestrator {
		return Config{}, fmt.Errorf("--fallback %s is the orchestrator itself; name another, or none", cfg.Orchestrator.Label())
	}
	cfg.LogDir = chosen(*raw["log-dir"], given("log-dir"), env(EnvLogDir), "")
	cfg.Skills = chosen(*raw["skills"], given("skills"), env(EnvSkills), "")
	cfg.Override = env(EnvOverride)
	if cfg.NoPost && cfg.Override != "" {
		return Config{}, fmt.Errorf("--no-post cannot be used with $%s: that command decides for itself what it posts", EnvOverride)
	}
	return cfg, nil
}

func viewFrom(screen, plain bool) (View, error) {
	switch {
	case screen && plain:
		return ViewAuto, errors.New("--tui and --headless cannot be used together")
	case screen:
		return ViewScreen, nil
	case plain:
		return ViewPlain, nil
	}
	return ViewAuto, nil
}

func watchValue(flag string, env func(string) string) string {
	if flag != "" && flag != noValue {
		return flag
	}
	if fromEnv := env(EnvWatch); fromEnv != "" {
		return fromEnv
	}
	return DefaultWatch
}

// prNumbers reads the positional arguments. Anything that is not a PR
// number is refused: a misspelled flag value must not be dropped silently.
func prNumbers(args []string) ([]int, error) {
	numbers := make([]int, 0, len(args))
	for _, arg := range args {
		if arg == Stats {
			return nil, fmt.Errorf("%s is a subcommand and goes first: autoreview %s", Stats, Stats)
		}
		n, err := strconv.Atoi(arg)
		if err != nil || n <= 0 || !allDigits(arg) {
			return nil, fmt.Errorf("%q is not a PR number", arg)
		}
		numbers = append(numbers, n)
	}
	return numbers, nil
}

// optionalValueFlags take their value with "=" only.
var optionalValueFlags = map[string]bool{"--watch": true, "-w": true, "--babysit": true, "-b": true}

// refuseSpacedValue catches `--watch 5`. The flag's value is optional, so
// the 5 would be read as PR #5 and the run would watch one PR at the default
// interval, with no sign that anything was misread.
func refuseSpacedValue(args []string) error {
	for i, arg := range args {
		if arg == "--" {
			return nil
		}
		if optionalValueFlags[arg] && i+1 < len(args) && allDigits(args[i+1]) {
			next := args[i+1]
			return fmt.Errorf("%s takes its value with \"=\": write %s=%s, or put PR %s before the flag", arg, arg, next, next)
		}
	}
	return nil
}
