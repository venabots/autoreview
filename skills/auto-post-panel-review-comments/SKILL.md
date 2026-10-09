---
name: auto-post-panel-review-comments
description: >
  Take a list of code-review findings (typically from a prior
  `panel-review`) and post the legitimate ones straight to the PR as ONE
  review — no select-list, no per-finding prompts. Each clean finding
  becomes an inline comment inside that review (a mergeable
  ` ```suggestion ` block when the fix is a drop-in, prose otherwise), and
  the review carries the verdict a caller or the user gave it: an approval
  or a request for changes from a caller that owns a gate, a request for
  changes when the user asks for one, and a plain comment review
  otherwise. Anything an automated
  reviewer already raised is +1'd instead of duplicated; uncertain or
  out-of-PR-scope findings go to Linear (when reachable) instead. Use when
  the user wants the findings posted automatically — "auto-post these",
  "just post the legitimate findings", "post them all to the PR, no
  triage", "auto mode", "post the panel review comments automatically",
  "request changes with these". Honors routing overrides like "post the
  LOW/polish ones to Linear". Do NOT use when the user wants to *pick*
  which findings go where (the interactive `post-panel-review-comments`),
  to *generate* the findings (run `panel-review` first), or to act on
  comments already on the PR (`pr-comment-handler`).
---

# auto-post-panel-review-comments

The zero-touch twin of `post-panel-review-comments`. Same findings, same
comment shape, same dedupe — but **no interactive triage**, and **one
review instead of one comment at a time**. Instead of two `AskUserQuestion`
select stages, this skill applies a fixed bar automatically. It does not
stop to confirm; it acts, then reports.

Everything it posts to the PR in one run lands as **a single GitHub
review**: one entry on the PR timeline, one notification, one verdict, with
every finding attached to it as an inline comment. It never posts findings
as separate comments.

**The comment body shape is shared with `post-panel-review-comments`** (the
finding verbatim, the mergeable-suggestion rules, the polish prefix). How
the comments land is not: the interactive skill posts each one by itself,
and this skill puts them all in one review. This file is complete for
everything this skill does. Do not take the posting mechanics from
`post-panel-review-comments`.

## Language

Write every word **you** author in ASD-STE100 Simplified Technical
English — the review body, the `**Location:**` line, the Linear title and
description, and the report.

- Write short sentences. Keep an instruction to 20 words. Keep a
  statement to 25 words.
- Put one idea in each sentence.
- Use the active voice. Write "the handler drops the second charge", not
  "the second charge is dropped by the handler".
- Use simple tenses. Write "the test fails", not "the test has been
  failing".
- Use one word for one meaning. Do not change "function" to "method" to
  "routine" in the same text.
- Use the simplest word that is correct. Remove jargon, idioms,
  metaphors, and figures of speech.
- Do not make a noun out of a verb. Write "when the job starts", not "at
  job initiation".
- Keep a paragraph to six sentences or fewer.

The finding body still posts **verbatim** — this rule governs the text
around it, not the finding itself. `panel-review` holds its panelists to
the same style, so a finding from the panel already reads this way.

The first line of the review body is a fixed string (see "The body"). Use
it exactly as written; do not restyle it.

## What "auto" changes

|                    | `post-panel-review-comments`               | this skill                                |
| ------------------ | ------------------------------------------ | ----------------------------------------- |
| Which findings act | user picks via select lists                | fixed bar (below), applied automatically  |
| Confirmation       | confirms drop count before acting          | none — posts immediately, reports after   |
| PR vs Linear       | Stage 1 (PR) then Stage 2 (Linear)         | auto-routed per finding (below)           |
| Dedupe             | two passes (detect at triage, act at post) | one pass at post time                     |
| How it lands       | one standalone comment per finding         | one review that holds every comment       |
| Verdict            | none                                       | the caller's, the user's, or comment only |

The comment body, the suggestions and the Linear ticket shape are
identical.

## Inputs

1. **Findings** — usually from a prior `panel-review`. Each needs a file
   path, line (or range), and body; a fix and severity when present.
2. **PR ref** — number or URL. Take it from the caller's context (the
   review's scope), exactly as `post-panel-review-comments` does.

   Auto mode can't prompt, so PR resolution has one extra guarded
   fallback: if no PR ref is in context, **list** the current branch's
   open PRs with `gh pr list --head <current-branch> --state open --json number,url,title,headRefOid,headRepositoryOwner,headRepository,isCrossRepository`
   and proceed **only if exactly one** is returned **and that PR's head is
   this repo, not a fork** (`isCrossRepository == false`, or
   `headRepositoryOwner`/`headRepository` matching your origin/push
   remote). The repo-match check matters because `--head` filters by
   branch _name_ only — a same-named branch on someone's fork can be the
   sole match, and posting to it would leak a review onto a stranger's PR.
   Don't use `gh pr view` for this — it resolves a single PR (and will
   happily return a merged/closed one), so it can't prove the "exactly one
   open PR" guard. If the count is zero, more than one, or the sole match
   is a fork PR, **do not post** — abort and report that a PR ref is
   required (list the candidates when there are several). Never send a
   review to a guessed PR. Always state in the report which PR was
   resolved and how (from context vs. branch auto-detect).

3. **Event and body line** — optional, from a caller that owns a gate
   (`auto-review`, `recheck-pr`). The caller hands you the review event
   (`APPROVE`, `REQUEST_CHANGES` or `COMMENT`) and the first line of the
   body. Use both as given. With no caller, you choose the event (see
   "The event").

If a finding lacks a file/line, it can't be an inline comment — route it
per "Routing" below (usually Linear or dropped), never guess a location.

## The bar (which findings act, and where)

Apply this to every finding, in order. No prompts.

1. **Drop pure noise.** A finding with no concrete file:line _and_ no
   actionable substance is dropped (counted in the report). Exception: a
   `panel-review` finding located at `PR description` (a promoted
   `Purpose (unknown)` flag) is actionable and in-scope; it has no line to
   anchor to, so it goes into the review body (see "The body", with
   `**Location:** PR description`).
2. **Route uncertain / out-of-scope findings to Linear** (see "Routing").
   A finding is uncertain when you cannot confidently confirm it's real
   from the diff (speculative, needs investigation, depends on unverified
   external context). A finding is out-of-scope when it's a real issue the
   review surfaced but it's **not about this PR's changes** (pre-existing
   bug in untouched code, unrelated tech debt).
3. **Everything else → the review.** A finding that is confidently
   in-scope and has a concrete file:line becomes an inline comment in the
   review — **unless** an automated reviewer already raised it, in which
   case +1 the existing comment instead (see "Dedupe"). A suggested fix is
   **not** required: when the finding has one, render it (suggestion or
   prose); when it doesn't, post the finding body alone and omit the fix
   line entirely — never invent one.

**"Actionable + deduped" is the default bar** — post confident, in-scope,
located findings; +1 the ones a bot already covered; send the uncertain
and out-of-scope ones to Linear. "Located" means a concrete file:line;
a fix is a bonus, not a gate. Severity is **not** a gate either by default
(a confident LOW with a clean fix is still worth a one-line suggestion).

### Honoring explicit overrides

If the user's request carries routing instructions, they win over the
default bar:

- "post the LOW / polish ones to Linear" → route LOW-severity findings to
  Linear instead of the PR.
- "only post HIGH/MEDIUM" / "drop anything below HIGH" → apply that
  severity gate; drop or Linear-route the rest per their phrasing.
- "everything to the PR" → skip the uncertain/out-of-scope Linear routing
  and post all located findings.
- "don't touch Linear" → PR-only; drop what doesn't qualify for the PR.
- "request changes with these" / "block it if it needs changes" → the
  event is `REQUEST_CHANGES` when a finding is MEDIUM or higher (see "The
  event").
- "comment only" / "don't request changes" / "don't block" → a
  `REQUEST_CHANGES` event becomes `COMMENT`, with the body
  `Comments only. No decision.`, also when a caller handed it to you. An
  `APPROVE` from a caller stands: the user turned off the block, not the
  approval.

Surface which override you applied in the report.

## Routing (PR vs Linear vs drop)

- **PR** — confident, in-scope, has a concrete file:line (a fix is
  optional). An inline comment in the review (suggestion or prose if a fix
  exists, finding body alone otherwise), or a block in the review body
  when its line cannot take an inline comment. This is where _most_
  findings should land. A finding located at `PR description` goes
  straight into the body.
- **Linear** — uncertain/needs-exploration findings, and real-but-out-of-
  PR-scope findings, plus anything an override sends there. Only when
  Linear is reachable from this session (see below).
- **Drop** — pure noise, or findings with no location an override didn't
  rescue. Always counted in the report, never silent.

### Linear availability and target (zero-touch constraint)

File to Linear **only when both** hold:

1. Linear is reachable from this session (an MCP server, a `linear` CLI, a
   configured token — whatever your runtime exposes), **and**
2. A target team/project is resolvable **without a prompt** — named in the
   user's request, present in the caller's context, or set via env/config.

If Linear is unreachable, or no team/project can be resolved without
asking, **do not prompt and do not silently drop** the Linear-bound
findings: list them in the report under a "needs your attention" section
so the user can file them manually. Zero-touch means never blocking on
input — degrade to reporting, not to a question.

Ticket shape is identical to `post-panel-review-comments`: title from the
finding's first sentence (≤ ~80 chars); description with the PR link +
file:line deep-link, `File:`, `Severity:`, blank line, the finding body,
then — **only when the finding has a suggested fix** — the fix as a
`**Possible Solution:**` prose line (**never** a ` ```suggestion ` block —
those are GitHub-inline-only; and never invent a fix when none was given).
**Do not set priority.**

## The review

One run submits **one review**, with one call:

```
POST /repos/{owner}/{repo}/pulls/{pull_number}/reviews
```

The payload has four parts: `commit_id`, `event`, `body`, and `comments`
(one entry per inline comment). Never post a finding with
`POST /pulls/{N}/comments`, `gh pr comment` or `gh pr review --comment`:
each of those is a separate timeline entry and a separate notification,
and none of them is part of the review.

Resolve the PR once up front with
`gh pr view <ref> --json number,url,title,author,isDraft,headRefOid`.
`headRefOid` is the `commit_id`; the `{owner}/{repo}` comes from the PR
`url` (`https://github.com/{owner}/{repo}/pull/N` — the base repo, which
is where a review is posted even for fork PRs).

**Caller-pinned commit_id.** If an orchestrator (e.g. `auto-review`) hands
you a specific head SHA to post against, treat it as the **effective
reviewed SHA** (`effective_sha = pinned commit_id ?? headRefOid`) and use
it **everywhere a SHA appears** — the review's `commit_id`, the
`blob/<SHA>/...` deep-links in the body, the Linear ticket links, and the
report. It's pinning the exact revision that was reviewed so nothing
drifts onto a head that was pushed mid-review (GitHub marks moved inline
comments outdated; the blob and Linear links stay anchored to the reviewed
code). Only re-resolve `headRefOid` when no commit_id was pinned.

### The event

The event is the verdict of the review. A caller's event wins. With no
caller event, apply these rules in order. The first rule that matches
wins:

1. `COMMENT` — the PR is your own, or it is a draft. GitHub rejects a
   request for changes from the author of the PR. A draft is the author
   saying "not ready", so it gets comments and no verdict.
2. `REQUEST_CHANGES` — the user asked for a verdict ("request changes with
   these", "block it if it needs changes"), and a finding that you post or
   `+1` is CRITICAL, HIGH or MEDIUM (`panel-review`'s must-fix and
   should-fix).
3. `COMMENT` — everything else. A request to post findings is not a
   request for a verdict, so "auto-post these" gets a comment review, with
   the body `Comments only. No decision.`

A verdict needs someone who asked for it. `auto-review` holds a post-only
request to the same rule, because a verdict that nobody asked for is the
costlier mistake. This skill never chooses `APPROVE` by itself: an
approval needs a gate, and the gate belongs to the caller. A caller that
hands you `APPROVE` has applied it.

Two limits apply to a caller's event too. Say so in the report when one
changes the event:

- **Your own PR.** When the PR is your own and the event is `APPROVE` or
  `REQUEST_CHANGES`, use `COMMENT`, and replace the first line of the body
  with `No decision: this is my own PR.` GitHub accepts no verdict from
  the author, and a comment review that still says `LGTM` reads as one.
- **A head that moved.** When a caller pinned a commit and the event is
  `APPROVE` or `REQUEST_CHANGES`, fetch `headRefOid` again immediately
  before you submit. The caller decided minutes ago, and the reactions and
  the Linear tickets take time too. When the head is no longer the pinned
  commit, use `COMMENT` with the caller's head-moved first line
  (`auto-review` and `recheck-pr` each define one). A verdict must not
  land on a head that nobody reviewed.

### The body

The body is short. Its first line is a fixed string:

| Event             | First line                                                |
| ----------------- | --------------------------------------------------------- |
| `APPROVE`         | the caller's approval body, verbatim (for example `LGTM`) |
| `REQUEST_CHANGES` | `This PR needs changes. See the comments.`                |
| `COMMENT`         | the caller's line, or `Comments only. No decision.`       |

A caller can hand you a different first line for any event (`recheck-pr`
does for a second round). Use the caller's line verbatim.

Only three things can follow the first line, each after a blank line:

- **Lines that the caller supplies.** `recheck-pr` adds one line for each
  finding that is still open, and a line that says the earlier request is
  satisfied. Use them verbatim, directly after the first line.
- **Findings that cannot be inline** (see "Which findings can be inline").
  One block per finding: a `**Location:**` line with the file:line and its
  blob deep-link (or `**Location:** PR description`), then the same body
  shape as an inline comment — except a suggestion **downgrades to a
  `**Possible Solution:**` prose line**, because a suggestion block only
  works on an inline comment.
- **Existing comments that block.** When the event is `REQUEST_CHANGES`
  and a finding you `+1` is CRITICAL, HIGH or MEDIUM, add one line for it:
  `Also blocking: <html_url>`, with the URL of the existing comment. The
  author must be able to see every reason for a request for changes.

**The body is never a summary.** No finding count, no risk recap, no list
of severities, no panelist names, no restated inline comments. Each
finding says its piece once, at its own line. The recap belongs to the
caller's chat report, not the PR.

### Comment body shape (shared with `post-panel-review-comments`)

Each entry in `comments` has this body:

- The finding body **verbatim**. No severity, no priority, no
  panelist/agent attribution. In particular, strip any `[LOW]` / `[MEDIUM]`
  / `[HIGH]` grade `panel-review` prefixed onto the finding. It never
  appears on the PR (a LOW becomes the `Small / Optional polish:` prefix
  below; everything else posts with no grade at all).
- When the fix is a **clean drop-in replacement for the commented
  line(s)**, render it as a mergeable suggestion:

  ````markdown
  <finding body verbatim>

  ```suggestion
  <exact replacement for the commented line(s)>
  ```
  ````

  The suggestion is inline-only, must anchor to exactly the lines it
  rewrites (and still include the finding's reported location — don't move
  the anchor off it), and the replacement must be exact with file-matching
  indentation. When the fix isn't a faithful drop-in, use a
  `**Possible Solution:** <fix>` prose line instead. A finding gets a
  suggestion **or** a prose fix line, never both, and never an invented
  fix.

- For **LOW / polish** findings, prefix the body with
  `Small / Optional polish:` (or equivalent soft framing).

**Line targeting:** single-line → `path`, `line`, `side: "RIGHT"`;
multi-line → `path`, `start_line`, `start_side: "RIGHT"`, `line` (the
**end** of the range), `side: "RIGHT"`.

**Order:** put the entries in `comments` HIGH → MEDIUM → LOW (within
severity, group by file), so the author reads them in priority order.
Findings with no severity sort last, preserving their input order.

**Deep-links:** build with `scripts/pr-line-url.sh <pr-url> <path>
<line-or-range>` for lines in the diff, or the blob fallback
`https://github.com/<OWNER>/<REPO>/blob/<SHA>/<PATH>#L<LINE>` for
lines outside the hunk. Used in the report, in the review body and in any
Linear body.

### Which findings can be inline

GitHub accepts an inline comment only on a line inside a diff hunk. A
review is one call, so **one comment on a line outside the diff makes
GitHub reject the whole review** (HTTP 422). Check every anchor before you
build the payload:

```bash
scripts/pr-diff-lines.sh <pr> [<effective_sha>]
```

It prints one row per hunk, `<path><TAB><first-line><TAB><last-line>`, for
the new side of the diff. Pass the effective SHA when a caller pinned one.

- A single-line finding is inline when its line is inside a row for its
  path.
- A multi-line finding is inline when its start and end are inside the
  **same** row. A range that crosses two hunks, or that starts outside
  every row, is not accepted: the finding fails the check.
- A finding that fails the check moves to the review body, as a
  `**Location:**` block (see "The body"). The same goes for a finding in a
  file with no rows at all: a binary file, or a file too large for a
  patch.

Never drop a finding because its line is outside the diff, and never move
its anchor to a different line to make it fit.

### Submit

Write the payload to a file in a temporary directory, not in the user's
checkout, then post it:

```json
{
  "commit_id": "<effective_sha>",
  "event": "REQUEST_CHANGES",
  "body": "This PR needs changes. See the comments.",
  "comments": [
    { "path": "src/pay.ts", "line": 88, "side": "RIGHT", "body": "<comment body>" },
    {
      "path": "src/pay.ts",
      "start_line": 40,
      "start_side": "RIGHT",
      "line": 44,
      "side": "RIGHT",
      "body": "<comment body>"
    }
  ]
}
```

```bash
gh api --method POST "repos/{owner}/{repo}/pulls/<N>/reviews" \
  --input "$payload" --jq '{id, state, html_url}'
```

The reply names the review: its `id`, its `state` (`APPROVED`,
`CHANGES_REQUESTED` or `COMMENTED`) and its `html_url`. Report the URL.
Omit `comments` when there is no inline comment; a review with a body
alone is valid.

**Check for a pending review first, before any write.** GitHub accepts one
pending review for each user on a PR, and a review that somebody started
in the browser and did not submit blocks yours. Make this check before the
first reaction, the first Linear ticket and the first thread reply, so a
run that cannot submit its review leaves nothing half done:

```bash
me="$(gh api user --jq .login)"
gh api "repos/{owner}/{repo}/pulls/<N>/reviews" --paginate \
  | jq -s --arg me "$me" 'add | map(select(.user.login == $me and .state == "PENDING")) | length'
```

Only your own pending review blocks you; another reviewer's draft does
not. When the count is not 0, **stop**. Do not delete the pending review — it
can hold comments a person wrote by hand. Post nothing, and report that
the pending review must be submitted or discarded on the PR first.

**Submit the review last.** Add the `+1` reactions and file the Linear
tickets first. A caller that replies to threads does that first too. The
latest review you submit is the one GitHub, and any tool that reads the PR
back, takes as this run's verdict.

**When there is nothing to say, post nothing.** Skip the review, and report
that you did, in three cases:

- No caller gave an event, and no finding is bound for the PR.
- The body is `Comments only. No decision.`, and there is no inline
  comment and no body block: every finding was a `+1`, went to Linear, or
  was dropped. A review that announces comments and holds none says
  nothing.
- The event is `COMMENT`, there is no inline comment and no body block,
  and your latest review on this PR already has the same body at the same
  commit. Check with
  `gh api "repos/{owner}/{repo}/pulls/<N>/reviews" --paginate | jq -s 'add | map(select(.user.login == "<you>")) | last | {id, state, body, commit_id}'`.
  Note the `id` whatever the answer: it is how you tell an old review from
  this run's after a call that failed.
  A second identical "no decision" review tells the author nothing new.

### When GitHub rejects the review

A review either lands whole or not at all. Read the error and act once:

- **A comment's line is not in the diff.** The error names a line, a
  hunk or a path that GitHub could not place. An anchor got past the
  check. Move **every** inline comment into the body as a `**Location:**`
  block, drop `comments`, and submit again. The review still lands, with
  every finding in it. Call this out in the report.
- **The pinned commit is not in the PR.** The error names the commit, or
  the review with every finding in the body is rejected again. A force-push
  removed the commit you reviewed. Submit a `COMMENT` review with no
  `commit_id`, so that it lands on the current head, with every finding in
  the body and the caller's head-moved first line. A verdict is not
  possible on a head that nobody reviewed.
- **A verdict on your own PR.** The error says that you cannot approve, or
  cannot request changes on, your own pull request. Change the event to
  `COMMENT` and the first line to `No decision: this is my own PR.`, and
  submit again.
- **You already have a pending review.** The check under "Submit" missed
  it, or somebody started one since. Do not delete it. Post nothing more,
  and report that the pending review must be submitted or discarded first.
- **Anything else** (5xx, timeout, rate limit, auth). The review can have
  landed although the call failed. Read your latest review again (the same
  call as the repeat check above) and compare its `id` with the one you
  noted before the submit. A newer review with this run's body at this
  run's commit is this run's review: it landed, so do not submit again. An
  older review never counts, whatever its body. Otherwise retry once. If
  that fails too, post nothing more and report the failure with the error
  text.

Never fall back to standalone comments. A finding posted outside the
review is the failure this skill exists to prevent.

### Withdrawing your earlier request for changes

Only when a caller says its earlier request for changes no longer stands
(`recheck-pr` does, when the author fixed every blocking finding but the
round cannot approve).

A new `APPROVE` or `REQUEST_CHANGES` review replaces your earlier verdict
by itself. A `COMMENT` review does not: GitHub keeps your earlier request
for changes in force, and the PR stays blocked on a request you no longer
make. So before you submit that `COMMENT` review, dismiss the earlier one:

```bash
me="$(gh api user --jq .login)"
stale="$(gh api "repos/{owner}/{repo}/pulls/<N>/reviews" --paginate \
  | jq -s --arg me "$me" 'add
      | map(select(.user.login == $me and (.state == "APPROVED" or .state == "CHANGES_REQUESTED")))
      | last | select(.state == "CHANGES_REQUESTED") | .id')"
[[ -n "$stale" ]] && gh api --method PUT \
  "repos/{owner}/{repo}/pulls/<N>/reviews/$stale/dismissals" \
  -f message="The requested changes are in." -f event=DISMISS
```

Dismiss only your own review, and only the latest one that carries a
verdict. A dismissal needs write access to the repo, and on a protected
branch it needs the right to dismiss reviews (an administrator, or a
person the branch rule names). Many reviewers do not have it. When GitHub
refuses it, carry on: submit the `COMMENT` review, and report that the earlier
request is still in force and why.

## Dedupe — one pass, at post time

Auto mode has no triage pass, so dedupe collapses to a single pass run
just before posting. The fetch and match rules are identical to
`post-panel-review-comments`:

- Fetch both surfaces:
  `gh api repos/{owner}/{repo}/pulls/{N}/comments --paginate` and
  `gh api repos/{owner}/{repo}/issues/{N}/comments --paginate`.
- Treat a comment as a candidate duplicate **only when it's from another
  automated reviewer** (`user.type == "Bot"`, or a known review-bot login:
  `coderabbitai`, `copilot-pull-request-reviewer`, `cursor`, `greptile`,
  `sourcery-ai`, a `github-actions` bot, etc.). Never dedupe against human
  comments or your own prior comments — and exclude your **own** posting
  identity first: if this skill runs in CI under a `github-actions` token,
  its earlier comments are _yours_, not "another bot's". Get your own login
  once per run with `gh api user --jq '.login'` and skip comments authored
  by it before treating any `github-actions` comment as a dedupe candidate.
- A finding **matches** when it's the same issue — same file,
  overlapping/adjacent line, same underlying problem (judge semantically;
  different bot wording still counts). When unsure two are the same issue,
  **post your own** rather than collapsing to a reaction.

For each PR-bound finding:

1. **Matched → react, don't duplicate.** Add a +1 reaction to the existing
   comment and leave the finding out of the review's `comments`:
   - inline: `gh api -X POST repos/{owner}/{repo}/pulls/comments/{id}/reactions -f content=+1`
   - top-level: `gh api -X POST repos/{owner}/{repo}/issues/comments/{id}/reactions -f content=+1`

   Do not reply to the existing comment: a reply is a separate comment
   outside the review. When you have materially new detail (a repro,
   another affected location, a better fix), put it in the review as an
   inline comment of its own that starts `Some more info:`. Default is +1
   alone.

   A matched finding still counts toward a request for changes, and a
   blocking one then gets its `Also blocking:` line in the body.

2. **No match → into the review** per "The review".

## Reporting back

Zero-touch means the report is the only feedback the user gets — make it
complete. After acting, report:

- **PR resolution** — which PR (`#N — title — url`) and how it was resolved
  (from context vs. branch auto-detect).
- **Review** — the review URL, its event, and its first body line. Say who
  chose the event: the caller, or this skill's rules.
- **Inline comments** — count, grouped; note which carried a mergeable
  suggestion vs. a prose fix.
- **In the review body** — which findings could not be inline, and that
  any suggestion was downgraded to prose. Call out distinctly. Say so when
  a rejected review made you move every comment there.
- **+1'd (deduped)** — for each, the existing comment's `html_url`, the bot
  that authored it, and whether you also added a `Some more info:` comment.
- **Filed to Linear** — ticket URLs, grouped.
- **Needs your attention** — findings bound for Linear that couldn't be
  filed (Linear unreachable / no resolvable team/project), so the user can
  handle them. Include file:line + deep-link.
- **Dropped** — count and one-line reason each.
- **Any override** applied to the default bar or the event.
- One-line summary:
  `Submitted 1 review on PR #X (<event>): N inline, J in the body. +1'd D existing automated comments, filed M to Linear, D2 need attention, dropped K`.
- A review that did not land, with the error text, for manual handling.

## Gotchas

- **No prompts, ever.** This is the whole point. If something would
  normally require asking (missing PR ref, unresolved Linear target),
  degrade to the report — abort posting for a missing PR, defer Linear
  findings to "needs your attention" — rather than blocking on input.
- **Don't post to the wrong PR.** The guarded auto-detect posts only to a
  branch's single unambiguous open PR; anything else aborts with a report.
  A wrong-PR review is the worst failure mode here.
- **One review, one notification.** However many findings there are, the
  author gets one review. Do not split a long list across several reviews,
  and do not post any finding outside it.
- **A request for changes blocks the merge** on a branch that requires
  reviews, and shows as a blocking review everywhere. It stays in force
  until you approve, or until someone dismisses it. That is why this skill
  submits one only when a caller or the user asked for a verdict. Never
  request changes on LOW/polish findings alone.
- **One bad anchor rejects everything.** Run `scripts/pr-diff-lines.sh`
  before you build the payload. A finding outside the diff goes into the
  body.
- **Only dedupe against other automated reviewers.** Same rule as the
  interactive skill — never +1-and-skip because a _human_ mentioned it, and
  never dedupe against your own earlier comments.
- **Suggestions are inline-only and anchor-exact.** A clean drop-in →
  ` ```suggestion `; otherwise prose. Body blocks and Linear tickets always
  use prose. A wrong one-click suggestion is worse than a hint — when
  unsure it's a faithful drop-in, use prose.
- **Severity isn't a gate on what posts.** Post confident located findings
  of any severity; gate by severity only when the user says so. Severity
  decides the event only when somebody asked for a verdict.
- **Never delete a pending review.** It can be the user's own draft. Check
  for one before the first write, not at the submit.
- **Never submit twice.** After a failed call, read your latest review
  before you retry. A second identical review is a second notification.

## Dry-run mode

If the user asks for a dry run ("don't actually post", "show me what
you'd do") or sets `AUTO_POST_PANEL_REVIEW_COMMENTS_DRY_RUN=1`, do
everything normally but write instead of calling the API:

- `./review.json` — the one review payload, exactly as it would be POSTed
  to `/pulls/{N}/reviews`: `commit_id`, `event`, `body`, `comments`. Write
  `null` when no review would be submitted. Dry-run can't observe what
  GitHub rejects, so trust `scripts/pr-diff-lines.sh` when you can run it;
  when you can't, put every PR-bound, non-deduped finding in `comments`
  and note in the report which ones you can already tell sit outside the
  diff.
- `./reactions.json` — array of planned +1 actions for deduped findings
  (`{comment_id, surface, existing_url, author}`).
- `./linear_tickets.json` — array of `{team, project, title, description}`
  objects for Linear-bound findings (or, when Linear isn't resolvable, an
  empty array plus the findings recorded in the report's "needs your
  attention").
- `./report.md` — the full per-finding disposition (inline with a
  suggestion / inline with prose / in the review body / +1 on existing
  comment / filed to Linear / needs attention / dropped — with reasons),
  the event and who chose it, the PR resolution line, and any override
  applied.

**On abort** (no PR resolved — see "Inputs"): write `./review.json` as
`null` (nothing was postable) and `./report.md` explaining why posting was
aborted and that a PR ref is required; skip `./reactions.json` and
`./linear_tickets.json`. Same shape as a real run's abort, just written to
disk instead of acted on.

Honor user-supplied paths if provided.
