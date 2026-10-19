package setup

import (
	"cmp"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/venabots/autoreview/internal/cli"
	"github.com/venabots/autoreview/internal/panel"
	"github.com/venabots/autoreview/internal/stats"
	"github.com/venabots/autoreview/internal/words"
)

// Seen is a model this machine has seated as a panelist, as the ledger
// has it: the spec it was last seated under, and how it did.
type Seen struct {
	Spec     string
	Launched int
	Answered int
}

// source is where the panel the form starts with came from.
type source int

const (
	nobody source = iota
	fromFile
	fromShell
	fromPath
)

// aliases are the claude models that keep their name across releases, so
// an option for each one stays right as models change.
var aliases = []string{"claude:opus", "claude:sonnet"}

type panelOption struct {
	spec, label string
	checked     bool
}

// prefilledPanel is the panel the checklist starts with: the file's, else
// the shell's, else every backend on PATH. One backend alone is not
// checked: a panel of one never approves, and blank seats it anyway.
func prefilledPanel(s Start) ([]string, source) {
	if len(s.Current.Panelists) > 0 {
		return slices.Clone(s.Current.Panelists), fromFile
	}
	if specs := strings.Fields(getenv(s, cli.EnvPanelists)); len(specs) > 0 {
		return specs, fromShell
	}
	if found := backendsOnPath(s); len(found) > 1 {
		return found, fromPath
	}
	return nil, nobody
}

// panelOptions is the checklist: the file's panel, the shell's, the models
// this machine has seated, each backend on PATH and the claude aliases,
// each model once, with the prefilled panel checked. Nothing here runs a
// process: a backend that lists its models would make the form wait on it.
func panelOptions(s Start) []panelOption {
	prefilled, _ := prefilledPanel(s)
	record := recordsByModel(s.Seen)
	var out []panelOption
	taken := map[string]bool{}
	add := func(spec, origin string) {
		if _, err := panel.ParseSpec(spec, "panelist"); err != nil || taken[modelOf(spec)] {
			return
		}
		taken[modelOf(spec)] = true
		label := strings.Join(slices.DeleteFunc([]string{spec, origin, record[modelOf(spec)]}, isBlank), " · ")
		out = append(out, panelOption{spec: spec, label: label, checked: slices.Contains(prefilled, spec)})
	}
	for _, spec := range s.Current.Panelists {
		add(spec, "in your settings")
	}
	for _, spec := range strings.Fields(getenv(s, cli.EnvPanelists)) {
		add(spec, "from $"+cli.EnvPanelists)
	}
	for _, seen := range byUse(s.Seen) {
		add(seen.Spec, "")
	}
	found := backendsOnPath(s)
	for _, backend := range found {
		add(backend, "its default model")
	}
	if slices.Contains(found, "claude") {
		for _, alias := range aliases {
			add(alias, "")
		}
	}
	return out
}

// modelOf is the model a spec names, as the ledger counts it, so two
// spellings of one model are one option and share one record.
func modelOf(spec string) string {
	backend, model, pinned := strings.Cut(spec, ":")
	if !pinned {
		b, m := stats.Cohort(backend, nil)
		return b + "/" + m
	}
	b, m := stats.Cohort(backend, &model)
	return b + "/" + m
}

// recordsByModel is each model's record, in words: how many reviews it
// sat, and how many of them it failed.
func recordsByModel(seen []Seen) map[string]string {
	totals := map[string]Seen{}
	for _, s := range seen {
		t := totals[modelOf(s.Spec)]
		t.Launched += s.Launched
		t.Answered += s.Answered
		totals[modelOf(s.Spec)] = t
	}
	out := map[string]string{}
	for model, t := range totals {
		out[model] = words.Count(t.Launched, "review")
		if failed := t.Launched - t.Answered; failed > 0 {
			out[model] += fmt.Sprintf(", %d failed", failed)
		}
	}
	return out
}

// byUse is the models seen, the most used first. It returns a new slice.
func byUse(seen []Seen) []Seen {
	out := slices.Clone(seen)
	slices.SortStableFunc(out, func(a, b Seen) int {
		return cmp.Or(cmp.Compare(b.Launched, a.Launched), cmp.Compare(a.Spec, b.Spec))
	})
	return out
}

func backendsOnPath(s Start) []string {
	if s.LookPath == nil {
		return nil
	}
	var found []string
	for _, backend := range panel.Backends {
		if _, err := s.LookPath(backend); err == nil {
			found = append(found, backend)
		}
	}
	return found
}

func getenv(s Start, name string) string {
	if s.Getenv == nil {
		return ""
	}
	return s.Getenv(name)
}

func isBlank(s string) bool { return s == "" }

// finalPanel is the checked models and then the added ones, each once. An
// empty panel is nil: blank seats every backend on PATH.
func finalPanel(picked []string, added string) []string {
	var out []string
	for _, spec := range append(slices.Clone(picked), strings.Fields(added)...) {
		if !slices.Contains(out, spec) {
			out = append(out, spec)
		}
	}
	return out
}

// addedBeside checks the added models, and the panel they make with the
// checked ones: each spec must parse, and a panel of one never approves.
// It reads picked when it checks, after the checklist set it.
func addedBeside(picked *[]string) func(string) error {
	return func(raw string) error {
		for _, spec := range strings.Fields(raw) {
			if _, err := panel.ParseSpec(spec, "panelist"); err != nil {
				return err
			}
		}
		if len(finalPanel(*picked, raw)) == 1 {
			return errors.New("check or add 2 or more models, or none: a panel of one never approves")
		}
		return nil
	}
}

// panelToWrite is the panel the file gets. A panel kept as the PATH gave
// it is only what blank already seats, so it is not written: written, it
// would leave out a backend installed later. A panel kept from the shell
// is written, so the file carries it and the variable can go. Any panel
// the person changed is written. The PATH's panel is compared as a set:
// the checklist can list a backend earlier than the PATH found it.
func panelToWrite(final, prefilled []string, from source) []string {
	if from == fromPath && slices.Equal(sorted(final), sorted(prefilled)) {
		return nil
	}
	return final
}

func sorted(specs []string) []string {
	out := slices.Clone(specs)
	slices.Sort(out)
	return out
}
