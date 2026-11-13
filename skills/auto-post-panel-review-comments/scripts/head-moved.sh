#!/usr/bin/env bash
# Say whether a PR's head still holds only the change that was reviewed, so a
# merge of the base during a review does not cost the review its verdict.
#
# Why this exists: a panel review takes minutes, and an author often merges
# the base branch in meanwhile ("Update branch", or `git merge origin/main`).
# The head SHA then differs from the reviewed one, and a plain comparison
# withholds the verdict on a change that nobody touched. A merge can also
# carry new code, in a conflict resolution or in an edit folded into the
# merge, so "the new commit is a merge" is not enough. This script checks
# that git's own merge of the two parents gives exactly the merge's tree.
#
# Usage:
#   head-moved.sh <pr-number-or-url> <reviewed-commit-sha>
#
# It prints one word:
#
#   same        The head is the reviewed commit.
#   base-merge  The head moved only by clean merges of the base branch: the
#               reviewed commit is on the head's first-parent line, every
#               commit after it is a two-parent merge whose other parent is in
#               the base branch, and each merge's tree is the one that
#               `git merge-tree` gives with no conflict. The PR's own change
#               is the one that was reviewed.
#   moved       Anything else: a new commit, a merge with a conflict or an
#               edit, a merge of another branch, or a force-push.
#
# When it cannot tell, it prints nothing, says why on stderr and exits 1.
# Treat that as `moved`.
#
# Run it in a clone of the PR's repository: it fetches the head and the base
# branch into it. It fetches from the remote whose URL names the PR's
# repository, or else from https://<host>/<owner>/<repo>.git.
#
# Requires: gh, and git 2.38 or later (`git merge-tree --write-tree`).

set -euo pipefail

die() {
  echo "head-moved.sh: $*" >&2
  exit 1
}

if [[ $# -ne 2 ]]; then
  echo "usage: $0 <pr-number-or-url> <reviewed-commit-sha>" >&2
  exit 1
fi

pr_ref="$1"
reviewed="$2"

# A ref that starts with "-" would reach gh and git as an option.
[[ "$pr_ref" =~ ^([0-9]+|https?://.+)$ ]] || die "not a PR number or URL: '$pr_ref'"
[[ "$reviewed" =~ ^[0-9a-f]{7,40}$ ]] || die "not a commit sha: '$reviewed'"

for tool in gh git; do
  command -v "$tool" >/dev/null 2>&1 || die "missing required tool: $tool"
done

pr_line="$(gh pr view "$pr_ref" --json url,headRefOid,baseRefName \
  --jq '[.url, .headRefOid, .baseRefName] | @tsv' 2>/dev/null)" ||
  die "could not resolve PR '$pr_ref' via gh"
IFS=$'\t' read -r pr_url head_sha base_ref <<<"$pr_line"
[[ "$head_sha" =~ ^[0-9a-f]{40}$ && -n "$base_ref" ]] ||
  die "gh gave no head or base for PR '$pr_ref'"

if [[ "$head_sha" == "$reviewed"* ]]; then
  echo same
  exit 0
fi

# The base repository owns the PR number and its refs/pull/<n>/head, even
# when the head is on a fork.
[[ "$pr_url" =~ ^https?://([^/]+)/([^/]+)/([^/]+)/pull/([0-9]+)$ ]] ||
  die "could not read the repository from PR url '$pr_url'"
host="${BASH_REMATCH[1]}"
nwo="${BASH_REMATCH[2]}/${BASH_REMATCH[3]}"
number="${BASH_REMATCH[4]}"

git rev-parse --git-dir >/dev/null 2>&1 || die "not inside a git repository"
[[ "$(git rev-parse --is-shallow-repository)" == false ]] ||
  die "a shallow clone cannot show a merge's parents"

# A remote that already names the repository has this machine's auth.
nwo_lc="$(tr '[:upper:]' '[:lower:]' <<<"$nwo")"
from="https://${host}/${nwo}.git"
while read -r name url _; do
  case "$(tr '[:upper:]' '[:lower:]' <<<"$url")" in
    */"$nwo_lc" | */"$nwo_lc".git | */"$nwo_lc"/ | *:"$nwo_lc" | *:"$nwo_lc".git)
      from="$name"
      break
      ;;
  esac
done < <(git remote -v | awk '$3 == "(fetch)"')

# Without these, a fetch over HTTPS with no credential waits on a prompt
# that nobody answers.
export GIT_TERMINAL_PROMPT=0 GCM_INTERACTIVE=never

base_tip="$(git ls-remote "$from" "refs/heads/$base_ref" | cut -f1)" ||
  die "could not read $base_ref from $from"
[[ "$base_tip" =~ ^[0-9a-f]{40}$ ]] || die "$from has no branch $base_ref"
git fetch --quiet --no-tags --no-write-fetch-head "$from" \
  "refs/heads/$base_ref" "refs/pull/$number/head" >&2 ||
  die "could not fetch the head and $base_ref from $from"
git cat-file -e "$head_sha^{commit}" 2>/dev/null ||
  die "the fetch did not bring the head $head_sha"

moved() {
  echo moved
  exit 0
}

# The reviewed commit is in the clone when it is an ancestor of the head. When
# it is missing, the head was force-pushed past it.
reviewed_full="$(git rev-parse --verify --quiet "$reviewed^{commit}")" || moved
git merge-base --is-ancestor "$reviewed_full" "$head_sha" || moved

# The head's first-parent line back to the reviewed commit, oldest first.
first=1
while read -r commit; do
  read -r p1 p2 extra <<<"$(git log -1 --format=%P "$commit")"
  if ((first)); then
    # The reviewed commit must be the line's own parent, not a commit that
    # came in from the side.
    [[ "$p1" == "$reviewed_full" ]] || moved
    first=0
  fi
  [[ -n "${p2:-}" && -z "${extra:-}" ]] || moved
  git merge-base --is-ancestor "$p2" "$base_tip" || moved
  status=0
  merged="$(git merge-tree --write-tree "$p1" "$p2")" || status=$?
  case "$status" in
    0) ;;
    1) moved ;;
    *) die "git merge-tree failed (it needs git 2.38 or later)" ;;
  esac
  [[ "${merged%%$'\n'*}" == "$(git rev-parse "$commit^{tree}")" ]] || moved
done < <(git rev-list --reverse --first-parent "$reviewed_full..$head_sha")

echo base-merge
