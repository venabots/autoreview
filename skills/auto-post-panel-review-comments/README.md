# auto-post-panel-review-comments

The zero-touch twin of [`post-panel-review-comments`](../post-panel-review-comments). Takes a list of review findings (typically from a `panel-review`) and posts the legitimate ones straight to the PR as **one GitHub review** — no select-list, no per-finding prompts, no confirmation. It applies a fixed bar automatically, submits the review, then reports.

## Install

```
npx skills add venabots/autoreview --skill auto-post-panel-review-comments
```

## How to use it

After running `panel-review` (or with any findings list that has `file:line` refs), ask for it to be posted automatically:

- "auto-post these to the PR"
- "just post the legitimate findings"
- "post them all, no triage"
- "post the panel review comments automatically"
- "fire these onto the PR without asking"
- "auto-post the findings, but send the LOW/polish ones to Linear"
- "request changes with these"
- "auto-post these, comment only"

It posts immediately and reports what it did. There is no interactive step. If you want to _pick_ what goes where, use the interactive [`post-panel-review-comments`](../post-panel-review-comments) instead.

## What it does

- **Submits one review** via `POST /pulls/{N}/reviews`: one timeline entry, one notification, one verdict. Every confident, in-scope, located finding is an inline comment inside it. The comment body is the finding verbatim plus the fix when there is one: a mergeable GitHub ` ```suggestion ` block (one-click **"Commit suggestion"**) when the fix is a clean drop-in replacement for the commented line(s), otherwise a `**Possible Solution:**` prose line. A finding with no suggested fix posts the body alone (no fix line — never an invented one). A concrete `file:line` is required; a fix is not. **No severity, no priority, no panelist attribution.** Ordered HIGH → MEDIUM → LOW.
- **Gives the review the verdict somebody asked for.** A caller that owns a gate ([`auto-review`](../auto-review), [`recheck-pr`](../recheck-pr)) hands it the event. By itself it submits a comment review with no verdict. It requests changes only when you ask ("request changes with these") and a finding it posts or +1s is MEDIUM or higher. It never approves by itself. Your own PR and a draft always get a comment review.
- **+1s instead of duplicating.** If another automated reviewer (CodeRabbit, Copilot, Cursor, Greptile, etc.) already raised a finding, it adds a +1 reaction to that comment instead of posting a duplicate. Materially new info goes into the review as its own inline comment, not as a reply. Human comments and its own prior comments are never deduped against.
- **Routes uncertain and out-of-scope findings to Linear** (when reachable) rather than the PR — speculative findings that need investigation, and real issues caught by the review that aren't about this PR's changes. Most findings still go to the PR directly.
- **Honors routing overrides.** Say "post the LOW/polish ones to Linear", "only post HIGH/MEDIUM", "everything to the PR", or "don't touch Linear" and it routes accordingly.
- **Keeps a finding outside the diff in the review body.** GitHub rejects a whole review when one comment sits on a line outside the diff. `scripts/pr-diff-lines.sh` lists the lines that accept a comment, and a finding that fails the check goes into the body under a `**Location:**` line, with any suggestion downgraded to prose. Nothing gets dropped silently.
- **Reports everything** at the end: the review URL and its event, the inline comments (suggestion vs prose), the findings in the body, +1'd duplicates, Linear tickets, anything that needs your attention, and what was dropped.

## What it does NOT do

- **No prompts, ever.** It's zero-touch by design. When something would normally need a question — a missing PR ref, or a Linear target it can't resolve — it degrades to the report (aborts posting for a missing PR; defers Linear findings to a "needs your attention" list) rather than blocking on input.
- **No wording rewrites.** Findings post as written. Soften specific wording before triggering the skill if needed.
- **No comment outside the review.** It never posts a finding as a standalone comment, not even as a fallback.
- **No summary in the review body.** The body is one fixed line, plus any finding that cannot be inline.
- **No approval by itself.** An approval needs a gate, and the gate belongs to the caller.

## Gotchas

- **It won't blast the wrong PR.** It takes the PR ref from context. If none is present it will auto-detect, but only a branch's single unambiguous open PR — anything else aborts with a report rather than guessing.
- **One review, one notification.** However long the list, the author gets one review.
- **A request for changes blocks the merge** on a branch that requires reviews. It stays in force until you approve or the review is dismissed. The skill submits one only when a caller or you asked for a verdict.
- **A pending review stops it.** If you started a review in the browser and did not submit it, GitHub accepts no second one. The skill checks before its first write, posts nothing and tells you; it never deletes your draft.
- **Mergeable suggestions are inline-only and anchor-exact.** A ` ```suggestion ` block replaces the comment's anchored line range verbatim, so it only goes on an inline comment whose range matches the rewritten lines, with indentation matching the file. Body blocks and Linear tickets always use prose. When a fix isn't a faithful drop-in, it uses prose — a wrong one-click suggestion is worse than a hint.
- **Severity isn't a gate on what posts.** A confident LOW with a clean fix still gets a one-line suggestion. Gate by severity only via an override. Severity does decide the verdict.
- **Different from the interactive skill.** [`post-panel-review-comments`](../post-panel-review-comments) lets you triage via select lists and posts each comment by itself; this one decides automatically and submits one review. Different from [`pr-comment-handler`](../pr-comment-handler), which acts on comments _already_ on the PR.
