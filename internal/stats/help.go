package stats

// Help is what `autoreview stats --help` prints. The text is v1's.
const Help = `autoreview stats: how each panelist model has done, across every recorded review.

Usage: autoreview stats [--since WHEN] [--repo NAME] [--json]
                        [--ledger PATH] [--help]

Every finished review is appended to a ledger -- one JSON line per review
with the panel, what each panelist reported, and which findings the synthesis
kept after checking them against the code. This reads that ledger back and
folds it per model.

  --since WHEN        Only reviews after WHEN: a span (7d, 2w) or a date
                      (2026-08-01).
  --repo NAME         Only reviews of a repo whose name contains NAME.
  --json              The same numbers as JSON, for another tool.
  --ledger PATH       Read this ledger instead of
                      $AUTOREVIEW_LEDGER or ~/.local/state/autoreview/ledger.jsonl.
  --help, -h          Show this help.

Columns:
  RUNS       times the model was launched on a panel
  AVAIL      launches that came back with a review, with a 95% interval
  RAW/RUN    findings the model itself reported, per answered run
  KEPT       findings the synthesis kept that name this model
  UNIQUE     kept findings no other panelist raised
  HIGH+      kept findings at HIGH or CRITICAL
  DROPPED    findings the synthesis listed under Disagreements: the model
             raised them, and verification overruled or downgraded them
  KEEP RATE  kept findings over reported findings, run by run, with a 95%
             interval -- the closest thing the pipeline has to precision
  SAMPLE     proven (30+ answered runs), emerging (10+), or thin

A ` + "`Flagged by`" + ` name that matches no panelist on its run (another review bot
the synthesis credited, say) is not scored; the footer says how many.
`
