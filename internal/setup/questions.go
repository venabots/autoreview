package setup

import (
	"fmt"
	"slices"
	"strings"

	"charm.land/huh/v2"

	"github.com/venabots/autoreview/internal/cli"
)

// The questions, in the order the form asks them. Each title is also the
// summary's name for the answer.
const (
	titleOrchestrator = "The agent that runs each review"
	titlePanel        = "The models in the review panel"
	titleAdded        = "More models for the panel"
	titleVerdicts     = "The verdicts a review may post"
	titleMaxPasses    = "Reviews one PR gets in a run"
	titleRest         = "The rest before a reviewed PR is checked again"
	titleWatch        = "The wait between two looks at the PR list"
	titleAutofix      = "Autofix your PRs when the view opens"
	titleAutofixCap   = "Fixes in a row on one of your PRs while nobody else acts"
	titleJobs         = "Reviews that run at once"
)

// questions is how many pages ask something. The summary is one more
// step.
const questions = 10

// page is one step of the form: one question, what its answer changes,
// and the field that takes it. Two questions on one screen is what made
// enter read as "skip": it moved to a question the person had not read.
type page struct {
	title   string
	explain string
	field   huh.Field
}

// questionForm is the questions, one to a page. The first page also says
// where the answers go and how the keys work.
func questionForm(s Start, a *answers, term Terminal) *huh.Form {
	pages := questionPages(s, a)
	groups := make([]*huh.Group, len(pages))
	for i, p := range pages {
		explain := p.explain
		if p.title == titlePanel && !term.Accessible {
			// The accessible checklist says itself how to choose.
			explain = sentences(explain, "Space or x checks or unchecks a model.")
		}
		step := huh.NewNote().
			Title(fmt.Sprintf("Step %d of %d: %s", i+1, questions+1, p.title)).
			Description(noteText(explain, term.Accessible))
		fields := []huh.Field{step, p.field}
		if i == 0 {
			fields = slices.Insert(fields, 0, huh.Field(huh.NewNote().Title("autoreview settings").
				Description(noteText(intro(s.Path, term.Accessible), term.Accessible))))
		}
		groups[i] = huh.NewGroup(fields...)
	}
	return huh.NewForm(groups...)
}

func questionPages(s Start, a *answers) []page {
	d := s.Defaults
	return []page{
		{titleOrchestrator, sentences(
			"The agent reads the PR, seats the review panel, and posts the review.",
			"Write claude or codex, with an optional model, as in codex:gpt-5.5.",
			defaultIs(d.Orchestrator), shellWins(s, cli.EnvOrchestrator)),
			huh.NewInput().Title("Agent").Value(&a.orchestrator).
				Validate(orchestratorBeside(s.Current.Fallback, unchosen(s.Current)))},
		{titlePanel, panelExplained(s), checklist(s, a)},
		{titleAdded, sentences(
			"Write each model that the list does not have as backend:model, separated by spaces, as in opencode:zai/glm-5.3.",
			"The backend is codex, claude or opencode. Blank adds none."),
			huh.NewInput().Title("More models").Value(&a.added).Validate(addedBeside(&a.picked))},
		{titleVerdicts, sentences(
			"What a review may do on GitHub. A review that approves lets the PR merge, and a request for changes blocks it until a later review approves.",
			defaultIs(verdictWords(d.Verdicts))),
			huh.NewSelect[string]().Title("Verdicts").Options(
				huh.NewOption("Approve, and request changes on a blocking finding", "approve-and-request-changes"),
				huh.NewOption("Approve only: a blocking finding is a comment review", "approve-only"),
				huh.NewOption("Comments only: never a verdict", "comment-only"),
			).Value(&a.verdicts)},
		{titleMaxPasses, sentences(
			"After this many reviews of one PR, the run leaves that PR alone. While the run watches, a new push starts the count again.",
			defaultIs(fmt.Sprint(d.MaxPasses)), shellWins(s, cli.EnvMaxPasses)),
			huh.NewInput().Title("Reviews per PR").Value(&a.maxPasses).Validate(upTo(cli.MaxPasses))},
		{titleRest, sentences(
			"After a review, the PR waits this long before the run may review it again.",
			"A bare number is minutes; 30m, 1h and 2d also work.",
			defaultIs(d.Rest), shellWins(s, cli.EnvBabysit)),
			huh.NewInput().Title("Rest").Value(&a.rest).Validate(interval)},
		{titleWatch, sentences(
			"The run reads the PR list again after this wait, to find new work. A shorter wait finds work sooner, and asks GitHub more often.",
			"A bare number is minutes.",
			defaultIs(d.Watch), shellWins(s, cli.EnvWatch)),
			huh.NewInput().Title("Wait").Value(&a.watch).Validate(interval)},
		{titleAutofix, sentences(
			"Yes starts the view with autofix on for all your PRs: a PR is fixed again each time it changes and needs work.",
			"No starts it off, and A in the view turns it on. A run with --once always starts it off.",
			defaultIs(yesNo(d.Autofix))),
			huh.NewConfirm().Title("Autofix").Affirmative("Yes").Negative("No").Value(&a.autofix)},
		{titleAutofixCap, sentences(
			"Autofix stops after this many fixes on one PR, until a person acts on the PR or it comes clean. This stops a fix that does not work from running again and again.",
			defaultIs(fmt.Sprint(d.AutofixCap))),
			huh.NewInput().Title("Fixes in a row").Value(&a.autofixCap).Validate(upTo(cli.MaxAutofixCap))},
		{titleJobs, sentences(
			"Each review seats every model in the panel, and one more agent runs it. So 2 reviews with a panel of 3 are about 8 agents at once.",
			"More reviews at once clear the list sooner, and spend more.",
			defaultIs(fmt.Sprint(d.Jobs)), shellWins(s, cli.EnvJobs)),
			huh.NewInput().Title("Reviews at once").Value(&a.jobs).Validate(upTo(cli.MaxJobs))},
	}
}

// panelExplained says what the panel does, why the list starts checked
// as it does, and the default.
func panelExplained(s Start) string {
	_, from := prefilledPanel(s)
	checked := map[source]string{
		fromFile:  "The panel in your settings file is checked.",
		fromShell: "The panel from $" + cli.EnvPanelists + " in your shell is checked. Keep it, and the settings file carries it.",
		fromPath:  "Each backend on your PATH is checked, on its default model. Kept as it is, nothing is written, so a backend you install later also joins.",
		nobody:    "Nothing is checked.",
	}[from]
	if len(panelOptions(s)) == 0 {
		checked = "This machine has no model to offer here. Add them on the next step, or leave both blank."
	}
	def := "none checked, so every backend on your PATH reviews"
	if len(s.Defaults.Panelists) > 0 {
		def = strings.Join(s.Defaults.Panelists, " ")
	}
	return sentences(
		"Each review asks every model in the panel on its own. A review approves only when 75% or more of the panel answers, so a panel of 4 can lose one model and still approve.",
		"Check 2 or more, or none.",
		checked, defaultIs(def), shellWins(s, cli.EnvPanelists))
}

// checklist is the panel's models, checked as the answers have them, so
// a second round shows what the first one chose.
func checklist(s Start, a *answers) *huh.MultiSelect[string] {
	options := panelOptions(s)
	choices := make([]huh.Option[string], len(options))
	for i, o := range options {
		choices[i] = huh.NewOption(o.label, o.spec).Selected(slices.Contains(a.picked, o.spec))
	}
	return huh.NewMultiSelect[string]().Title("Models").Options(choices...).Value(&a.picked).
		Height(min(len(choices), maxShown) + 1)
}

// maxShown is the most models the checklist shows at once. The list
// scrolls past it. Below it, every model shows: the default height cut
// off the last one, which then looked as if it was not offered.
const maxShown = 12
