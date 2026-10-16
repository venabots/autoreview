package panel

import (
	"reflect"
	"testing"
)

func noEnv(string) string { return "" }

func mustParse(t *testing.T, args ...string) Config {
	t.Helper()
	p, err := Parse(args, noEnv)
	if err != nil {
		t.Fatalf("Parse(%q): %v", args, err)
	}
	if p.Help || p.Version {
		t.Fatalf("Parse(%q) asked for help or the version", args)
	}
	return p.Config
}

func TestTheDefaultsAreV1s(t *testing.T) {
	c := mustParse(t)
	want := Config{Target: Target{Kind: Uncommitted}, TimeoutSecs: 600, Synthesize: true, Synth: Spec{Backend: "claude"}}
	if !reflect.DeepEqual(c, want) {
		t.Errorf("defaults %+v, want %+v", c, want)
	}
}

func TestTheEnvironmentSetsTheTimeoutAndTheSynthesizer(t *testing.T) {
	env := func(name string) string {
		return map[string]string{EnvTimeout: "45", EnvSynth: "codex:gpt-5"}[name]
	}
	p, err := Parse(nil, env)
	if err != nil {
		t.Fatal(err)
	}
	if p.Config.TimeoutSecs != 45 || p.Config.Synth != (Spec{Backend: "codex", Model: "gpt-5"}) {
		t.Errorf("config %+v", p.Config)
	}
	p, err = Parse([]string{"--timeout", "9", "--synth=claude"}, env)
	if err != nil || p.Config.TimeoutSecs != 9 || p.Config.Synth.Backend != "claude" {
		t.Errorf("a flag must beat the environment: %+v, %v", p.Config, err)
	}
}

func TestBothValueSpellingsWork(t *testing.T) {
	cases := []struct {
		args []string
		want func(Config) bool
	}{
		{[]string{"--base", "main"}, func(c Config) bool { return c.Target == Target{Kind: Base, Ref: "main"} }},
		{[]string{"--base=origin/main"}, func(c Config) bool { return c.Target == Target{Kind: Base, Ref: "origin/main"} }},
		{[]string{"--timeout=30"}, func(c Config) bool { return c.TimeoutSecs == 30 }},
		{[]string{"--focus=auth"}, func(c Config) bool { return c.Focus == "auth" }},
		{[]string{"--focus", "the auth path"}, func(c Config) bool { return c.Focus == "the auth path" }},
		{[]string{"--log-dir", "/tmp/x"}, func(c Config) bool { return c.LogDir == "/tmp/x" }},
		{[]string{"--staged"}, func(c Config) bool { return c.Target.Kind == Staged }},
		{[]string{"--staged", "--uncommitted"}, func(c Config) bool { return c.Target.Kind == Uncommitted }},
		{[]string{"--no-synthesis"}, func(c Config) bool { return !c.Synthesize }},
	}
	for _, c := range cases {
		if got := mustParse(t, c.args...); !c.want(got) {
			t.Errorf("%q gave %+v", c.args, got)
		}
	}
}

func TestATreeReviewTakesAnOptionalPath(t *testing.T) {
	cases := []struct {
		args []string
		want Target
	}{
		{[]string{"--tree"}, Target{Kind: Tree}},
		{[]string{"--tree", "src"}, Target{Kind: Tree, Path: "src"}},
		{[]string{"--tree=src/panel"}, Target{Kind: Tree, Path: "src/panel"}},
	}
	for _, c := range cases {
		if got := mustParse(t, c.args...).Target; got != c.want {
			t.Errorf("%q: target %+v, want %+v", c.args, got, c.want)
		}
	}
	// A flag after it is a flag, not the path.
	c := mustParse(t, "--tree", "--focus", "auth")
	if c.Target != (Target{Kind: Tree}) || c.Focus != "auth" {
		t.Errorf("--tree --focus auth: %+v", c)
	}
	if _, err := Parse([]string{"--tree="}, noEnv); err == nil || err.Error() != "--tree expects a value" {
		t.Errorf("--tree=: %v", err)
	}
}

func TestPanelistsAccumulateInOrder(t *testing.T) {
	c := mustParse(t, "--panelist", "codex", "--panelist=claude:opus-4.8")
	want := []Spec{{Backend: "codex"}, {Backend: "claude", Model: "opus-4.8"}}
	if !reflect.DeepEqual(c.Panelists, want) {
		t.Errorf("panelists %+v, want %+v", c.Panelists, want)
	}
}

func TestTheSynthesizerIsASpecToo(t *testing.T) {
	c := mustParse(t, "--synth", "codex:gpt-5")
	if c.Synth != (Spec{Backend: "codex", Model: "gpt-5"}) {
		t.Errorf("synth %+v", c.Synth)
	}
	_, err := Parse([]string{"--synth", "gpt4"}, noEnv)
	if err == nil || err.Error() != `unknown --synth backend "gpt4" (expected one of: codex, claude, opencode)` {
		t.Errorf("--synth gpt4: %v", err)
	}
	_, err = Parse([]string{"--synth", "claude:"}, noEnv)
	if err == nil || err.Error() != `--synth "claude:" names no model after the colon` {
		t.Errorf("--synth claude: %v", err)
	}
}

func TestBadInputIsRefusedWithAReason(t *testing.T) {
	cases := []struct {
		args     []string
		want     string
		wantHelp bool
	}{
		{[]string{"--timeout", "0"}, `--timeout expects an integer >= 1, got "0"`, false},
		{[]string{"--timeout", "abc"}, `--timeout expects an integer >= 1, got "abc"`, false},
		// A value over the maximum must say so, not blame the minimum.
		{[]string{"--timeout", "9999999999"}, `--timeout expects an integer <= 999999999, got "9999999999"`, false},
		{[]string{"--timeout", "99999999999999999999999"}, `--timeout expects an integer <= 999999999, got "99999999999999999999999"`, false},
		{[]string{"--base"}, "--base expects a value", false},
		{[]string{"--focus="}, "--focus expects a value", false},
		{[]string{"--panelist", "nope"}, `unknown panelist backend "nope" (expected one of: codex, claude, opencode)`, false},
		{[]string{"--nope"}, "unknown arg: --nope", true},
		{[]string{"main"}, "unknown arg: main", true},
		// -V, not -v: lowercase is verbose in most tools.
		{[]string{"-v"}, "unknown arg: -v", true},
	}
	for _, c := range cases {
		_, err := Parse(c.args, noEnv)
		if err == nil || err.Error() != c.want || WantsHelp(err) != c.wantHelp {
			t.Errorf("%q: error %v (help %v), want %q (help %v)", c.args, err, err != nil && WantsHelp(err), c.want, c.wantHelp)
		}
	}
}

func TestABaseThatLooksLikeAFlagIsTakenAsTheValue(t *testing.T) {
	// The target check names the cause. Reported here as a missing value,
	// it would send the person looking in the wrong place.
	if got := mustParse(t, "--base", "--output=/tmp/x").Target; got != (Target{Kind: Base, Ref: "--output=/tmp/x"}) {
		t.Errorf("target %+v", got)
	}
}

func TestHelpAndVersionFlags(t *testing.T) {
	for _, arg := range []string{"--help", "-h"} {
		if p, err := Parse([]string{"--staged", arg}, noEnv); err != nil || !p.Help {
			t.Errorf("%s: %+v, %v", arg, p, err)
		}
	}
	for _, arg := range []string{"--version", "-V"} {
		if p, err := Parse([]string{arg}, noEnv); err != nil || !p.Version {
			t.Errorf("%s: %+v, %v", arg, p, err)
		}
	}
}

func TestParseDoesNotChangeTheArguments(t *testing.T) {
	args := []string{"--tree", "src", "--panelist", "codex"}
	before := append([]string(nil), args...)
	mustParse(t, args...)
	if !reflect.DeepEqual(args, before) {
		t.Errorf("args became %q", args)
	}
}

// The person's settings name a panel for every run. A flag or a variable
// given for one run wins over them.
func TestTheSettingsPanelAppliesWhereNoFlagNamesOne(t *testing.T) {
	saved := Settings{Panelists: []Spec{{Backend: "claude", Model: "opus-4.8"}, {Backend: "codex"}}, Synth: &Spec{Backend: "codex", Model: "gpt-5.5"}}

	plain := withSettings(mustParse(t), saved)
	if !reflect.DeepEqual(plain.Panelists, saved.Panelists) || plain.Synth != *saved.Synth {
		t.Errorf("with no flag the run seats %+v and synthesizes with %+v, want the settings", plain.Panelists, plain.Synth)
	}

	named := withSettings(mustParse(t, "--panelist", "opencode", "--synth", "claude"), saved)
	if !reflect.DeepEqual(named.Panelists, []Spec{{Backend: "opencode"}}) || named.Synth != (Spec{Backend: "claude"}) {
		t.Errorf("the flags did not win: %+v, %+v", named.Panelists, named.Synth)
	}

	p, err := Parse(nil, func(name string) string {
		if name == EnvSynth {
			return "opencode"
		}
		return ""
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := withSettings(p.Config, saved).Synth; got != (Spec{Backend: "opencode"}) {
		t.Errorf("$PANEL_SYNTH did not win over the settings: %+v", got)
	}
}
