package setup

import (
	"fmt"
	"regexp"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/huh/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/venabots/autoreview/internal/cli"
	"github.com/venabots/autoreview/internal/config"
)

// odd are defaults that no field shows by chance, so a test that finds one
// in the text found the default and not a value that happens to match it.
func odd() config.Values {
	return config.Values{
		Orchestrator: "codex", Jobs: 7, Verdicts: "approve-only",
		Watch: "4m", Rest: "45m", MaxPasses: 9, Autofix: false, AutofixCap: 6,
	}
}

func explained(t *testing.T, s Start) map[string]string {
	t.Helper()
	a := answersOf(s.Current)
	out := map[string]string{}
	for _, p := range questionPages(s, &a) {
		out[p.title] = p.explain
	}
	return out
}

// drawn is the page the form shows, with the escapes stripped. A group
// lays out its fields only on an update, so the form gets a size first.
func drawn(form *huh.Form) string {
	form.Update(tea.WindowSizeMsg{Width: 200, Height: 60})
	return ansi.Strip(form.View())
}

// The full-screen form shows one page at a time. Each page is one question
// under its step, so enter on a page leaves no other question behind it.
func TestEachPageOfTheFormAsksOneQuestion(t *testing.T) {
	s := start()
	a := answersOf(s.Current)
	pages := questionPages(s, &a)
	if len(pages) != questions {
		t.Fatalf("%d pages, want %d", len(pages), questions)
	}
	form := questionForm(s, &a, Terminal{})
	form.Init()
	stepAt := regexp.MustCompile(`Step (\d+) of (\d+)`)
	for i, p := range pages {
		view := drawn(form)
		steps := stepAt.FindAllStringSubmatch(view, -1)
		if len(steps) != 1 || steps[0][1] != fmt.Sprint(i+1) || steps[0][2] != fmt.Sprint(questions+1) {
			t.Fatalf("page %d shows the steps %v, want only step %d of %d:\n%s", i+1, steps, i+1, questions+1, view)
		}
		if !strings.Contains(view, p.title) {
			t.Errorf("page %d does not ask %q:\n%s", i+1, p.title, view)
		}
		for j, other := range pages {
			if j != i && strings.Contains(view, other.title) {
				t.Errorf("page %d also asks %q:\n%s", i+1, other.title, view)
			}
		}
		form.NextGroup()
	}
}

func TestTheFirstPageSaysWhereTheAnswersGoAndHowTheKeysWork(t *testing.T) {
	s := start()
	a := answersOf(s.Current)
	form := questionForm(s, &a, Terminal{})
	form.Init()
	view := strings.Join(strings.Fields(drawn(form)), " ")
	for _, want := range []string{
		"These go to /home/me/.config/autoreview/config.toml",
		"enter keeps the answer shown and goes to the next step",
		"shift+tab goes back one step",
	} {
		if !strings.Contains(view, want) {
			t.Errorf("the first page does not say %q:\n%s", want, view)
		}
	}
}

// A changed default must change the text: each page names the default it
// is handed, which the caller takes from a run with no settings at all.
func TestEachQuestionNamesItsDefault(t *testing.T) {
	s := start()
	s.Defaults = odd()
	text := explained(t, s)
	for title, want := range map[string]string{
		titleOrchestrator: "Default: codex.",
		titlePanel:        "Default: none checked, so every backend on your PATH reviews.",
		titleVerdicts:     "Default: approve only.",
		titleMaxPasses:    "Default: 9.",
		titleRest:         "Default: 45m.",
		titleWatch:        "Default: 4m.",
		titleAutofix:      "Default: no.",
		titleAutofixCap:   "Default: 6.",
		titleJobs:         "Default: 7.",
	} {
		if !strings.Contains(text[title], want) {
			t.Errorf("%q says %q, want it to say %q", title, text[title], want)
		}
	}
	if !strings.Contains(text[titleAdded], "Blank adds none") {
		t.Errorf("%q says %q, want it to say that blank adds none", titleAdded, text[titleAdded])
	}
}

func TestAQuestionSaysWhenAVariableOfTheShellWinsOverIt(t *testing.T) {
	for _, c := range []struct {
		variable, title string
	}{
		{cli.EnvOrchestrator, titleOrchestrator},
		{cli.EnvPanelists, titlePanel},
		{cli.EnvMaxPasses, titleMaxPasses},
		{cli.EnvBabysit, titleRest},
		{cli.EnvWatch, titleWatch},
		{cli.EnvJobs, titleJobs},
	} {
		s := start()
		s.Getenv = shell(map[string]string{c.variable: "3"})
		note := "Your shell sets $" + c.variable + ", which wins over this answer. Remove it from your shell after you save."
		for title, text := range explained(t, s) {
			if says := strings.Contains(text, note); says != (title == c.title) {
				t.Errorf("with $%s set, %q says the shell wins: %v", c.variable, title, says)
			}
		}
	}
	for title, text := range explained(t, start()) {
		if strings.Contains(text, "Your shell sets") {
			t.Errorf("with no variable set, %q says the shell wins: %q", title, text)
		}
	}
}

// The full-screen note reads _ and * as marks, which would eat the
// underscores of a variable's name. The accessible form prints the text
// as it is, so it gets it as it is.
func TestANoteKeepsTheMarksOfAVariableOnTheFullScreenOnly(t *testing.T) {
	text := "$PANEL_REVIEW_PANELISTS *x* `y` a\\b"
	if got, want := noteText(text, false), "$PANEL\\_REVIEW\\_PANELISTS \\*x\\* \\`y\\` a\\\\b"; got != want {
		t.Errorf("full screen: %q, want %q", got, want)
	}
	if got := noteText(text, true); got != text {
		t.Errorf("accessible: %q, want it unchanged", got)
	}
}
