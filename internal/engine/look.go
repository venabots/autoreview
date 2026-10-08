package engine

import (
	"fmt"
	"time"

	"github.com/venabots/autoreview/internal/mine"
	"github.com/venabots/autoreview/internal/prs"
	"github.com/venabots/autoreview/internal/queue"
)

// look starts reading both lists. Each answers through the inbox, exactly
// once: a look that panicked answers with an error, because a look that
// never answers would leave the run looking forever.
func (e *Engine) look(w workers, r run) run {
	w.group.Go(func() {
		listing, err := guarded(func() (prs.Listing, error) { return e.deps.List(w.ctx) })
		e.in.put(listed{listing: listing, err: err})
	})
	w.group.Go(func() {
		list, err := guarded(func() ([]mine.PR, error) { return e.deps.Mine(w.ctx) })
		e.in.put(mineListed{list: list, err: err})
	})
	r.snap.Looking = true
	r.snap.NextLookAt = time.Time{}
	r.pending = 2
	return r
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

// afterLook takes a look that has fully answered: it starts the first
// pass, ends a run that only looks once, and sets the time of the next
// look.
func (e *Engine) afterLook(w workers, r run, timer *time.Timer) run {
	if !r.snap.Looking || r.pending > 0 {
		return r
	}
	r.snap.Looking = false
	r.looks++
	if r.looks == 1 {
		snap := r.snap
		e.journal(Looked{Snapshot: &snap})
		r = e.afterFirstLook(w, r)
	}
	if r.snap.Phase == Done || e.cfg.Watch == 0 {
		return r
	}
	r.snap.NextLookAt = time.Now().Add(e.cfg.Watch)
	timer.Reset(e.cfg.Watch)
	return r
}

// afterFirstLook does what the run was started for, now that it knows the
// PRs.
func (e *Engine) afterFirstLook(w workers, r run) run {
	switch {
	case e.cfg.Review && r.snap.ListErr == "":
		return e.startPass(w, r, ready(r.snap.Reviews))
	case e.cfg.Review && e.cfg.HoldOpen:
		// The list could not be read. A held run stays: the next look may
		// read it, and the person can see the error.
		return r
	case e.cfg.Watch == 0 || e.cfg.Review:
		r.snap.Phase = Done
		r.code = lookedOnceCode(r.snap)
	}
	return r
}

// lookedOnceCode fails a run whose only look at the review list failed. A
// failed look at your own PRs does not: the run was asked to review.
func lookedOnceCode(s Snapshot) int {
	if s.ListErr != "" {
		return ExitFailed
	}
	return ExitOK
}

// withListing takes a look's answer. A failed look keeps the rows the last
// good one found: the list on screen must not empty because GitHub was slow.
func withListing(s Snapshot, in listed, cfg Config) Snapshot {
	if in.err != nil {
		s.ListErr = in.err.Error()
		return s
	}
	s.Looked = true
	s.ListErr = ""
	s.Reviews = queue.Rows(in.listing, cfg.Gates, cfg.Only)
	s.Truncated = in.listing.Truncated
	return s
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
