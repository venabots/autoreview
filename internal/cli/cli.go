// Package cli turns the command line and the environment into one Config.
// Bad input is an error with a message that names the flag, and the caller
// exits 1 on it: a run started by cron must not go ahead on a guess.
package cli

import (
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/pflag"

	"github.com/venabots/autoreview/internal/config"
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
	// NoAutofix starts the view with autofix off for your PRs. Only the
	// settings file sets it; the zero value is the default, autofix on.
	NoAutofix bool
	// AutofixCap is the most fixes one of your PRs gets in a row while
	// nobody else acts on it. Only the settings file sets it.
	AutofixCap int
	// Verdicts is which events a review may submit. Only the settings file
	// sets it.
	Verdicts review.Verdicts
	// Panelists is the panel a review seats, each backend[:model], or nil
	// for the one panel-review.sh picks: $PANEL_REVIEW_PANELISTS, or every
	// backend on PATH.
	Panelists []string
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

// Panel is the subcommand that reviews one change with several models. It
// is read as a subcommand only as the first argument, as Stats is.
const Panel = "panel"

// Setup is the subcommand that asks for the settings and writes them.
const Setup = "setup"

// DefaultWatch is the wait between looks when nobody chose one.
const DefaultWatch = "2"

// EnvWatch names the variable that sets the wait between looks.
const EnvWatch = "AUTOREVIEW_WATCH_INTERVAL"

// valued names the flags that take a value, in the order their values are
// checked. The first one has a short form.
var valued = []string{"jobs", "timeout", "budget", "log-dir", "skills", "focus", "orchestrator", "fallback", "max-idle", "max-passes"}

// noValue marks a --watch given with no value. No interval can contain it.
const noValue = "\x00"

// Parse reads the arguments after the program name, with no settings
// file. env looks a variable up, and an empty value counts as unset.
func Parse(args []string, env func(string) string) (Config, error) {
	return ParseWith(args, env, config.File{})
}

// ParseWith reads the arguments with the person's settings file between
// the variables and the built-in defaults. The file only gives values: a
// run off a terminal loops when --watch or --babysit is given, whatever the
// file says.
func ParseWith(args []string, env func(string) string, file config.File) (Config, error) {
	cfg, err := parseFlags(args, env, file)
	if err != nil || cfg.Help || cfg.Version {
		return cfg, err
	}
	if err := CheckFile(file); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

// CheckFile checks each value the settings file sets, by itself and with
// no variable, so a bad key stops every run and not only one that no flag
// or variable replaces it in. A bad variable stays the business of the run
// that reads it, as it was before the file.
//
// The pair of orchestrator and fallback is left to the run that uses the
// two: a fallback of claude in the file is right for a run whose variable
// moves the orchestrator to codex, and checked against the default it
// would clash.
func CheckFile(file config.File) error {
	if !file.Found {
		return nil
	}
	alone := file
	alone.Review.Fallback = nil
	if _, err := parseFlags(nil, func(string) string { return "" }, alone); err != nil {
		return err
	}
	if file.Review.Fallback == nil {
		return nil
	}
	if _, err := review.ParseFallback(*file.Review.Fallback); err != nil {
		return fmt.Errorf("review.fallback in %s: %w", file.Path, err)
	}
	return nil
}

// parseFlags reads the arguments with the file's values as fallbacks.
func parseFlags(args []string, env func(string) string, file config.File) (Config, error) {
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
	set := settingsOf(file)
	value, told := set.pick("watch", watch, watch != "" && watch != noValue, env(EnvWatch), DefaultWatch)
	interval, err := ParseInterval(value, "watch")
	if err != nil {
		return Config{}, told(err)
	}
	cfg.View, cfg.Only, cfg.Watch = view, only, interval
	given := func(name string) bool { return fs.Changed(name) }
	cfg = withFileSwitches(cfg, set, given)
	if cfg.Panelists, err = panelists(file, env); err != nil {
		return Config{}, err
	}
	cfg, err = withReviewFlags(cfg, raw, given, env, set)
	if err != nil {
		return Config{}, err
	}
	return withLoopFlags(cfg, raw, babysit, given, env, set)
}

// withFileSwitches takes the flags with no value from the settings file
// when they were not given. --all=false turns one off for one run.
func withFileSwitches(cfg Config, set settings, given func(string) bool) Config {
	switched := func(name string, flag bool) bool {
		if on, ok := set.on[name]; ok && !given(name) {
			return on
		}
		return flag
	}
	cfg.All = switched("all", cfg.All)
	cfg.Dependabot = switched("dependabot", cfg.Dependabot)
	cfg.Stacked = switched("stacked", cfg.Stacked)
	cfg.SkipCI = switched("skip-wait-for-ci", cfg.SkipCI)
	cfg.NoAutofix = !switched("autofix", true)
	return cfg
}

// withReviewFlags checks the flags that say how a review runs.
func withReviewFlags(cfg Config, raw map[string]*string, given func(string) bool, env func(string) string, set settings) (Config, error) {
	for _, name := range valued {
		if err := refuseFlagAsValue(name, *raw[name]); err != nil {
			return Config{}, err
		}
	}
	pick := func(name, envName, fallback string) (string, func(error) error) {
		return set.pick(name, *raw[name], given(name), env(envName), fallback)
	}
	var err error
	value, told := pick("jobs", EnvJobs, defaultJobs)
	if cfg.Jobs, err = count("jobs", value, 1, MaxJobs); err != nil {
		return Config{}, told(err)
	}
	value, told = pick("timeout", EnvTimeout, defaultTimeout)
	timeout, err := count("timeout", value, 0, maxTimeout)
	if err != nil {
		return Config{}, told(err)
	}
	cfg.Timeout = seconds(timeout)
	if value, told = pick("budget", EnvBudget, ""); value != "" || given("budget") {
		if cfg.Budget, err = budget(value); err != nil {
			return Config{}, told(err)
		}
	}
	// --focus has no variable.
	if value, told = set.pick("focus", *raw["focus"], given("focus"), "", ""); value != "" || given("focus") {
		if cfg.Focus, err = ParseFocus(value); err != nil {
			return Config{}, told(err)
		}
	}
	value, told = pick("orchestrator", EnvOrchestrator, "claude")
	if cfg.Orchestrator, err = review.ParseOrchestrator(value); err != nil {
		return Config{}, told(err)
	}
	value, told = pick("fallback", EnvFallback, "auto")
	if cfg.Fallback, err = review.ParseFallback(value); err != nil {
		return Config{}, told(err)
	}
	if cfg.Fallback.Kind == review.FallbackSpec && cfg.Fallback.Spec == cfg.Orchestrator {
		return Config{}, told(fmt.Errorf("--fallback %s is the orchestrator itself; name another, or none", cfg.Orchestrator.Label()))
	}
	cfg.LogDir = chosen(*raw["log-dir"], given("log-dir"), env(EnvLogDir), "")
	cfg.Skills = chosen(*raw["skills"], given("skills"), env(EnvSkills), "")
	cfg.Override = env(EnvOverride)
	// A machine that ran 0.24 from cron set this variable to replace the
	// reviewer. Read as nothing, it would leave the built-in reviewer to
	// run, and that one posts.
	if env(EnvRetiredOverride) != "" {
		return Config{}, fmt.Errorf("$%s is no longer read, and the run would not use it: move the command to $%s, or unset it", EnvRetiredOverride, EnvOverride)
	}
	value, told = set.pick("verdicts", "", false, "", "approve-and-request-changes")
	if cfg.Verdicts, err = review.ParseVerdicts(value); err != nil {
		return Config{}, told(fmt.Errorf("the verdicts %w", err))
	}
	if cfg.Verdicts != review.VerdictsAll && cfg.Override != "" {
		return Config{}, told(fmt.Errorf("verdicts %s cannot be used with $%s: that command decides for itself what it posts", cfg.Verdicts, EnvOverride))
	}
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

// prNumbers reads the positional arguments. Anything that is not a PR
// number is refused: a misspelled flag value must not be dropped silently.
func prNumbers(args []string) ([]int, error) {
	numbers := make([]int, 0, len(args))
	for _, arg := range args {
		if arg == Stats || arg == Panel || arg == Setup {
			return nil, fmt.Errorf("%s is a subcommand and goes first: autoreview %s", arg, arg)
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
// interval, with no sign that anything was misread. The same goes for a
// group of short flags that ends in one of them, such as `-aw 5`.
func refuseSpacedValue(args []string) error {
	for i, arg := range args {
		if arg == "--" {
			return nil
		}
		flag, ok := optionalValueFlag(arg)
		if ok && i+1 < len(args) && allDigits(args[i+1]) {
			next := args[i+1]
			return fmt.Errorf("%s takes its value with \"=\": write %s=%s, or put PR %s before the flag", flag, arg, next, next)
		}
	}
	return nil
}

// optionalValueFlag is the flag with an optional value that arg ends in:
// arg itself, or the last letter of a group of short flags. A group with -j
// in it is left alone, because -j takes the rest of the group as its value.
func optionalValueFlag(arg string) (string, bool) {
	if optionalValueFlags[arg] {
		return arg, true
	}
	if len(arg) < 3 || arg[0] != '-' || arg[1] == '-' || strings.ContainsAny(arg, "=j") {
		return "", false
	}
	last := "-" + arg[len(arg)-1:]
	return last, optionalValueFlags[last]
}
