package prs

import (
	"slices"
	"strings"
)

// ByAuthor says whether login is one of authors: who opened a PR that
// Options.Authors keeps, or whose PR a review may approve. An
// empty list keeps every author.
//
// GitHub matches a login without regard to case, so "Alice" names the
// same person as "alice". An app's account is "dependabot" in GraphQL
// and "dependabot[bot]" in REST, and either spelling names it. A deleted
// account has an empty login, which no author matches.
func ByAuthor(login string, authors []string) bool {
	if len(authors) == 0 {
		return true
	}
	login = strings.TrimSuffix(login, botSuffix)
	return login != "" && slices.ContainsFunc(authors, func(a string) bool {
		return strings.EqualFold(strings.TrimSuffix(a, botSuffix), login)
	})
}

const botSuffix = "[bot]"
