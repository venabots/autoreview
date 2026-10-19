package prs

import (
	"context"

	"github.com/venabots/autoreview/internal/gh"
)

// queryLimit is how many PRs one call asks for. A repo with more than this
// returns a full page and the true total is unknown, which is why a count
// that reaches it is reported as "50+" rather than as fact.
const queryLimit = 50

// Query is one GraphQL call for the open PRs, enough activity to rank
// engagement, and the checks on the head commit. That last is its own aliased
// field rather than a line in the commits list: the rollup is wanted for one
// commit, and asking for it on a hundred would make the query pay for
// ninety-nine answers it throws away.
//
// The branch names and the commit oids are what find a PR sitting on top of
// another open PR: the names catch a declared stack, and the oids catch a
// branch cut from another PR's branch that still says "base: main". Both ride
// along on fields this one call was making anyway. A PR with more than 100
// commits loses its oldest, which can only ever hide an overlap, never invent
// one.
const Query = `
      query($owner:String!, $name:String!) {
        repository(owner:$owner, name:$name) {
          defaultBranchRef { name }
          pullRequests(states:OPEN, first:50, orderBy:{field:UPDATED_AT, direction:DESC}) {
            nodes {
              number
              title
              isDraft
              updatedAt
              reviewDecision
              headRefOid
              baseRefName
              headRefName
              isCrossRepository
              mergeable
              author { login }
              comments(last:100) { nodes { author { login } updatedAt } }
              reviews(last:100)  { nodes { author { login } submittedAt } }
              commits(last:100)  { nodes { commit { oid committedDate author { user { login } } } } }
              headCommit: commits(last:1) { nodes { commit { statusCheckRollup { state } } } }
            }
          }
        }
      }`

// Fetch asks GitHub for the open PRs and returns the answer unread, so that a
// caller can keep the bytes and read them again with different options
// without a second call. The argv is v1's, token for token.
func Fetch(ctx context.Context, run gh.Runner, owner, name string) ([]byte, error) {
	return run(ctx, "api", "graphql", "-F", "owner="+owner, "-F", "name="+name, "-f", "query="+Query)
}
