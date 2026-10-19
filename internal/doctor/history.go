package doctor

import (
	"cmp"
	"slices"

	"github.com/venabots/autoreview/internal/ledger"
	"github.com/venabots/autoreview/internal/stats"
)

// Model is one panelist model as the ledger saw it in the window.
type Model struct {
	Backend  string
	Model    string
	Launched int
	Answered int
	// LastAnswer is the last time the model answered, in the whole ledger,
	// as an epoch second. Zero means it never did. A model that has been
	// down for a week must not read as "never answered" because the window
	// is a day.
	LastAnswer int64
}

// Health is a model's state, in one word.
type Health int

const (
	Healthy Health = iota
	// Failing is a model that answered fewer than half of its launches.
	Failing
	// Down is a model that answered none of its launches.
	Down
)

// Health is how the model did in the window.
func (m Model) Health() Health {
	switch {
	case m.Answered == 0:
		return Down
	case m.Answered*2 < m.Launched:
		return Failing
	}
	return Healthy
}

// Stalled is a PR whose latest review in the window gave no verdict while
// panelists were missing. Such a PR waits for a review nobody will start,
// because the run counts it as reviewed.
type Stalled struct {
	Repo     string
	PR       uint64
	At       int64
	Decision string
	Seated   int
	Answered int
}

// History is what the ledger says about the window.
type History struct {
	Reviews int
	// SpendUSD is the cost of the reviews that recorded one.
	SpendUSD float64
	Models   []Model
	Stalled  []Stalled
}

// historyOf reads the records at or after since. records is every record
// in the ledger, so a model's last answer can be older than the window.
func historyOf(records []ledger.Record, since int64) History {
	window := slices.DeleteFunc(slices.Clone(records), func(r ledger.Record) bool { return r.At < since })
	return History{
		Reviews:  len(window),
		SpendUSD: spend(window),
		Models:   withLastAnswer(modelsIn(window), records),
		Stalled:  stalled(window),
	}
}

func spend(records []ledger.Record) float64 {
	total := 0.0
	for _, r := range records {
		if r.CostUSD != nil {
			total += *r.CostUSD
		}
	}
	return total
}

type cohort struct{ backend, model string }

func cohortOf(p ledger.PanelEntry) cohort {
	backend, model := stats.Cohort(p.Name, p.Model)
	return cohort{backend, model}
}

func answered(p ledger.PanelEntry) bool {
	return p.OK != nil && *p.OK
}

// modelsIn counts each model's launches, worst first, so the model that
// needs a person is the first line read.
func modelsIn(records []ledger.Record) []Model {
	counts := map[cohort]Model{}
	for _, r := range records {
		for _, p := range r.Panel {
			c := cohortOf(p)
			m := counts[c]
			m.Backend, m.Model = c.backend, c.model
			m.Launched++
			if answered(p) {
				m.Answered++
			}
			counts[c] = m
		}
	}
	models := make([]Model, 0, len(counts))
	for _, m := range counts {
		models = append(models, m)
	}
	slices.SortFunc(models, func(a, b Model) int {
		return cmp.Or(
			cmp.Compare(b.Health(), a.Health()),
			cmp.Compare(a.Backend, b.Backend),
			cmp.Compare(a.Model, b.Model),
		)
	})
	return models
}

// withLastAnswer gives each model the time it last answered anywhere in
// the ledger. It returns new values.
func withLastAnswer(models []Model, records []ledger.Record) []Model {
	last := map[cohort]int64{}
	for _, r := range records {
		for _, p := range r.Panel {
			if c := cohortOf(p); answered(p) && r.At > last[c] {
				last[c] = r.At
			}
		}
	}
	out := make([]Model, len(models))
	for i, m := range models {
		m.LastAnswer = last[cohort{m.Backend, m.Model}]
		out[i] = m
	}
	return out
}
