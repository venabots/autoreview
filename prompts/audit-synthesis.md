# Panel synthesis request: code audit

Several independent reviewers audited the same code as it is now. There is no
change and no diff. None of them saw the others' findings. Their reports are
below, verbatim.

Your job is to turn them into one report a human acts on. That is judgment
work, not concatenation: you decide what is real, what agrees, what is
speculative, and what to drop.

You are running in the repository the reviewers looked at. Read the code.

## Read consensus with care

In an audit, each reviewer chooses where to spend its time. Two reviewers can
read different halves of a large tree. So a finding that only one panelist
raised is often a finding in code the others did not read. It is not a
disagreement. Silence from a panelist is evidence only when its report shows it
read that code.

## Verify before you surface

A panelist finding is questionable when any of these holds:

- Its severity is CRITICAL or HIGH. In an audit, verify every one of these,
  because consensus is weaker evidence than in a change review.
- The `Fix:` line does not obviously address the stated issue.
- The line number looks wrong: the referenced line does not hold the code the
  finding describes, or is out of range for the file.
- Two panelists disagree about whether the same code is a bug.
- The reasoning depends on caller behavior, framework guarantees, or
  downstream consumers the panelist did not actually check.

For each questionable finding, open the file and confirm the bug exists as
described before you surface it. If verification disproves it, drop it and say
so under `### Disagreements`. If verification sharpens it (you find the right
line), surface the corrected version.

Never repeat a claim you could have falsified in thirty seconds with a read.

**Misinterpretation check.** Read enough of the code to form your own view of
what it does. Compare that against each panelist's `Purpose:` line. A panelist
that misread what the code is for produces confidently wrong findings. When
this triggers, put a bold `**Misinterpretation detected:**` paragraph directly
after `### Risk`, name the panelist and what it got wrong, and drop the findings
whose substance depends on the misreading. When nothing is wrong, write
nothing.

## Drop these

- Any finding with no `file:line`.
- Any finding with no `Fix:` (a LOW finding is exempt -- see the shape below).
- Style nits a linter or formatter would catch.
- Anything a panelist that FAILED did not actually produce. A failed panelist
  contributed nothing; do not count it toward consensus.

A panelist that answered `NO_FINDINGS` is **not** a dropped panelist. It
audited the code and found nothing, which is a result: count it in the roster.

## Output

Emit only these sections, in this order. **Most are conditional** — emit a
heading only when it has content. Blank sections and "none" placeholders bury
the signal.

1. `### Overview` (always)
2. `### Risk` (always)
3. A misinterpretation callout, when one is warranted
4. `### must-fix` / `### should-fix` / `### polish` (each only when non-empty)
5. `### Disagreements` (only when panelists actually contradict each other)

### Overview

First line, verbatim shape, so a wrong-target review is obvious at a glance:

```
**Reviewing:** <the target line given below>
```

Then one sentence of plain language saying what this code is for. Then one or
two sentences on coverage: which areas the panel read with care, and which
large areas no panelist reported on. Do not editorialize; evaluation belongs in
Risk and the buckets.

### Risk

`LOW` / `MEDIUM` / `HIGH` / `CRITICAL`, then one sentence pointing at
observable signals — verified findings, the areas they sit in — not vibes. This
rates the code as it is now, not a change.

- **LOW** — nothing verified above MEDIUM, and the findings are local.
- **MEDIUM** — real, fixable bugs exist. Nothing verified above MEDIUM in auth,
  sessions, payments, migrations, crypto or production infra.
- **HIGH** — a verified HIGH finding, or a verified design problem that makes a
  class of bugs likely in load-bearing code.
- **CRITICAL** — a verified finding that can lose data, bypass auth, leak
  credentials, or fail in production on an ordinary input.

### must-fix / should-fix / polish

The buckets are the findings list. `must-fix` is CRITICAL and HIGH,
`should-fix` is MEDIUM, `polish` is LOW. Omit any bucket that is empty.

Shape, for CRITICAL / HIGH / MEDIUM:

```
- [SEVERITY] file:line — one-sentence issue. Fix: one-sentence change. Flagged by: codex (gpt-5.5)
```

When two or more panelists raised the same finding, prefix the count and name
them all:

```
- [MEDIUM] src/a.rs:130 — the issue. Fix: the change. Flagged by 2: claude (claude-opus-5), codex (gpt-5.5)
```

When they assigned different severities, use the higher and say so inline:
`Flagged by 2: claude (claude-opus-5) [LOW], codex (gpt-5.5) [MEDIUM] — using higher.`

LOW findings collapse to one line and need no `Fix:`.

Within a bucket: findings raised by more panelists first, then group by file.

**Dedup.** Two panelists raised the same finding when they cite the same
`file:line` (or overlapping ranges) and the underlying claim is the same. A
different suggested fix at the same location is still consensus on the bug —
pick the better fix and note the other. A _different_ bug at the same line is
two findings, not one.

### Disagreements (only when panelists actually contradict each other)

One flagged it, another examined the same code and said it was fine; or your
verification falsified a raised finding. Lay out both positions with the
disputed `file:line`. Do not pick a side unless verification settles it.
Severity-only splits do not belong here — those go inline on `Flagged by:`.

## How to write

ASD-STE100 Simplified Technical English. Short sentences, active voice, one
idea per sentence, one word per meaning. No emoji. No preamble, no sign-off:
start at `### Overview` and stop when the last section ends.
