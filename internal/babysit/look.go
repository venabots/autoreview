package babysit

import (
	"maps"

	"github.com/venabots/autoreview/internal/mine"
	"github.com/venabots/autoreview/internal/task"
)

// Due is one PR to fix now, and the task it gets: a full fix, or only a fix
// of its conflicts when it is approved.
type Due struct {
	PR   int
	Kind task.Kind
}

// Look is the state after one look at your PRs, and the PRs to fix now, in
// the order of list. busy says which PRs have a job running or waiting:
// those wait for the next look. A nil busy means none do.
//
// A fix is recorded as started when Look returns it, so the engine must
// start one on each PR it returns, of the kind it names. Do not call Look
// after a look that failed: the list would be the last good one, and a fix
// that just ended would take its state from before the fix.
func (s State) Look(list []mine.PR, busy func(pr int) bool) (State, []Due) {
	open := map[int]bool{}
	for _, pr := range list {
		open[pr.Number] = true
	}
	next := State{all: s.all, prs: keepOpen(s.prs, open), watches: keepOpen(s.watches, open)}
	var due []Due
	for _, pr := range list {
		if !next.Babysat(pr.Number) || pr.CrossRepo || (busy != nil && busy(pr.Number)) {
			continue
		}
		w, kind, fix := step(next.watches[pr.Number], pr)
		next.watches[pr.Number] = w
		if fix {
			due = append(due, Due{PR: pr.Number, Kind: kind})
		}
	}
	return next, due
}

// Finished is the state after a task on pr ended. Its own push is not a
// change: the next settled look's state becomes the one to compare with.
// Call it for every task that ends on the PR, of any kind, one started by
// hand included.
func (s State) Finished(pr int) State {
	w, ok := s.watches[pr]
	if !ok {
		return s
	}
	watches := maps.Clone(s.watches)
	w.settle = true
	watches[pr] = w
	return State{all: s.all, prs: s.prs, watches: watches}
}

// keepOpen is m without the PRs that left the list. A PR that left was
// merged or closed: there is nothing to babysit. The copy is always new, so
// Look may write to it.
func keepOpen[V any](m map[int]V, open map[int]bool) map[int]V {
	out := make(map[int]V, len(m))
	for pr, v := range m {
		if open[pr] {
			out[pr] = v
		}
	}
	return out
}
