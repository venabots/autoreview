package stats

import (
	"cmp"
	"fmt"
	"slices"
	"strings"

	"github.com/venabots/autoreview/internal/ledger"
)

// folded is what the fold found: the cohorts, and the credits it could not
// place.
type folded struct {
	cohorts []cohort
	// unresolved is the `Flagged by` names that matched nobody on their
	// run's panel, with how often each came. It is shown so that a
	// systematic miss is seen and not lost.
	unresolved map[string]int
}

// fold adds every run into its cohorts. The cohorts with the most kept
// findings come first, then the most launched, then by backend and model.
func fold(runs []ledger.Record) folded {
	totals := map[key]cohort{}
	unresolved := map[string]int{}
	for _, run := range runs {
		tallies, unplaced := tally(run)
		for k, t := range tallies {
			totals[k] = totals[k].plus(t).seenAt(run.At)
		}
		for _, name := range unplaced {
			unresolved[name]++
		}
	}
	cohorts := make([]cohort, 0, len(totals))
	for k, c := range totals {
		c.key = k
		cohorts = append(cohorts, c)
	}
	slices.SortFunc(cohorts, func(a, b cohort) int {
		return cmp.Or(cmp.Compare(b.kept, a.kept), cmp.Compare(b.runs, a.runs), a.key.compare(b.key))
	})
	return folded{cohorts: cohorts, unresolved: unresolved}
}

// tally is what one run adds to each cohort on its panel, and the names its
// findings credit that are not on the panel.
func tally(run ledger.Record) (map[key]cohort, []string) {
	roster := rosterOf(run.Panel)
	credits, unplaced := credit(run.Findings, roster)
	tallies := map[key]cohort{}
	for i, p := range run.Panel {
		k := roster[i].key
		tallies[k] = tallies[k].plus(launch(p))
	}
	for k, c := range credits {
		tallies[k] = tallies[k].plus(c)
	}
	for k, c := range tallies {
		tallies[k] = withKeepRate(c, run.Reviewed)
	}
	return tallies, unplaced
}

// launch is one panelist's part of a run: that it was launched, whether it
// answered, and what it reported.
func launch(p ledger.PanelEntry) cohort {
	c := cohort{runs: 1}
	answered := p.OK != nil && *p.OK
	switch {
	case answered:
		c.answered = 1
	case p.OK != nil:
		c.failed = 1
	}
	if answered && p.Findings != nil {
		c.rawFindings = *p.Findings
		c.rawRuns = 1
	}
	if p.DurationSecs != nil {
		c.durations = []uint64{*p.DurationSecs}
	}
	return c
}

// credit is what a run's synthesis credited each cohort with, and the names
// it could not place.
func credit(findings []ledger.Finding, roster []seat) (map[key]cohort, []string) {
	credits := map[key]cohort{}
	var unplaced []string
	for _, f := range findings {
		keys, missed := sources(f, roster)
		unplaced = append(unplaced, missed...)
		score := scoreOf(f, f.Count == 1 && len(keys) == 1)
		for _, k := range keys {
			credits[k] = credits[k].plus(score)
		}
	}
	return credits, unplaced
}

// sources is the cohorts one finding names, each once, and the names on it
// that are no panelist.
func sources(f ledger.Finding, roster []seat) (keys []key, missed []string) {
	for _, source := range f.FlaggedBy {
		k, ok := resolve(source, roster)
		switch {
		case !ok:
			missed = append(missed, tidy(source.Name))
		case !slices.Contains(keys, k):
			keys = append(keys, k)
		}
	}
	return keys, missed
}

// scoreOf is what one finding is worth to a cohort it names. A finding
// under Disagreements counts against the cohort and for nothing else.
func scoreOf(f ledger.Finding, unique bool) cohort {
	if f.Dropped {
		return cohort{dropped: 1}
	}
	score := cohort{kept: 1}
	if unique {
		score.keptUnique = 1
	}
	if f.Severity != nil && (*f.Severity == "HIGH" || *f.Severity == "CRITICAL") {
		score.keptHigh = 1
	}
	return score
}

// withKeepRate sets one run's part of the keep rate. The rate needs both
// sides from the same run: kept findings are a rate only against the count
// they were kept from, and never more than that count. A run whose review
// text was not read has no findings for a reason that is not "nothing was
// kept", so it must not pull the rate down.
func withKeepRate(c cohort, reviewed bool) cohort {
	if !reviewed || c.rawRuns == 0 {
		return c
	}
	c.keepNum = min(c.kept, c.rawFindings)
	c.keepDen = c.rawFindings
	return c
}

// selectRuns is the runs a report is about: at or after since, in a repo
// whose name contains repo in any case, when either is given.
func selectRuns(runs []ledger.Record, since *int64, repo string) []ledger.Record {
	needle := lower(repo)
	var out []ledger.Record
	for _, r := range runs {
		if since != nil && r.At < *since {
			continue
		}
		if repo != "" && (r.Repo == nil || !strings.Contains(lower(*r.Repo), needle)) {
			continue
		}
		out = append(out, r)
	}
	return out
}

// overview is what the runs as a whole say, above the table of models.
type overview struct {
	runs  int
	repos int
	// firstAt and lastAt are nil when there is no run.
	firstAt   *int64
	lastAt    *int64
	decisions map[string]int
	risks     map[string]int
	sources   map[string]int
}

func overviewOf(runs []ledger.Record) overview {
	o := overview{
		runs:      len(runs),
		decisions: map[string]int{},
		risks:     map[string]int{},
		sources:   map[string]int{},
	}
	repos := map[string]bool{}
	for _, r := range runs {
		if r.Repo != nil {
			repos[lower(*r.Repo)] = true
		}
		first, last := r.At, r.At
		if o.firstAt != nil {
			first, last = min(*o.firstAt, r.At), max(*o.lastAt, r.At)
		}
		o.firstAt, o.lastAt = &first, &last
		o.decisions[orUnknown(r.Decision)]++
		o.risks[orUnknown(r.Risk)]++
		o.sources[r.Source]++
	}
	o.repos = len(repos)
	return o
}

func orUnknown(s *string) string {
	if s == nil {
		return "unknown"
	}
	return *s
}

// attentionLaunches is the launches it takes before a cohort that seldom
// answers is called out. Four failures in four is too few to say.
const attentionLaunches = 5

// attention names the cohorts that cost runs and give nothing back: launched
// enough to matter and answering less than half the time. A misconfigured
// alias looks exactly like this, and nothing else in the pipeline says so.
func attention(cohorts []cohort) []string {
	notes := []string{}
	for _, c := range cohorts {
		launches := c.answered + c.failed
		if launches < attentionLaunches || c.availability().value >= 0.5 {
			continue
		}
		notes = append(notes, fmt.Sprintf("%s on %s answered %d of %d launches",
			c.key.model, c.key.backend, c.answered, launches))
	}
	return notes
}
