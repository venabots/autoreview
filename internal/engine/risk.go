package engine

import (
	"fmt"
	"maps"

	"github.com/venabots/autoreview/internal/prs"
	"github.com/venabots/autoreview/internal/review"
)

// riskAsk is where the risk check of one head of a PR stands.
type riskAsk struct {
	head     string
	answered bool
	check    review.RiskCheck
}

// riskChecked is the risk script's answer about one head of a PR.
type riskChecked struct {
	pr    prs.PR
	check review.RiskCheck
}

func (riskChecked) input() {}

// askRisks runs the risk script on each PR on the list whose approval stops
// at a risk, once for each head. The check is part of the look, as the
// rebase question is: the look ends when every check answers, so a pass
// never starts a review whose level the script has not set. A check that
// panics answers as failed, which holds the approval: a check that never
// answered would leave the run looking forever.
func (e *Engine) askRisks(w workers, r run) run {
	if e.deps.Risk == nil || !e.cfg.Review || !r.snap.Looked {
		return r
	}
	risks := maps.Clone(r.risks)
	if risks == nil {
		risks = map[int]riskAsk{}
	}
	for _, row := range r.snap.Reviews {
		pr := row.PR
		if _, ceiling := e.levelFor(pr.Author); ceiling == review.RiskNone || risks[pr.Number].head == pr.Head {
			continue
		}
		risks[pr.Number] = riskAsk{head: pr.Head}
		r.pending++
		w.group.Go(func() {
			check, err := guarded(func() (review.RiskCheck, error) { return e.deps.Risk(w.ctx, pr), nil })
			if err != nil {
				check = review.RiskCheck{Why: err.Error()}
			}
			e.in.put(riskChecked{pr: pr, check: check})
		})
	}
	r.risks = risks
	return r
}

// riskCheckedInto keeps an answer. An answer about a head the PR has left
// is dropped: the new head is checked at the look that found it.
func riskCheckedInto(r run, in riskChecked) run {
	if r.risks[in.pr.Number].head != in.pr.Head {
		return r
	}
	risks := maps.Clone(r.risks)
	risks[in.pr.Number] = riskAsk{head: in.pr.Head, answered: true, check: in.check}
	r.risks = risks
	return r
}

// heldByRisk says the risk check keeps the approval of the PR for a person,
// and why. A check that has not answered for this head holds it too: a
// person can ask for a review before the look that checks it ends.
func (e *Engine) heldByRisk(pr prs.PR, ceiling review.Risk, risks map[int]riskAsk) (string, bool) {
	if ceiling == review.RiskNone || e.deps.Risk == nil {
		return "", false
	}
	ask := risks[pr.Number]
	lead := fmt.Sprintf("PR #%d waits for a person to approve it", pr.Number)
	switch {
	case ask.head != pr.Head || !ask.answered:
		return lead + ": its risk check has not answered for this head", true
	case !ask.check.Holds(ceiling):
		return "", false
	case ask.check.Risk == review.RiskNone:
		return withWhy(lead+": its risk check failed", ask.check.Why), true
	}
	return withWhy(fmt.Sprintf("%s: %s rated it %s, above the ceiling of %s", lead, ask.check.Script, ask.check.Risk, ceiling), ask.check.Why), true
}

func withWhy(text, why string) string {
	if why == "" {
		return text
	}
	return text + ": " + why
}
