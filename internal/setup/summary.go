package setup

import (
	"fmt"
	"strings"

	"charm.land/huh/v2"
)

// choice is what the person does with the answers on the last step.
type choice int

const (
	choiceSave choice = iota
	choiceBack
	choiceLeave
)

// summaryForm is the last step: every answer, and what to do with them.
// The answers are written into the note when the page is made, so the
// accessible form, which never draws a note again, prints them too.
func summaryForm(a answers, final []string, term Terminal, next *choice) *huh.Form {
	return huh.NewForm(huh.NewGroup(
		huh.NewNote().
			Title(fmt.Sprintf("Step %d of %d: Check your answers", questions+1, questions+1)).
			Description(noteText(summaryText(a, final), term.Accessible)),
		huh.NewSelect[choice]().Title("Save these settings?").Options(
			huh.NewOption("Save them", choiceSave),
			huh.NewOption("Go back and change an answer", choiceBack),
			huh.NewOption("Leave without saving", choiceLeave),
		).Value(next),
	))
}

// summaryText is each answer under the name of its question.
func summaryText(a answers, final []string) string {
	panel := "every backend on your PATH"
	if len(final) > 0 {
		panel = strings.Join(final, " ")
	}
	lines := [][2]string{
		{titleOrchestrator, a.orchestrator},
		{titlePanel, panel},
		{titleVerdicts, verdictWords(a.verdicts)},
		{titleMaxPasses, a.maxPasses},
		{titleRest, a.rest},
		{titleWatch, a.watch},
		{titleAutofix, yesNo(a.autofix)},
		{titleAutofixCap, a.autofixCap},
		{titleJobs, a.jobs},
	}
	out := make([]string, len(lines))
	for i, l := range lines {
		out[i] = l[0] + ": " + strings.TrimSpace(l[1])
	}
	return strings.Join(out, "\n")
}
