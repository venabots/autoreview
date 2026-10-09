package stats

import (
	"strings"

	"github.com/venabots/autoreview/internal/ledger"
)

// seat is one panelist on one run: the name the run gave it, and its cohort.
type seat struct {
	name string
	key  key
}

// rosterOf is a run's panel as the fold compares it.
func rosterOf(panel []ledger.PanelEntry) []seat {
	roster := make([]seat, len(panel))
	for i, p := range panel {
		roster[i] = seat{name: tidy(p.Name), key: canonical(p.Name, p.Model)}
	}
	return roster
}

// resolve is the cohort a `Flagged by` name means on one run's panel. ok is
// false for a name that is no panelist on this run: another review bot the
// synthesis credited, or prose the parser let through. Neither is a model's
// score.
func resolve(source ledger.Source, roster []seat) (key, bool) {
	if source.Model != nil {
		return resolveModel(source, roster)
	}
	return resolveName(source.Name, roster)
}

// resolveModel places a source that names its model. The model decides. It
// credits the panelist that ran that exact model, or nobody: to fall back to
// the name would credit a different model on the same backend.
// "claude (claude-opus-5)" on a panel whose only claude ran claude-fable-5 is
// not that panelist.
func resolveModel(source ledger.Source, roster []seat) (key, bool) {
	named := canonical(source.Name, source.Model)
	if hasKey(roster, named) {
		return named, true
	}
	// The roster may not have recorded a model. One panelist on the backend
	// with no model is the one meant. More than one, or a known and
	// different model, is not.
	return sole(roster, func(k key) bool {
		return k.backend == named.backend && k.model == noModel
	})
}

// resolveName places a source that is only a name. The synthesis writes the
// name the run gave the panelist, so that match comes first. It also names a
// panelist by its model alone ("glm-5.3"), by backend and model
// ("opencode-glm-5.3") when the run called it plain "opencode", by a bare
// backend ("claude") when one panelist was on it, and by the model's family
// ("glm") when one was on the panel.
func resolveName(written string, roster []seat) (key, bool) {
	name := tidy(written)
	for _, s := range roster {
		if lower(s.name) == name {
			return s.key, true
		}
	}
	model := normalizeModel(name)
	if k, ok := sole(roster, func(k key) bool { return k.model == model }); ok {
		return k, true
	}
	if spelled := canonical(written, nil); hasKey(roster, spelled) {
		return spelled, true
	}
	// A bare backend name means the one panelist on that backend. A longer
	// name that starts with the backend ("claude-code-bot", "claude-opus-5")
	// is an outside bot or a model not on the panel, and gets no credit.
	backend := backendOf(name)
	if k, ok := sole(roster, func(k key) bool { return k.backend == backend }); ok && name == backend {
		return k, true
	}
	return sole(roster, func(k key) bool {
		return strings.HasPrefix(k.model, model+"-") || strings.HasPrefix(k.model, model+"/")
	})
}

func hasKey(roster []seat, want key) bool {
	for _, s := range roster {
		if s.key == want {
			return true
		}
	}
	return false
}

// sole is the one cohort on the roster that matches. Two different cohorts
// that match are an ambiguous name, and nobody is credited.
func sole(roster []seat, match func(key) bool) (key, bool) {
	var found key
	count := 0
	for _, s := range roster {
		if !match(s.key) || (count > 0 && s.key == found) {
			continue
		}
		found = s.key
		count++
	}
	return found, count == 1
}
