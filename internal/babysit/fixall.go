package babysit

import (
	"github.com/venabots/autoreview/internal/mine"
	"github.com/venabots/autoreview/internal/prs"
	"github.com/venabots/autoreview/internal/task"
)

// ToFix is the PRs in list that "F" fixes now, in the order of list, each
// with a full fix. A PR that needs no work, a PR from a fork and a PR with a
// job running get nothing. An approved PR is fixed too: a person pressed the
// key, and the approval hold is only for autofix, which pushes when nobody
// asked. A nil busy means no PR has a job.
func ToFix(list []mine.PR, busy func(pr int) bool) []Due {
	var out []Due
	for _, pr := range list {
		if pr.CrossRepo || !NeedsWork(pr) || (busy != nil && busy(pr.Number)) {
			continue
		}
		out = append(out, Due{PR: pr.Number, Kind: task.Fix})
	}
	return out
}

// LeftForAPerson says autofix leaves work on the PR for a person: it is
// approved, so autofix fixes only a conflict, and something other than a
// conflict needs work. Without this, turning autofix on for such a PR
// starts nothing and says nothing about why.
func LeftForAPerson(pr mine.PR) bool {
	return pr.Approved() && (pr.CI == prs.CIFailing || pr.Review == mine.ReviewChangesRequested || pr.OpenThreads() > 0)
}
