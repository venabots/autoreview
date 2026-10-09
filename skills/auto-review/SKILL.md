---
name: auto-review
description: >
  One-shot "review, then submit one real review" pipeline for a PR. Runs
  `panel-review`, decides a verdict, then submits ONE GitHub review through
  `auto-post-panel-review-comments` with every legitimate finding attached
  to it as an inline comment. The review approves only when the PR comes
  back clean enough (at least 75% of the panel returned, findings
  LOW/polish only, no must-fix or should-fix, approach sound). It requests
  changes when a finding blocks. It is a plain comment review when no
  decision is possible (too few reviewers answered, a draft, your own PR,
  a head that moved). Use for "auto-review this PR", "auto review", "auto
  panel review", "auto-panel-review", "review it and approve if it's
  clean", "review it and request changes if it needs them". Any review
  request prefixed with "auto" means this skill and the full
  review→decide→submit pipeline, even when it also says "panel" — prefer it
  over `panel-review` whenever "auto" appears. A post-only request ("review
  and post the comments") submits a comment review with no verdict;
  ambiguous intent defaults to post-only. Do NOT use when the user only
  wants the review
  (`panel-review`), only wants to post findings already in hand
  (`auto-post-panel-review-comments`), wants fix-and-rereview to
  convergence (`panel-review-loop`), or just wants to approve
  (`approve-pr`).
---

# auto-review

A thin orchestrator that chains three existing skills into one
hands-off pass over a PR:

```
panel-review  →  decide the verdict  →  auto-post-panel-review-comments (one review)
                                              └─ approved?  →  approve-pr (follow-up only)
```

Each pass leaves **one review** on the PR, and every comment of the pass
is inside it:

- **Approve** — the PR is clean and the gate passes.
- **Request changes** — a finding blocks.
- **Comment** — no decision is possible. The review says why.

It **does not reimplement** the three skills — it calls each in turn and
adds one new piece of logic: the **approval gate**, and the verdict that
follows from it. Everything about how reviews run, and how the review and
its comments are shaped/submitted/deduped, lives in those skills; this
file owns only the wiring, the gate and the verdict.

## Language

Each sub-skill holds its own text to ASD-STE100 Simplified Technical
English; let it. Write the same way for anything **this** skill adds —
the step-5 report, and any finding you add to the review for a
finding-based blocker (see step 3):

- Write short sentences. Keep an instruction to 20 words. Keep a
  statement to 25 words.
- Put one idea in each sentence.
- Use the active voice. Use simple tenses.
- Use one word for one meaning.
- Use the simplest word that is correct. Remove jargon, idioms,
  metaphors, and figures of speech.
- Do not make a noun out of a verb.

The review bodies under "Review body" and the `DECISION:` line that ends
the report are fixed strings. Use them exactly as written; do not restyle
them.

## Prerequisite

This skill orchestrates `panel-review` and
`auto-post-panel-review-comments` (always needed), and `approve-pr` (only
after a review that approves — see step 4). A post-only run needs just the
first two. If a required skill is missing,
stop and tell the user to install it rather than hand-rolling its
behavior — the value here is composing the real skills, not duplicating
them.

## Target

`auto-review` operates on **a PR** — it submits a review on it. Resolve
the PR the way `panel-review` does (named PR, or the current branch's PR).
Capture **both the PR ref and its head SHA** (`headRefOid`) once, up
front, and reuse the **same** PR for every later step.

The head SHA matters because a panel review takes minutes — long enough
for the author to push during it. Pin the SHA you reviewed: submit the
review against that `commit_id`, and **before you decide the verdict,
re-fetch the PR head and confirm it still matches** the reviewed SHA. If
it changed, the current head was never reviewed — give no verdict on it,
and report that a re-run is needed (this is gate condition #7 below).

If the review ends up running against a non-PR target (`--uncommitted`,
`--base`, a bare `--commit` with no PR), there's nothing to submit a
review on: run the review, report its synthesis, note that no review was
submitted because the target isn't a PR, write `DECISION: No action`, and
stop.

Pass the user's `panel-review` options through (panelist selection,
`--focus`, deep mode if they asked for a "deep auto-review"). Default to
the standard multi-panelist run — the approval gate leans on having
several independent reviewers (see the gate).

## The pipeline

### 1. Review — run `panel-review` (kept separate)

Invoke the `panel-review` skill and let it run end to end: it launches
`panel-review.sh`, streams per-panelist progress, and produces the
synthesis (Risk, Goal / Approach / Purpose / Proof checks, must-fix /
should-fix / polish buckets, disagreements). Don't reimplement its fan-out.

**Reuse a review that already ran.** If a fresh `panel-review` for this
exact PR/SHA is already available in context (the user just ran it, or
supplied its synthesis + per-panelist outcomes), use that instead of
re-running — re-running wastes a multi-minute fan-out. Only re-run when no
current review is in hand. Either way you need the same two captures below;
a reused review must include the **per-panelist exit statuses AND the
launched panelist set** (the `exit N` markers / `## <name> (exit N)`
headers, and the panelist list / any `--panelist` flags) — coverage
(gate #1) needs the exit statuses and the narrowing check (gate #2) needs
the launched set. If a supplied review lacks either, treat that property
as not established and **don't approve** on it.

Capture two things for later steps:

- **Per-panelist outcome** — which panelists returned a verdict vs.
  failed/timed out. `panel-review` surfaces this two ways: a
  `panel-review: <name> (<model>) done (exit N)` heartbeat per panelist,
  and a `## <name> / <model> (exit N)` section header in its combined
  output. `exit 0` (including a `NO_FINDINGS` verdict) counts as
  **returned**; a non-zero exit (crash, timeout, the flaky `database is
locked`, etc.) counts as **missing**. **Cross-check the count**: record
  how many panelists returned `exit 0` out of how many launched — approval
  needs ≥ 75% of the launched set (see gate #1), so you must know both
  numbers, not just that "some" returned. If you can't establish
  per-panelist exit status for every launched panelist (e.g. the output was
  truncated), treat coverage as **not** established and don't approve —
  never infer coverage from the synthesis alone. Coverage is the gate's
  central safety property.
- **The synthesized findings** — the buckets and the approach verdict.

### 2. Decide — the verdict comes before the post

A review carries its verdict, so decide it before anything is posted.
Evaluate the **approval gate** (below), then choose the review event with
the rules under "The verdict". Re-fetch the PR head now, immediately
before you decide (gate #7).

The head is checked once more at the submit. `auto-post-panel-review-comments`
fetches it again immediately before it posts an `APPROVE` or a
`REQUEST_CHANGES` review. When the head moved in between, it submits the
head-moved comment review (verdict rule 3) in its place. Report that
verdict, not the one you decided here.

An approval says that the whole review landed. When a routing override
sends findings to Linear, check that Linear is reachable before you
decide. If it is not, and the verdict would be `APPROVE`, use `COMMENT`
with the body `No decision: some findings could not be filed.` and report
what did not file. Any other verdict stands.

### 3. Post — one review, through `auto-post-panel-review-comments`

Hand the `auto-post-panel-review-comments` flow six things: the
synthesized findings, the PR ref, **the head SHA you captured before the
review**, the **event** from step 2, the **first line of the review
body** (see "Review body"; a request for changes has none, and the flow
chooses it), and — with an `APPROVE` or a `REQUEST_CHANGES`
event — the **head-moved line** from the same table, which it uses when
the head moves before the submit. Tell it to post against that pinned
`commit_id` instead of re-resolving the head (it accepts a caller-pinned
commit_id; see its "The review" section).

It submits **one review**. Every finding it posts is an inline comment
inside that review (mergeable suggestion / prose / body-only). A finding
whose line cannot take an inline comment goes into the review body. It
`+1`s anything a bot already raised, routes uncertain / out-of-PR-scope
findings to Linear, and honors routing overrides. All of that (the bar,
dedupe, the check on each line, Linear rules, zero-touch behavior, **and
the comment format**) is defined in that skill; use it verbatim.

Pinning matters: a push during the review would otherwise make auto-post
re-resolve to the new head and anchor your comments to code no panelist
saw. With the SHA pinned, comments anchor to exactly what was reviewed
(GitHub marks them outdated if the author has since moved the lines).

**Posting goes through `auto-post-panel-review-comments` and nothing else.**
Do not post the `panel-review` synthesis to the PR directly, and do not use
a built-in review/comment path (`/review`, `/code-review --comment`,
`gh pr comment`, or a hand-written `gh pr review`) to place the findings.
Three failure modes to refuse outright:

- **No comment outside the review.** One pass, one review. A finding
  posted as its own comment is a second notification, and it is not part
  of the verdict.
- **No severity-tagged comments.** `panel-review` labels findings `[LOW]`,
  `[MEDIUM]`, `[HIGH]` in its synthesis; those tags never reach the PR.
  `auto-post` strips them (a LOW becomes a `Small / Optional polish:`
  prefix, everything else posts the finding prose verbatim with no grade).
- **No summary / overview in the review body.** The body is one fixed
  line, plus any finding that cannot be inline. It is never a recap of the
  review. The consolidated summary is the step-5 report in chat.

A review is submitted **whatever the verdict**: a clean PR can still have
polish comments worth leaving, and they ride inside the approval itself.

**Every finding-based blocker must be visible in the review.** If a
finding-based gate failure — a must-fix/should-fix (#3), a substantiated
`Approach (questionable)` flag, or a verified `Purpose (stated, not served)`
flag (#4) — makes you request changes but isn't already in the PR-bound
posting set, add it before posting: at its root-cause `file:line` when it
has one, otherwise in the review body. (This is only about findings; the
structural reasons — draft #5, self-authored #6, head-moved #7, coverage
#1 and #2 — have no finding to post. The first line of the review body
names them.) A request for changes whose reason the author can't see on
the PR is a silent block; never request changes on a finding you didn't
post.

If the review does not land (GitHub API errors, a pending review in the
way), **surface the failure**. Nothing is on the PR, so there is no
approval and no request for changes: the verdict line is
`DECISION: No action`.

### 4. After an approval — run `approve-pr` for the follow-up

The approval is already on the PR: it is the review from step 3. When that
review approved, invoke the `approve-pr` skill and tell it **the approval
already landed** (its "The approval already landed" path). It confirms the
approval on GitHub and does its follow-up (the Slack reaction). It does
**not** submit a second review. Skip this step for any other verdict.

### 5. Report

One consolidated report: the review summary (Risk + bucket counts), what
auto-post did (the review URL and its event, inline comments / findings in
the body / `+1`'d / filed to Linear / dropped / needs attention), and the
**verdict** — approved (with the body used), changes requested (with the
findings that block), or no decision (with the specific gate condition
that failed).

Then end with one line, on its own. Nothing follows it except a machine
trailer the caller's system prompt asks for:

```
DECISION: Approve
```

The line says which review you submitted, one of four fixed strings:

1. `DECISION: Approve` — you submitted an approving review.
2. `DECISION: Request changes` — you submitted a review that requests
   changes.
3. `DECISION: Comment` — you submitted a comment review: no decision, or a
   post-only run.
4. `DECISION: No action` — you submitted no review: a dry run, a target
   that is not a PR, your own PR with nothing to post, a "no decision"
   review that would repeat your last one, or a review that did not land.

Put no reason on the line; the report above is the reason. A reader who
scrolls to the end gets the answer. A caller that reads only the last line
gets the same one. The line is fixed text, like the review bodies. When
`pr-review-tab` drives this skill, the tab's one-line outcome goes above
the line, and the line stays last.

## The approval gate

Approve **only if every one** of these holds. If any fails, the review
does not approve: "The verdict" says whether it requests changes or
comments.

1. **Reviewer coverage — ≥ 75% of the panel returned.** At least **75% of
   the launched panelists returned a verdict** (`exit 0`) — concretely,
   `count(exit 0) >= ceil(0.75 × count(launched))` — **and** the hard floor
   in #2 (≥ 2 returned) holds. One panelist dropping out of a four-panel
   run (3/4 = 75%) still clears this, provided the returning reviewers are
   clean; losing two of four (2/4 = 50%), a panelist out of three
   (2/3 ≈ 67%), or the sole survivor of a two-panel run (1/2 = 50%) does
   not. "Returned a verdict" means a clean `exit 0`, not merely "produced
   some output"; any non-zero exit (crash, timeout, the flaky `database is
locked`) is a **missing** reviewer, not a returned one. If you can't
   establish per-panelist exit status for every launched panelist (e.g. the
   output was truncated), treat coverage as **not** established and don't
   approve — never infer coverage from the synthesis alone. The quorum only
   forgives the _missing_ reviewers: the panelists that **did** return must
   still clear every other gate condition (no blocking findings #3, sound
   approach #4) — a 75% quorum never lowers the bar on the reviewers
   present. Coverage is the gate's central safety property.
2. **Enough independent reviewers — at least two, not narrowed.** Two
   things must both hold: (a) a hard floor of **≥ 2 distinct panelists
   returned `exit 0`** — one opinion is never enough to auto-stamp, even
   if it's the only supported CLI installed on the host; and (b) the panel
   wasn't **narrowed** below `panel-review`'s default via `--panelist`
   (you launched the review in step 1, so you know whether it was
   narrowed). A single-reviewer run — whether hand-picked with
   `--panelist` or just the only CLI on PATH — fails this: leave approval
   to a human and say so. (This is the bar for the _approval_; the review
   and posting still run on whatever panel the user chose, and a finding
   that blocks still gets a request for changes.)
3. **No blocking findings.** The synthesis has **zero must-fix
   (CRITICAL/HIGH)** and **zero should-fix (MEDIUM)** findings. Only
   **polish (LOW)** findings, or none at all.
4. **Sound approach, served purpose.** No substantiated
   `Approach (questionable)` flag and no verified
   `Purpose (stated, not served)` flag — independent invariants: a
   wrong-layer change, or a change that does not do what its description
   says, must not be stamped even if every line-level finding is LOW,
   regardless of how those flags' severities happen to be bucketed. (They
   usually also land in must-fix, so #3 often catches them too — but don't
   rely on that mapping; check the approach and purpose verdicts directly.)

   A verified `Purpose (unknown)` does **not** withhold approval. A thin
   description is worth a comment, not a block; `panel-review` buckets it
   as LOW and it rides along in the polish comments. Likewise a verified
   `Proof (missing)`: `panel-review` buckets it as LOW except on auth,
   session handling, payments, schema migrations, crypto, or production
   infra, where it lands in must-fix and #3 withholds approval on it.

   A rendering defect does not withhold approval either. `panel-review`
   caps one at LOW unless it blocks the primary action, misstates money or
   state, locks people out, or breaks the layout at a supported viewport —
   and an uncapped one lands in should-fix or must-fix, where #3 catches it.
   Do not second-guess the cap here: a finding tagged `(ui)` at LOW rides
   along in the polish comments like any other.

5. **Not a draft.** The PR is **not** a draft. `gh pr review --approve`
   succeeds on draft PRs, but a draft is the author explicitly saying
   "not ready" — check `gh pr view <ref> --json isDraft --jq '.isDraft'`
   and **don't approve** when `true`. A draft gets comments and no
   verdict.
6. **Not your own PR.** GitHub rejects an approving review from the PR
   author. Compare the authenticated user (`gh api user --jq '.login'`)
   against the PR author (`gh pr view <ref> --json author --jq
'.author.login'`); if they match, **don't approve**. GitHub rejects a
   request for changes from the author too, so your own PR gets comments
   and no verdict.
7. **Head unchanged since review.** Re-fetch the PR head SHA and confirm
   it still equals the `headRefOid` you captured before the review (see
   "Target"). If the author pushed during the run, the current head was
   never reviewed — **don't approve**, and don't request changes either:
   the findings are for an older commit and can already be fixed. The
   comments still post, anchored to the reviewed SHA.

When the gate passes, Risk will be LOW by construction.

## The verdict

The review event follows from the gate. Apply these rules in order. The
first rule that matches wins, so every pass has exactly one verdict:

1. `COMMENT` — **post-only.** The invocation did not ask for a verdict
   (see "Autonomy").
2. `COMMENT` — **no verdict is possible on this PR.** It is your own PR
   (#6), or it is a draft (#5).
3. `COMMENT` — **the head moved** since the review (#7).
4. `REQUEST_CHANGES` — **a finding blocks.** The synthesis has a must-fix
   or a should-fix finding (#3), a substantiated `Approach (questionable)`
   flag, or a verified `Purpose (stated, not served)` flag (#4).
5. `APPROVE` — **the gate passes.** Rules 1-4 did not match, and coverage
   (#1) and panel size (#2) hold. One exception: when a routing override
   sends findings to a Linear that is not reachable, the event is
   `COMMENT` (see step 2).
6. `COMMENT` — **too few reviewers to decide.** Nothing blocks, but
   coverage (#1) or panel size (#2) failed. When both failed, use the
   coverage line when fewer panelists returned than were launched, and the
   panel-size line otherwise.

Rule 4 comes before the coverage rules on purpose. Coverage protects the
approval: a short panel that found nothing has not shown that the PR is
clean. A finding that you verified shows that the PR needs a change, and
that holds however many reviewers answered. So a must-fix from two of four
panelists gets a request for changes, and a clean result from the same two
gets no decision.

A request for changes is the default for a blocking finding. On a branch
that requires reviews it blocks the merge until a later pass approves,
which `recheck-pr` does when the author fixes or explains every finding.
If the user says "don't request changes",
"don't block" or "comment only", rule 4 gives `COMMENT` with the body
`Comments only. No decision.` instead.

**When there is nothing to post, post nothing** in three cases, and write
`DECISION: No action`:

- **Your own PR with no finding to post.** A review that tells you about
  your own PR is noise. Report in chat.
- **A post-only run with no finding to post.** A review that says
  `Comments only. No decision.` and holds no comment says nothing.
- **A repeat.** The event is `COMMENT`, there is no comment to attach, and
  your latest review on this PR already says the same thing at the same
  commit (`auto-post-panel-review-comments` makes this check). A second
  identical "no decision" review tells the author nothing new.

Every other "no decision" is posted, even with no comment attached: the
author must be able to see that the review ran and why it gave no verdict.

## Review body

The first line of the review body is fixed. Short, ASCII, no emoji (repo
convention), no review summary dump. Hand it to
`auto-post-panel-review-comments` with the event; it adds only the
findings that cannot be inline.

| Verdict                       | First line of the body                                                          |
| ----------------------------- | ------------------------------------------------------------------------------- |
| Approve, no findings at all   | `LGTM`                                                                          |
| Approve, LOW/polish posted    | `LGTM, just some small comments, nothing blocking`                              |
| Request changes               | chosen by `auto-post-panel-review-comments` (see below)                         |
| Comment: post-only            | `Comments only. No decision.`                                                   |
| Comment: your own PR          | `No decision: this is my own PR.`                                               |
| Comment: draft                | `No decision: this PR is a draft.`                                              |
| Comment: head moved           | `No decision: the head moved during the review. These comments are for <sha7>.` |
| Comment: coverage short (#1)  | `No decision: only <k> of <n> reviewers answered.`                              |
| Comment: panel too small (#2) | `No decision: the panel was too small for a verdict.`                           |
| Comment: Linear unreachable   | `No decision: some findings could not be filed.`                                |

Use those bodies as-is (don't vary the wording — a stable body keeps the
review auditable). `<sha7>` is the first seven characters of the reviewed
SHA; `<k>` and `<n>` are the returned and launched panelist counts.

A request for changes is the one verdict with no line in this table. Hand
`auto-post-panel-review-comments` the event and no first line. It takes
one of its three lines from what the review shows the author: one blocking
item, more than one, or an approach to look at again (see its "The body").
The count is not known here, because a finding can still go to Linear or
become a `+1`. Report the line that it used.

A user-supplied explicit approval message is passed through verbatim
**only when the verdict is Approve** (it's the approval body). It never
goes on a request for changes or a comment review.

## Autonomy

`auto-review` is automatic by design: once started it reviews, decides,
and submits its review without stopping for confirmation, then reports
once. The verdict is gated by the strict conditions above — that gate,
plus an invocation that asked for a verdict, is the authorization. Three
honest limits on that autonomy:

- **A verdict needs an approval intent.** The review approves or requests
  changes only when the invocation actually asks for the full
  review→decide→**submit** pipeline. Any request that prefixes **"auto"**
  onto a review — "auto-review", "auto review", "auto panel review", "auto
  panel-review", "auto-panel-review" — carries approval intent: the "auto"
  _is_ the cue, and the word "panel" in the middle changes nothing. So do
  explicit cues like "review and approve if clean", "stamp it if clean",
  "approve when ready", "request changes if it needs them". When the user
  asked only to **review and post** ("review and post the comments",
  "panel-review then auto-post", "post the findings"), treat it as
  post-only: the review is a `COMMENT` review with every comment attached
  and the body `Comments only. No decision.` (verdict rule 1). Report the
  verdict you _would_ have given. (The opt-out phrases "don't approve" /
  "no auto-approve" force post-only regardless.)

  **Never ask the user whether to approve or to request changes.**
  Ambiguous intent resolves _silently_ to post-only — submit the comment
  review, then report the verdict you would have given and note that no
  approval intent was detected. Stopping mid-pipeline to ask defeats the
  point of an `auto-` skill; an unrequested verdict is the costlier
  mistake, and reporting the withheld one costs the user one follow-up
  word.

- **Surface the PR before you submit.** Even though it's automatic, name
  the PR (`PR #N — title — url`) in the flow so a wrong target is visible
  before the review notification goes out.
- **Never give a verdict on an unreviewed head.** If the PR head moved
  between the review and the verdict (see "Target"), the review is a
  comment review — the comments still post, but an approval would cover
  code no panelist saw, and a request for changes can name a fault that is
  already fixed.

## Gotchas

- **Don't reimplement the sub-skills.** Call `panel-review`,
  `auto-post-panel-review-comments`, and `approve-pr`. The wiring, the
  gate and the verdict are the only things this skill adds.
- **One pass, one review.** Every comment of the pass is inside the review
  that carries the verdict. Never post a comment by itself, and never
  submit a comment review and then a separate approval.
- **Decide first, then post.** The verdict is part of the review, so the
  gate runs before anything reaches the PR. Do not post the comments and
  work out the verdict afterwards.
- **Never dump the synthesis onto the PR.** All posting goes through
  `auto-post-panel-review-comments`. No `[LOW]`/`[MEDIUM]`/`[HIGH]`-tagged
  comments (those tags are panel-review's internal grading, stripped before
  posting) and no "review summary" in the review body. One comment per
  finding, and the recap lives in the chat report. See step 3.
- **Coverage below 75% blocks approval, not a request for changes.** The
  flaky local CLIs sometimes exit non-zero (`database is locked`,
  timeouts). One panelist dropping out of a four-panel run (3/4 = 75%)
  still clears the coverage gate as long as the returning reviewers are
  clean; falling below 75% of the launched set — or below the ≥ 2 floor —
  does not. When coverage falls short and nothing blocks, submit the "no
  decision" comment review, and report which panelist was missing so the
  user can re-run. When a finding blocks, request changes whatever the
  coverage.
- **Polish rides inside the approval.** A clean-enough-to-approve PR can
  still have LOW comments worth leaving; they are the inline comments of
  the approving review, which has the "small comments, nothing blocking"
  body. Approval and polish comments are not mutually exclusive.
- **A request for changes is the default when a finding blocks.** A
  must-fix or a should-fix finding gets `REQUEST_CHANGES`, not a comment
  review. A comment review means "no decision", and a PR with a known
  fault has a decision. The user can turn it off for a run ("don't request
  changes").
- **Never request changes on polish.** LOW findings do not block. A PR
  with only polish findings is approved, or gets no decision when coverage
  is short.
- **Same PR throughout.** Resolve the PR once and reuse it for the review,
  the verdict and the post — don't re-detect per step (the branch could be
  read differently, or drift).

## Dry-run mode

If the user asks for a dry run, or sets `AUTO_REVIEW_DRY_RUN=1`, run the
review for real (it's read-only) but submit nothing. Write:

- the `auto-post-panel-review-comments` dry-run artifacts for the posting
  step: `./review.json` (the one review payload: `commit_id`, `event`,
  `body`, `comments`; `null` when no review would be submitted),
  `./reactions.json`, `./linear_tickets.json`.
  Note that auto-post's dry-run **also writes a `./report.md` by default**
  (and an abort `./report.md`), which would clobber auto-review's
  consolidated report — so **pass the auto-post step `./post-report.md` as
  its report path** (its dry-run honors caller-supplied paths). That keeps
  `./report.md` free for auto-review's own consolidated report below; fold
  auto-post's `./post-report.md` disposition into it.
- `./approval.json` — the verdict. `event` is the review event
  (`"APPROVE"`, `"REQUEST_CHANGES"`, `"COMMENT"`, or `null` when no review
  would be submitted). `body` is the first line of the review body, or
  `null` with no review. `coverage` is the returned/launched count (a
  quorum ≥ 75% passes — e.g. `"3/4"`). Examples:
  `{ "approve": false, "event": "COMMENT", "reason": "coverage 2/4 below 75%", "body": "No decision: only 2 of 4 reviewers answered.", "coverage": "2/4" }`,
  `{ "approve": false, "event": "REQUEST_CHANGES", "reason": "1 should-fix finding", "body": "Just one thing I think we should update before this gets in.", "coverage": "3/3" }`,
  and on a pass with one panelist lost:
  `{ "approve": true, "event": "APPROVE", "reason": "clean: LOW/polish only, quorum 3/4", "body": "LGTM, just some small comments, nothing blocking", "coverage": "3/4" }`.
- `./report.md` — auto-review's **final consolidated** report (review
  summary + posting disposition + verdict). The single
  authoritative report; auto-post's posting detail lives in
  `./post-report.md` and is summarized here.

Honor user-supplied paths if provided.
