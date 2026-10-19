package cli

import (
	"fmt"
	"strconv"

	"github.com/venabots/autoreview/internal/config"
)

// settings is the person's settings file as the flags read it: each value
// as text, the way its flag takes it, by the flag's name.
type settings struct {
	path string
	text map[string]setting
	on   map[string]bool
}

// setting is one value from the file, and the key it was written under.
type setting struct {
	key   string
	value string
}

func settingsOf(f config.File) settings {
	s := settings{path: f.Path, text: map[string]setting{}, on: map[string]bool{}}
	text := func(name, key string, v *string) {
		// An empty value is a value: each check says whether it means
		// anything. An empty budget or focus means none.
		if v != nil {
			s.text[name] = setting{key: key, value: *v}
		}
	}
	number := func(name, key string, v *int) {
		if v != nil {
			s.text[name] = setting{key: key, value: strconv.Itoa(*v)}
		}
	}
	flag := func(name string, v *bool) {
		if v != nil {
			s.on[name] = *v
		}
	}
	text("orchestrator", "review.orchestrator", f.Review.Orchestrator)
	text("fallback", "review.fallback", f.Review.Fallback)
	number("jobs", "review.jobs", f.Review.Jobs)
	number("timeout", "review.timeout", f.Review.Timeout)
	text("budget", "review.budget", f.Review.Budget)
	text("focus", "review.focus", f.Review.Focus)
	text("verdicts", "review.verdicts", f.Review.Verdicts)
	text("watch", "loop.watch", f.Loop.Watch)
	text("babysit", "loop.rest", f.Loop.Rest)
	number("max-passes", "loop.max_passes", f.Loop.MaxPasses)
	number("max-idle", "loop.max_idle", f.Loop.MaxIdle)
	text("ci-wait", "loop.ci_wait", f.Loop.CIWait)
	number("autofix-cap", "mine.autofix_cap", f.Mine.AutofixCap)
	flag("autofix", f.Mine.Autofix)
	flag("all", f.PRs.Approved)
	flag("dependabot", f.PRs.Dependabot)
	flag("stacked", f.PRs.Stacked)
	flag("skip-wait-for-ci", f.PRs.SkipCI)
	return s
}

// pick is the value that applies to the flag name: the flag when it was
// given, else its variable, else the settings file, else the default.
//
// told adds to an error where the value came from, when that was the file.
// An error that named only the flag would send the person to look for a
// flag that nobody typed.
func (s settings) pick(name, flag string, given bool, fromEnv, fallback string) (value string, told func(error) error) {
	switch {
	case given:
		return flag, asIs
	case fromEnv != "":
		return fromEnv, asIs
	}
	if v, ok := s.text[name]; ok {
		return v.value, s.from(v)
	}
	return fallback, asIs
}

func (s settings) from(v setting) func(error) error {
	return func(err error) error {
		return fmt.Errorf("%s in %s: %w", v.key, s.path, err)
	}
}

func asIs(err error) error {
	return err
}
