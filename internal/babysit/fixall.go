package babysit

import (
	"github.com/venabots/autoreview/internal/mine"
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
