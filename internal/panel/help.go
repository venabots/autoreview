package panel

// Help is what `autoreview panel --help` prints. The text is v1's, with the
// program named as the subcommand it now is.
const Help = `autoreview panel: review one change with several models at once, then synthesize.

Usage: autoreview panel [--base REF | --uncommitted | --staged | --tree [PATH]]
                        [--panelist BACKEND[:MODEL]]...
                        [--focus TEXT] [--timeout SECONDS] [--log-dir DIR]
                        [--synth BACKEND[:MODEL]] [--no-synthesis] [--help]

Every backend CLI on PATH (codex, claude, opencode) reviews the change in
parallel through dash-p, each one blind to the others. Their reports print as
they land. One more model call then reads all of them, verifies the
questionable claims against the code, and writes the report.

  --base REF          Review REF...HEAD -- what this branch added. Each
                      panelist gets its own worktree and may run the tests.
  --uncommitted       Review what is not committed yet (default). Panelists
                      read your working tree and change nothing.
  --staged            Review the index only.
  --tree [PATH]       Audit the code as it is at HEAD, with no change to
                      review: the whole repository, or only PATH. Each
                      panelist gets its own worktree and may run the tests.
                      Uncommitted edits are not part of it.
  --panelist B[:M]    Add one panelist: a backend (codex, claude, opencode)
                      and optionally the model to pin it to. Repeatable. The
                      default is every backend found on PATH, on its own
                      default model.
  --focus TEXT        What the reviewers should pay attention to.
  --timeout SECONDS   Give up on a panelist that runs this long (default 600,
                      or $PANEL_TIMEOUT).
  --log-dir DIR       Where to keep each panelist's output (default: a temp
                      directory, printed on every run).
  --synth B[:M]       Which model writes the synthesis (default claude, or
                      $PANEL_SYNTH).
  --no-synthesis      Print the panelist sections and stop. For comparing the
                      panel against a synthesis you write yourself.
  --help, -h          Show this help.
  --version, -V       Show the version.

The synthesis runs in this repository with read-only access, because verifying
a finding means reading the code it is about. It is told which panelists
failed, so it does not read a dead panelist's silence as agreement.

$DASHP_BIN names a dash-p binary to use.

Exit status is 0 when the report was produced. A panel where some panelists
failed still exits 0 and says so -- it is a thinner review, not a broken run.
An interrupted run exits 130.
`
