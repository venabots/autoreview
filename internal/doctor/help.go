package doctor

// Help is what `autoreview doctor --help` prints.
const Help = `autoreview doctor: is the panel answering, and did any PR go without a verdict?

Usage: autoreview doctor [--since WHEN] [--no-probe] [--ledger PATH] [--help]

It asks each panelist one short question now, through dash-p, the way a
review launches it. A panelist that does not answer shows the reason in its
own words: no credits, a usage limit, a flag its CLI no longer takes. Then it
reads the ledger for the window: how often each model answered, what the
reviews cost, and which PRs got no verdict while a panelist was missing.

  --since WHEN        The window: a span (1d, 2w) or a date (2026-08-01).
                      The default is 1d.
  --no-probe          Do not ask the panel. Only read the ledger.
  --ledger PATH       Read this ledger instead of
                      $AUTOREVIEW_LEDGER or ~/.local/state/autoreview/ledger.jsonl.
  --help, -h          Show this help.

The panel is the one a review seats: $PANEL_REVIEW_PANELISTS, then
panel.panelists in the settings file, then every backend on PATH.

Exit status 1 means a problem: a panelist that does not answer, a PR with
no verdict, a required program that is missing, or a ledger that cannot be
read. With --no-probe, a model that answered no launch in the window is a
problem too.
`
