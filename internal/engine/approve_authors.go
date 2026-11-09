package engine

import (
	"github.com/venabots/autoreview/internal/prs"
	"github.com/venabots/autoreview/internal/review"
)

// verdictsFor is the verdict level of a review of the author's PR. When
// the person names the authors a review may approve, the PR of anyone else
// gets the run's level without the approval: a teammate whose code is fine
// can still send a change that does not fit, and a person checks it first.
// GitHub logins match in any case.
func (e *Engine) verdictsFor(author string) review.Verdicts {
	if prs.ByAuthor(author, e.cfg.ApproveAuthors) {
		return e.cfg.Verdicts
	}
	return e.cfg.Verdicts.WithoutApproval()
}
