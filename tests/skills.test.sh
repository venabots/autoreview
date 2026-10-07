#!/usr/bin/env bash
# The helper scripts the review skills carry, run against a fake `gh`.
#
# A review is one POST, and GitHub rejects all of it when one comment sits on
# a line outside the diff. pr-diff-lines.sh is what tells the reviewer which
# lines are inside, so a wrong answer here costs a whole review.

set -euo pipefail
# shellcheck source=helpers.sh
. "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/helpers.sh"

echo "skills"
DIFF_LINES="$TESTS_DIR/../skills/auto-post-panel-review-comments/scripts/pr-diff-lines.sh"
WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT
mkdir -p "$WORK/bin"

# Every call is logged, so a test can say which endpoint the script chose.
cat >"$WORK/bin/gh" <<'FAKE'
#!/usr/bin/env bash
printf '%s\n' "$*" >>"$FAKE_GH_LOG"
case "$*" in
  "pr view 9 --json number,url,headRefOid,baseRefOid")
    printf '%s\n' '{"number":9,"url":"https://github.com/acme/widgets/pull/9","headRefOid":"aaaa1111aaaa1111","baseRefOid":"bbbb2222bbbb2222"}'
    ;;
  "api repos/acme/widgets/pulls/9/files --paginate")
    # Two pages, as gh prints them: one array after the other.
    cat <<'JSON'
[{"filename":"src/pay.rs","patch":"@@ -10,3 +10,5 @@ fn charge() {\n context\n+added\n+added\n context\n context\n@@ -40 +42,0 @@ fn refund() {\n-removed\n@@ -60,2 +61 @@\n-old\n-old\n+new"},
 {"filename":"assets/logo.png"}]
[{"filename":"docs/a b.md","patch":"@@ -0,0 +1,2 @@\n+one\n+@@ not a hunk header"}]
JSON
    ;;
  "pr view 12 --json number,url,headRefOid,baseRefOid")
    printf '%s\n' '{"number":12,"url":"https://ghe.example.com/acme/widgets/pull/12","headRefOid":"dddd4444","baseRefOid":"eeee5555"}'
    ;;
  "api repos/acme/widgets/pulls/12/files --paginate")
    printf '%s\n' '[{"filename":"src/ghe.rs","patch":"@@ -1 +1,2 @@\n a\n+b"}]'
    ;;
  "pr view 13 --json number,url,headRefOid,baseRefOid")
    printf '%s\n' '{"number":13,"url":"not-a-pr-url","headRefOid":"ffff6666","baseRefOid":"aaaa7777"}'
    ;;
  "api repos/acme/widgets/compare/bbbb2222bbbb2222...cccc3333")
    printf '%s\n' '{"files":[{"filename":"src/old.rs","patch":"@@ -1,2 +1,3 @@\n a\n+b\n c"}]}'
    ;;
  *)
    echo "fake gh: unexpected call: $*" >&2
    exit 1
    ;;
esac
FAKE
chmod +x "$WORK/bin/gh"

run_diff_lines() {
  : >"$WORK/gh.log"
  set +e
  FAKE_GH_LOG="$WORK/gh.log" PATH="$WORK/bin:$PATH" bash "$DIFF_LINES" "$@" 2>&1
  local status=$?
  set -e
  printf '%s' "$status" >"$WORK/status"
}

tab="$(printf '\t')"

# --- The PR as it stands ----------------------------------------------------
out="$(run_diff_lines 9)"
assert_equals "the head diff exits 0" "$(cat "$WORK/status")" "0"
assert_contains "a hunk is the lines on the new side" "$out" "src/pay.rs${tab}10${tab}14"
assert_contains "a hunk with no count is one line" "$out" "src/pay.rs${tab}61${tab}61"
assert_not_contains "a hunk that only removes has no line to comment on" "$out" "src/pay.rs${tab}42"
assert_not_contains "a file with no patch has no line either" "$out" "logo.png"
assert_contains "the second page is read too" "$out" "docs/a b.md${tab}1${tab}2"
assert_equals "an added line that looks like a header is not one" \
  "$(printf '%s\n' "$out" | wc -l | tr -d ' ')" "3"
assert_contains "the head diff comes from the PR's files" \
  "$(cat "$WORK/gh.log")" "pulls/9/files"

# --- A head given by its short form is still the head -----------------------
out="$(run_diff_lines 9 aaaa1111)"
assert_contains "a short head sha reads the same diff" "$out" "src/pay.rs${tab}10${tab}14"
assert_not_contains "...and does not ask for a compare" "$(cat "$WORK/gh.log")" "compare"

# --- A reviewed commit that is no longer the head ---------------------------
out="$(run_diff_lines 9 cccc3333)"
assert_equals "an older commit exits 0" "$(cat "$WORK/status")" "0"
assert_equals "an older commit is diffed against the base" "$out" "src/old.rs${tab}1${tab}3"
assert_not_contains "...and not read from the head's files" "$(cat "$WORK/gh.log")" "pulls/9/files"

# --- A host that is not github.com --------------------------------------------
out="$(run_diff_lines 12)"
assert_equals "a GitHub Enterprise PR reads its owner and repo" "$out" "src/ghe.rs${tab}1${tab}2"
out="$(run_diff_lines 13)"
assert_equals "a url of another shape exits 1" "$(cat "$WORK/status")" "1"
assert_contains "...saying what it could not read" "$out" "could not read owner/repo from PR url 'not-a-pr-url'"
assert_not_contains "...before it calls the API" "$(cat "$WORK/gh.log")" "api "

# --- Bad input ---------------------------------------------------------------
out="$(run_diff_lines)"
assert_equals "no PR exits 1" "$(cat "$WORK/status")" "1"
assert_contains "...with the usage" "$out" "usage:"
out="$(run_diff_lines 404)"
assert_equals "a PR gh cannot find exits 1" "$(cat "$WORK/status")" "1"
assert_contains "...saying which" "$out" "could not resolve PR '404'"

finish
