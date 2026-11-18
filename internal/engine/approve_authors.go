package engine

import (
	"fmt"

	"github.com/venabots/autoreview/internal/prs"
	"github.com/venabots/autoreview/internal/review"
)

// levelFor is the verdict level of a review of the author's PR, and the
// highest risk at which it may approve. When the person names the authors
// a review may approve, the PR of anyone else gets the run's level without
// the approval, or with a risk ceiling when the person set one: a
// teammate's small change merges on the review, and a change whose mistake
// costs money or data waits for a person. GitHub logins match in any case.
func (e *Engine) levelFor(author string) (review.Verdicts, review.Risk) {
	return review.LevelFor(prs.ByAuthor(author, e.cfg.ApproveAuthors), e.cfg.Verdicts, e.cfg.ApproveOthersUpTo)
}

// aboveCeiling is the note for an approval that the review reported at a
// risk above the PR's ceiling, or "". The approval has landed by then, so
// the note is how the person learns that the skill did not hold it. A
// review that reports no risk gets no note: codex writes no trailer, and a
// note on each of its approvals would be noise.
func aboveCeiling(j Job) string {
	if j.Verdict != "approved" || j.MaxRisk == review.RiskNone || j.Trailer == nil {
		return ""
	}
	risk, ok := review.ParseRisk(j.Trailer.Risk)
	if !ok || risk <= j.MaxRisk {
		return ""
	}
	return fmt.Sprintf("PR #%d was approved at risk %s, above its ceiling of %s: the review skill did not follow %s",
		j.PR, risk, j.MaxRisk, review.MaxRiskFlag)
}
