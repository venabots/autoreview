package engine

import (
	"fmt"

	"github.com/venabots/autoreview/internal/prs"
	"github.com/venabots/autoreview/internal/queue"
)

// noteAbsent says, once, which of the PRs a person named the run shows
// nowhere, and marks the run for a failed exit. A person who names a PR
// expects a review of it. Without this, a run asked for a draft, a closed
// PR or a bot's PR ends clean with no review and no word.
func (e *Engine) noteAbsent(r run) run {
	if r.namedChecked || !e.cfg.Review || !r.snap.Looked || r.snap.ListErr != "" {
		return r
	}
	r.namedChecked = true
	for _, a := range queue.Missing(r.listing, e.cfg.Only) {
		e.journal(Noted{Text: fmt.Sprintf("PR #%d was asked for but is not reviewed: %s", a.Number, whyAbsent(a, r.snap.Truncated))})
		r.absent = true
	}
	return r
}

// whyAbsent is the reason in words. A list that came back full may not
// hold every open PR, so it cannot say that a PR is closed.
func whyAbsent(a queue.Absent, truncated bool) string {
	switch {
	case !a.Open && truncated:
		return "it is not among the open PRs that were read"
	case !a.Open:
		return "it is not an open PR in this repository"
	case a.Hidden == prs.HiddenDraft:
		return "it is a draft"
	}
	return "a bot opened it, and --dependabot reviews those"
}

// endCode is the run's exit status. A run that reviewed nothing for a PR a
// person named did not do what it was asked, whatever else went well.
func endCode(r run) int {
	if r.code == ExitOK && r.absent {
		return ExitFailed
	}
	return r.code
}
