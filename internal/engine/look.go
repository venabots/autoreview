package engine

import (
	"fmt"
	"strings"
	"time"

	"github.com/venabots/autoreview/internal/mine"
	"github.com/venabots/autoreview/internal/prs"
	"github.com/venabots/autoreview/internal/queue"
	"github.com/venabots/autoreview/internal/words"
)

// ciPoll is how often a run that waits for checks looks again.
const ciPoll = 30 * time.Second

// maxLookFails is how many failed looks in a row end a run that babysits,
// or a run that waits for checks. A run that watches never gives up.
const maxLookFails = 3

// look starts reading both lists. Each answers through the inbox, exactly
// once: a look that panicked answers with an error, because a look that
// never answers would leave the run looking forever.
func (e *Engine) look(w workers, r run) run {
	w.group.Go(func() {
		listing, err := guarded(func() (prs.Listing, error) { return e.deps.List(w.ctx) })
		e.in.put(listed{listing: listing, err: err})
	})
	r, asked := mineAsked(r)
	bots := r.snap.MineBots
	w.group.Go(func() {
		list, err := guarded(func() ([]mine.PR, error) { return e.deps.Mine(w.ctx, bots) })
		e.in.put(mineListed{seq: asked, list: list, err: err})
	})
	r.snap.Looking = true
	r.snap.NextLookAt = time.Time{}
	r.pending = 2
	return r
}

// lookNow starts a look at once. The timer is stopped, so the look it had
// set does not follow this one; the look sets the next one when it ends.
// A look under way already answers the ask.
func (e *Engine) lookNow(w workers, r run) run {
	if r.snap.Looking || r.snap.Phase == Stopping || r.snap.Phase == Done {
		return r
	}
	w.timer.Stop()
	return e.look(w, r)
}

// guarded turns a panic in a look into that look's error.
func guarded[T any](read func() (T, error)) (value T, err error) {
	defer func() {
		if p := recover(); p != nil {
			err = fmt.Errorf("the look failed unexpectedly: %v", p)
		}
	}()
	return read()
}

func (e *Engine) policy() queue.Policy {
	return queue.Policy{MaxPasses: e.cfg.MaxPasses, Rest: e.cfg.Rest, PushResets: e.cfg.Loop == LoopWatch}
}

// withListing takes a look's answer. A failed look keeps the rows the last
// good one found: the list on screen must not empty because GitHub was slow.
func (e *Engine) withListing(r run, in listed) run {
	if in.err != nil {
		r.snap.ListErr = in.err.Error()
		return r
	}
	r.listing = in.listing
	r.snap.Looked = true
	r.snap.ListErr = ""
	r.snap.Truncated = in.listing.Truncated
	return e.regate(r)
}

// regate works out where each PR stands again, from the last list and
// what the run remembers. A rest ends with time, so this runs at each look
// as well as after each review.
func (e *Engine) regate(r run) run {
	if !r.snap.Looked {
		return r
	}
	rows := queue.Rebasing(queue.Rows(r.listing, e.cfg.Gates, e.cfg.Only, r.track), r.asks)
	r.snap.Reviews = queue.Gated(rows, r.track, e.policy(), time.Now())
	return r
}

// afterLook takes a look that has fully answered, decides what the run
// does about it, and sets the time of the next look.
func (e *Engine) afterLook(w workers, r run) run {
	if !r.snap.Looking || r.pending > 0 {
		return r
	}
	r.snap.Looking = false
	r.looks++
	r.lookFails++
	if r.snap.ListErr == "" {
		r.lookFails = 0
	}
	if r.looks == 1 {
		snap := r.snap
		e.journal(Looked{Snapshot: &snap})
	}
	r = e.noteAbsent(r)
	return e.schedule(w, e.decide(w, r))
}

// decide is what one look leads to. A look during a pass only keeps the
// list fresh.
func (e *Engine) decide(w workers, r run) run {
	switch {
	case !e.cfg.Review:
		if e.cfg.Watch == 0 {
			r.snap.Phase = Done
			r.code = lookedOnceCode(r.snap)
		}
		return r
	case r.snap.Phase != Idle:
		return r
	case r.lookFails > 0:
		return e.lookFailed(r)
	case !r.ciUntil.IsZero():
		return e.waitedForChecks(w, r)
	case r.looks == 1:
		return e.firstLook(w, r)
	case !r.snap.Watching:
		return r
	}
	return e.laterLook(w, r)
}

// lookedOnceCode fails a run whose only look at the review list failed. A
// failed look at your own PRs does not: the run was asked to review.
func lookedOnceCode(s Snapshot) int {
	if s.ListErr != "" {
		return ExitFailed
	}
	return ExitOK
}

// lookFailed decides whether a failed look ends the run. A run that
// watches, and a run under the view, try again at the next look. A run
// that needs the list to start fails at once. A run that babysits or waits
// for checks gives up after a few failures in a row: one failed look
// concludes nothing, but a list that never comes back is a fault.
func (e *Engine) lookFailed(r run) run {
	switch {
	case e.cfg.HoldOpen || e.cfg.Loop == LoopWatch:
		return r
	case r.looks == 1:
		r.snap.Phase = Done
		r.code = ExitFailed
		return r
	case r.lookFails < maxLookFails:
		return r
	}
	e.journal(Noted{Text: fmt.Sprintf("the PR list failed to refresh %d times in a row; giving up", r.lookFails)})
	r.snap.Phase = Done
	r.code = ExitFailed
	return r
}

// firstLook starts the run's work. A run that looks once waits first for
// checks that are still running: a PR whose checks pass in a minute would
// otherwise be left out of the only pass the run makes. Failing checks are
// not waited for.
func (e *Engine) firstLook(w workers, r run) run {
	pending := pendingChecks(r.snap.Reviews)
	if e.cfg.CIWait > 0 && len(pending) > 0 {
		r.ciUntil = time.Now().Add(e.cfg.CIWait)
		e.journal(Noted{Text: fmt.Sprintf("waiting up to %s for the checks of %s; --skip-wait-for-ci reviews without them",
			words.Interval(e.cfg.CIWait), strings.Join(pending, " "))})
		return r
	}
	return e.firstPass(w, r)
}

func (e *Engine) waitedForChecks(w workers, r run) run {
	if len(pendingChecks(r.snap.Reviews)) > 0 && time.Now().Before(r.ciUntil) {
		return r
	}
	r.ciUntil = time.Time{}
	return e.firstPass(w, r)
}

func (e *Engine) firstPass(w workers, r run) run {
	rows := ready(r.snap.Reviews)
	if len(rows) == 0 {
		e.journal(NothingToReview{})
		return e.idleAfterLook(r)
	}
	return e.startPass(w, r, rows)
}

// laterLook reviews what has become ready since the last pass.
func (e *Engine) laterLook(w workers, r run) run {
	if rows := ready(r.snap.Reviews); len(rows) > 0 {
		r.idle = 0
		r.passEnded = false
		return e.startPass(w, r, rows)
	}
	if !r.passEnded {
		r.idle++
	}
	r.passEnded = false
	return e.idleAfterLook(r)
}

// idleAfterLook is a run with nothing to start. A run that looks once ends,
// or holds under the view. A run that watches waits for the next look. A
// run that babysits ends when it owes no PR anything more, or when nothing
// has changed for too many looks in a row.
func (e *Engine) idleAfterLook(r run) run {
	switch e.cfg.Loop {
	case LoopBabysit:
		rows := r.snap.Reviews
		if !queue.Watched(rows, r.track) && !heldByChecks(rows) {
			e.journal(Noted{Text: "nothing left to babysit"})
			r.snap.Phase = Done
			return r
		}
		if e.cfg.MaxIdle > 0 && r.idle >= e.cfg.MaxIdle {
			e.journal(Noted{Text: fmt.Sprintf("nothing has changed in %s since the last review", words.Count(r.idle, "idle check"))})
			r.snap.Phase = Done
		}
		return r
	case LoopWatch:
		if r.snap.Watching || e.cfg.HoldOpen {
			return r
		}
	}
	return e.rest(r, r.code)
}

// schedule sets the time of the next look.
func (e *Engine) schedule(w workers, r run) run {
	switch {
	case r.snap.Phase == Done:
		return r
	case !r.ciUntil.IsZero():
		return e.lookIn(w, r, ciPoll)
	case e.cfg.Watch > 0:
		return e.lookIn(w, r, e.untilNextLook(r))
	}
	return r
}

// untilNextLook is the watch interval, or less when a rest ends before it:
// a PR whose rest is over is reviewed then, and does not wait a whole
// interval more showing a rest of nothing.
func (e *Engine) untilNextLook(r run) time.Duration {
	next := e.cfg.Watch
	if !r.snap.Watching {
		return next
	}
	now := time.Now()
	for _, row := range r.snap.Reviews {
		if left := row.Until.Sub(now); row.Wait == queue.WaitResting && left > 0 && left < next {
			next = left
		}
	}
	return next
}

func (e *Engine) lookIn(w workers, r run, d time.Duration) run {
	r.snap.NextLookAt = time.Now().Add(d)
	w.timer.Reset(d)
	return r
}

func pendingChecks(rows []queue.Row) []string {
	out := []string{}
	for _, row := range rows {
		if row.Wait == queue.WaitChecks && row.PR.CI == prs.CIPending {
			out = append(out, fmt.Sprintf("#%d", row.PR.Number))
		}
	}
	return out
}

// heldByChecks says a PR waits only for its checks. Checks settle in
// minutes, so a run that babysits waits for them. A PR held under another
// PR does not count: it waits for a person to merge, and a run that waited
// for that would sit idle all day.
func heldByChecks(rows []queue.Row) bool {
	for _, row := range rows {
		if row.Wait == queue.WaitChecks {
			return true
		}
	}
	return false
}

// setWatch turns the loop on or off. Turned on while idle, it reviews what
// is ready now, without waiting for the next look.
func (e *Engine) setWatch(w workers, r run, on bool) run {
	if r.snap.Watching == on {
		return r
	}
	r.snap.Watching = on
	if !on {
		e.journal(Noted{Text: "no longer looking for work; R still reviews a PR"})
		return r
	}
	e.journal(Noted{Text: "watching: looking for work every " + words.Interval(e.cfg.Watch)})
	if r.snap.Phase != Idle || !r.snap.Looked || r.snap.ListErr != "" {
		return r
	}
	r = e.regate(r)
	if rows := ready(r.snap.Reviews); len(rows) > 0 {
		return e.startPass(w, r, rows)
	}
	return r
}
