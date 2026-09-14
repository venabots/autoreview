# A review says why it did not approve

Recorded: 2026-09-14
Status: accepted

## Context

The summary said a PR was commented on rather than approved, and stopped
there. A reader could not tell a misaligned button from a double charge
without opening the PR and reading the review, so a pass of ten PRs was a
list of ten things to go and look at.

VERDICT cannot answer this. It is GitHub's readback, and GitHub records what
landed, not what is wrong.

## Decision

The trailer carries `blockers`: one entry per must-fix or should-fix
finding, worst first, each with a severity, a `domain`, a `reversible` flag,
a location and a one-sentence gist. Polish findings are not blockers, so a
LOW finding never holds a PR back in the summary.

Two axes, not one. `domain` says which kind of bug it is (money, data,
security, correctness, ui, perf, docs) and `reversible` says whether the
damage can be undone once it lands. A HIGH in money that cannot be undone
and a HIGH in ui that can are not the same night's work.

The block prints under the finished board row, under the same line off a
TTY, and under the results table at the end, headed
`not approved yet because:`. Three blockers show; the rest are counted.

The asymmetry in [0002](0002-the-verdict-is-read-back-from-github.md) holds:
whether the PR is approved comes from GitHub, and why it is not comes from
the reviewer. An approved PR prints no block, whatever its trailer claims.

## Consequences

- A reviewer that reports no blockers prints nothing, so an older reviewer
  or an overridden command costs a missing block, not an error.
- Blockers are agent text on a terminal in raw mode: each field is stripped
  of control bytes, the gist is cut at 120 characters, and the list at 8.
- The severity a blocker carries is the synthesis's, so the skill's rubric
  decides what blocks a merge. Moving a class of finding to LOW there moves
  it out of this block too.
- One more thing a reviewer can get wrong. A blocker naming the wrong domain
  is worse than no domain, which is why the field may be null.
