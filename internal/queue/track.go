package queue

import (
	"cmp"
	"maps"
	"slices"
	"time"
)

// Policy is how often a run may review one PR.
type Policy struct {
	// MaxPasses is how many reviews one PR gets in a run. Zero means no
	// limit. Every review is activity on the PR, so an author who answers
	// makes it reviewable again: the cap is what keeps that from going on
	// for as long as the run does.
	MaxPasses int
	// Rest is how long a PR waits after its review before it may be
	// reviewed again. Zero means no wait.
	Rest time.Duration
	// PushResets lets a new head commit start the count again. A run that
	// watches all day would otherwise stop reviewing a PR that is still
	// being worked on.
	PushResets bool
}

// Record is what a run remembers about one PR it reviewed.
type Record struct {
	Passes int
	// Head is the commit the last review saw. A different head is a push;
	// a comment does not change it.
	Head      string
	RestUntil time.Time
	// Finished is set when this run's review approved the PR. It stays set
	// for the run: the PR list lags the approval, and a PR the run approved
	// must not be reviewed again because the list has not caught up.
	Finished bool
}

// Track is every PR a run has reviewed. A Track is never changed: each
// method returns a new one.
type Track map[int]Record

// Reviewed is the track after a review of pr at head ended at at.
func (t Track) Reviewed(pr int, head string, at time.Time, approved bool, pol Policy) Track {
	rec := t[pr]
	passes := rec.Passes + 1
	if pushed(rec.Head, head) && pol.PushResets {
		passes = 1
	}
	next := maps.Clone(t)
	if next == nil {
		next = Track{}
	}
	next[pr] = Record{
		Passes:    passes,
		Head:      head,
		RestUntil: at.Add(pol.Rest),
		Finished:  rec.Finished || approved,
	}
	return next
}

// pushed says the head moved. An unknown head on either side never counts
// as a push: nobody can prove one.
func pushed(before, after string) bool {
	return before != "" && after != "" && before != after
}

// Gated is the rows with the run's own memory applied. A PR this run
// approved is approved. A PR that would be reviewed next waits when it is
// capped or resting.
func Gated(rows []Row, t Track, pol Policy, now time.Time) []Row {
	out := make([]Row, len(rows))
	for i, row := range rows {
		out[i] = gate(row, t, pol, now)
	}
	slices.SortStableFunc(out, func(a, b Row) int { return cmp.Compare(a.Wait, b.Wait) })
	return out
}

func gate(row Row, t Track, pol Policy, now time.Time) Row {
	rec, ok := t[row.PR.Number]
	switch {
	case !ok:
		return row
	case rec.Finished:
		row.Wait = WaitApproved
	case row.Wait != WaitNext:
	case capped(rec, row.PR.Head, pol):
		row.Wait = WaitCapped
	case now.Before(rec.RestUntil):
		row.Wait, row.Until = WaitResting, rec.RestUntil
	}
	return row
}

func capped(rec Record, head string, pol Policy) bool {
	if pol.MaxPasses == 0 || (pol.PushResets && pushed(rec.Head, head)) {
		return false
	}
	return rec.Passes >= pol.MaxPasses
}

// Watched says the run still owes this PR attention: it reviewed it, the
// PR is open, and the run neither approved it nor used up its reviews. A
// run that babysits stays while it watches any.
func Watched(rows []Row, t Track) bool {
	for _, row := range rows {
		if _, ok := t[row.PR.Number]; ok && row.Wait != WaitApproved && row.Wait != WaitCapped {
			return true
		}
	}
	return false
}
