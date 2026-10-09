package cli

// Help is what --help prints.
const Help = `autoreview: one place to review your team's PRs and handle your own.

Usage: autoreview [PR ...] [--watch[=MINUTES]] [--babysit[=MINUTES]] [--once]
                  [--no-post] [--continue] [--jobs N] [--focus TEXT]
                  [--timeout SECONDS] [--budget USD] [--orchestrator SPEC]
                  [--skills DIR|installed] [--log-dir DIR] [--max-passes N]
                  [--max-idle N] [--all] [--dependabot] [--stacked]
                  [--skip-wait-for-ci] [--tui | --headless]
       autoreview stats [--since WHEN] [--repo NAME] [--json]
                  (how each model has done; see autoreview stats --help)
       autoreview --help | --version

Run it inside a GitHub checkout. A PR is ready for review when something on
it is new to you, its checks have passed, and it does not sit on another open
PR.

On a terminal it opens the full-screen view and watches: at each look it
reviews the PRs that have become ready. R reviews the selected PR now, x x
stops a review, r reopens a finished review in a new terminal tab, w stops
or starts watching, and q quits. Press ? in the view for every key.

Off a terminal (a pipe, cron, CI), or with --headless, it makes one pass: it
waits for checks that are still running, reviews every PR that is ready,
prints one line for each step, and exits 0 only when every review ran to its
end.

  PR ...              Limit the run to these PR numbers, and review each one
                      whatever its state.
  --watch[=MIN], -w   Look at the PR list every MIN minutes (default 2, or
                      $AUTOREVIEW_WATCH_INTERVAL). Off a terminal this keeps
                      the run going until it is stopped. A bare number is
                      minutes; 30m, 1h and 2d also work. Give the value with
                      "=".
  --babysit[=MIN], -b Off a terminal: make a pass every MIN minutes (default
                      30, or $AUTOREVIEW_BABYSIT_INTERVAL) until no PR is left
                      to review or to wait for. In the view and under --watch
                      it is how long a reviewed PR rests before it may be
                      reviewed again.
  --once              One pass. In the view, the view then holds; press w to
                      watch after all.
  --max-passes N      Reviews one PR gets in a run (default 3, or
                      $AUTOREVIEW_MAX_PASSES). While the run watches, a push
                      starts the count again.
  --max-idle N        Looks in a row that find nothing to review before
                      --babysit stops (default 3, or $AUTOREVIEW_MAX_IDLE).
  --no-post, -n       Review, and leave the PR alone: no comments, no verdict.
                      The reviewer runs the skill that has no posting step.
                      Each review is written to <run dir>/pass-N/pr-N.review.md.
  --continue, -C      Resume this machine's earlier review session for a PR
                      and re-check it, in place of a fresh review. A later
                      pass in the same run always does this.
  --jobs N, -j N      Reviews to run at once (default 2, or $AUTOREVIEW_JOBS).
                      A review is itself several agents.
  --focus TEXT        What the reviewers look at in this run.
  --timeout SECONDS   Stop a review that runs this long (default 3600, or
                      $AUTOREVIEW_TIMEOUT). 0 means no limit.
  --budget USD        The most one review may spend (or
                      $AUTOREVIEW_MAX_BUDGET_USD). Claude only.
  --orchestrator SPEC The agent that runs each review: claude or codex, with
                      an optional model, as in codex:gpt-5.5 (default claude,
                      or $AUTOREVIEW_ORCHESTRATOR).
  --skills VALUE      Which review skills run (or $AUTOREVIEW_SKILLS). Unset:
                      the ones this binary carries. A directory of
                      <name>/SKILL.md: those. The word "installed": none are
                      staged, and the reviewer uses what is installed.
  --log-dir DIR       Where the run directory is made (default: a temporary
                      directory, or $AUTOREVIEW_LOG_DIR).
  --all, -a           Treat approved PRs as open for review.
  --dependabot, -d    Include Dependabot's PRs.
  --stacked, -s       Do not hold a PR that sits on another open PR.
  --skip-wait-for-ci  Do not hold a PR whose checks have not passed, and do
                      not wait for them.
  --tui               Open the full-screen view. This is the default on a
                      terminal.
  --headless          Print plain lines, also on a terminal.
  --help, -h          Print this text.
  --version, -V       Print the version.

$AUTOREVIEW_CI_WAIT is how long a one-pass run waits for checks that are
still running (default 30 minutes). $AUTOREVIEW_CMD names a shell command that
replaces the built-in reviewer; the PR number replaces each {} in it, or is
added at the end. $AUTOREVIEW_LEDGER names the file that records each review;
"off" records nothing. $DASHP_BIN names a dash-p binary to use.
`
