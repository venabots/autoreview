package term

import (
	"runtime"
	"strings"
	"testing"
)

func TestDetectPicksTheTerminalInV1Order(t *testing.T) {
	everything := map[string]string{"HERDR_ENV": "1", "CMUX_SURFACE_ID": "surface:1", "TERM_PROGRAM": "ghostty"}
	cases := []struct {
		name     string
		goos     string
		programs []string
		vars     map[string]string
		want     Kind
	}{
		{"Herdr wins when all three are there", "darwin", []string{"herdr", "cmux"}, everything, Herdr},
		{"Herdr with no herdr on PATH falls through to cmux", "darwin", []string{"cmux"}, everything, Cmux},
		{"with neither program Ghostty is still found", "darwin", nil, everything, Ghostty},
		{"HERDR_ENV must be exactly 1", "darwin", []string{"herdr"}, map[string]string{"HERDR_ENV": "0"}, None},
		{"a herdr on PATH with no HERDR_ENV is not Herdr", "darwin", []string{"herdr"}, nil, None},
		{"cmux needs its variable", "darwin", []string{"cmux"}, nil, None},
		{"cmux needs its program", "darwin", nil, map[string]string{"CMUX_SURFACE_ID": "surface:1"}, None},
		{"cmux is found on Linux too", "linux", []string{"cmux"}, map[string]string{"CMUX_SURFACE_ID": "surface:1"}, Cmux},
		{"a variable set to nothing is not set", "darwin", []string{"cmux"}, map[string]string{"CMUX_SURFACE_ID": ""}, None},
		{"Ghostty is macOS only", "linux", nil, map[string]string{"TERM_PROGRAM": "ghostty"}, None},
		{"another terminal is none", "darwin", []string{"herdr", "cmux"}, map[string]string{"TERM_PROGRAM": "Apple_Terminal"}, None},
		{"an empty environment is none", "darwin", nil, nil, None},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := Detect(envOf(c.goos, c.programs, c.vars)); got != c.want {
				t.Errorf("Detect = %q, want %q", got.Name(), c.want.Name())
			}
		})
	}
}

func TestAnEnvWithNoFunctionsDetectsNothing(t *testing.T) {
	if got := Detect(Env{GOOS: "darwin"}); got != None {
		t.Errorf("Detect = %q, want none", got.Name())
	}
	onlyGet := Env{Get: func(string) string { return "1" }}
	if got := Detect(onlyGet); got != None {
		t.Errorf("Detect with no LookPath = %q, want none", got.Name())
	}
}

func TestATerminalIsNamedAsAMessageWritesIt(t *testing.T) {
	cases := []struct {
		kind Kind
		want string
	}{
		{None, ""},
		{Herdr, "Herdr"},
		{Cmux, "cmux"},
		{Ghostty, "Ghostty"},
		{Kind(99), ""},
	}
	for _, c := range cases {
		if got := c.kind.Name(); got != c.want {
			t.Errorf("Kind(%d).Name() = %q, want %q", c.kind, got, c.want)
		}
	}
}

func TestOSEnvReadsThisProcess(t *testing.T) {
	t.Setenv("AUTOREVIEW_TERM_TEST", "here")
	env := OSEnv()
	if got := env.Get("AUTOREVIEW_TERM_TEST"); got != "here" {
		t.Errorf("Get = %q, want the variable this test set", got)
	}
	if _, err := env.LookPath("sh"); err != nil {
		t.Errorf("LookPath(sh): %v", err)
	}
	if env.GOOS != runtime.GOOS {
		t.Errorf("GOOS = %q, want %q", env.GOOS, runtime.GOOS)
	}
}

func TestWithNoTerminalOpenRunsNothingAndSaysWhy(t *testing.T) {
	cases := []struct {
		name string
		env  Env
		want []string
	}{
		{
			"the three terminals are named with what was found",
			envOf("darwin", []string{"herdr", "cmux"}, map[string]string{"TERM_PROGRAM": "Apple_Terminal"}),
			[]string{
				"no terminal that can open a tab",
				"Herdr, cmux, or Ghostty on macOS",
				"HERDR_ENV=<unset> CMUX_SURFACE_ID=<unset> TERM_PROGRAM=Apple_Terminal",
			},
		},
		{
			"a variable with no program behind it is shown",
			envOf("linux", nil, map[string]string{"HERDR_ENV": "1", "CMUX_SURFACE_ID": "surface:4"}),
			[]string{"Herdr, cmux, or Ghostty on macOS", "HERDR_ENV=1 CMUX_SURFACE_ID=surface:4 TERM_PROGRAM=<unset>"},
		},
		{
			"Ghostty off macOS is told that it is macOS only",
			envOf("linux", nil, map[string]string{"TERM_PROGRAM": "ghostty"}),
			[]string{"no terminal that can open a tab", "Ghostty opens one on macOS only (AppleScript)", "Herdr", "cmux"},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rec := answering()
			kind, err := open(t, rec.run, c.env, "PR 9 Resume", "cd /src && claude --resume x")
			if kind != None || err == nil {
				t.Fatalf("Open = %q, %v; want none and an error", kind.Name(), err)
			}
			for _, want := range c.want {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("error = %q, want it to hold %q", err, want)
				}
			}
			if strings.Contains(err.Error(), "\n") {
				t.Errorf("error = %q, want one line", err)
			}
			if len(rec.calls) != 0 {
				t.Errorf("Open ran %v with no terminal", rec.steps())
			}
		})
	}
}
