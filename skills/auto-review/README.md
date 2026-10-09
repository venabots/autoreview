# auto-review

One-shot "review, then submit one real review" pipeline for a PR. It chains three existing skills:

```
panel-review  →  decide the verdict  →  auto-post-panel-review-comments (one review)
                                              └─ approved?  →  approve-pr (follow-up only)
```

Each pass leaves **one GitHub review** on the PR, with every comment of the pass inside it:

- **Approve** — the PR is clean and the gate passes.
- **Request changes** — a finding blocks.
- **Comment** — no decision is possible. The first line of the review says why.

It doesn't reimplement the three skills — it calls each in turn and adds one new piece of logic: the **approval gate**, and the verdict that follows from it.

## Install

```
npx skills add venabots/autoreview --skill auto-review
```

Requires [`panel-review`](../panel-review) and [`auto-post-panel-review-comments`](../auto-post-panel-review-comments) installed; [`approve-pr`](../approve-pr) is needed only for the follow-up after an approval (post-only runs don't use it).

## How to use it

On a PR (or its branch), ask:

- "auto-review this PR"
- "auto review"
- "auto panel review" / "auto panel-review" (any "auto" + review phrasing)
- "review and post the comments"
- "panel-review then auto-post"
- "review it and approve if it's clean"
- "review it and request changes if it needs them"
- "auto-review, but send the LOW/polish ones to Linear"
- "review and post only, don't approve" (a comment review with no verdict)
- "auto-review, but don't request changes" (a blocking finding gets a comment review)

Anything that prefixes **"auto"** onto a review request lands here, not on
`panel-review`, and carries approval intent. The skill never stops to ask
whether to approve or to request changes: the gate decides, and an
ambiguous invocation silently falls back to post-only.

## What it does

1. **Reviews** — runs the `panel-review` skill end to end (kept separate; its fan-out, streaming, and synthesis are untouched). Captures which panelists returned a verdict and the synthesized findings.
2. **Decides** — evaluates the approval gate and picks the review event before anything is posted, because a review carries its verdict.
3. **Posts one review** — runs the `auto-post-panel-review-comments` flow with that event: one `POST /pulls/{N}/reviews` whose inline comments are the legitimate findings (mergeable suggestion / prose / body-only). It `+1`s anything a bot already raised, routes uncertain / out-of-PR-scope findings to Linear, honors routing overrides. A finding on a line outside the diff goes into the review body. Nothing is posted as a separate comment.
4. **Follows up an approval** — when the review approved, `approve-pr` does its follow-up (the Slack reaction). It does not submit a second review.

A verdict needs an invocation that asked for one ("auto-review", "approve if clean", "stamp it if clean"). A post-only or ambiguous request ("review and post the comments") still submits one review, as a comment review with the body `Comments only. No decision.`, and reports the verdict it would have given.

## The verdict

The first rule that matches wins:

| #   | When                                                         | Review            | First line of the body                                                          |
| --- | ------------------------------------------------------------ | ----------------- | ------------------------------------------------------------------------------- |
| 1   | Post-only request                                            | `COMMENT`         | `Comments only. No decision.`                                                   |
| 2   | Your own PR, or a draft                                      | `COMMENT`         | `No decision: this is my own PR.` / `No decision: this PR is a draft.`          |
| 3   | The head moved during the review                             | `COMMENT`         | `No decision: the head moved during the review. These comments are for <sha7>.` |
| 4   | A must-fix or should-fix finding, or a questionable approach | `REQUEST_CHANGES` | `Just a few things I think we should update before this gets in.` (see below)   |
| 5   | The gate passes                                              | `APPROVE`         | `LGTM` / `LGTM, just some small comments, nothing blocking`                     |
| 6   | Nothing blocks, but too few reviewers answered               | `COMMENT`         | `No decision: only <k> of <n> reviewers answered.`                              |

The line on a request for changes follows what the review asks for. One blocking item gets `Just one thing I think we should update before this gets in.` A questionable approach gets `I think we should take another look at the approach before this gets in.`

Rule 4 comes before rule 6 on purpose. A short panel that found nothing has not shown that the PR is clean, so it gets no decision. A verified finding shows that the PR needs a change, however many reviewers answered.

On a branch that requires reviews, a request for changes blocks the merge until a later pass approves. [`recheck-pr`](../recheck-pr) does that when the author fixes or explains every finding.

The head is checked again immediately before the review is submitted. If the author pushed after the verdict was decided, the review is the head-moved comment review.

## The approval gate

It approves **only when every one** of these holds:

- **Reviewer coverage — ≥ 75% of the panel returned** — at least 75% of the launched panelists returned a verdict (`exit 0`), and the ≥2 floor below holds. One panelist crashing/timing out (e.g. the flaky `database is locked`) in a four-panel run (3/4) still clears this as long as the returning reviewers are clean; dropping below 75% (2/4, 2/3, 1/2) does not → no approval.
- **Enough independent reviewers — at least two, not narrowed** — a hard floor of ≥2 distinct panelists returning `exit 0` (one opinion is never enough — even if it's the only CLI installed on the host), and the panel wasn't narrowed below `panel-review`'s default via `--panelist`. A single-reviewer run is too thin to auto-stamp.
- **No blocking findings** — zero must-fix (CRITICAL/HIGH) and zero should-fix (MEDIUM); only polish (LOW) findings, or none.
- **Sound approach, served purpose** — no substantiated `Approach (questionable)` flag, and no verified `Purpose (stated, not served)` flag. Code that does something other than what its description says is not auto-stamped. A `Purpose (unknown)` does not withhold approval: a thin description is bucketed LOW by `panel-review` and rides along as a polish comment. Same for `Proof (missing)` — bucketed LOW, except on auth, session handling, payments, schema migrations, crypto, or production infra, where it lands in must-fix and the blocking-findings gate withholds on it.
- **Not a draft** — a draft PR is the author saying "not ready" (`gh pr view --json isDraft`); it gets comments and no verdict.
- **Not your own PR** — GitHub accepts no approval and no request for changes from the author; your own PR gets comments and no verdict.
- **Head unchanged** — the PR head SHA is re-fetched before the verdict and must still match the SHA that was reviewed; if the author pushed during the multi-minute review, there is no verdict (the current head was never reviewed).

## The review body

Short, ASCII, one fixed line, no review dump. The table above lists the main first lines, and the skill has the full list. Only a finding that cannot be an inline comment follows it.

## What it does NOT do

- **No reimplementation.** It composes `panel-review`, `auto-post-panel-review-comments`, and `approve-pr`; all their mechanics live in those skills.
- **No comment outside the review.** One pass is one review and one notification. It never posts a finding by itself, and never submits a comment review and then a separate approval.
- **No request for changes on polish.** LOW findings never block.
- **No approval without coverage.** Coverage falling below 75% of the launched panel (or below the ≥2 floor), or a narrowed panel, blocks the stamp even if the findings are all LOW. Draft PRs, your own PRs, a head that moved mid-review, and post-only requests also get no verdict (non-finding reasons).

## Gotchas

- **It targets a PR.** If the review runs against a non-PR target (`--uncommitted`, `--base`), there's nothing to submit a review on — it reports the review and stops.
- **Coverage below 75% blocks approval, not a request for changes.** Losing one of four panelists (3/4) still allows a clean approval; falling below 75% of the launched panel is not a clean bill of health — the review says "no decision" and the report names the missing panelist. A blocking finding still gets a request for changes.
- **A request for changes stays until a later pass approves.** That is the point: the PR carries a blocking review for a known fault, and a branch that requires reviews cannot merge. Say "don't request changes" for a run that must not block.
- **Same PR throughout.** The PR is resolved once and reused for the review, the verdict, and the post.
- **Different from its parts.** [`panel-review`](../panel-review) only reviews; [`auto-post-panel-review-comments`](../auto-post-panel-review-comments) only posts; [`panel-review-loop`](https://github.com/venables/skills/tree/main/skills/panel-review-loop) iterates fix-and-rereview; [`approve-pr`](../approve-pr) only approves. `auto-review` is the one-pass review → verdict → one-review composition.
