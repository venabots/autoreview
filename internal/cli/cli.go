// Package cli turns the command line and the environment into one Config.
// Bad input is an error with a message that names the flag, and the caller
// exits 1 on it: a run started by cron must not go ahead on a guess.
package cli

import (
	"errors"
	"fmt"
	"io"
	"strconv"

	"github.com/spf13/pflag"
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
	Watch   Interval
	View    View
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

// noValue marks a --watch given with no value. No interval can contain it.
const noValue = "\x00"

// Parse reads the arguments after the program name. env looks a variable
// up, and an empty value counts as unset.
func Parse(args []string, env func(string) string) (Config, error) {
	if err := refuseSpacedValue(args); err != nil {
		return Config{}, err
	}

	var cfg Config
	var watch string
	var screen, plain bool
	fs := pflag.NewFlagSet("autoreview", pflag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.Usage = func() {}
	fs.BoolVarP(&cfg.All, "all", "a", false, "")
	fs.BoolVarP(&cfg.Dependabot, "dependabot", "d", false, "")
	fs.BoolVarP(&cfg.Stacked, "stacked", "s", false, "")
	fs.BoolVar(&cfg.SkipCI, "skip-wait-for-ci", false, "")
	fs.StringVarP(&watch, "watch", "w", "", "")
	fs.Lookup("watch").NoOptDefVal = noValue
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
var optionalValueFlags = map[string]bool{"--watch": true, "-w": true}

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
