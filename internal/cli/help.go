package cli

// Help is what --help prints.
const Help = `autoreview: one place to review your team's PRs and handle your own.

Usage: autoreview [PR ...] [--watch[=MINUTES]] [--all] [--dependabot]
                  [--stacked] [--skip-wait-for-ci] [--tui | --headless]
       autoreview stats [--since WHEN] [--repo NAME] [--json]
                  (how each model has done; see autoreview stats --help)
       autoreview --help | --version

Run it inside a GitHub checkout. On a terminal it opens the full-screen view
and keeps looking at the PR list. Anywhere else (a pipe, cron, CI) it looks
once, prints one line for each PR, and exits.

This build reads the lists and starts no review.

  PR ...              Limit the run to these PR numbers.
  --watch[=MIN], -w   How often the view looks at the PR list (default 2, or
                      $AUTOREVIEW_WATCH_INTERVAL). A bare number is minutes;
                      30m, 1h and 2d also work. Give the value with "=".
  --all, -a           Treat approved PRs as open for review.
  --dependabot, -d    Include Dependabot's PRs.
  --stacked, -s       Do not hold a PR that sits on another open PR.
  --skip-wait-for-ci  Do not hold a PR whose checks have not passed.
  --tui               Open the full-screen view. This is the default on a
                      terminal.
  --headless          Print plain lines, also on a terminal.
  --help, -h          Print this text.
  --version, -V       Print the version.

Keys in the view: press ? to see them all.
`
