// Package setup asks a person for the settings they are most likely to
// change: before the first run on a terminal, and on `autoreview setup`.
//
// It asks and returns the answers. It writes nothing: the caller checks
// the answers the way a run reads them, and only then writes the file.
package setup

import (
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"

	"charm.land/huh/v2"

	"github.com/venabots/autoreview/internal/cli"
	"github.com/venabots/autoreview/internal/config"
	"github.com/venabots/autoreview/internal/panel"
	"github.com/venabots/autoreview/internal/review"
)

// ErrAborted is a form the person left with ctrl-C. Nothing is written.
var ErrAborted = errors.New("setup stopped; nothing was written")

// Terminal is where the form reads and draws. Accessible asks one line at
// a time, for a screen reader, and for a test.
type Terminal struct {
	In         io.Reader
	Out        io.Writer
	Accessible bool
}

// answers is the form's text, field by field.
type answers struct {
	orchestrator, panel, verdicts, maxPasses, rest, watch, autofixCap, jobs string
}

func answersOf(v config.Values) answers {
	return answers{
		orchestrator: v.Orchestrator,
		panel:        strings.Join(v.Panelists, " "),
		verdicts:     v.Verdicts,
		maxPasses:    strconv.Itoa(v.MaxPasses),
		rest:         v.Rest,
		watch:        v.Watch,
		autofixCap:   strconv.Itoa(v.AutofixCap),
		jobs:         strconv.Itoa(v.Jobs),
	}
}

// Ask runs the form, starting from current. path is where the answers will
// go, and the form says so first. The keys it does not ask keep the values
// of current.
func Ask(current config.Values, path string, term Terminal) (config.Values, error) {
	a := answersOf(current)
	form := huh.NewForm(
		huh.NewGroup(
			huh.NewNote().Title("autoreview settings").Description(intro(path)),
			huh.NewInput().Title("The agent that runs each review").
				Description("claude or codex, with an optional model, as in codex:gpt-5.5").
				Value(&a.orchestrator).Validate(orchestratorBeside(current.Fallback)),
			huh.NewInput().Title("The models in the review panel").
				Description("Each backend[:model], separated by spaces, as in claude:opus-4.8 codex. Blank is every backend on PATH.").
				Value(&a.panel).Validate(panelists),
			huh.NewSelect[string]().Title("The verdicts a review may post").Options(
				huh.NewOption("Approve, and request changes on a blocking finding", "approve-and-request-changes"),
				huh.NewOption("Approve only: a blocking finding is a comment review", "approve-only"),
				huh.NewOption("Comments only: never a verdict", "comment-only"),
			).Value(&a.verdicts),
		),
		huh.NewGroup(
			huh.NewInput().Title("Reviews one PR gets in a run").Value(&a.maxPasses).Validate(upTo(cli.MaxPasses)),
			huh.NewInput().Title("The rest before a reviewed PR is checked again").
				Description("A bare number is minutes; 30m, 1h and 2d also work.").Value(&a.rest).Validate(interval),
			huh.NewInput().Title("The wait between two looks at the PR list").Value(&a.watch).Validate(interval),
			huh.NewInput().Title("Fixes in a row on one of your PRs while nobody else acts").Value(&a.autofixCap).Validate(upTo(cli.MaxAutofixCap)),
			huh.NewInput().Title("Reviews that run at once").Value(&a.jobs).Validate(upTo(cli.MaxJobs)),
		),
	).WithInput(term.In).WithOutput(term.Out).WithAccessible(term.Accessible)
	if err := form.Run(); err != nil {
		if errors.Is(err, huh.ErrUserAborted) {
			return config.Values{}, ErrAborted
		}
		return config.Values{}, fmt.Errorf("the settings form: %w", err)
	}
	return withAnswers(current, a), nil
}

// intro is plain text: the note draws markdown, and would eat the marks
// of a variable's name or of a command in backticks.
func intro(path string) string {
	return "These go to " + path + ", and every run reads them. A flag or a variable still wins for one run. Run autoreview setup to change them, or edit the file."
}

// withAnswers is current with what the form asked replaced. Each answer
// passed its check, so it converts.
func withAnswers(current config.Values, a answers) config.Values {
	next := current
	next.Orchestrator = strings.TrimSpace(a.orchestrator)
	next.Panelists = strings.Fields(a.panel)
	next.Verdicts = a.verdicts
	next.MaxPasses, _ = strconv.Atoi(strings.TrimSpace(a.maxPasses))
	next.Rest = strings.TrimSpace(a.rest)
	next.Watch = strings.TrimSpace(a.watch)
	next.AutofixCap, _ = strconv.Atoi(strings.TrimSpace(a.autofixCap))
	next.Jobs, _ = strconv.Atoi(strings.TrimSpace(a.jobs))
	return next
}

// orchestratorBeside checks the agent, and refuses the one the settings
// already name as its fallback: the run would refuse that pair, and the
// answers would be lost after the form.
func orchestratorBeside(fallback string) func(string) error {
	return func(raw string) error {
		o, err := review.ParseOrchestrator(strings.TrimSpace(raw))
		if err != nil {
			return err
		}
		if f, err := review.ParseFallback(fallback); err == nil && f.Kind == review.FallbackSpec && f.Spec == o {
			return fmt.Errorf("%s is the fallback in the settings file too; choose another agent, or change review.fallback in the file", o.Label())
		}
		return nil
	}
}

// panelists checks each spec, and refuses a panel of one: auto-review
// approves only when several models reviewed, so one would never approve.
func panelists(raw string) error {
	specs := strings.Fields(raw)
	for _, spec := range specs {
		if _, err := panel.ParseSpec(spec, "panelist"); err != nil {
			return err
		}
	}
	if len(specs) == 1 {
		return errors.New("name 2 or more, or none: a panel of one never approves")
	}
	return nil
}

// upTo is the check of a count from 1 to most, the limit the run itself
// uses.
func upTo(most int) func(string) error {
	return func(raw string) error {
		if n, err := strconv.Atoi(strings.TrimSpace(raw)); err != nil || n < 1 || n > most {
			return fmt.Errorf("a whole number from 1 to %d", most)
		}
		return nil
	}
}

func interval(raw string) error {
	_, err := cli.ParseInterval(strings.TrimSpace(raw), "wait")
	return err
}
