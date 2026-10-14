package babysit

import "github.com/venabots/autoreview/internal/mine"

// ToFix is the PRs in list that "F" fixes now, in the order of list, and
// the fix each gets. It uses the rules autofix uses, once and with no
// memory: a PR that needs no work, a PR from a fork and a PR with a job
// running get nothing, and an approved PR gets only its conflicts fixed.
// One key on every PR must not push to an approved PR for a thread that a
// person meant to leave open. A nil busy means no PR has a job.
func ToFix(list []mine.PR, busy func(pr int) bool) []Due {
	var out []Due
	for _, pr := range list {
		if pr.CrossRepo || !NeedsWork(pr) || (busy != nil && busy(pr.Number)) {
			continue
		}
		if kind, ok := fixFor(pr.Approved(), pr.Merge); ok {
			out = append(out, Due{PR: pr.Number, Kind: kind})
		}
	}
	return out
}
