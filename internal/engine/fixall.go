package engine

import (
	"fmt"
	"slices"

	"github.com/venabots/autoreview/internal/babysit"
	"github.com/venabots/autoreview/internal/mine"
	"github.com/venabots/autoreview/internal/task"
	"github.com/venabots/autoreview/internal/words"
)

// FixAll is "F": one fix on each of your PRs that needs one now, an
// approved one included, since a person pressed the key. Unlike autofix, it
// does not come back.
type FixAll struct{}

func (FixAll) input()   {}
func (FixAll) command() {}

// fixAll starts the fixes. It reads the list the view shows, so the PRs
// it fixes are the ones the person saw when they pressed the key.
func (e *Engine) fixAll(w workers, r run) run {
	if why := e.refusal(task.Fix); why != "" {
		e.journal(Noted{Text: "not fixing your PRs: " + why})
		return r
	}
	busy := func(pr int) bool { return liveTask(r.tasks, pr) >= 0 }
	due := babysit.ToFix(r.snap.Mine, busy)
	if len(due) == 0 {
		e.journal(Noted{Text: "none of your PRs needs a fix now"})
		return r
	}
	e.journal(Noted{Text: fmt.Sprintf("fixing %s of yours", words.Count(len(due), "PR"))})
	for _, d := range due {
		i := slices.IndexFunc(r.snap.Mine, func(p mine.PR) bool { return p.Number == d.PR })
		found := r.snap.Mine[i]
		r = e.requestTask(w, r, task.Request{PR: d.PR, Kind: d.Kind, Title: found.Title, Branch: found.Branch, CrossRepo: found.CrossRepo})
	}
	return r
}
