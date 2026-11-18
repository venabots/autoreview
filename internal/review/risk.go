package review

import (
	"fmt"
	"strings"
)

// Risk is a rating on the scale that the synthesis rates a change by. A
// clean review of a migration or of the code that moves money still let
// the PR merge, because the approval never read the rating. A person who
// trusts a review to approve a teammate's small change does not trust it
// with a change whose mistake costs money or data, so the approval of
// another author's PR stops at a risk the person names.
type Risk int

const (
	// RiskNone is no rating. As the ceiling for another author's PR it
	// approves nothing. On a Request it sends no option.
	RiskNone Risk = iota
	RiskLow
	RiskMedium
	RiskHigh
	RiskCritical
)

// MaxRiskFlag is the option the review skills read the ceiling from. A
// copy of a skill that does not name it does not know the setting.
const MaxRiskFlag = "--max-risk"

var riskWords = []string{"none", "low", "medium", "high", "critical"}

func (r Risk) String() string {
	if r < RiskNone || r > RiskCritical {
		return fmt.Sprintf("risk(%d)", int(r))
	}
	return riskWords[r]
}

// ParseCeiling reads the highest risk at which a review may approve
// another author's PR, as the settings file writes it. A ceiling of high
// would hold only what a CRITICAL finding already holds, so it is not one
// of the words.
func ParseCeiling(raw string) (Risk, error) {
	for _, r := range []Risk{RiskNone, RiskLow, RiskMedium} {
		if raw == r.String() {
			return r, nil
		}
	}
	return RiskNone, fmt.Errorf("expects none, low or medium, got %q", raw)
}

// ParseRisk reads the rating a reviewer reported: its first word, in any
// case, with markdown emphasis and a trailing colon or comma trimmed, as in
// "HIGH", "**high**" or "HIGH — touches a migration". The word must be a
// rating and nothing else. "LOWISH", "LOW-ish" and "low/medium" are false:
// read as LOW, any of them would let an approval through that the review
// never rated.
func ParseRisk(raw string) (Risk, bool) {
	fields := strings.Fields(raw)
	if len(fields) == 0 {
		return RiskNone, false
	}
	word := strings.Trim(fields[0], "*_`:,.")
	for _, r := range []Risk{RiskLow, RiskMedium, RiskHigh, RiskCritical} {
		if strings.EqualFold(word, r.String()) {
			return r, true
		}
	}
	return RiskNone, false
}

// LevelFor is the verdict level of a review of one PR, and the highest
// risk at which it may approve, or RiskNone for no ceiling.
//
// The PR of an author the person names, or of any author when the person
// names none, gets the run's level. The PR of anyone else gets the level
// without the approval, so a person checks a teammate's change that does
// not fit before it merges. With a ceiling, it keeps the run's level up to
// that risk instead: a small change merges on the review, and a change
// whose mistake costs money or data waits for a person.
func LevelFor(named bool, run Verdicts, ceiling Risk) (Verdicts, Risk) {
	switch {
	case named:
		return run, RiskNone
	case ceiling == RiskNone || !run.Approves():
		return run.WithoutApproval(), RiskNone
	}
	return run, ceiling
}

// maxRiskOption is the option a prompt carries for the ceiling, or "" for
// no ceiling and for a skill that submits no verdict.
func maxRiskOption(r Request) string {
	skill := Skill(r)
	if r.MaxRisk == RiskNone || (skill != "auto-review" && skill != "recheck-pr") {
		return ""
	}
	return MaxRiskFlag + " " + r.MaxRisk.String()
}
