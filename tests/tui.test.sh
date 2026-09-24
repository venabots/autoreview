#!/usr/bin/env bash
# tui: autoreview --tui on a terminal. The full-screen view draws on the
# alternate screen and sends the plain lines to the run log while it is up,
# so this file reads both: what reached the terminal, and what the log kept.
# Every run presses q, or the driver would wait out its timeout.

set -euo pipefail
# shellcheck source=helpers.sh
. "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/helpers.sh"

echo "tui"
if ! command -v python3 >/dev/null 2>&1; then
  echo "  skip  python3 not installed; nothing here can hold a pty"
  finish
  exit 0
fi
setup_sandbox
trap teardown_sandbox EXIT

# Run autoreview --tui under the pty driver. The driver's own arguments come
# first; autoreview's follow the --.
run_tui() {
  local driver=()
  while [[ $# -gt 0 && "$1" != "--" ]]; do
    driver+=("$1")
    shift
  done
  shift
  reset_spawn_log
  rm -rf "$SANDBOX/out/logs"
  : >"$SANDBOX/out/web"
  set +e
  python3 "$TESTS_DIR/pty.py" --timeout 40 --cols 120 --rows 30 --out "$SANDBOX/out/pty" \
    ${driver[@]+"${driver[@]}"} -- \
    bash -c 'cd "$1" && TERM=xterm-256color "$2" --log-dir "$3" --tui "${@:4}"; echo "autoreview-exit=$?"; stty -a' \
    _ "$SANDBOX/repo" "$AUTOREVIEW" "$SANDBOX/out/logs" "$@"
  set -e
  cat "$SANDBOX/out/pty"
}

# What the run wrote to its log while the view was up.
run_log() {
  cat "$SANDBOX"/out/logs/run-*/autoreview.log 2>/dev/null || true
}

tty_is_cooked() {
  local settings
  settings="$(tail -c 2000 "$SANDBOX/out/pty" | LC_ALL=C tr '\r' '\n')"
  [[ "$settings" == *" icanon"* && "$settings" != *"-icanon"* ]] || return 1
  [[ "$settings" == *" echo "* || "$settings" == *" echo"$'\n'* ]] || return 1
  [[ "$settings" != *" -echo "* ]]
}

check_cooked() {
  if tty_is_cooked; then
    ok "$1"
  else
    not_ok "$1" "stty -a after the run: $(tail -c 300 "$SANDBOX/out/pty" | LC_ALL=C tr '\r\n' '  ')"
  fi
}

# Every open PR seen: nothing for the sweep to do, which is the state the
# view is most worth opening in. Changes the fixture for good, so the cases
# that need actionable PRs come first.
seen_everything() {
  jq '.data.repository.pullRequests.nodes |= map(.comments = {"nodes":[{"author":{"login":"me"},"updatedAt":"2026-08-20T10:00:00Z"}]})' \
    "$SANDBOX/fixtures/prs.json" >"$SANDBOX/fixtures/prs.next"
  mv "$SANDBOX/fixtures/prs.next" "$SANDBOX/fixtures/prs.json"
}

# How many reviews of PR $1 the fake dash-p was asked for.
reviews_of() {
  grep -cE -- "-- (/auto-review|/recheck-pr) $1\$" "$CLAUDE_LOG" 2>/dev/null || true
}

# --- One pass: the screen, the log, and the summary after it ---------------
# The pass is over in a second or so. The view stays up, saying so, until q.
out="$(FAKE_CLAUDE_TRAILER=9 run_tui --key 3.0:j --key 4.0:q --)"
assert_contains "a one-pass run exits 0" "$out" "autoreview-exit=0"
assert_contains "the view opens on the alternate screen" "$out" $'\e[?1049h'
assert_contains "...and leaves it" "$out" $'\e[?1049l'
assert_contains "a row says what the run did about the PR" "$out" "no verdict"
assert_contains "the detail pane shows the last review" "$out" "LAST"
assert_contains "...and why it is not approved yet" "$out" "retried"
assert_contains "...and how to reopen it" "$out" "--resume"
assert_contains "the footer says the run is over" "$out" "quits"
assert_contains "the summary prints after the view closes" "$out" "╭"
assert_contains "...and names the run directory" "$out" "logs: $SANDBOX/out/logs/run-"
assert_not_contains "the plain lines stay off the screen" "$out" "start   #9"
assert_contains "...and go to the run log" "$(run_log)" "start   #9 @alice (reviewing)"
assert_contains "...with the pass summary" "$(run_log)" "SESSION"
check_cooked "the terminal is given back cooked"

# Under --no-post every VERDICT reads "nothing posted". The line that says
# why goes under the whole-run summary, not only into the log.
out="$(run_tui --key 3.0:q -- --no-post)"
assert_contains "--no-post says why nothing landed, under the summary" "$out" \
  "nothing was posted to any PR; the reviews are in $SANDBOX/out/logs/run-"

# --- r opens the review in a new tab, o opens the PR ----------------------
# The sandbox drives the cmux spawner, whose fake records the command a tab
# was sent. The fake gh records a --web call.
out="$(run_tui --key 3.0:r --key 3.5:o --key 4.5:q --)"
assert_contains "r opens a tab" "$(spawned_labels)" "Resume"
assert_contains "...running the review's session in its repo" "$(cat "$SPAWN_LOG")" "&& claude --resume "
assert_contains "o opens the PR in the browser" "$(cat "$SANDBOX/out/web")" "view"
assert_contains "...for this repo" "$(cat "$SANDBOX/out/web")" "--web --repo acme/widgets"
assert_contains "the run still exits 0" "$out" "autoreview-exit=0"

# --- x x stops a running review -------------------------------------------
# Both reviews sleep. Two presses of x stop the selected one; q twice quits
# with the other still running, which takes the interrupt path.
out="$(FAKE_CLAUDE_SLEEP=8 run_tui --key 2.0:x --key 2.3:x --key 4.0:q --key 4.3:q --)"
assert_contains "the stopped review fails as stopped" "$(run_log)" "FAILED  #9 (stopped"
assert_contains "...and says who stopped it" "$(run_log)" "stopped the review of PR #9"
assert_contains "quitting mid-pass interrupts" "$out" "interrupted; stopping running reviews"
assert_contains "...and exits 130" "$out" "autoreview-exit=130"
assert_contains "...with the summary" "$out" "failed (stopped)"
check_cooked "the terminal is given back after an interrupt"
sleep 0.5
if pgrep -f "$FAKE_SLEEP_TAG" >/dev/null 2>&1; then
  not_ok "no stopped review survives" "a reviewer's child is still running"
  pkill -f "$FAKE_SLEEP_TAG" >/dev/null 2>&1 || true
else
  ok "no stopped review survives"
fi

# --- A watch run: reviewed PRs wait, and R reviews one now -----------------
# After the first pass both PRs rest for the babysit interval. The first row
# is a resting PR; R wakes the run and reviews it again at once.
out="$(run_tui --key 3.0:R --key 7.0:q -- --watch=1)"
assert_contains "a reviewed PR rests under a watch run" "$out" "· rest"
assert_contains "...resting" "$out" "resting after its review"
assert_equals "R reviews it again at once" "$(reviews_of 9)" "2"
assert_equals "...and only it" "$(reviews_of 8)" "1"
assert_contains "q ends a watch run" "$out" "autoreview-exit=130"
check_cooked "the terminal is given back after a watch run"

# --- A babysit run: R reviews one PR now, the rest keep their interval ------
# The fixture never changes, so after the first pass both PRs are queued for
# the next one, which waits out the interval first. R takes the first of
# them through on its own.
out="$(run_tui --key 3.0:R --key 7.0:q -- --babysit=1)"
assert_contains "the next pass's PRs are listed as waiting for it" "$out" "next pass"
assert_equals "R reviews the asked-for PR at once" "$(reviews_of 9)" "2"
assert_equals "...and the rest keep their interval" "$(reviews_of 8)" "1"
assert_contains "q ends a babysit run" "$out" "autoreview-exit=130"

# --- w turns the looking for work on and off -------------------------------
# A one-shot run starts watching when w is pressed, and stops when it is
# pressed again, which leaves it waiting for q like any run with nothing
# left to do.
out="$(run_tui --key 3.0:w --key 6.0:w --key 8.0:q --)"
assert_contains "w starts the run watching" "$(run_log)" "watching: looking for work every 2m"
assert_contains "...and it polls" "$(run_log)" "next check in 2m"
assert_contains "...and the view says so" "$out" "watching"
assert_contains "w again stops it" "$(run_log)" "no longer looking for work"
assert_contains "...leaving the run waiting for q" "$out" "autoreview-exit=0"
check_cooked "the terminal is given back after a toggled run"

# --- A quiet repo: the view opens anyway ----------------------------------
# The sweep has nothing to review, which without --tui is a one-line exit.
# The view opens on it instead: every open PR is there, saying why it is
# being left alone, and R reviews one on the spot.
seen_everything
out="$(run_tui --key 3.0:R --key 8.0:q --)"
assert_contains "a quiet repo has nothing for the sweep" "$out" "no NEW or UPDATED PRs to review"
assert_contains "...and the view opens all the same" "$out" $'\e[?1049h'
assert_contains "...listing the PRs it is leaving alone" "$out" "seen"
assert_contains "...and saying so in the footer" "$out" "nothing to review"
assert_equals "R reviews one of them on the spot" "$(reviews_of 9)" "1"
assert_equals "...and only that one" "$(reviews_of 8)" "0"
assert_contains "the run exits 0 when q closes it" "$out" "autoreview-exit=0"
check_cooked "the terminal is given back after a quiet run"

finish
