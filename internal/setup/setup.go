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
	"maps"
	"slices"
	"strconv"
	"strings"

	"charm.land/huh/v2"

	"github.com/venabots/autoreview/internal/config"
)

// ErrAborted is a form the person left with ctrl-C. Nothing is written.
var ErrAborted = errors.New("setup stopped; nothing was written")

// ErrNotSaved is a form the person finished and chose not to save.
// Nothing is written.
var ErrNotSaved = errors.New("you left setup without saving; nothing was written")

// Terminal is where the form reads and draws. Accessible asks one line at
// a time, for a screen reader, and for a test.
type Terminal struct {
	In         io.Reader
	Out        io.Writer
	Accessible bool
}

// Start is what the form starts from. The shell, the PATH and the history
// arrive here rather than being read by the form, so a test decides them.
type Start struct {
	// Current is every setting as the file and the defaults give it.
	Current config.Values
	// Defaults is every setting as a run with no settings file has it:
	// what each question names as its default.
	Defaults config.Values
	// Path is where the answers will go.
	Path string
	// Getenv reads the shell, and LookPath the PATH. Nil reads nothing
	// and finds nothing.
	Getenv   func(string) string
	LookPath func(string) (string, error)
	// Seen is each model this machine has seated as a panelist.
	Seen []Seen
}

// answers is the form's text, field by field.
type answers struct {
	orchestrator string
	// picked is the models checked on the list, and added the ones
	// typed after it.
	picked []string
	added  string

	verdicts, maxPasses, rest, watch, autofixCap, jobs string
	autofix                                            bool
}

func answersOf(v config.Values) answers {
	return answers{
		orchestrator: v.Orchestrator,
		picked:       slices.Clone(v.Panelists),
		verdicts:     v.Verdicts,
		maxPasses:    strconv.Itoa(v.MaxPasses),
		rest:         v.Rest,
		watch:        v.Watch,
		autofix:      v.Autofix,
		autofixCap:   strconv.Itoa(v.AutofixCap),
		jobs:         strconv.Itoa(v.Jobs),
	}
}

// Ask runs the form, one question to a page, then shows every answer and
// asks to save them. Going back from the summary asks each question again,
// from the answers given. The keys it does not ask keep the values of
// s.Current.
func Ask(s Start, term Terminal) (config.Values, error) {
	prefilled, from := prefilledPanel(s)
	a := answersOf(s.Current)
	a.picked = slices.Clone(prefilled)
	for {
		if err := run(questionForm(s, &a, term), term); err != nil {
			return config.Values{}, err
		}
		final := finalPanel(a.picked, a.added)
		next := choiceSave
		if err := run(summaryForm(a, final, term, &next), term); err != nil {
			return config.Values{}, err
		}
		switch next {
		case choiceSave:
			return withAnswers(s.Current, a, panelToWrite(final, prefilled, from)), nil
		case choiceLeave:
			return config.Values{}, ErrNotSaved
		}
	}
}

func run(form *huh.Form, term Terminal) error {
	err := form.WithInput(term.In).WithOutput(term.Out).WithAccessible(term.Accessible).Run()
	switch {
	case errors.Is(err, huh.ErrUserAborted):
		return ErrAborted
	case err != nil:
		return fmt.Errorf("the settings form: %w", err)
	}
	return nil
}

// withAnswers is current with what the form asked replaced. Each answer
// passed its check, so it converts. panel is the panel to write.
func withAnswers(current config.Values, a answers, panel []string) config.Values {
	next := current
	next.Orchestrator = strings.TrimSpace(a.orchestrator)
	next.Panelists = panel
	next.Verdicts = a.verdicts
	next.MaxPasses, _ = strconv.Atoi(strings.TrimSpace(a.maxPasses))
	next.Rest = strings.TrimSpace(a.rest)
	next.Watch = strings.TrimSpace(a.watch)
	next.Autofix = a.autofix
	next.AutofixCap, _ = strconv.Atoi(strings.TrimSpace(a.autofixCap))
	next.Jobs, _ = strconv.Atoi(strings.TrimSpace(a.jobs))
	next.Set = chosen(current, next)
	return next
}

// chosen is the keys the person chose: those the file set before, and each
// answer that differs from what the file had. Enter on a default chooses
// nothing, so a later default still reaches that person.
func chosen(current, next config.Values) map[string]bool {
	set := maps.Clone(current.Set)
	if set == nil {
		set = map[string]bool{}
	}
	for key, changed := range map[string]bool{
		"review.orchestrator": next.Orchestrator != current.Orchestrator,
		"panel.panelists":     !slices.Equal(next.Panelists, current.Panelists),
		"review.verdicts":     next.Verdicts != current.Verdicts,
		"loop.max_passes":     next.MaxPasses != current.MaxPasses,
		"loop.rest":           next.Rest != current.Rest,
		"loop.watch":          next.Watch != current.Watch,
		"mine.autofix":        next.Autofix != current.Autofix,
		"mine.autofix_cap":    next.AutofixCap != current.AutofixCap,
		"review.jobs":         next.Jobs != current.Jobs,
	} {
		if changed {
			set[key] = true
		}
	}
	return set
}

// unchosen is the agent the form shows when the file does not set one: the
// default, which an answer that keeps it does not write as a value. It is
// empty when the file sets the agent, which goes back as a value.
func unchosen(current config.Values) string {
	if current.Set["review.orchestrator"] {
		return ""
	}
	return current.Orchestrator
}
