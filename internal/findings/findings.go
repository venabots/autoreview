// Package findings reads what a synthesized review says about each finding
// back out of its text.
//
// The synthesis is prose written to a shape. Every finding that survived
// verification is one bullet with a severity tag, a location and a trailing
// `Flagged by:` clause that names the panelists that raised it. That clause
// is the only place the review records which model found what, so it is what
// the statistics stand on.
//
// Everything here is best effort: a line that does not fit the shape is
// skipped and is never a reason to fail. It is also v1's parser, rule for
// rule. v1 still appends to the same ledger, and a text the two versions read
// differently would score one model two ways.
package findings

import (
	"slices"
	"strings"

	"github.com/venabots/autoreview/internal/ledger"
)

// flaggedBy opens the clause that names who raised a finding.
const flaggedBy = "Flagged by"

// finding is one bullet as it was read. A severity, a location and a model
// are "" when the text gave none: none of the three is ever empty when it is
// there, so "" cannot be mistaken for a value.
type finding struct {
	severity string
	location string
	sources  []source
	count    uint32
	dropped  bool
	// issue is the bullet's text before the clause. It is compared and never
	// stored: it keeps two different bugs at one location apart.
	issue string
}

// source is one panelist in a `Flagged by` clause.
type source struct {
	name  string
	model string
}

// Parse is every finding in the text, in order, each one once. The result is
// never nil, so a review with no finding is written to the ledger as `[]`.
//
// A resumed session quotes its earlier findings when it checks them again,
// and the same finding must not count twice. The copy is in the same text,
// so the comparison never leaves it.
func Parse(reviewText string) []ledger.Finding {
	var kept []finding
	var current section
	for _, line := range logicalLines(reviewText) {
		if strings.HasPrefix(line, "#") {
			current = sectionOf(line)
			continue
		}
		if !continuesSection(line) {
			current = section{}
		}
		found, ok := readLine(line, current)
		if !ok {
			continue
		}
		if i := slices.IndexFunc(kept, found.restates); i >= 0 {
			kept[i] = combined(kept[i], found)
		} else {
			kept = append(kept, found)
		}
	}
	return ledgerFindings(kept)
}

// readLine reads one logical line as a finding. Only a line with the clause
// is one, except under Disagreements, where a bullet is prose to a shape of
// its own.
func readLine(line string, in section) (finding, bool) {
	switch {
	case strings.Contains(line, flaggedBy):
		return parseBullet(line, in)
	case in.disagreements:
		return parseDisagreement(line)
	}
	return finding{}, false
}

// restates says the two are one finding: the severity, the location and the
// issue text all match. The sources and the count are not compared, so a
// copy that names more panelists still reads as the same finding.
func (f finding) restates(other finding) bool {
	return f.severity == other.severity && f.location == other.location && f.issue == other.issue
}

// combined is a finding and a later copy of it, as one. It keeps the fullest
// credit: every source of both and the higher count. Without that a panelist
// would lose a mention, and a finding two of them raised would read as one
// that only the first did.
//
// A later copy under Disagreements is the last word of the synthesis: the
// finding it kept before did not hold, so the result is dropped.
func combined(first, again finding) finding {
	sources := slices.Clone(first.sources)
	for _, s := range again.sources {
		if !slices.Contains(sources, s) {
			sources = append(sources, s)
		}
	}
	return finding{
		severity: first.severity,
		location: first.location,
		sources:  sources,
		count:    max(first.count, again.count, uint32(len(sources))),
		dropped:  first.dropped || again.dropped,
		issue:    first.issue,
	}
}

// ledgerFindings is the findings in the ledger's own types. Both lists are
// made even when they are empty.
func ledgerFindings(kept []finding) []ledger.Finding {
	out := make([]ledger.Finding, len(kept))
	for i, f := range kept {
		flagged := make([]ledger.Source, len(f.sources))
		for j, s := range f.sources {
			flagged[j] = ledger.Source{Name: s.name, Model: given(s.model)}
		}
		out[i] = ledger.Finding{
			Severity:  given(f.severity),
			Location:  given(f.location),
			FlaggedBy: flagged,
			Count:     f.count,
			Dropped:   f.dropped,
		}
	}
	return out
}

// given is nil for a part the text did not give. The ledger leaves such a
// field out, where a pointer to "" would write an empty string.
func given(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
