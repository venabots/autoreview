package cli

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/venabots/autoreview/internal/config"
)

// githubLogin is the shape GitHub gives a login: letters, digits and
// hyphens, and no hyphen first.
var githubLogin = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9-]*$`)

// noApproveAuthors is the logins whose PRs a review never approves, from
// the file. A leading @ is dropped, as a person often writes one.
func noApproveAuthors(file config.File) ([]string, error) {
	out := make([]string, 0, len(file.Review.NoApproveAuthors))
	for _, raw := range file.Review.NoApproveAuthors {
		login := strings.TrimPrefix(strings.TrimSpace(raw), "@")
		if !githubLogin.MatchString(login) {
			return nil, fmt.Errorf("review.no_approve_authors in %s: %q is not a GitHub login", file.Path, raw)
		}
		out = append(out, login)
	}
	return out, nil
}
