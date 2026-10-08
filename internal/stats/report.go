package stats

import (
	"cmp"
	"fmt"
	"maps"
	"math"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/venabots/autoreview/internal/sanitize"
	"github.com/venabots/autoreview/internal/words"
)

// report is everything the three renderings print.
type report struct {
	overview  overview
	cohorts   []cohort
	attention []string
	// unresolved is the `Flagged by` names that matched no panelist, with
	// their counts.
	unresolved map[string]int
	ledger     string
	since      *int64
}

// labelWidth is the width a name is cut to, so a table can hold it.
const labelWidth = 40

var header = []string{
	"MODEL", "BACKEND", "RUNS", "AVAIL", "RAW/RUN", "KEPT", "UNIQUE", "HIGH+", "DROPPED", "KEEP RATE", "SAMPLE",
}

// The columns of a row that the table colors.
const (
	colModel  = 0
	colAvail  = 3
	colSample = 10
)

// label makes a name from the ledger safe to print, and short. An agent
// wrote the name, and a terminal obeys what it is sent.
func label(s string) string {
	return ansi.Truncate(sanitize.Line(s), labelWidth, "…")
}

// ratioLabel is `97% (93-99)`: the point, and where the truth plausibly is.
func ratioLabel(r ratio) string {
	if !r.known {
		return "-"
	}
	return fmt.Sprintf("%.0f%% (%.0f-%.0f)", r.value*100, r.low*100, r.high*100)
}

// row is one cohort's cells, in the order of the header.
func row(c cohort) []string {
	perRun := "-"
	if v, ok := c.rawPerRun(); ok {
		perRun = fmt.Sprintf("%.1f", v)
	}
	return []string{
		label(c.key.model),
		label(c.key.backend),
		strconv.FormatUint(c.runs, 10),
		ratioLabel(c.availability()),
		perRun,
		strconv.FormatUint(c.kept, 10),
		strconv.FormatUint(c.keptUnique, 10),
		strconv.FormatUint(c.keptHigh, 10),
		strconv.FormatUint(c.dropped, 10),
		ratioLabel(c.keepRate()),
		c.sample(),
	}
}

// date is the UTC day of an epoch second.
func date(epoch int64) string {
	return time.Unix(epoch, 0).UTC().Format(time.DateOnly)
}

// headline counts the reviews and says what days and repos they cover.
func headline(r report) string {
	o := r.overview
	line := words.Count(o.runs, "review")
	if o.firstAt != nil {
		first, last := date(*o.firstAt), date(*o.lastAt)
		if first == last {
			line += " on " + first
		} else {
			line += " from " + first + " to " + last
		}
	}
	if o.repos > 0 {
		line += " across " + words.Count(o.repos, "repo")
	}
	return line
}

// byCount is a tally's names, the most frequent first and then by name.
func byCount(tally map[string]int) []string {
	names := slices.Collect(maps.Keys(tally))
	slices.SortFunc(names, func(a, b string) int {
		return cmp.Or(cmp.Compare(tally[b], tally[a]), cmp.Compare(a, b))
	})
	return names
}

// breakdown is `8 commented, 4 approved`. The names come from a trailer an
// agent wrote, so each one is made safe like every other cell.
func breakdown(tally map[string]int) string {
	names := byCount(tally)
	parts := make([]string, len(names))
	for i, name := range names {
		parts[i] = fmt.Sprintf("%d %s", tally[name], label(name))
	}
	return strings.Join(parts, ", ")
}

// unresolvedShown is how many unplaced names the footer lists by name.
const unresolvedShown = 5

// notScored says how many credits went to a name not on its panel, and the
// names that took the most.
func notScored(unresolved map[string]int) string {
	names := byCount(unresolved)
	total := 0
	for _, n := range unresolved {
		total += n
	}
	shown := make([]string, 0, unresolvedShown)
	for _, name := range names[:min(len(names), unresolvedShown)] {
		shown = append(shown, fmt.Sprintf("%s x%d", label(name), unresolved[name]))
	}
	more := ""
	if len(names) > unresolvedShown {
		more = fmt.Sprintf(", %d more", len(names)-unresolvedShown)
	}
	return fmt.Sprintf("not scored: %s credited to a name not on its panel (%s%s)",
		words.Count(total, "finding"), strings.Join(shown, ", "), more)
}

// medians is each cohort's middle launch time, for the cohorts that have
// one. Only panel runs record a time for each panelist.
func medians(cohorts []cohort) []string {
	var timed []string
	for _, c := range cohorts {
		if secs, ok := c.medianSecs(); ok {
			timed = append(timed, label(c.key.model)+" "+words.Duration(seconds(secs)))
		}
	}
	return timed
}

// seconds is a count of seconds as a duration. A count too large for one is
// the longest duration there is; it must not wrap round to a negative one.
func seconds(n uint64) time.Duration {
	const most = uint64(math.MaxInt64 / int64(time.Second))
	return time.Duration(min(n, most)) * time.Second
}

// attentionPrefix starts a footer line about a cohort that seldom answers.
// The table colors the line by it.
const attentionPrefix = "needs attention: "

// footer is the lines under the table.
func footer(r report) []string {
	var lines []string
	if len(r.overview.decisions) > 0 {
		lines = append(lines, "decisions: "+breakdown(r.overview.decisions))
	}
	if len(r.overview.risks) > 0 {
		lines = append(lines, "risk: "+breakdown(r.overview.risks))
	}
	if timed := medians(r.cohorts); len(timed) > 0 {
		lines = append(lines, "median time per panelist (panel runs): "+strings.Join(timed, ", "))
	}
	for _, note := range r.attention {
		// The note carries a model name an agent reported.
		lines = append(lines, attentionPrefix+sanitize.Line(note))
	}
	if len(r.unresolved) > 0 {
		lines = append(lines, notScored(r.unresolved))
	}
	return append(lines, "ledger: "+r.ledger)
}
