# Rendering defects cap at LOW

Recorded: 2026-09-14
Status: accepted

## Context

The severity rubric graded a defect by the code it sat in, so a visual bug
in a busy component could be raised to MEDIUM by two panelists agreeing it
looked bad. A MEDIUM withholds an auto-approval, which meant a two-pixel
misalignment held a PR the same way an off-by-one in a balance did.

The cost of getting rendering wrong is that someone sees it and fixes it in
the next commit. The cost of getting money wrong is that someone loses money.

## Decision

A defect whose whole effect is visual is a LOW finding, whatever it looks
like and however many panelists raised it. Four things lift it back up, and
each is about what the user can no longer do: it blocks the primary action,
it misstates money or state, it locks keyboard and screen-reader users out,
or it breaks the layout at a supported viewport.

The cap does not run the other way. A money, data or security defect that
happens to surface in a component is not a rendering defect.

Every finding also names its domain -- money, data, security, correctness,
ui, perf, docs -- with `irreversible` added when the damage cannot be undone
once it lands. That is the tag a reader triages by, and the same two axes the
trailer reports in [0017](0017-a-review-says-why-it-did-not-approve.md).

## Consequences

- More PRs auto-approve. A visual defect is now posted as a polish comment
  and does not withhold the stamp.
- The judgment moves to the four exceptions. A reviewer that reads "blocks
  the primary action" too narrowly will wave through a broken flow, which is
  the failure this trades against.
- The severity a blocker carries is the synthesis's, so this rubric decides
  what shows in the summary's "not approved yet because" block too.
- The rubric lives in the vendored `panel-review` skill, which under
  [0013](0013-skills-are-vendored-and-versioned-with-the-binaries.md) ships
  with the binaries. The installed copy under `~/.agents/skills` is a
  separate checkout and has to be synced.
