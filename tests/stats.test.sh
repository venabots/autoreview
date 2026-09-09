#!/usr/bin/env bash
# autoreview stats: the ledger read back per model, and the transcript import.

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
assert_contains "...and how to read the past in" "$out" "autoreview stats --import"

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

# --- Import from transcripts -------------------------------------------------
# A transcript in the shape Claude Code writes: the review text, then the
# fenced trailer, in assistant messages of a session started by /auto-review.
mkdir -p "$SANDBOX/claude/projects/-w-gadgets"
cat >"$SANDBOX/claude/projects/-w-gadgets/11111111-1111-4111-8111-111111111111.jsonl" <<'EOF'
{"type":"user","sessionId":"11111111-1111-4111-8111-111111111111","cwd":"/w/gadgets","uuid":"u0","timestamp":"2026-08-20T10:00:00.000Z","message":{"role":"user","content":"<command-message>auto-review</command-message>\n<command-name>/auto-review</command-name>\n<command-args>42</command-args>"}}
{"type":"assistant","sessionId":"11111111-1111-4111-8111-111111111111","cwd":"/w/gadgets","uuid":"u1","timestamp":"2026-08-20T10:05:00.000Z","message":{"role":"assistant","model":"claude-fable-5","content":[{"type":"text","text":"### Risk\nHIGH\n### must-fix\n- [HIGH] src/pay.ts:10 — double charge. Fix: guard. Flagged by 2: codex (gpt-5.5), opencode (xai/grok-4.6)\n### polish\n- [LOW] src/a.ts:1 — nit. Flagged by: codex (gpt-5.5)"}]}}
{"type":"assistant","sessionId":"11111111-1111-4111-8111-111111111111","cwd":"/w/gadgets","uuid":"u2","timestamp":"2026-08-20T10:06:00.000Z","message":{"role":"assistant","model":"claude-fable-5","content":[{"type":"text","text":"Posted.\n\n```autoreview\n{\"decision\":\"changes-requested\",\"risk\":\"HIGH\",\"findings\":{\"must_fix\":1,\"should_fix\":0,\"polish\":1},\"panel\":[{\"name\":\"codex\",\"model\":\"gpt-5.5\",\"ok\":true,\"findings\":3,\"top\":\"HIGH\"},{\"name\":\"opencode\",\"model\":\"xai/grok-4.6\",\"ok\":true,\"findings\":1,\"top\":\"HIGH\"}]}\n```"}]}}
EOF
out="$(run_stats --import)"
assert_equals "an import reports cleanly" "$(last_status)" "0"
assert_contains "it says what it read" "$out" "imported 1 review from 1 transcript under $SANDBOX/claude/projects (0 already recorded)"
assert_contains "the imported review joins the count" "$out" "2 reviews from 2026-08-20 to "
assert_contains "...from its own repo" "$out" "across 2 repos"
assert_contains "a provider prefix folds into the model" "$out" "grok-4.6"
assert_not_contains "...leaving no raw spelling behind" "$out" "xai/"
assert_contains "the imported decision is counted" "$out" "1 changes-requested"

out="$(run_stats --import)"
assert_contains "a second import adds nothing" "$out" "imported 0 reviews from 1 transcript"
assert_contains "...because it was already recorded" "$out" "(1 already recorded)"
assert_contains "...and the count holds" "$out" "2 reviews from"

out="$(run_stats --json --repo gadgets)"
assert_equals "the imported run carries its PR and session" \
  "$(printf '%s' "$out" | jq -r '.cohorts[] | select(.key.model == "gpt-5.5") | "kept=\(.kept) unique=\(.kept_unique) high=\(.kept_high) raw=\(.raw_findings)"')" \
  "kept=2 unique=1 high=1 raw=3"
assert_equals "...and grok is credited with the shared finding only" \
  "$(printf '%s' "$out" | jq -r '.cohorts[] | select(.key.model == "grok-4.6") | "kept=\(.kept) unique=\(.kept_unique)"')" \
  "kept=1 unique=0"

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
