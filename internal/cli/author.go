package cli

import (
	"fmt"
	"slices"
	"strings"
)

// botSuffix ends the login of an app's account in GitHub's REST answers,
// as in "dependabot[bot]". GraphQL gives the same account without it.
const botSuffix = "[bot]"

// authors reads the --author values. Each one is a login, or several
// split by commas, with or without a leading "@". Logins that differ only
// in case are one person to GitHub, so the second spelling is dropped.
//
// An empty login is refused. GitHub reports a deleted account as an empty
// login, so `--author=` would otherwise keep only the PRs of deleted
// accounts, and the run would review nothing with no sign of why.
func authors(values []string) ([]string, error) {
	var out []string
	for _, value := range values {
		if err := refuseFlagAsValue("author", value); err != nil {
			return nil, err
		}
		for part := range strings.SplitSeq(value, ",") {
			login := strings.TrimPrefix(strings.TrimSpace(part), "@")
			if !isLogin(login) {
				return nil, fmt.Errorf("--author: %q is not a GitHub login", part)
			}
			if !slices.ContainsFunc(out, func(seen string) bool { return strings.EqualFold(seen, login) }) {
				out = append(out, login)
			}
		}
	}
	return out, nil
}

// isLogin accepts what GitHub allows in a login: letters, digits and
// hyphens, with no hyphen at either end, and the underscore that an
// enterprise's managed accounts carry. An app's login may end in "[bot]".
// A typo such as "alice/x" is refused here: a login that matches no PR
// would leave the run with nothing to review.
func isLogin(s string) bool {
	s = strings.TrimSuffix(s, botSuffix)
	if s == "" || strings.HasPrefix(s, "-") || strings.HasSuffix(s, "-") {
		return false
	}
	for _, r := range s {
		if !isLoginRune(r) {
			return false
		}
	}
	return true
}

func isLoginRune(r rune) bool {
	return r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_'
}

// CheckAuthors refuses an --author that names nobody but the person
// running the sweep. Their own PRs are never reviewed, so the run would
// review nothing, and a run from cron would do that every time.
func CheckAuthors(authors []string, me string) error {
	if len(authors) == 0 || me == "" {
		return nil
	}
	for _, login := range authors {
		if !strings.EqualFold(login, me) {
			return nil
		}
	}
	return fmt.Errorf("--author names only you (@%s), and your own PRs are never reviewed: they are on the Maintain tab", me)
}
