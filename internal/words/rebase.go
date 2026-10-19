package words

import (
	"fmt"

	"github.com/venabots/autoreview/internal/prs"
	"github.com/venabots/autoreview/internal/rebase"
)

// Rebase is where asking Dependabot to rebase a PR stands, in a few words,
// for a row that waits on it.
func Rebase(s rebase.State) string {
	switch s {
	case rebase.Checking:
		return "checking for a rebase"
	case rebase.WouldAsk:
		return "would ask Dependabot to rebase"
	case rebase.OthersCommitted:
		return "needs a person"
	}
	return "asked Dependabot to rebase"
}

// RebaseNote is the line a run logs when it learns where asking stands, and
// empty for a state that says nothing new: a check under way, or a base
// that has not moved.
func RebaseNote(pr prs.PR, s rebase.State) string {
	switch s {
	case rebase.Asked:
		return fmt.Sprintf("asked Dependabot to rebase PR #%d: %s", pr.Number, RebaseReason(pr))
	case rebase.WouldAsk:
		return fmt.Sprintf("would ask Dependabot to rebase PR #%d: %s (--no-post)", pr.Number, RebaseReason(pr))
	case rebase.AskedBefore:
		return fmt.Sprintf("PR #%d waits for Dependabot: you asked it to rebase already", pr.Number)
	case rebase.OthersCommitted:
		return fmt.Sprintf("PR #%d needs a person: somebody other than Dependabot committed to it, so Dependabot will not rebase it", pr.Number)
	}
	return ""
}

// RebaseReason is why a Dependabot PR needs a rebase.
func RebaseReason(pr prs.PR) string {
	if pr.Conflicts {
		return "it conflicts with " + pr.Base
	}
	return "its checks fail and " + pr.Base + " has moved on"
}
