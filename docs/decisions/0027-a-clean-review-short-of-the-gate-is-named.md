# A clean review short of the gate is named

Recorded: 2026-10-05
Status: accepted

## Context

The review skill approves only when at least `ceil(0.75 × panel)` panelists
answered, and at least 2 (gate 1 in `skills/auto-review/SKILL.md`). When a
provider is down, a review can come back with two clean answers out of four.
The gate holds it, which is correct. But the summary showed it as
"commented", like a review that found a bug, so a reader had to open the PR
to learn that it only waited for reviewers.

## Decision

- When a review is not approved, its trailer reports no must-fix and no
  should-fix finding, and fewer panelists answered than the gate needs (but
  at least one), the run says so:
  `looks clean, but only 2 of 4 reviewers answered; approval needs 3`.
- The line goes under the step line and in the end-of-run summary, before
  the "not approved yet because" block. In the view the row says
  `clean 2/4` in green, and the detail pane has the same line.
- The threshold is computed in `src/quorum.rs`, with the same rule as the
  skill. A finding count the trailer did not report is not a zero.
- Nothing is approved. The verdict is still read back from GitHub (decision
  0002), and an approved PR says nothing here.

## Consequences

- The line depends on the trailer, which is the reviewer's own report. A
  reviewer that reports a wrong panel count gives a wrong line. It cannot
  give an approval.
- If the skill's gate changes, `src/quorum.rs` must change with it, or the
  line names the wrong number.
