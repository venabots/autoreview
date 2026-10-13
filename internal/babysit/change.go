package babysit

import (
	"fmt"
	"maps"

	"github.com/venabots/autoreview/internal/mine"
)

// Scope is what a change is about: one PR, or every PR of yours.
type Scope int

const (
	// ScopeOne is the "b" key: the selected PR.
	ScopeOne Scope = iota
	// ScopeAll is the "B" key: every PR of yours, PRs opened later included.
	ScopeAll
)

// Change is what the "b" or "B" key asked for. The view sends it and the
// engine applies it, so a press during a look is not lost.
type Change struct {
	Scope Scope
	// PR is the PR that "b" named. It is zero for ScopeAll.
	PR int
	On bool
}

// One is "b": babysit this PR, or stop.
func One(pr int, on bool) Change {
	return Change{Scope: ScopeOne, PR: pr, On: on}
}

// All is "B": babysit every PR of yours, or none.
func All(on bool) Change {
	return Change{Scope: ScopeAll, On: on}
}

// ForkError is the refusal to babysit a PR from a fork. Its branch is not on
// origin, so every fix would be refused and would still count against the
// cap.
type ForkError struct {
	PR int
}

func (e ForkError) Error() string {
	return fmt.Sprintf("PR #%d is from a fork; its branch is not on origin, so nothing can fix it here", e.PR)
}

// Toggle is the change "b" asks for on pr. A fork is refused before anything
// else: with all babysat, "b" on a fork would read as a stop, and turn
// babysitting off for PRs opened later.
func (s State) Toggle(pr mine.PR) (Change, error) {
	if pr.CrossRepo {
		return Change{}, ForkError{PR: pr.Number}
	}
	return One(pr.Number, !s.Babysat(pr.Number)), nil
}

// ToggleAll is the change "B" asks for.
func (s State) ToggleAll() Change {
	return All(!s.all)
}

// Apply is the state after c. list is your PRs as the last look found them:
// stopping one PR while all are babysat keeps the rest of them babysat, and
// a fork in it is refused.
func (s State) Apply(c Change, list []mine.PR) (State, error) {
	if c.Scope == ScopeAll {
		return s.setAll(c.On), nil
	}
	if c.On && isFork(c.PR, list) {
		return s, ForkError{PR: c.PR}
	}
	if c.On {
		return s.start(c.PR), nil
	}
	return s.stopOne(c.PR, list), nil
}

// Stop ends all babysitting. It is for turning the looking off: babysitting
// with no looks would say it babysits and never fix.
func (s State) Stop() State {
	return s.setAll(false)
}

func (s State) setAll(on bool) State {
	if !on {
		return State{}
	}
	return State{all: true, prs: s.prs, watches: s.watches}
}

func (s State) start(pr int) State {
	next := cloneSet(s.prs)
	next[pr] = true
	return State{all: s.all, prs: next, watches: s.watches}
}

func (s State) stopOne(pr int, list []mine.PR) State {
	next := cloneSet(s.prs)
	if s.all {
		for _, p := range list {
			if !p.CrossRepo {
				next[p.Number] = true
			}
		}
	}
	delete(next, pr)
	watches := maps.Clone(s.watches)
	delete(watches, pr)
	return State{prs: next, watches: watches}
}

func isFork(pr int, list []mine.PR) bool {
	for _, p := range list {
		if p.Number == pr {
			return p.CrossRepo
		}
	}
	return false
}

func cloneSet(set map[int]bool) map[int]bool {
	next := maps.Clone(set)
	if next == nil {
		next = map[int]bool{}
	}
	return next
}

// Said is what a change does, in the words the view flashes and the run
// log keeps.
func (c Change) Said() string {
	switch {
	case c.Scope == ScopeAll && c.On:
		return "babysitting all your PRs, and any you open: each is fixed when it changes and needs work"
	case c.Scope == ScopeAll:
		return "no longer babysitting your PRs"
	case c.On:
		return fmt.Sprintf("babysitting PR #%d: it is fixed when it changes and needs work", c.PR)
	}
	return fmt.Sprintf("no longer babysitting PR #%d", c.PR)
}
