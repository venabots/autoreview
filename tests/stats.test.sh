#!/usr/bin/env bash
# autoreview stats: the ledger read back per model.

set -euo pipefail
# shellcheck source=helpers.sh
. "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/helpers.sh"

echo "stats"
setup_sandbox
trap teardown_sandbox EXIT

run_stats() {
  set +e
  ( cd "$SANDBOX/repo" && "$AUTOREVIEW" stats "$@" 2>&1 )
  local status=$?
  set -e
  printf '%s' "$status" >"$SANDBOX/out/status"
}

# --- Nothing recorded yet ---------------------------------------------------
out="$(run_stats)"
assert_equals "an empty ledger is not an error" "$(last_status)" "0"
assert_contains "...and says where the history will go" "$out" "no reviews recorded yet in $AUTOREVIEW_LEDGER"

out="$(run_stats --json)"
assert_equals "an empty ledger under --json is still JSON" \
  "$(printf '%s' "$out" | jq -r '.overview.runs')" "0"

# --- A review, then the numbers ---------------------------------------------
# The fake reviewer reports a two-model panel for PR #8: codex found one LOW
# finding, claude found nothing.
run_autoreview --auto >/dev/null
FAKE_CLAUDE_TRAILER='8' run_autoreview --auto >/dev/null
out="$(run_stats)"
assert_equals "a recorded review reports cleanly" "$(last_status)" "0"
assert_contains "the headline counts the reviews" "$out" "1 review on "
assert_contains "...and the repos" "$out" "across 1 repo"
assert_contains "the columns are named" "$out" "MODEL"
assert_contains "...through the last" "$out" "SAMPLE"
assert_contains "each model gets a row" "$out" "gpt-5.5"
assert_contains "...on its backend" "$out" "codex"
assert_contains "...and the other model too" "$out" "claude-opus-4.7"
assert_contains "one answered launch is 100% with a wide interval" "$out" "100% (21-100)"
assert_contains "a run this small is thin" "$out" "thin"
assert_contains "the decisions are summed" "$out" "decisions: 1 commented"
assert_contains "...and the risk" "$out" "risk: 1 LOW"
assert_contains "the ledger is named" "$out" "ledger: $AUTOREVIEW_LEDGER"

out="$(run_stats --json)"
assert_equals "--json is valid JSON with the cohorts" \
  "$(printf '%s' "$out" | jq -r '.cohorts | map(.key.model) | sort | join(",")')" \
  "claude-opus-4.7,gpt-5.5"
assert_equals "...carrying the ratios as numerator and denominator" \
  "$(printf '%s' "$out" | jq -r '.cohorts[] | select(.key.model == "gpt-5.5") | "\(.availability.numerator)/\(.availability.denominator) raw=\(.raw_findings)"')" \
  "1/1 raw=1"

# --- Filters ------------------------------------------------------------------
out="$(run_stats --repo widgets)"
assert_contains "--repo matches on a substring" "$out" "gpt-5.5"
out="$(run_stats --repo gadgets)"
assert_contains "...and says when nothing matches" "$out" "no reviews match (1 review recorded"
out="$(run_stats --since 2999-01-01)"
assert_contains "--since a future date matches nothing" "$out" "no reviews match"
out="$(run_stats --since 7d)"
assert_contains "--since a span includes today's review" "$out" "gpt-5.5"

# --- Bad input ----------------------------------------------------------------
out="$(run_stats --bogus)"
assert_equals "an unknown flag exits 1" "$(last_status)" "1"
assert_contains "...naming it" "$out" "unknown arg: --bogus"
assert_contains "...with the help" "$out" "Usage: autoreview stats"
out="$(run_stats --since soon)"
assert_equals "a bad --since exits 1" "$(last_status)" "1"
assert_contains "...saying what it wanted" "$out" "--since expects a span like 7d or 2w"
out="$(run_stats --help)"
assert_equals "--help exits 0" "$(last_status)" "0"
assert_contains "...and explains the columns" "$out" "KEEP RATE"
out="$(AUTOREVIEW_LEDGER=off run_stats)"
assert_equals "a ledger that is off is refused" "$(last_status)" "1"
assert_contains "...saying how to read one anyway" "$out" "pass --ledger PATH"

printf '\n%d passed, %d failed\n' "$pass_count" "$fail_count"
[[ "$fail_count" -eq 0 ]]
