package review

import "fmt"

// The gate the review skill approves on: 3 of every 4 panelists must
// answer, and never fewer than 2. It is gate 1 of the auto-review skill. If
// the skill's gate changes and this does not, the line names the wrong
// number.
const (
	quorumNumerator   = 3
	quorumDenominator = 4
	quorumFloor       = 2
)

// Shortfall is how far short of the gate a clean review fell.
type Shortfall struct {
	Answered, Launched, Needed int
}

// Quorum is how many of the launched panelists the gate needs: three
// quarters of them, rounded up, and never fewer than two.
func Quorum(launched int) int {
	roundedUp := (launched*quorumNumerator + quorumDenominator - 1) / quorumDenominator
	return max(roundedUp, quorumFloor)
}

// CleanButShort is the shortfall of a review that found nothing above
// polish and was still not approved, because too few of the panel answered.
// verdict is the resolved verdict, or "" when nothing landed.
//
// The gate is right to hold such a review: two clean answers out of four is
// not the review it asks for. But a summary that shows it as "commented",
// like a review that found a bug, sends a reader to open a PR that only
// waits for a reviewer to come back. This names the difference. It
// approves nothing.
//
// It is false for an approved PR, for a review with a finding above polish,
// for a trailer that did not report its findings, for a panel that met the
// gate, and for a panel where nobody answered: then nothing came back to be
// clean.
func CleanButShort(verdict string, t *Trailer) (Shortfall, bool) {
	if verdict == "approved" || t == nil || !clean(*t) {
		return Shortfall{}, false
	}
	launched := len(t.Panel)
	answered := 0
	for _, panelist := range t.Panel {
		if panelist.OK != nil && *panelist.OK {
			answered++
		}
	}
	needed := Quorum(launched)
	if answered == 0 || answered >= needed {
		return Shortfall{}, false
	}
	return Shortfall{Answered: answered, Launched: launched, Needed: needed}, true
}

// clean says the findings are polish at most. A count the reviewer did not
// report is not a zero, so a trailer with no counts is not clean.
func clean(t Trailer) bool {
	f := t.Findings
	if f == nil || f.MustFix == nil || f.ShouldFix == nil {
		return false
	}
	return *f.MustFix == 0 && *f.ShouldFix == 0
}

// Line is the sentence for a summary and a detail pane.
func (s Shortfall) Line() string {
	return fmt.Sprintf("looks clean, but only %d of %d reviewers answered; approval needs %d", s.Answered, s.Launched, s.Needed)
}

// Short is the few characters a list row has room for.
func (s Shortfall) Short() string {
	return fmt.Sprintf("clean %d/%d", s.Answered, s.Launched)
}
