#!/usr/bin/env bash
# tui: autoreview on a terminal. The full-screen view draws on the
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
# first; autoreview's follow the --. $VIEW_FLAG replaces --tui: set it empty
# for the default, or to --headless.
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
    bash -c 'cd "$1" && TERM=xterm-256color "$2" --log-dir "$3" ${4:+"$4"} "${@:5}"; echo "autoreview-exit=$?"; saved="$(stty -g)"; stty -icanon -echo min 0 time 2; n="$(dd bs=64 count=1 2>/dev/null | wc -c)"; stty "$saved"; echo "leftover=$((n))"; stty -a' \
    _ "$SANDBOX/repo" "$AUTOREVIEW" "$SANDBOX/out/logs" "${VIEW_FLAG---tui}" "$@"
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
out="$(FAKE_CLAUDE_TRAILER=9 run_tui --key 3.0:j --key 4.0:q -- --once)"
assert_contains "a one-pass run exits 0" "$out" "autoreview-exit=0"
assert_contains "the view opens on the alternate screen" "$out" $'\e[?1049h'
assert_contains "...and leaves it" "$out" $'\e[?1049l'
assert_contains "a row says what the run did about the PR" "$out" "no verdict"
assert_contains "the detail pane shows the last review" "$out" "LAST"
assert_contains "...and why it is not approved yet" "$out" "retried"
assert_contains "...and how to reopen it" "$out" "--resume"
assert_contains "the summary prints after the view closes" "$out" "PR  RESULT"
assert_contains "...and names the run directory" "$out" "logs: $SANDBOX/out/logs/run-"
assert_not_contains "the plain lines stay off the screen" "$out" "start   #9"
assert_contains "...and go to the run log" "$(run_log)" "start   #9 @alice (reviewing)"
assert_contains "...with the pass summary" "$(run_log)" "SESSION"
check_cooked "the terminal is given back cooked"

# --- No flag: a terminal gets the view, and the view watches ---------------
# The screen stays up until q, so it keeps looking for work without being
# asked: the run is a --watch run at the default intervals. q ends it the
# way it ends any watch run.
out="$(VIEW_FLAG='' run_tui --key 6.0:q --)"
assert_contains "without a flag a terminal gets the view" "$out" $'\e[?1049h'
assert_contains "...and the view watches" "$out" "watching"
assert_contains "...resting each PR it reviewed" "$out" "rests"
assert_contains "...and the plain lines go to the run log" "$(run_log)" "start   #9"
assert_contains "...which shows it polling" "$(run_log)" "next check in 2m"
assert_contains "q ends it like any watch run" "$out" "autoreview-exit=130"
check_cooked "the terminal is given back after a default run"

# --tui asks for the same view, and gets the same run.
out="$(run_tui --key 4.0:q --)"
assert_contains "--tui watches too" "$(run_log)" "next check in 2m"

# --once is one pass in the view: it waits for q and exits 0.
out="$(VIEW_FLAG='' run_tui --key 3.0:q -- --once)"
assert_not_contains "--once keeps the view to one pass" "$out" "watching"
assert_not_contains "...and does not poll" "$(run_log)" "next check in"
assert_contains "...and q closes it" "$out" "autoreview-exit=0"

# --once and a loop ask for opposite things.
out="$(VIEW_FLAG='' run_tui -- --once --watch)"
assert_contains "--once with --watch is refused" "$out" \
  "error: --once and --watch cannot be used together"
assert_contains "...with exit 1" "$out" "autoreview-exit=1"

# --- --headless: plain lines on a terminal ---------------------------------
out="$(VIEW_FLAG=--headless run_tui --)"
assert_not_contains "--headless keeps the view closed" "$out" $'\e[?1049h'
assert_contains "...and prints the plain lines on the terminal" "$out" "start   #9"
assert_contains "...and the plain summary" "$out" "PR  RESULT"
assert_contains "...and exits when the pass ends" "$out" "autoreview-exit=0"
check_cooked "...and leaves the terminal cooked"

# --- An empty --pick on a terminal exits at once ---------------------------
# The person chose nothing, and R refuses every PR outside a pick, so there
# is no view to open. A view here would wait for a q nobody sends, and the
# driver would time out before the exit line.
out="$(VIEW_FLAG='' run_tui -- --pick)"
assert_not_contains "an empty --pick keeps the view closed" "$out" $'\e[?1049h'
assert_contains "...and exits 0 without waiting for q" "$out" "autoreview-exit=0"

# Under --no-post every VERDICT reads "nothing posted". The line that says
# why goes under the whole-run summary, not only into the log.
out="$(run_tui --key 3.0:q -- --once --no-post)"
assert_contains "--no-post says why nothing landed, under the summary" "$out" \
  "nothing was posted to any PR; the reviews are in $SANDBOX/out/logs/run-"

# --- r opens the review in a new tab, o opens the PR ----------------------
# The sandbox drives the cmux spawner, whose fake records the command a tab
# was sent. The fake gh records a --web call.
out="$(run_tui --key 3.0:r --key 3.5:o --key 4.5:q -- --once)"
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

# --- Nothing typed at the view reaches the shell ---------------------------
# The view asks the terminal to report every mouse movement, and an
# interrupted run takes about a second to stop its reviews, in which it reads
# nothing. Reports sent in that second sit in the terminal's input, and the
# shell would read them as typing: bells, a prompt in the wrong mode, a
# command line that starts with junk. The driver sends one after the ctrl-C
# and the wrapper counts the bytes left for the shell.
mouse_move="$(printf '\033[<35;40;12M')"
out="$(FAKE_CLAUDE_SLEEP=8 run_tui --key 2.0:$'\x03' --key 2.3:"$mouse_move" --key 2.5:"$mouse_move" --)"
assert_contains "ctrl-C interrupts the run" "$out" "autoreview-exit=130"
assert_contains "...and leaves nothing for the shell to read" "$out" "leftover=0"
check_cooked "...and the terminal is cooked"

# --- A watch run: reviewed PRs wait, and R reviews one now -----------------
# After the first pass both PRs rest for the babysit interval. The first row
# is a resting PR; R wakes the run and reviews it again at once.
out="$(run_tui --key 3.0:R --key 7.0:q -- --watch=1)"
assert_contains "a reviewed PR rests under a watch run" "$out" "· rest"
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

# --- The mouse: capture, the wheel, and a click ---------------------------
# Mouse reporting is escape sequences like any key, so the driver sends them
# the same way: SGR press (M) and release (m), one-based column and row.
# 65 is a wheel-down notch, 0 the left button.
#
# #8's checks fail, so it is held and #9 is the only review: the list is then
# #8 first (the run is waiting on it) and #9 second, whatever the clock did.
set_ci 8 FAILURE
wheel_down="$(printf '\033[<65;80;10M')"
click_row2="$(printf '\033[<0;5;5M')"
unclick_row2="$(printf '\033[<0;5;5m')"
out="$(FAKE_CLAUDE_TRAILER=9 run_tui \
  --key 3.0:"$wheel_down" --key 3.2:"$wheel_down" \
  --key 4.0:"$click_row2" --key 4.1:"$unclick_row2" --key 5.0:q -- --once)"
assert_contains "the screen asks the terminal for the mouse" "$out" $'\e[?1000h'
# The detail pane's title, which only the selected PR draws.
assert_contains "a click selects the row it lands on" "$out" "Add retry logic"
assert_contains "the footer offers the mouse key" "$out" "m mouse"
assert_contains "the run still exits 0" "$out" "autoreview-exit=0"
check_cooked "the terminal is given back after a mouse run"
set_ci 8 SUCCESS

# --- w turns the looking for work on and off -------------------------------
# A one-pass run starts watching when w is pressed, and stops when it is
# pressed again, which leaves it waiting for q like any run with nothing
# left to do.
out="$(run_tui --key 3.0:w --key 6.0:w --key 8.0:q -- --once)"
assert_contains "w starts the run watching" "$(run_log)" "watching: looking for work every 2m"
assert_contains "...and it polls" "$(run_log)" "next check in 2m"
assert_contains "...and the view says so" "$out" "watching"
assert_contains "w again stops it" "$(run_log)" "no longer looking for work"
assert_contains "...leaving the run waiting for q" "$out" "autoreview-exit=0"
check_cooked "the terminal is given back after a toggled run"

# --- f types what the reviewers are told ----------------------------------
# The focus is the one thing worth changing mid-run, so it is typed on the
# screen and reaches the next review's prompt. R is what starts that review.
out="$(run_tui --key 3.0:f --key 3.4:"the ledger migration" --key 3.8:$'\r' \
  --key 4.4:R --key 8.0:q -- --watch=1)"
assert_contains "f says what was typed" "$(run_log)" \
  "the reviewers are now told: the ledger migration"
assert_contains "...and the header carries it" "$out" "focus: the ledger"
assert_contains "the next review is told the same" "$(claude_calls)" \
  '--focus "the ledger migration"'
assert_contains "q ends the run" "$out" "autoreview-exit=130"

# --- My PRs: your own PRs, and a task on one --------------------------------
# Tab shows the PRs the review list hides because you wrote them. f on one
# fixes it: the skill runs in a worktree of its own, on the PR's branch,
# and the summary says what GitHub shows it did.
make_origin
install_task_skills
out="$(FAKE_GH_HEAD=sha4-new run_tui --key 3.0:$'\t' --key 4.0:f --key 9.0:q -- --once)"
assert_contains "the tab bar counts your PRs" "$out" "My PRs 1"
assert_contains "tab shows your PR and where it stands" "$out" "awaiting review"
assert_contains "the tab has its own keys in the footer" "$out" "b babysit"
assert_contains "f fixes it with the installed skill" "$(claude_calls)" "-- /babysit-pr 4"
# The directory as the job saw it: macOS reports /var as /private/var.
assert_contains "...in a worktree of its own, on the PR's branch" "$(job_dirs)" "/worktrees/pr-4"
assert_not_contains "...not in your checkout" "$(job_dirs)" "4 $SANDBOX/repo"
assert_contains "...and the log says what it is doing" "$(run_log)" "start   #4 @me (fixing)"
assert_contains "the summary says it pushed, as GitHub reports" "$out" "pushed"
assert_contains "the run still exits 0" "$out" "autoreview-exit=0"
if [[ -d "$(echo "$SANDBOX"/out/logs/run-*/worktrees/pr-4)" ]]; then
  not_ok "a task that left nothing unpushed takes its worktree with it" "the worktree is still there"
else
  ok "a task that left nothing unpushed takes its worktree with it"
fi
assert_equals "...and your checkout never left its branch" \
  "$(git -C "$SANDBOX/repo" rev-parse --abbrev-ref HEAD)" "$(git -C "$SANDBOX/repo" symbolic-ref --short HEAD)"
check_cooked "the terminal is given back after a task"

# A task that pushed nothing says so: GitHub reports the commit the
# worktree started at.
out="$(FAKE_GH_HEAD="$(git -C "$SANDBOX/repo" rev-parse HEAD)" run_tui --key 3.0:$'\t' --key 4.0:c --key 9.0:q -- --once)"
assert_contains "c answers the comments with the installed skill" "$(claude_calls)" "-- /pr-comment-handler 4"
assert_contains "...and a task that moved nothing says so" "$out" "nothing pushed"

# u merges the base in through sync-main, in the same worktree.
out="$(run_tui --key 3.0:$'\t' --key 4.0:u --key 9.0:q -- --once)"
assert_contains "u fixes conflicts with the installed sync-main" "$(claude_calls)" "-- /sync-main 4"
assert_contains "...in the PR's worktree" "$(job_dirs)" "/worktrees/pr-4"

# b babysits the PR. The run starts watching, because a fix follows a look,
# and the first look finds the PR needs work: it has an open thread.
cp "$SANDBOX/fixtures/mine.json" "$SANDBOX/fixtures/mine.saved"
jq '.data.search.nodes[0].reviewThreads.nodes = [{"isResolved":false}]' \
  "$SANDBOX/fixtures/mine.saved" >"$SANDBOX/fixtures/mine.json"
out="$(run_tui --key 3.0:$'\t' --key 4.0:b --key 9.0:w --key 12.0:q -- --once)"
assert_contains "b starts watching, so the run looks again" "$(run_log)" "babysitting needs the run to keep looking for work"
assert_contains "...and fixes the PR that needs work" "$(run_log)" "babysitting: fixing PR #4, which changed and needs work"
assert_contains "...with the fix skill" "$(claude_calls)" "-- /babysit-pr 4"
assert_contains "...and the tab bar counts it" "$out" "babysitting 1"
# w turns the looking off, and babysitting with it: with no looks it could
# only say it babysits.
assert_contains "w stops babysitting along with the looking" "$(run_log)" \
  "no longer babysitting your PRs: the run stopped looking for work"
mv "$SANDBOX/fixtures/mine.saved" "$SANDBOX/fixtures/mine.json"

# Without the skill the key says so, and nothing runs.
uninstall_task_skills
out="$(run_tui --key 3.0:$'\t' --key 4.0:f --key 6.0:q -- --once)"
assert_contains "f without the skill says it is not installed" "$out" "babysit-pr is not installed"
assert_not_contains "...and runs nothing" "$(claude_calls)" "/babysit-pr"
drop_origin

# --- A quiet repo: the view opens anyway ----------------------------------
# The sweep has nothing to review, which without --tui is a one-line exit.
# The view opens on it instead: every open PR is there, saying why it is
# being left alone, and R reviews one on the spot.
seen_everything
out="$(run_tui --key 3.0:R --key 8.0:q -- --once)"
assert_contains "a quiet repo has nothing for the sweep" "$out" "no NEW or UPDATED PRs to review"
assert_contains "...and the view opens all the same" "$out" $'\e[?1049h'
assert_contains "...listing the PRs it is leaving alone" "$out" "seen"
assert_contains "...and saying so in the footer" "$out" "nothing to review"
assert_equals "R reviews one of them on the spot" "$(reviews_of 9)" "1"
assert_equals "...and only that one" "$(reviews_of 8)" "0"
assert_contains "the run exits 0 when q closes it" "$out" "autoreview-exit=0"
check_cooked "the terminal is given back after a quiet run"

finish
