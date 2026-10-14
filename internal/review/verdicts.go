package review

import "fmt"

// Verdicts is which review events a review may submit. The person picks
// it in the settings file. A team that wants the reviewer to stay out of
// the merge can keep it to comments.
type Verdicts int

const (
	// VerdictsAll lets the review approve and request changes, as it
	// decides.
	VerdictsAll Verdicts = iota
	// VerdictsApproveOnly lets it approve, and makes a review that would
	// request changes a comment review with the same findings.
	VerdictsApproveOnly
	// VerdictsCommentOnly makes every review a comment review.
	VerdictsCommentOnly
)

// VerdictsFlag is the option the review skills read the level from. A copy
// of a skill that does not name it does not know the setting.
const VerdictsFlag = "--verdicts"

var verdictWords = []string{"approve-and-request-changes", "approve-only", "comment-only"}

// ParseVerdicts reads a level as the settings file writes it.
func ParseVerdicts(raw string) (Verdicts, error) {
	for i, word := range verdictWords {
		if raw == word {
			return Verdicts(i), nil
		}
	}
	return VerdictsAll, fmt.Errorf("expects approve-and-request-changes, approve-only or comment-only, got %q", raw)
}

func (v Verdicts) String() string {
	return verdictWords[v]
}

// Approves says a review under this level may approve.
func (v Verdicts) Approves() bool {
	return v != VerdictsCommentOnly
}

// Allows says the level allows the verdict that landed. A verdict it does
// not allow means the skill that ran did not follow the option.
func (v Verdicts) Allows(verdict string) bool {
	switch verdict {
	case "approved":
		return v.Approves()
	case "changes requested":
		return v == VerdictsAll
	}
	return true
}

// verdictOption is the option a prompt carries for the level, or "" for
// the default and for a skill that submits no verdict.
func verdictOption(r Request) string {
	skill := Skill(r)
	if r.Verdicts == VerdictsAll || (skill != "auto-review" && skill != "recheck-pr") {
		return ""
	}
	return VerdictsFlag + " " + r.Verdicts.String()
}
