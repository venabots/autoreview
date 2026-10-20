package mine

import (
	"context"
	"fmt"
	"strings"

	"github.com/venabots/autoreview/internal/gh"
)

// The Maintain tab can hold Dependabot's PRs beside your own. Dependabot
// keeps its PRs up to date, but it never fixes a bump that breaks the
// build: that PR waits for a person for as long as nobody looks.
//
// Its PRs come from a second search with the same fields as yours, plus
// who wrote each commit. Dependabot stops rebasing a PR as soon as anybody
// else pushes to it, so a commit that is not Dependabot's means the PR is
// ours to keep up to date from then on.

// dependabotAuthor is the name Dependabot writes its commits under.
const dependabotAuthor = "dependabot[bot]"

// BotQuery is Query with the commit authors added. Your own search keeps
// v1's fields, so the authors are asked for only here.
var BotQuery = strings.Replace(Query,
	"              headCommit:",
	"              commits(last:50) { nodes { commit { author { name } } } }\n              headCommit:",
	1)

// BotTerms is the search that finds Dependabot's open PRs in one repo.
func BotTerms(owner, name string) string {
	return fmt.Sprintf("repo:%s/%s is:pr is:open author:app/dependabot", owner, name)
}

// ListWith reads your open PRs and, when bots is set, Dependabot's too, all
// ranked as one list. A failed bot search fails the read: a list without
// the bots would read as if every one of them had merged.
func ListWith(ctx context.Context, run gh.Runner, owner, name string, bots bool) ([]PR, error) {
	yours, err := List(ctx, run, owner, name)
	if err != nil || !bots {
		return yours, err
	}
	raw, err := run(ctx, "api", "graphql", "-f", "q="+BotTerms(owner, name), "-f", "query="+BotQuery)
	if err != nil {
		return nil, err
	}
	theirs, err := ParseBots(raw)
	if err != nil {
		return nil, err
	}
	return Ranked(append(yours, theirs...)), nil
}

// ParseBots is Dependabot's PRs out of a search response, each marked as a
// bot's, and as pushed to when one of its commits is somebody else's.
func ParseBots(raw []byte) ([]PR, error) {
	nodes, err := parseNodes(raw)
	if err != nil {
		return nil, err
	}
	list := make([]PR, 0, len(nodes))
	for _, n := range nodes {
		pr := n.pr()
		pr.Bot = true
		pr.OthersPushed = n.othersPushed()
		list = append(list, pr)
	}
	return Ranked(list), nil
}

func (n node) othersPushed() bool {
	for _, c := range n.Commits.Nodes {
		if c.Commit.Author != nil && c.Commit.Author.Name != dependabotAuthor {
			return true
		}
	}
	return false
}
