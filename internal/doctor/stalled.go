package doctor

import (
	"cmp"
	"slices"

	"github.com/venabots/autoreview/internal/ledger"
)

// verdicts are the decisions that end a review. Anything else, a comment
// review or nothing at all, leaves the PR waiting for another.
var verdicts = []string{"approved", "changes-requested"}

type prKey struct {
	repo string
	pr   uint64
}

// stalled is each PR whose latest review gave no verdict with a panelist
// missing, newest first. Only the latest review counts: a PR that got a
// verdict on its second try is not waiting for anything.
func stalled(records []ledger.Record) []Stalled {
	latest := map[prKey]ledger.Record{}
	for _, r := range records {
		if r.Repo == nil || r.PR == nil {
			continue
		}
		k := prKey{*r.Repo, *r.PR}
		if prev, ok := latest[k]; !ok || r.At >= prev.At {
			latest[k] = r
		}
	}
	var out []Stalled
	for k, r := range latest {
		if s, ok := stalledReview(k, r); ok {
			out = append(out, s)
		}
	}
	slices.SortFunc(out, func(a, b Stalled) int {
		return cmp.Or(cmp.Compare(b.At, a.At), cmp.Compare(a.Repo, b.Repo), cmp.Compare(a.PR, b.PR))
	})
	return out
}

// stalledReview is the review as a stalled PR, or false when it gave a
// verdict or its whole panel answered. A comment review from a full panel
// is a decision the panel made, not a fault.
func stalledReview(k prKey, r ledger.Record) (Stalled, bool) {
	decision := "none"
	if r.Decision != nil {
		decision = *r.Decision
	}
	if slices.Contains(verdicts, decision) {
		return Stalled{}, false
	}
	n := 0
	for _, p := range r.Panel {
		if answered(p) {
			n++
		}
	}
	if len(r.Panel) > 0 && n == len(r.Panel) {
		return Stalled{}, false
	}
	return Stalled{Repo: k.repo, PR: k.pr, At: r.At, Decision: decision, Seated: len(r.Panel), Answered: n}, true
}
