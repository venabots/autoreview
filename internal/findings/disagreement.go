package findings

import "strings"

// raiseVerbs are the words of a panelist that raised a claim. The words of
// one that defended the code ("said", "found", "called it fine") are left
// out on purpose.
var raiseVerbs = [...]string{"claim", "rated", "flag", "raised", "tagged", "argued"}

// settledMarks are the words that say verification settled a claim against
// the panelist that raised it.
var settledMarks = [...]string{
	"falsif", "disprov", "overrul", "downgrad", "dropped",
	"does not hold", "did not hold", "not a bug", "unfounded",
}

// parseDisagreement reads a Disagreements bullet that has no `Flagged by`
// clause. It is prose, but prose to a shape: the location, a dash, then the
// panelist that made the claim (`codex (gpt-5.6-sol) claimed...`, `grok-4.6
// rated...`) and what verification made of it.
//
// Only the first name is read. A second one usually agreed with the
// synthesizer and is credited with nothing here.
//
// The section also holds splits between panelists that nobody settled. Only
// a bullet that says the claim was settled against the panelist that raised
// it is a dropped finding. Anything else would count against a model that
// nobody overruled.
func parseDisagreement(line string) (finding, bool) {
	if !startsBullet(line) {
		return finding{}, false
	}
	text := unlink(strings.TrimLeft(line, "-* "))
	head, after, ok := cutAtFirst(text, " — ", " -- ", ": ")
	if !ok {
		return finding{}, false
	}
	sources := sourceList(after)
	if len(sources) == 0 {
		return finding{}, false
	}
	first := sources[0]
	if !raisedBy(after, first.name) || !containsAny(asciiLower(text), settledMarks[:]) {
		return finding{}, false
	}
	return finding{
		severity: tagIn(head),
		location: locationIn(head),
		sources:  []source{first},
		count:    1,
		dropped:  true,
		issue:    strings.TrimSpace(head),
	}, true
}

// raisedBy says the named panelist raised the claim: a raise verb follows
// the name in its own clause.
//
// The first name in a bullet can be a defender. "X and Y found the guard
// sufficient. Z rated it HIGH." names X first, but Z is the one overruled.
// To count that bullet against X would debit the wrong model, so it is not
// counted at all.
func raisedBy(after, name string) bool {
	tail := after
	if i := strings.Index(after, name); i >= 0 {
		tail = after[i+len(name):]
	}
	clause, _, _ := strings.Cut(tail, ". ")
	clause, _, _ = strings.Cut(clause, " and ")
	return containsAny(asciiLower(clause), raiseVerbs[:])
}

// cutAtFirst cuts s at the first separator of the list that s has. The
// order of the list decides, and not the place in s.
func cutAtFirst(s string, separators ...string) (before, after string, ok bool) {
	for _, sep := range separators {
		if before, after, ok := strings.Cut(s, sep); ok {
			return before, after, true
		}
	}
	return "", "", false
}
