package doctor

import (
	"cmp"
	"slices"
	"strings"

	"github.com/venabots/autoreview/internal/ledger"
)

// Seen is one model the ledger has seated, as a panel spec that would
// seat it again, and how it did over the whole ledger.
type Seen struct {
	Spec     string
	Launched int
	Answered int
}

// SeenModels is each model the ledger has seated, the most used first.
// The counts join the spellings of one model, as stats does. The spec is
// the spelling the model last ran under, as the panelist reported it: the
// spelling stats keeps drops a provider, and opencode needs the provider
// to run the model.
func SeenModels(records []ledger.Record) []Seen {
	counts := map[cohort]Seen{}
	last := map[cohort]int64{}
	for _, r := range records {
		for _, p := range r.Panel {
			c := cohortOf(p)
			s := counts[c]
			s.Launched++
			if answered(p) {
				s.Answered++
			}
			if r.At >= last[c] {
				s.Spec, last[c] = specOf(c.backend, p), r.At
			}
			counts[c] = s
		}
	}
	out := make([]Seen, 0, len(counts))
	for _, s := range counts {
		out = append(out, s)
	}
	slices.SortFunc(out, func(a, b Seen) int {
		return cmp.Or(cmp.Compare(b.Launched, a.Launched), cmp.Compare(a.Spec, b.Spec))
	})
	return out
}

// specOf is the panel spec of one seat: the backend, and the model the
// panelist reported, else the one its name carries, else none.
func specOf(backend string, p ledger.PanelEntry) string {
	model := ""
	if p.Model != nil {
		model = strings.TrimSpace(*p.Model)
	}
	if model == "" || model == "?" || model == "unknown" || model == p.Name {
		model, _ = strings.CutPrefix(p.Name, backend+"-")
		if model == p.Name {
			model = ""
		}
	}
	if model == "" {
		return backend
	}
	return backend + ":" + model
}
