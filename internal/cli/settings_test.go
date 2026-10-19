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

func TestTheVerdictLevelComesFromTheFileAndRefusesAnOverride(t *testing.T) {
	if cfg := parse(t); cfg.Verdicts != review.VerdictsAll {
		t.Errorf("the default level is %v", cfg.Verdicts)
	}
	commentOnly := withSettings(func(f *config.File) { f.Review.Verdicts = ptr("comment-only") })
	if cfg := parseWith(t, nil, commentOnly); cfg.Verdicts != review.VerdictsCommentOnly {
		t.Errorf("the level is %v, want the file's comment-only", cfg.Verdicts)
	}
	_, err := ParseWith(nil, noEnv, withSettings(func(f *config.File) { f.Review.Verdicts = ptr("never") }))
	if want := "review.verdicts in " + settingsPath + ": the verdicts expects approve-and-request-changes"; err == nil || !strings.Contains(err.Error(), want) {
		t.Errorf("a bad level: %v, want %q", err, want)
	}
	_, err = ParseWith(nil, envOf(map[string]string{EnvOverride: "my-review {}"}), commentOnly)
	if err == nil || !strings.Contains(err.Error(), "verdicts comment-only cannot be used with $AUTOREVIEW_CMD") {
		t.Errorf("a level with an override: %v", err)
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

// An empty value is a value the person wrote. Only "no budget" and "no
// focus" mean something when empty; anywhere else the run would quietly
// take the default for a key that names another choice.
func TestAnEmptyValueInTheFileIsAnErrorWhereItMeansNothing(t *testing.T) {
	cases := []struct {
		edit func(*config.File)
		key  string
	}{
		{func(f *config.File) { f.Review.Verdicts = ptr("") }, "review.verdicts"},
		{func(f *config.File) { f.Review.Orchestrator = ptr("") }, "review.orchestrator"},
		{func(f *config.File) { f.Loop.Watch = ptr("") }, "loop.watch"},
		{func(f *config.File) { f.Panel.Synth = ptr("") }, "panel.synth"},
	}
	for _, c := range cases {
		_, err := ParseWith(nil, noEnv, withSettings(c.edit))
		if err == nil || !strings.Contains(err.Error(), c.key+" in "+settingsPath) {
			t.Errorf("an empty %s: %v, want an error that names it", c.key, err)
		}
	}
}

// A bad value stops the run, as the README says, whether or not a flag or
// a variable replaces it in this run. A file that is broken is found the
// first time it is read, not the first time nobody overrides it.
func TestTheWholeFileIsCheckedWhereAFlagOrAVariableWins(t *testing.T) {
	cases := []struct {
		env  map[string]string
		args []string
		edit func(*config.File)
		key  string
	}{
		{nil, []string{"-j", "4"}, func(f *config.File) { f.Review.Jobs = ptr(0) }, "review.jobs"},
		{map[string]string{EnvPanelists: "codex claude"}, nil, func(f *config.File) { f.Panel.Panelists = []string{"gemini"} }, "panel.panelists"},
		{nil, nil, func(f *config.File) { f.Panel.Synth = ptr("gemini") }, "panel.synth"},
	}
	for _, c := range cases {
		_, err := ParseWith(c.args, envOf(c.env), withSettings(c.edit))
		if err == nil || !strings.Contains(err.Error(), c.key+" in "+settingsPath) {
			t.Errorf("%s with %q: %v, want an error that names it", c.key, c.args, err)
		}
	}
	for _, flag := range []string{"--help", "--version"} {
		if _, err := ParseWith([]string{flag}, noEnv, withSettings(func(f *config.File) { f.Review.Jobs = ptr(0) })); err != nil {
			t.Errorf("%s with a bad value in the file: %v", flag, err)
		}
	}
}

// The pair of orchestrator and fallback is checked on the two that this run
// uses. Checked against the defaults, a fallback of claude in the file would
// clash with the default orchestrator, though this run's variable moves it
// to codex.
func TestTheFallbackIsCheckedAgainstTheOrchestratorThisRunUses(t *testing.T) {
	file := withSettings(func(f *config.File) { f.Review.Fallback = ptr("claude") })
	if _, err := ParseWith(nil, envOf(map[string]string{EnvOrchestrator: "codex"}), file); err != nil {
		t.Errorf("codex from the variable with claude as the file's fallback: %v", err)
	}
}

// Each value in the file is checked by itself, with no variable: a run
// whose variable replaces a bad key still stops, and the next run without
// the variable cannot be the first to find it.
func TestABadFileValueStopsTheRunWhenAVariableReplacesIt(t *testing.T) {
	_, err := ParseWith(nil, envOf(map[string]string{EnvJobs: "4"}), withSettings(func(f *config.File) { f.Review.Jobs = ptr(0) }))
	if err == nil || !strings.Contains(err.Error(), "review.jobs in "+settingsPath) {
		t.Errorf("review.jobs = 0 under AUTOREVIEW_JOBS=4: %v, want an error that names the key", err)
	}
}

// The file's check reads no variable. A bad variable that a flag replaces
// passes, as it did before the file existed.
func TestTheFilesCheckDoesNotReadAVariableThatAFlagReplaces(t *testing.T) {
	file := withSettings(func(*config.File) {})
	if _, err := ParseWith([]string{"--jobs=4"}, envOf(map[string]string{EnvJobs: "0"}), file); err != nil {
		t.Errorf("AUTOREVIEW_JOBS=0 with --jobs=4: %v", err)
	}
}

func TestCheckFileTakesAFileThatOnlyTheDefaultsMakeClash(t *testing.T) {
	if err := CheckFile(withSettings(func(f *config.File) { f.Review.Fallback = ptr("claude") })); err != nil {
		t.Errorf("fallback claude with no orchestrator in the file: %v", err)
	}
	err := CheckFile(withSettings(func(f *config.File) { f.Review.Fallback = ptr("gemini") }))
	if err == nil || !strings.Contains(err.Error(), "review.fallback in "+settingsPath) {
		t.Errorf("a bad fallback: %v, want an error that names it", err)
	}
}

func TestTheFileCanStartTheViewWithAutofixOff(t *testing.T) {
	if cfg := parse(t); cfg.NoAutofix {
		t.Error("with no file, autofix is off at the start")
	}
	if cfg := parseWith(t, nil, withSettings(func(f *config.File) { f.Mine.Autofix = ptr(false) })); !cfg.NoAutofix {
		t.Error("mine.autofix = false left autofix on at the start")
	}
	if cfg := parseWith(t, nil, withSettings(func(f *config.File) { f.Mine.Autofix = ptr(true) })); cfg.NoAutofix {
		t.Error("mine.autofix = true turned autofix off at the start")
	}
}
