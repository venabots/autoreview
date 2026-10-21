package prs

import "strings"

// retryPrefixes and retrySuffixes are the first lines of a review that gave
// no verdict for a reason that can pass: a panelist that did not answer, a
// tracker that could not be reached, a panel that did not run, or a head
// that moved while the review read it. Older copies of the skills wrote some
// of them differently, and a PR one of them left behind must not wait for
// ever, so the older lines are here too.
//
// A deliberate "no decision" is not here: a post-only review, a draft, your
// own PR, optional comments still open, or a panel too small for a verdict
// because too few agents are installed. Another review would say the same.
var (
	retryPrefixes = []string{
		"No decision: only ", // an older skill: "only 2 of 4 reviewers answered."
		"No decision: some findings could not be filed.",
		"No decision: the new commits need a panel review, and it did not run.",
		"No decision: the head moved during the ",
	}
	retrySuffixes = []string{
		"so I'm withholding approval for now.",
	}
)

// undecided says a review body gave no verdict for a reason that can pass.
// Only the first line counts: the skills fix it, and the rest is findings.
func undecided(body string) bool {
	first, _, _ := strings.Cut(strings.TrimSpace(body), "\n")
	first = strings.TrimSpace(first)
	for _, p := range retryPrefixes {
		if strings.HasPrefix(first, p) {
			return true
		}
	}
	for _, s := range retrySuffixes {
		if strings.HasSuffix(first, s) {
			return true
		}
	}
	return false
}
