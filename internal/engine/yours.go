package engine

import (
	"fmt"

	"github.com/venabots/autoreview/internal/mine"
)

// Your PRs are read at each look, and once more when a task ends: a fix
// changes its PR, and a row that kept the state from before the fix until
// the next look would say for minutes that the fix did nothing.
//
// Two reads can be on their way at once, so each gets a number when it
// starts. An answer older than the newest one taken is dropped: a look that
// began before the fix pushed must not put back what the later read
// replaced.

// mineAsked numbers a read of your PRs that is about to start.
func mineAsked(r run) (run, int) {
	r.mineAsked++
	return r, r.mineAsked
}

func withMine(s Snapshot, in mineListed) Snapshot {
	if in.err != nil {
		s.MineErr = in.err.Error()
		return s
	}
	s.MineLooked = true
	s.MineErr = ""
	s.Mine = in.list
	return s
}

// mineListed takes a look's answer about your PRs, and lets babysitting
// act on it.
func (e *Engine) mineListed(w workers, r run, in mineListed) run {
	if in.seq < r.mineTaken {
		return r
	}
	r.mineTaken = in.seq
	if in.err != nil && r.snap.MineLooked {
		// The first look's failure is in the lists it prints. A later
		// one would leave a stale list on screen with nothing said.
		e.journal(Noted{Text: fmt.Sprintf("could not read your PRs (%s); the Maintain tab keeps its last list", in.err)})
	}
	r.snap = withMine(r.snap, in)
	if in.err != nil {
		return r
	}
	r.looked = in.list
	return e.babysitLook(w, r)
}

// refreshMine reads your PRs once more, outside a look. A run that is
// ending does not: nobody would see the answer, and the run would wait
// for it.
func (e *Engine) refreshMine(w workers, r run) run {
	if r.snap.Phase == Stopping || r.snap.Phase == Done {
		return r
	}
	r, asked := mineAsked(r)
	bots := r.snap.MineBots
	w.group.Go(func() {
		list, err := guarded(func() ([]mine.PR, error) { return e.deps.Mine(w.ctx, bots) })
		e.in.put(mineRefreshed{seq: asked, list: list, err: err})
	})
	return r
}

// mineRefreshedInto takes the answer of the read that follows a task. It
// only changes what the tab shows. Babysitting does not look at it: this
// read is seconds after a push, before the checks have started and before
// GitHub has worked out the merge, and it must not become the state that
// later looks are compared with.
//
// A failed read is dropped. The next look says so if GitHub is still down,
// and until then the list on screen is no older than it was.
func mineRefreshedInto(r run, in mineRefreshed) run {
	if in.err != nil || in.seq < r.mineTaken {
		return r
	}
	r.mineTaken = in.seq
	r.snap = withMine(r.snap, mineListed{list: in.list})
	return r
}

// includeBots puts the bots' PRs on the Maintain tab, or takes them off,
// and reads the list again at once: a key that changed nothing until the
// next look, minutes away, would read as a key that does nothing.
func (e *Engine) includeBots(w workers, r run, on bool) run {
	if r.snap.MineBots == on {
		return r
	}
	r.snap.MineBots = on
	if on {
		e.journal(Noted{Text: "the Maintain tab shows Dependabot's PRs too"})
	} else {
		e.journal(Noted{Text: "the Maintain tab shows only your PRs"})
	}
	return e.refreshMine(w, r)
}
