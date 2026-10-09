#!/usr/bin/env bash
# Print the lines of a PR that accept an inline review comment, so this skill
# can tell which findings go into the review's `comments[]` and which go into
# its body (see SKILL.md "Which findings can be inline").
#
# Why this exists: the skill submits one review, in one POST. GitHub rejects
# the whole POST with HTTP 422 when a single comment sits on a line outside
# the diff. One misplaced finding would then cost every other comment, so the
# lines are checked before the review is built.
#
# A line accepts a comment when it is inside a diff hunk. This script prints
# one row per hunk, for the new (RIGHT) side, which is the side this skill
# comments on:
#
#   <path><TAB><first-line><TAB><last-line>
#
# A single-line comment needs its `line` inside one row for its path. A
# multi-line comment needs `start_line` and `line` inside the SAME row: GitHub
# does not accept a range that crosses two hunks.
#
# A file with no rows has no line that accepts a comment: a binary file, a
# file too large for GitHub to send a patch, or a file where the PR only
# removes lines.
#
# Usage:
#   pr-diff-lines.sh <pr-number-or-url> [<reviewed-commit-sha>]
#
# Pass the reviewed commit when the caller pinned one. When it is the PR head,
# or absent, the rows come from the PR's own file list. When the head moved
# after the review, the rows come from a compare of the base against the
# reviewed commit, because the review's comments anchor to that commit and
# GitHub checks them against its diff. A compare lists 300 files at most.
#
# Requires: gh, jq.

set -euo pipefail

if [[ $# -lt 1 || $# -gt 2 ]]; then
  echo "usage: $0 <pr-number-or-url> [<reviewed-commit-sha>]" >&2
  exit 1
fi

pr_ref="$1"
reviewed_sha="${2:-}"

for tool in gh jq; do
  if ! command -v "$tool" >/dev/null 2>&1; then
    echo "pr-diff-lines.sh: missing required tool: $tool" >&2
    exit 1
  fi
done

pr_json="$(gh pr view "$pr_ref" --json number,url,headRefOid,baseRefOid 2>/dev/null || true)"
if [[ -z "$pr_json" ]]; then
  echo "pr-diff-lines.sh: could not resolve PR '$pr_ref' via gh" >&2
  exit 1
fi

pr_number="$(jq -r '.number' <<<"$pr_json")"
pr_url="$(jq -r '.url' <<<"$pr_json")"
head_sha="$(jq -r '.headRefOid' <<<"$pr_json")"
base_sha="$(jq -r '.baseRefOid' <<<"$pr_json")"

# Owner/repo come from the URL: the base repo owns the PR number, and it is
# where a review is posted even when the head is a fork. Any host matches, so
# a GitHub Enterprise URL resolves too. A URL of another shape stops here,
# before it becomes an API path that fails with a message about nothing.
owner_repo="$(printf '%s' "$pr_url" | sed -E 's|https?://[^/]+/([^/]+)/([^/]+)/pull/.*|\1/\2|')"
if [[ ! "$owner_repo" =~ ^[^/]+/[^/]+$ ]]; then
  echo "pr-diff-lines.sh: could not read owner/repo from PR url '$pr_url'" >&2
  exit 1
fi

# One row per hunk. A hunk header is the only patch line that starts with
# "@@": every content line starts with a space, "+", "-" or "\". A header with
# no count is one line, and a count of 0 is a hunk that only removes lines.
read -r -d '' rows <<'JQ' || true
select(.patch != null)
| .filename as $path
| .patch
| split("\n")[]
| select(startswith("@@"))
| capture("^@@ -[0-9]+(,[0-9]+)? \\+(?<start>[0-9]+)(,(?<count>[0-9]+))? @@")
| (.start | tonumber) as $start
| ((.count // "1") | tonumber) as $count
| select($count > 0)
| [$path, $start, $start + $count - 1]
| @tsv
JQ

# A short sha still names the head: reviews are often quoted by their first
# eight characters.
if [[ -z "$reviewed_sha" || "$head_sha" == "$reviewed_sha"* ]]; then
  gh api "repos/$owner_repo/pulls/$pr_number/files" --paginate | jq -r ".[] | $rows"
else
  gh api "repos/$owner_repo/compare/$base_sha...$reviewed_sha" | jq -r ".files[] | $rows"
fi
