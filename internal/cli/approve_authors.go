package cli

import (
	"fmt"
	"strings"

	"github.com/venabots/autoreview/internal/config"
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
