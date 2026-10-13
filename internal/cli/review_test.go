package cli

import (
	"strings"
	"testing"
	"time"

	"github.com/venabots/autoreview/internal/review"
)

func envOf(vars map[string]string) func(string) string {
	return func(name string) string { return vars[name] }
}

func TestReviewFlagDefaults(t *testing.T) {
	cfg := parse(t)
	if cfg.Jobs != 2 || cfg.Timeout != time.Hour || cfg.Budget != "" || cfg.Focus != "" {
		t.Errorf("defaults = %+v", cfg)
	}
	if cfg.Orchestrator != review.Claude() || cfg.NoPost || cfg.Continue || cfg.Once || cfg.Override != "" {
		t.Errorf("defaults = %+v", cfg)
	}
}

func TestReviewFlags(t *testing.T) {
	cfg := parse(t, "-n", "-C", "--once", "-j", "4", "--timeout=0", "--budget", "2.50",
		"--log-dir", "/logs", "--skills", "installed", "--focus", "  be strict\tabout \"the\" migration \\", "--orchestrator", "codex:gpt-5.5")
	if !cfg.NoPost || !cfg.Continue || !cfg.Once || cfg.Jobs != 4 || cfg.Timeout != 0 {
		t.Errorf("flags = %+v", cfg)
	}
	if cfg.Budget != "2.50" || cfg.LogDir != "/logs" || cfg.Skills != "installed" {
		t.Errorf("flags = %+v", cfg)
	}
	if cfg.Focus != "be strict about 'the' migration" {
		t.Errorf("focus = %q, want one line with no double quote and no trailing backslash", cfg.Focus)
	}
	if cfg.Orchestrator != (review.Orchestrator{Backend: "codex", Model: "gpt-5.5"}) {
		t.Errorf("orchestrator = %+v", cfg.Orchestrator)
	}
}

func TestTheEnvironmentSetsDefaultsAndAFlagWins(t *testing.T) {
	env := envOf(map[string]string{
		EnvJobs: "3", EnvTimeout: "600", EnvBudget: "1.25", EnvLogDir: "/var/log/ar",
		EnvSkills: "installed", EnvOrchestrator: "codex", EnvOverride: "my-review {}",
	})
	cfg, err := Parse(nil, env)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if cfg.Jobs != 3 || cfg.Timeout != 10*time.Minute || cfg.Budget != "1.25" || cfg.LogDir != "/var/log/ar" {
		t.Errorf("from the environment: %+v", cfg)
	}
	if cfg.Skills != "installed" || cfg.Orchestrator.Backend != "codex" || cfg.Override != "my-review {}" {
		t.Errorf("from the environment: %+v", cfg)
	}

	cfg, err = Parse([]string{"--jobs=8", "--orchestrator", "claude", "--budget=9"}, env)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if cfg.Jobs != 8 || cfg.Orchestrator.Backend != "claude" || cfg.Budget != "9" {
		t.Errorf("a flag did not win over its variable: %+v", cfg)
	}
}

func TestReviewFlagsRefuse(t *testing.T) {
	cases := []struct {
		name    string
		args    []string
		env     map[string]string
		wantErr string
	}{
		{"zero jobs", []string{"--jobs", "0"}, nil, `--jobs expects an integer >= 1, got "0"`},
		{"jobs that are not a number", []string{"-j", "many"}, nil, `--jobs expects an integer >= 1, got "many"`},
		{"too many jobs", []string{"--jobs=5000"}, nil, `--jobs expects an integer <= 1024, got "5000"`},
		{"a signed number of jobs", []string{"--jobs=+2"}, nil, `--jobs expects an integer >= 1`},
		{"a negative timeout", []string{"--timeout=-5"}, nil, `--timeout expects an integer >= 0`},
		{"a timeout with a unit", []string{"--timeout", "10m"}, nil, `--timeout expects an integer >= 0, got "10m"`},
		{"jobs from a bad variable", nil, map[string]string{EnvJobs: "lots"}, `--jobs expects an integer >= 1, got "lots"`},
		{"a budget of zero", []string{"--budget", "0.00"}, nil, `--budget expects a positive amount in USD`},
		{"a budget with a sign", []string{"--budget=$5"}, nil, `--budget expects a positive amount in USD, such as 2.50, got "$5"`},
		{"a budget with two points", []string{"--budget", "1.2.3"}, nil, `--budget expects a positive amount`},
		{"an empty budget", []string{"--budget="}, nil, `--budget expects a positive amount`},
		{"a blank focus", []string{"--focus", "  "}, nil, "--focus expects text, not blank space"},
		{"a focus that is only a backslash", []string{"--focus", `\`}, nil, "--focus expects text"},
		{"a focus too long for a prompt", []string{"--focus", strings.Repeat("a", 2001)}, nil, "--focus is 2001 characters; the most a prompt carries is 2000"},
		{"a flag where a value goes", []string{"--focus", "--no-post"}, nil, "--focus expects a value, but found the flag --no-post"},
		{"a flag where a directory goes", []string{"--log-dir", "--all"}, nil, "--log-dir expects a value, but found the flag --all"},
		{"an unknown orchestrator", []string{"--orchestrator", "gemini"}, nil, `"gemini"`},
		{"an orchestrator with no model after the colon", []string{"--orchestrator=codex:"}, nil, "no model"},
		{"no-post with an override", []string{"--no-post"}, map[string]string{EnvOverride: "my-review"}, "--no-post cannot be used with $AUTOREVIEW_CMD"},
		{"a flag with no value", []string{"--focus"}, nil, "flag needs an argument: --focus"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := Parse(c.args, envOf(c.env))
			if err == nil || !strings.Contains(err.Error(), c.wantErr) {
				t.Errorf("Parse(%q) error = %v, want one containing %q", c.args, err, c.wantErr)
			}
		})
	}
}

func TestParseFocusCountsCharactersNotBytes(t *testing.T) {
	if _, err := ParseFocus(strings.Repeat("語", 2000)); err != nil {
		t.Errorf("2000 wide characters were refused: %v", err)
	}
	if got, _ := ParseFocus("a\n\nb"); got != "a b" {
		t.Errorf("ParseFocus = %q, want the line breaks collapsed", got)
	}
}

func TestTheFallback(t *testing.T) {
	cases := []struct {
		args []string
		env  map[string]string
		want review.Fallback
	}{
		{nil, nil, review.Fallback{Kind: review.FallbackAuto}},
		{[]string{"--fallback", "none"}, nil, review.Fallback{Kind: review.FallbackNone}},
		{[]string{"--fallback=codex:gpt-5.5"}, nil, review.Fallback{Kind: review.FallbackSpec, Spec: review.Orchestrator{Backend: "codex", Model: "gpt-5.5"}}},
		{nil, map[string]string{EnvFallback: "off"}, review.Fallback{Kind: review.FallbackNone}},
	}
	for _, c := range cases {
		cfg, err := Parse(c.args, envOf(c.env))
		if err != nil || cfg.Fallback != c.want {
			t.Errorf("Parse(%q) fallback = %+v, %v; want %+v", c.args, cfg.Fallback, err, c.want)
		}
	}
	for _, bad := range [][]string{{"--fallback", "claude"}, {"--orchestrator", "codex", "--fallback", "codex"}, {"--fallback", "gemini"}} {
		if _, err := Parse(bad, noEnv); err == nil {
			t.Errorf("Parse(%q) was accepted", bad)
		}
	}
}
