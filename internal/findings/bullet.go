package findings

import (
	"cmp"
	"strconv"
	"strings"
)

// parseBullet reads a line that has a `Flagged by` clause. It is false when
// the clause names nobody and gives no count: that is prose and not a
// finding.
func parseBullet(line string, in section) (finding, bool) {
	before, clause, _ := strings.Cut(line, flaggedBy)
	head := unlink(before)
	count, sources := parseSources(clause)
	if count == 0 {
		return finding{}, false
	}
	return finding{
		severity: cmp.Or(tagIn(head), in.severity),
		location: locationIn(head),
		sources:  sources,
		count:    count,
		dropped:  in.disagreements,
		issue:    strings.TrimSpace(head),
	}, true
}

// parseSources reads what follows "Flagged by": `: a (m), b (m2)`,
// `2: a (m) [LOW], b (m2) [MEDIUM] — using higher.`, `3.` or `all 4.` A
// count with no names is still a count.
func parseSources(clause string) (uint32, []source) {
	// The note after the list ("— using higher.") is not a panelist.
	clause, _, _ = strings.Cut(clause, " — ")
	clause, _, _ = strings.Cut(clause, " -- ")
	clause = strings.TrimSpace(clause)

	countPart, list, hasList := strings.Cut(clause, ":")
	if !hasList {
		countPart = strings.TrimRight(clause, ".")
	}
	countPart = strings.TrimSpace(countPart)
	explicit, hasCount := explicitCount(countPart)

	var sources []source
	if hasList {
		sources = sourceList(unlink(list))
	}
	// A word where a count belongs, with no list, is prose ("Flagged by
	// nobody") and not a finding. With a list the word is only "all" and the
	// list is the truth, so the finding is kept.
	if len(sources) == 0 && !hasCount && countPart != "" {
		return 0, nil
	}
	return max(explicit, uint32(len(sources))), sources
}

// explicitCount is the number the clause gives before its list: the 2 of
// "2:" and the 4 of "all 4.". It is false when there is none.
func explicitCount(countPart string) (uint32, bool) {
	word := countPart
	for strings.HasPrefix(word, "all") {
		word = strings.TrimPrefix(word, "all")
	}
	word = strings.TrimSpace(strings.TrimRight(strings.TrimSpace(word), "."))
	// v1 reads a count with a plus sign, and strconv does not.
	n, err := strconv.ParseUint(strings.TrimPrefix(word, "+"), 10, 32)
	if err != nil {
		return 0, false
	}
	return uint32(n), true
}
