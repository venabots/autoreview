package prs

import (
	"context"
	"strings"

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
// one. The stack fields are GitHub's own stacks, which need neither.
const Query = queryHead + stackFields + queryTail

// QueryWithoutStacks is Query for a server whose schema has no stacks, such
// as an older GitHub Enterprise Server. GraphQL refuses a whole query that
// names one field the schema lacks, so asking for stacks there would cost
// the whole list.
const QueryWithoutStacks = queryHead + queryTail

const queryHead = `
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
              headCommit: commits(last:1) { nodes { commit { statusCheckRollup { state } } } }`

const stackFields = `
              stack { number size }
              stackEntry { position }`

const queryTail = `
            }
          }
        }
      }`

// Fetch asks GitHub for the open PRs and returns the answer unread, so that a
// caller can keep the bytes and read them again with different options
// without a second call. The argv is v1's in shape; the query asks for
// GitHub's stacks as well. A server that has none is asked once more
// without them.
func Fetch(ctx context.Context, run gh.Runner, owner, name string) ([]byte, error) {
	raw, err := fetchWith(ctx, run, owner, name, Query)
	if err != nil && lacksStacks(err) {
		return fetchWith(ctx, run, owner, name, QueryWithoutStacks)
	}
	return raw, err
}

func fetchWith(ctx context.Context, run gh.Runner, owner, name, query string) ([]byte, error) {
	return run(ctx, "api", "graphql", "-F", "owner="+owner, "-F", "name="+name, "-f", "query="+query)
}

// lacksStacks says the server refused the query for a stack field it does
// not have. GitHub words that as "Field 'stack' doesn't exist on type
// 'PullRequest'", and gh passes the line on. Any other refusal is not asked
// again: a rate limit or an outage would only fail twice.
func lacksStacks(err error) bool {
	msg := err.Error()
	return strings.Contains(msg, "Field 'stack") && strings.Contains(msg, "doesn't exist on type 'PullRequest'")
}
