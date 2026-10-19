package words

import "strings"

// Authors names whose PRs the review list keeps: "by @alice", "by @alice
// or @bob", "by @alice, @bob or @carol". It is empty when the list keeps
// everyone's. The view's tab and the plain list header both say it, so a
// short list never reads as every open PR.
func Authors(logins []string) string {
	if len(logins) == 0 {
		return ""
	}
	named := make([]string, len(logins))
	for i, login := range logins {
		named[i] = "@" + login
	}
	last := len(named) - 1
	if last == 0 {
		return "by " + named[0]
	}
	return "by " + strings.Join(named[:last], ", ") + " or " + named[last]
}
