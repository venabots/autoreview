package setup

import (
	"errors"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/venabots/autoreview/internal/cli"
)

func onPath(names ...string) func(string) (string, error) {
	return func(name string) (string, error) {
		if slices.Contains(names, name) {
			return "/usr/bin/" + name, nil
		}
		return "", errors.New("not found")
	}
}

func shell(vars map[string]string) func(string) string {
	return func(name string) string { return vars[name] }
}

func specsOf(options []panelOption) []string {
	out := make([]string, len(options))
	for i, o := range options {
		out[i] = o.spec
	}
	return out
}

func checkedOf(options []panelOption) []string {
	var out []string
	for _, o := range options {
		if o.checked {
			out = append(out, o.spec)
		}
	}
	return out
}

func TestTheChecklistOffersTheFileThenTheShellThenTheHistoryThenThePathWithNoModelTwice(t *testing.T) {
	s := Start{
		Current: withPanel(current(), "claude:opus", "codex:gpt-5.6-sol"),
		Getenv:  shell(map[string]string{cli.EnvPanelists: "codex:gpt-5.6-sol opencode:zai/glm-5.3"}),
		Seen: []Seen{
			{Spec: "codex:gpt-5.6-sol", Launched: 41, Answered: 39},
			{Spec: "opencode:grok-4", Launched: 12, Answered: 12},
			{Spec: "gemini:pro", Launched: 3, Answered: 3},
		},
		LookPath: onPath("codex", "claude"),
	}
	want := []string{
		"claude:opus", "codex:gpt-5.6-sol", // the file
		"opencode:zai/glm-5.3", // the shell, less what the file had
		"opencode:grok-4",      // the history; gemini is no backend, so it is left out
		"codex", "claude",      // the PATH, in panel.Backends order
		"claude:sonnet", // the stable alias the PATH did not already give
	}
	if got := specsOf(panelOptions(s)); !reflect.DeepEqual(got, want) {
		t.Errorf("options = %q, want %q", got, want)
	}
}

func TestThePanelStartsCheckedFromTheFileElseTheShellElseThePath(t *testing.T) {
	for _, c := range []struct {
		name string
		s    Start
		want []string
		from source
	}{
		{"the file wins", Start{
			Current: withPanel(current(), "claude:opus", "codex"),
			Getenv:  shell(map[string]string{cli.EnvPanelists: "codex opencode"}), LookPath: onPath("codex", "claude", "opencode"),
		}, []string{"claude:opus", "codex"}, fromFile},
		{"then the shell", Start{
			Current: current(), Getenv: shell(map[string]string{cli.EnvPanelists: " codex:gpt-5.6-sol  opencode:zai/glm-5.3 "}),
			LookPath: onPath("codex", "claude"),
		}, []string{"codex:gpt-5.6-sol", "opencode:zai/glm-5.3"}, fromShell},
		{"then every backend on the PATH", Start{Current: current(), LookPath: onPath("opencode", "codex")}, []string{"codex", "opencode"}, fromPath},
		// One backend alone is a panel of one, which never approves. Blank
		// seats it all the same, so nothing is checked.
		{"but not a PATH of one", Start{Current: current(), LookPath: onPath("claude")}, nil, nobody},
		{"and nothing when nothing is found", Start{Current: current()}, nil, nobody},
	} {
		t.Run(c.name, func(t *testing.T) {
			got, from := prefilledPanel(c.s)
			if !reflect.DeepEqual(got, c.want) || from != c.from {
				t.Errorf("prefilledPanel = %q from %v, want %q from %v", got, from, c.want, c.from)
			}
			if checked := checkedOf(panelOptions(c.s)); !reflect.DeepEqual(checked, c.want) {
				t.Errorf("checked = %q, want %q", checked, c.want)
			}
		})
	}
}

func TestAnOptionSaysWhereItCameFromAndHowTheModelHasDone(t *testing.T) {
	s := Start{
		Current: withPanel(current(), "codex:gpt-5.6-sol", "claude:opus"),
		Getenv:  shell(map[string]string{cli.EnvPanelists: "opencode:zai/glm-5.3"}),
		Seen: []Seen{
			{Spec: "codex:gpt-5.6-sol", Launched: 41, Answered: 39},
			{Spec: "opencode:grok-4", Launched: 12, Answered: 12},
		},
		LookPath: onPath("codex"),
	}
	labels := map[string]string{}
	for _, o := range panelOptions(s) {
		labels[o.spec] = o.label
	}
	for spec, want := range map[string]string{
		"codex:gpt-5.6-sol":    "codex:gpt-5.6-sol · in your settings · 41 reviews, 2 failed",
		"claude:opus":          "claude:opus · in your settings",
		"opencode:zai/glm-5.3": "opencode:zai/glm-5.3 · from $PANEL_REVIEW_PANELISTS",
		"opencode:grok-4":      "opencode:grok-4 · 12 reviews",
		"codex":                "codex · its default model",
	} {
		if labels[spec] != want {
			t.Errorf("the label of %s is %q, want %q", spec, labels[spec], want)
		}
	}
}

func TestTheAddedModelsJoinTheCheckedOnesOnceEach(t *testing.T) {
	got := finalPanel([]string{"claude:opus", "codex"}, "  codex opencode:zai/glm-5.3  claude:opus ")
	want := []string{"claude:opus", "codex", "opencode:zai/glm-5.3"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("finalPanel = %q, want %q", got, want)
	}
	if got := finalPanel(nil, ""); got != nil {
		t.Errorf("an empty panel is %q, want nil: blank seats every backend on PATH", got)
	}
}

func TestTheAddedModelsAreCheckedWithTheCheckedOnes(t *testing.T) {
	for _, c := range []struct {
		picked []string
		added  string
		bad    string
	}{
		{nil, "", ""},
		{[]string{"claude", "codex"}, "", ""},
		{[]string{"claude"}, "codex:gpt-5.5", ""},
		{nil, "claude codex", ""},
		{[]string{"claude"}, "", "a panel of one never approves"},
		{nil, "codex", "a panel of one never approves"},
		// The same model twice is still one reviewer.
		{[]string{"codex"}, "codex", "a panel of one never approves"},
		{[]string{"claude"}, "gemini", "unknown panelist backend"},
		{[]string{"claude"}, "codex:", "names no model"},
	} {
		picked := c.picked
		err := addedBeside(&picked)(c.added)
		switch {
		case c.bad == "" && err != nil:
			t.Errorf("%q + %q refused: %v", c.picked, c.added, err)
		case c.bad != "" && (err == nil || !strings.Contains(err.Error(), c.bad)):
			t.Errorf("%q + %q gave %v, want %q", c.picked, c.added, err, c.bad)
		}
	}
}

// A panel from the shell is the person's choice, and the file carries it,
// so the variable can go. A panel from the PATH is only what blank already
// seats: written down, it would leave out a backend installed later.
func TestAPanelFromTheShellIsWrittenAndOneFromThePathIsNot(t *testing.T) {
	for _, c := range []struct {
		name      string
		prefilled []string
		from      source
		final     []string
		want      []string
	}{
		{"from the shell, kept", []string{"codex", "opencode"}, fromShell, []string{"codex", "opencode"}, []string{"codex", "opencode"}},
		{"from the PATH, kept", []string{"codex", "claude"}, fromPath, []string{"codex", "claude"}, nil},
		{"from the PATH, changed", []string{"codex", "claude"}, fromPath, []string{"codex", "claude:opus"}, []string{"codex", "claude:opus"}},
		{"from the file, kept", []string{"claude", "codex"}, fromFile, []string{"claude", "codex"}, []string{"claude", "codex"}},
		{"emptied", []string{"claude", "codex"}, fromFile, nil, nil},
	} {
		t.Run(c.name, func(t *testing.T) {
			if got := panelToWrite(c.final, c.prefilled, c.from); !reflect.DeepEqual(got, c.want) {
				t.Errorf("panelToWrite = %q, want %q", got, c.want)
			}
		})
	}
}
