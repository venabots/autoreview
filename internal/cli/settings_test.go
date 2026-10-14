package cli

import (
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/venabots/autoreview/internal/config"
	"github.com/venabots/autoreview/internal/review"
)

func ptr[T any](v T) *T { return &v }

const settingsPath = "/home/me/.config/autoreview/config.toml"

func withSettings(edit func(*config.File)) config.File {
	f := config.File{Path: settingsPath, Found: true}
	edit(&f)
	return f
}

func parseWith(t *testing.T, env map[string]string, file config.File, args ...string) Config {
	t.Helper()
	cfg, err := ParseWith(args, envOf(env), file)
	if err != nil {
		t.Fatalf("ParseWith(%q): %v", args, err)
	}
	return cfg
}

func TestTheFlagWinsThenTheVariableThenTheFileThenTheDefault(t *testing.T) {
	file := withSettings(func(f *config.File) {
		f.Review.Jobs, f.Review.Timeout, f.Review.Orchestrator = ptr(6), ptr(60), ptr("codex")
		f.Loop.MaxPasses, f.Loop.MaxIdle = ptr(7), ptr(8)
		f.Loop.Watch, f.Loop.Rest, f.Loop.CIWait = ptr("5m"), ptr("2h"), ptr("10m")
	})
	fromFile := parseWith(t, nil, file)
	if fromFile.Jobs != 6 || fromFile.Timeout != time.Minute || fromFile.Orchestrator != (review.Orchestrator{Backend: "codex"}) {
		t.Errorf("the file's values did not apply: %+v", fromFile)
	}
	if fromFile.MaxPasses != 7 || fromFile.MaxIdle != 8 || fromFile.Watch.Text != "5m" || fromFile.Babysit.Text != "2h" || fromFile.CIWait.Text != "10m" {
		t.Errorf("the file's loop values did not apply: %+v", fromFile)
	}

	env := map[string]string{EnvJobs: "5", EnvMaxPasses: "4", EnvWatch: "3", EnvBabysit: "45", EnvCIWait: "20"}
	fromEnv := parseWith(t, env, file)
	if fromEnv.Jobs != 5 || fromEnv.MaxPasses != 4 || fromEnv.Watch.Text != "3m" || fromEnv.Babysit.Text != "45m" || fromEnv.CIWait.Text != "20m" {
		t.Errorf("a variable did not win over the file: %+v", fromEnv)
	}

	fromFlags := parseWith(t, env, file, "-j", "3", "--max-passes", "2", "--watch=9", "--babysit=15")
	if fromFlags.Jobs != 3 || fromFlags.MaxPasses != 2 || fromFlags.Watch.Text != "9m" || fromFlags.Babysit.Text != "15m" {
		t.Errorf("a flag did not win over the variable and the file: %+v", fromFlags)
	}

	defaults := parseWith(t, nil, config.File{Path: settingsPath})
	if defaults.Jobs != 2 || defaults.MaxPasses != 3 || defaults.Watch.Text != "2m" || defaults.Babysit.Text != "30m" {
		t.Errorf("with no file, the defaults are %+v", defaults)
	}
}

func TestTheAutofixCapComesFromTheFile(t *testing.T) {
	if cfg := parse(t); cfg.AutofixCap != 3 {
		t.Errorf("the default cap is %d, want 3", cfg.AutofixCap)
	}
	if cfg := parseWith(t, nil, withSettings(func(f *config.File) { f.Mine.AutofixCap = ptr(5) })); cfg.AutofixCap != 5 {
		t.Errorf("the cap is %d, want the file's 5", cfg.AutofixCap)
	}
	_, err := ParseWith(nil, noEnv, withSettings(func(f *config.File) { f.Mine.AutofixCap = ptr(0) }))
	if want := "mine.autofix_cap in " + settingsPath + ": the autofix cap expects an integer >= 1"; err == nil || !strings.Contains(err.Error(), want) {
		t.Errorf("a cap of 0: %v, want %q", err, want)
	}
}

func TestThePanelComesFromTheFileUnlessTheVariableNamesOne(t *testing.T) {
	file := withSettings(func(f *config.File) { f.Panel.Panelists = []string{"claude:opus-4.8", "codex"} })
	if cfg := parseWith(t, nil, file); !slices.Equal(cfg.Panelists, []string{"claude:opus-4.8", "codex"}) {
		t.Errorf("panelists %q, want the file's", cfg.Panelists)
	}
	// panel-review.sh reads the variable itself, so the run leaves it be.
	if cfg := parseWith(t, map[string]string{EnvPanelists: "opencode"}, file); cfg.Panelists != nil {
		t.Errorf("panelists %q, want none when $%s names a panel", cfg.Panelists, EnvPanelists)
	}
	_, err := ParseWith(nil, noEnv, withSettings(func(f *config.File) { f.Panel.Panelists = []string{"claude", "gemini"} }))
	if want := "panel.panelists in " + settingsPath + ": unknown panelist backend \"gemini\""; err == nil || !strings.Contains(err.Error(), want) {
		t.Errorf("a bad panelist: %v, want %q", err, want)
	}
}

func TestABareWatchOrBabysitTakesTheFilesValue(t *testing.T) {
	file := withSettings(func(f *config.File) { f.Loop.Watch, f.Loop.Rest = ptr("5m"), ptr("1h") })
	cfg := parseWith(t, nil, file, "--watch", "--babysit")
	if cfg.Watch.Text != "5m" || cfg.Babysit.Text != "1h" {
		t.Errorf("a bare flag took %q and %q, want the file's 5m and 1h", cfg.Watch.Text, cfg.Babysit.Text)
	}
}

// A cron job that runs autoreview once must not start to loop because the
// person's file names a wait between looks.
func TestTheFileGivesValuesAndNeverMakesARunLoop(t *testing.T) {
	file := withSettings(func(f *config.File) { f.Loop.Watch, f.Loop.Rest = ptr("5m"), ptr("1h") })
	cfg := parseWith(t, nil, file)
	if cfg.WatchGiven || cfg.BabysitGiven {
		t.Errorf("the file made the run loop: watch given %v, babysit given %v", cfg.WatchGiven, cfg.BabysitGiven)
	}
}

func TestABadValueInTheFileNamesItsKeyAndTheFile(t *testing.T) {
	cases := []struct {
		edit func(*config.File)
		want string
	}{
		{func(f *config.File) { f.Review.Jobs = ptr(0) }, "review.jobs in " + settingsPath + ": --jobs expects an integer >= 1"},
		{func(f *config.File) { f.Loop.Rest = ptr("soon") }, "loop.rest in " + settingsPath + ": invalid babysit interval"},
		{func(f *config.File) { f.Loop.CIWait = ptr("0") }, "loop.ci_wait in " + settingsPath + ": invalid CI wait interval"},
		{func(f *config.File) { f.Review.Budget = ptr("free") }, "review.budget in " + settingsPath + ": --budget expects"},
		{func(f *config.File) { f.Review.Orchestrator = ptr("gemini") }, "review.orchestrator in " + settingsPath},
		{func(f *config.File) {
			f.Review.Orchestrator, f.Review.Fallback = ptr("codex"), ptr("codex")
		}, "review.fallback in " + settingsPath + ": --fallback codex is the orchestrator itself"},
	}
	for _, c := range cases {
		_, err := ParseWith(nil, noEnv, withSettings(c.edit))
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("ParseWith = %v, want an error with %q", err, c.want)
		}
	}
}

func TestAnEmptyFocusOrBudgetInTheFileIsUnset(t *testing.T) {
	cfg := parseWith(t, nil, withSettings(func(f *config.File) { f.Review.Focus, f.Review.Budget = ptr(""), ptr("") }))
	if cfg.Focus != "" || cfg.Budget != "" {
		t.Errorf("focus %q, budget %q: want both unset", cfg.Focus, cfg.Budget)
	}
	cfg = parseWith(t, nil, withSettings(func(f *config.File) { f.Review.Focus, f.Review.Budget = ptr("the \"migration\""), ptr("2.50") }))
	if cfg.Focus != "the 'migration'" || cfg.Budget != "2.50" {
		t.Errorf("focus %q, budget %q: want the file's values, checked as the flags are", cfg.Focus, cfg.Budget)
	}
	if cfg = parseWith(t, nil, withSettings(func(f *config.File) { f.Review.Focus = ptr("tests") }), "--focus", "docs"); cfg.Focus != "docs" {
		t.Errorf("focus %q, want the flag's", cfg.Focus)
	}
}

func TestTheFilesSwitchesApplyUnlessTheFlagIsGiven(t *testing.T) {
	file := withSettings(func(f *config.File) {
		f.PRs.Approved, f.PRs.Dependabot, f.PRs.Stacked, f.PRs.SkipCI = ptr(true), ptr(true), ptr(false), ptr(true)
	})
	cfg := parseWith(t, nil, file)
	if !cfg.All || !cfg.Dependabot || cfg.Stacked || !cfg.SkipCI {
		t.Errorf("the file's switches did not apply: %+v", cfg)
	}
	cfg = parseWith(t, nil, file, "--all=false", "--stacked")
	if cfg.All || !cfg.Stacked || !cfg.Dependabot {
		t.Errorf("a flag did not win over the file's switch: %+v", cfg)
	}
}
