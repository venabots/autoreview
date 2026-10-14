package cli

import (
	"slices"
	"strings"
	"testing"
	"time"
)

func noEnv(string) string { return "" }

func parse(t *testing.T, args ...string) Config {
	t.Helper()
	cfg, err := Parse(args, noEnv)
	if err != nil {
		t.Fatalf("Parse(%q): %v", args, err)
	}
	return cfg
}

func TestParseDefaults(t *testing.T) {
	cfg := parse(t)
	if cfg.View != ViewAuto || len(cfg.Only) != 0 {
		t.Errorf("defaults = %+v", cfg)
	}
	if cfg.Watch.Duration != 2*time.Minute || cfg.Watch.Text != "2m" {
		t.Errorf("default watch = %+v, want 2m", cfg.Watch)
	}
	if cfg.All || cfg.Dependabot || cfg.Stacked || cfg.SkipCI {
		t.Errorf("a filter flag is on by default: %+v", cfg)
	}
}

func TestParseFlags(t *testing.T) {
	cfg := parse(t, "-a", "--dependabot", "-s", "--skip-wait-for-ci", "--headless")
	if !cfg.All || !cfg.Dependabot || !cfg.Stacked || !cfg.SkipCI || cfg.View != ViewPlain {
		t.Errorf("flags = %+v", cfg)
	}
	if parse(t, "--tui").View != ViewScreen {
		t.Error("--tui did not choose the screen")
	}
}

func TestParsePositionalPRs(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want []int
	}{
		{"one PR", []string{"213"}, []int{213}},
		{"two PRs", []string{"213", "240"}, []int{213, 240}},
		{"a flag after the PRs", []string{"213", "--all"}, []int{213}},
		{"a flag between the PRs", []string{"213", "-a", "240"}, []int{213, 240}},
		{"a watch value before a PR", []string{"--watch=5", "213"}, []int{213}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := parse(t, c.args...).Only; !slices.Equal(got, c.want) {
				t.Errorf("Only = %v, want %v", got, c.want)
			}
		})
	}
}

func TestParseWatch(t *testing.T) {
	cases := []struct {
		name string
		args []string
		env  string
		want time.Duration
	}{
		{"a bare flag is the default", []string{"--watch"}, "", 2 * time.Minute},
		{"a value with =", []string{"--watch=5"}, "", 5 * time.Minute},
		{"the short flag with =", []string{"-w=1h"}, "", time.Hour},
		{"the environment sets the default", nil, "10", 10 * time.Minute},
		{"a bare flag takes the environment's value", []string{"-w"}, "10", 10 * time.Minute},
		{"the flag outranks the environment", []string{"--watch=3"}, "10", 3 * time.Minute},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			env := func(name string) string {
				if name == EnvWatch {
					return c.env
				}
				return ""
			}
			cfg, err := Parse(c.args, env)
			if err != nil {
				t.Fatalf("Parse(%q): %v", c.args, err)
			}
			if cfg.Watch.Duration != c.want {
				t.Errorf("watch = %v, want %v", cfg.Watch.Duration, c.want)
			}
		})
	}
}

func TestParseRefuses(t *testing.T) {
	cases := []struct {
		name    string
		args    []string
		wantErr string
	}{
		{"both views", []string{"--tui", "--headless"}, "--tui and --headless cannot be used together"},
		{"a word where a PR goes", []string{"status"}, `"status" is not a PR number`},
		{"the subcommand after a flag", []string{"--all", "stats"}, "stats is a subcommand and goes first: autoreview stats"},
		{"the subcommand after a PR", []string{"213", "stats"}, "stats is a subcommand and goes first"},
		{"the panel subcommand after a flag", []string{"--all", "panel"}, "panel is a subcommand and goes first: autoreview panel"},
		{"PR zero", []string{"0"}, `"0" is not a PR number`},
		{"a signed number", []string{"+5"}, `"+5" is not a PR number`},
		{"an unknown flag", []string{"--pick"}, "unknown flag: --pick"},
		{"a watch value after a space", []string{"--watch", "5"}, `--watch takes its value with "=": write --watch=5`},
		{"a short watch value after a space", []string{"-w", "5"}, `-w takes its value with "="`},
		{"a watch value after a group of short flags", []string{"-aw", "30"}, `-w takes its value with "=": write -aw=30, or put PR 30 before the flag`},
		{"a babysit value after a group of short flags", []string{"-nb", "15"}, `-b takes its value with "=": write -nb=15`},
		{"a zero interval", []string{"--watch=0"}, "invalid watch interval"},
		{"an interval in seconds", []string{"--watch=30s"}, "invalid watch interval"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := Parse(c.args, noEnv)
			if err == nil || !strings.Contains(err.Error(), c.wantErr) {
				t.Errorf("Parse(%q) error = %v, want one containing %q", c.args, err, c.wantErr)
			}
		})
	}
}

func TestParseRefusesABadEnvironmentInterval(t *testing.T) {
	_, err := Parse(nil, func(string) string { return "soon" })
	if err == nil || !strings.Contains(err.Error(), `invalid watch interval: "soon"`) {
		t.Errorf("Parse error = %v", err)
	}
}

func TestParseHelpAndVersionSkipTheRest(t *testing.T) {
	bad := func(string) string { return "soon" }
	for _, flag := range []string{"--help", "-h", "--version", "-V"} {
		cfg, err := Parse([]string{flag}, bad)
		if err != nil {
			t.Errorf("Parse(%s) with a bad environment: %v", flag, err)
		}
		if !cfg.Help && !cfg.Version {
			t.Errorf("Parse(%s) set neither Help nor Version", flag)
		}
	}
}

func TestTheHelpNamesTheMyPRsKeysAndTheirSkills(t *testing.T) {
	for _, want := range []string{"My PRs tab", "f fixes the selected PR once", "A autofixes all of them", "babysit-pr and sync-main",
		"Autofix is on for all of them when the view opens"} {
		if !strings.Contains(Help, want) {
			t.Errorf("the help does not say %q", want)
		}
	}
}

// The check for a spaced value must not refuse what a person may write: a
// group with its value after "=", a group with no value before a PR that a
// "--" sets apart, and a value flag whose value is a number.
func TestAGroupOfShortFlagsStillParses(t *testing.T) {
	cfg, err := Parse([]string{"-aw=30", "213"}, noEnv)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if !cfg.All || cfg.Watch.Duration != 30*time.Minute || !slices.Equal(cfg.Only, []int{213}) {
		t.Errorf("-aw=30 213 gave all %v, watch %v, PRs %v", cfg.All, cfg.Watch.Duration, cfg.Only)
	}
	if _, err := Parse([]string{"-aj", "4"}, noEnv); err != nil {
		t.Errorf("-aj 4 was refused: %v", err)
	}
}
