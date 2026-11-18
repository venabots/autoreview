package cli

import (
	"fmt"
	"strings"

	"github.com/venabots/autoreview/internal/config"
	"github.com/venabots/autoreview/internal/review"
)

// approveAuthors is the logins whose PRs a review may approve, from the
// file. Empty lets a review approve every author. A leading @ is dropped,
// as a person often writes one.
func approveAuthors(file config.File) ([]string, error) {
	out := make([]string, 0, len(file.Review.ApproveAuthors))
	for _, raw := range file.Review.ApproveAuthors {
		login := strings.TrimPrefix(strings.TrimSpace(raw), "@")
		if !isLogin(login) {
			return nil, fmt.Errorf("review.approve_authors in %s: %q is not a GitHub login", file.Path, raw)
		}
		out = append(out, login)
	}
	return out, nil
}

// approveOthersUpTo is the risk ceiling for the PRs of the authors the file
// does not name, from the file. A ceiling with no list of authors limits
// nobody, and kept in silence it would let a person think a migration
// waits for them, so it is an error.
func approveOthersUpTo(file config.File, named []string) (review.Risk, error) {
	if file.Review.ApproveOthersUpTo == nil {
		return review.RiskNone, nil
	}
	ceiling, err := review.ParseCeiling(*file.Review.ApproveOthersUpTo)
	if err != nil {
		return review.RiskNone, fmt.Errorf("review.approve_others_up_to in %s: %w", file.Path, err)
	}
	if ceiling != review.RiskNone && len(named) == 0 {
		return review.RiskNone, fmt.Errorf("review.approve_others_up_to in %s is %s, but review.approve_authors names nobody, so a review approves every author at any risk; name the authors whose PRs it approves at any risk", file.Path, ceiling)
	}
	return ceiling, nil
}
