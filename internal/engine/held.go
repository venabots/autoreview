package engine

import (
	"slices"
	"strings"

	"github.com/venabots/autoreview/internal/review"
)

// verdictsFor is the verdict level of a review of the author's PR: the
// run's level, without the approval when the person holds that author's
// PRs for a human check. GitHub logins match in any case.
func (e *Engine) verdictsFor(author string) review.Verdicts {
	if heldAuthor(e.cfg.NoApproveAuthors, author) {
		return e.cfg.Verdicts.WithoutApproval()
	}
	return e.cfg.Verdicts
}

func heldAuthor(logins []string, author string) bool {
	return slices.ContainsFunc(logins, func(login string) bool { return strings.EqualFold(login, author) })
}
