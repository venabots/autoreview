package engine

import (
	"fmt"
	"maps"
	"slices"

	"github.com/venabots/autoreview/internal/prs"
	"github.com/venabots/autoreview/internal/queue"
	"github.com/venabots/autoreview/internal/rebase"
	"github.com/venabots/autoreview/internal/words"
)

// rebaseAnswered is where asking about one head of a PR stands.
type rebaseAnswered struct {
	pr    prs.PR
	state rebase.State
	err   error
}

func (rebaseAnswered) input() {}

// askRebases asks about each stale Dependabot PR on the list whose head the
// run has not asked about. The question is part of the look: the look ends
// when every one is answered, so a pass never reviews a PR that was about
// to be held, and a run that ends after one look does not end before its
// comment is posted.
//
// A PR a person named is left out: the person asked for its review.
func (e *Engine) askRebases(w workers, r run) run {
	if e.deps.Rebase == nil || !e.cfg.Review || !r.snap.Looked {
		return r
	}
	asks := maps.Clone(r.asks)
	if asks == nil {
		asks = map[int]queue.Ask{}
	}
	for _, row := range r.snap.Reviews {
		pr := row.PR
		if !rebase.Needs(pr) || slices.Contains(e.cfg.Only, pr.Number) || asks[pr.Number].Head == pr.Head {
			continue
		}
		asks[pr.Number] = queue.Ask{Head: pr.Head, State: rebase.Checking}
		r.pending++
		w.group.Go(func() {
			state, err := e.deps.Rebase(w.ctx, pr)
			e.in.put(rebaseAnswered{pr: pr, state: state, err: err})
		})
	}
	r.asks = asks
	return e.regate(r)
}

// rebaseAnswered takes an answer, and says what it means once. An answer
// about a head the PR has left is dropped: the question for the new head
// is asked at the look that found it.
func (e *Engine) rebaseAnswered(r run, in rebaseAnswered) run {
	if r.asks[in.pr.Number].Head != in.pr.Head {
		return r
	}
	asks := maps.Clone(r.asks)
	asks[in.pr.Number] = queue.Ask{Head: in.pr.Head, State: in.state}
	r.asks = asks
	switch {
	case in.err != nil:
		e.journal(Noted{Text: fmt.Sprintf("could not ask Dependabot to rebase PR #%d, so it is treated as usual: %v", in.pr.Number, in.err)})
	case words.RebaseNote(in.pr, in.state) != "":
		e.journal(Noted{Text: words.RebaseNote(in.pr, in.state)})
	}
	return e.regate(r)
}
