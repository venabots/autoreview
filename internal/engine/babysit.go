package engine

import (
	"fmt"
	"slices"

	"github.com/venabots/autoreview/internal/babysit"
	"github.com/venabots/autoreview/internal/mine"
	"github.com/venabots/autoreview/internal/sanitize"
	"github.com/venabots/autoreview/internal/task"
)

// Babysit turns babysitting on or off: "b" on one of your PRs, "B" on all
// of them. Babysitting does not depend on w: the run looks at your PRs at
// every look, whether it looks for reviews or not.
type Babysit struct{ Change babysit.Change }

func (Babysit) input()   {}
func (Babysit) command() {}

// refusals says why each kind of task cannot run in this run. It is
// worked out once: the skills are looked for on disk.
func (e *Engine) refusals() map[task.Kind]string {
	out := map[task.Kind]string{}
	for _, k := range []task.Kind{task.Fix, task.Comments, task.Conflicts} {
		switch {
		case e.cfg.NoPost:
			out[k] = "--no-post leaves every PR alone"
		case e.deps.TaskRefusal != nil:
			out[k] = e.deps.TaskRefusal(k)
		}
	}
	return out
}

// refusal says why a task of kind k cannot run in this run, or is empty.
func (e *Engine) refusal(k task.Kind) string {
	return e.Snapshot().Refusals[k]
}

// setBabysit applies a change. Turning babysitting on is refused for any
// reason a fix would be: every look would otherwise start a fix that is
// refused, and say so.
//
// A PR turned on is looked at straight away, from the last look, so a PR
// that needs work now does not wait a whole interval for its first fix.
// It is the last look and not the read that follows a task: that read is
// seconds after a push, before the checks and the merge state are known.
func (e *Engine) setBabysit(w workers, r run, c babysit.Change) run {
	if why := e.refusal(task.Fix); c.On && why != "" {
		e.journal(Noted{Text: fmt.Sprintf("not babysitting %s: %s", babysatWhat(c), why)})
		return r
	}
	next, err := r.snap.Babysit.Apply(c, r.looked)
	if err != nil {
		e.journal(Noted{Text: sanitize.Line(err.Error())})
		return r
	}
	r.snap.Babysit = next
	e.journal(Noted{Text: c.Said()})
	if !c.On || !r.snap.MineLooked {
		return r
	}
	return e.babysitLook(w, r)
}

func babysatWhat(c babysit.Change) string {
	if c.Scope == babysit.ScopeAll {
		return "your PRs"
	}
	return fmt.Sprintf("PR #%d", c.PR)
}

// babysitLook starts a fix on each babysat PR that changed and needs work.
// A PR that is approved gets none, and the log says so once: a babysat PR
// with a red build that nothing fixes would otherwise look forgotten.
// It runs only after a look at your PRs that worked: after a failed one,
// the list is the last good one, and a fix that just ended would take its
// state from before the fix.
func (e *Engine) babysitLook(w workers, r run) run {
	if !r.snap.Babysit.Any() || r.snap.Phase == Stopping || r.snap.Phase == Done {
		return r
	}
	busy := func(pr int) bool { return liveTask(r.tasks, pr) >= 0 }
	before := r.snap.Babysit
	next, due := before.Look(r.looked, busy)
	r.snap.Babysit = next
	for _, pr := range r.looked {
		if next.OnHold(pr.Number) && !before.OnHold(pr.Number) {
			e.journal(Noted{Text: fmt.Sprintf("babysitting: PR #%d is approved, so it gets no fix until that changes", pr.Number)})
		}
	}
	for _, pr := range due {
		i := slices.IndexFunc(r.looked, func(p mine.PR) bool { return p.Number == pr })
		found := r.looked[i]
		e.journal(Noted{Text: fmt.Sprintf("babysitting: fixing PR #%d, which changed and needs work", pr)})
		r = e.requestTask(w, r, task.Request{PR: pr, Kind: task.Fix, Title: found.Title, Branch: found.Branch, CrossRepo: found.CrossRepo})
	}
	return r
}

// fixEnded tells babysitting a fix on pr is over, so its own push does not
// read as a change at the next look. A fix started by hand counts too.
func fixEnded(r run, k task.Kind, pr int) run {
	if k == task.Fix {
		r.snap.Babysit = r.snap.Babysit.Finished(pr)
	}
	return r
}
