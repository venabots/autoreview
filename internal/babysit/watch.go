package babysit

import (
	"github.com/venabots/autoreview/internal/mine"
	"github.com/venabots/autoreview/internal/prs"
)

// fingerprint is what a PR looked like when it was last fixed. A different
// one is a change worth another fix.
type fingerprint struct {
	head      string
	updatedAt string
	ci        prs.CI
	merge     mine.Merge
}

func fingerprintOf(pr mine.PR) fingerprint {
	return fingerprint{head: pr.Head, updatedAt: pr.UpdatedAt, ci: pr.CI, merge: pr.Merge}
}

// watch is one babysat PR's history in this run.
type watch struct {
	// baseline is the state at the last fix, or at the first settled look
	// after it. hasBaseline is false until the first fix.
	baseline    fingerprint
	hasBaseline bool
	// settle says a fix just ended: the next settled look's state becomes
	// the baseline. waited counts the looks spent waiting for it.
	settle bool
	waited int
	streak int
	fixes  int
	// seen says threads and review hold what others last left on the PR.
	// The set is replaced at each look and never written to after.
	seen    bool
	threads map[string]bool
	review  mine.Review
}

// step is one look at one babysat PR that has no job running: the PR's next
// watch, and whether it gets a fix now.
func step(w watch, pr mine.PR) (watch, bool) {
	threads := idSet(pr.ThreadIDs)
	acted := w.seen && (hasNew(threads, w.threads) || reviewedAgain(w.review, pr.Review))
	// A push makes the checks and the merge look clean for a minute, so only
	// a settled look that finds the PR clean starts the count again.
	settled := pr.CI != prs.CIPending && pr.Merge != mine.MergeUnknown
	next := w
	if acted || (settled && !NeedsWork(pr)) {
		next.streak = 0
	}
	next.seen, next.threads, next.review = true, threads, pr.Review
	now := fingerprintOf(pr)
	// Somebody acting during the fix is new work, and is not swallowed by
	// the look that records what the fix left.
	if next.settle && !acted {
		return settle(next, now, settled)
	}
	next.settle, next.waited = false, 0
	changed := !next.hasBaseline || next.baseline != now
	if NeedsWork(pr) && (changed || acted) && next.streak < MaxStreak {
		next.baseline, next.hasBaseline = now, true
		return started(next), true
	}
	return next, false
}

// settle is the first look after a fix. It records what the fix left without
// acting on it. Checks still running and a merge GitHub has not worked out
// are not what the fix left, so the look waits for them, up to SettleLooks:
// otherwise their settling would read as a change.
func settle(w watch, now fingerprint, settled bool) (watch, bool) {
	next := w
	if !settled && next.waited < SettleLooks {
		next.waited++
		return next, false
	}
	next.settle, next.waited = false, 0
	broke := next.hasBaseline && fixBroke(next.baseline, now)
	next.baseline, next.hasBaseline = now, true
	if broke && next.streak < MaxStreak {
		return started(next), true
	}
	return next, false
}

// fixBroke says the fix's own push broke the build or left conflicts that
// were not there before it. That is work, not the fix's baseline.
func fixBroke(before, now fingerprint) bool {
	return (now.ci == prs.CIFailing && before.ci != prs.CIFailing) ||
		(now.merge == mine.MergeConflicts && before.merge != mine.MergeConflicts)
}

func started(w watch) watch {
	next := w
	next.streak++
	next.fixes++
	return next
}

// reviewedAgain says the review decision changed because somebody decided.
// A push dismisses a stale approval on some repos: that is the fix's own
// push, not a reviewer acting.
func reviewedAgain(before, now mine.Review) bool {
	dismissed := before == mine.ReviewApproved && now == mine.ReviewRequired
	return before != now && !dismissed
}

// hasNew says a thread is open that was not open before. Threads are told
// apart by id: a count goes down when one is resolved and another is opened
// in the same minute.
func hasNew(now, before map[string]bool) bool {
	for id := range now {
		if !before[id] {
			return true
		}
	}
	return false
}

func idSet(ids []string) map[string]bool {
	set := make(map[string]bool, len(ids))
	for _, id := range ids {
		set[id] = true
	}
	return set
}
