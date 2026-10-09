# A request for changes reads as a note from a teammate

Recorded: 2026-10-09
Status: accepted

## Context

Decision 0029 gave each round one review with a verdict, and a fixed first
line for each verdict. The line on a request for changes was
`This PR needs changes. See the comments.`, and `This PR still needs
changes.` in a later round.

That line is what the author sees first, in the notification and at the top
of the review. It reads as a ruling. It is the same for one unvalidated
field and for a change at the wrong layer, so it says nothing about how
much the review asks for.

## Decision

The line is still fixed text, taken from a short table. The table now has
more than one row, and the review picks the row from what it shows the
author.

A first review (`auto-post-panel-review-comments` owns the table):

- A substantiated `Approach (questionable)` flag:
  `I think we should take another look at the approach before this gets in.`
- Exactly one blocking item:
  `Just one thing I think we should update before this gets in.`
- Any other count:
  `Just a few things I think we should update before this gets in.`

A later round (`recheck-pr` owns the table):

- One row above polish open: `Just one thing left before this gets in.`
- Any other count: `Just a few things left before this gets in.`

Two choices inside that decision:

- **The count is of what the review shows.** A blocking item is an inline
  comment or a body finding at MEDIUM or above, or an `Also blocking:` line.
  A finding that went to Linear does not count, and a LOW comment does not
  count.
- **`auto-post-panel-review-comments` picks the line for a first review.**
  `auto-review` decides the event before the routing, so it does not know
  the count. It hands over the event and no first line.

## Consequences

- The tone changed and the verdict did not. The event is still
  `REQUEST_CHANGES`, and on a branch that requires reviews it still blocks
  the merge.
- The body is still not a summary. The line gives no count above "a few"
  and names no severity.
- "A few" is also the line for two items and for ten. A PR with ten
  blocking findings gets a softer line than it deserves; the comments carry
  the weight.
- A purpose that is not served takes the count line, not the approach line.
- The binaries do not read the line. The VERDICT column comes from the
  review state (decision 0002), so no plain output string changed.
- The evals pin each line byte for byte. A wording change ships with its
  eval change.
- An installed copy of the skills shadows the bundled one (decision 0013).
  An older installed copy keeps the old line.
