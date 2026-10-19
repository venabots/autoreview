package queue

import (
	"slices"

	"github.com/venabots/autoreview/internal/rebase"
)

// Ask is what a run found when it asked about rebasing one head of a PR.
type Ask struct {
	Head  string
	State rebase.State
}

// Rebasing is the rows with what the run asked Dependabot applied. A row
// waits on the rebase only for the head it was asked about: once Dependabot
// pushes, the PR is new and is reviewed as usual. It applies over a row that
// would be reviewed next, held for its checks, or seen, which is what your
// own comment makes it. An approved, resting or capped row keeps its wait.
func Rebasing(rows []Row, asks map[int]Ask) []Row {
	out := slices.Clone(rows)
	for i, row := range out {
		ask, ok := asks[row.PR.Number]
		if !ok || ask.Head != row.PR.Head || !ask.State.Holds() {
			continue
		}
		switch row.Wait {
		case WaitNext, WaitChecks, WaitSeen:
			out[i].Wait, out[i].Rebase = WaitRebase, ask.State
		}
	}
	return out
}
