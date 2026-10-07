# A review round submits one review

Recorded: 2026-10-06
Status: accepted

## Context

A review posted each finding as a standalone inline comment, one API call and
one notification each. Then, when the gate passed, it submitted an approval.
When the gate failed it submitted nothing more: the skills said that a
blocking review from an automated pass was heavy-handed, and that the open
comments carried the signal.

That left three problems.

- A PR with a known fault and a PR that only waited for reviewers looked the
  same on GitHub: some comments, no verdict. The VERDICT column read
  `commented` for both.
- A PR with a must-fix finding stayed mergeable. Nothing on the PR said that
  the reviewer wanted a change before the merge.
- Ten findings were ten notifications, and none of them belonged to the
  approval that followed.

## Decision

Each round submits **one GitHub review**, with one `POST /pulls/{N}/reviews`.
Every comment of the round is an inline comment inside that review. The
review carries a verdict:

- **Approve** when the gate passes. Polish comments ride inside the approval.
- **Request changes** when a finding above LOW holds up: a must-fix or a
  should-fix finding, a questionable approach, or a purpose that is not
  served. In a later round, the same goes for a finding that is still
  outstanding or is new.
- **Comment** when no decision is possible: too few of the panel answered,
  the PR is a draft or the reviewer's own, the head moved, or the new commits
  need a panel that did not run. The first line of the body says which.

The rules live in the skills, in this order of authority:

- `auto-post-panel-review-comments` owns how a review is built and submitted.
- `auto-review` owns the gate and the verdict for a first review.
- `recheck-pr` owns them for a later round.
- `approve-pr` no longer submits the approval for those two. It runs after
  one, for its follow-up only.

Four choices inside that decision:

- **A finding outranks coverage.** A short panel that found nothing has not
  shown that the PR is clean, so it gets no decision. A verified finding
  shows that the PR needs a change, however many reviewers answered. So the
  request for changes is decided before the coverage rules.
- **The verdict is decided before anything is posted.** It is part of the
  review. The gate used to run after the comments were on the PR.
- **A finding outside the diff goes into the review body.** GitHub rejects
  the whole review when one comment sits on a line outside the diff.
  `scripts/pr-diff-lines.sh` lists the lines that accept a comment. There is
  no fallback to a standalone comment.
- **A request that no longer stands is withdrawn.** A comment review does not
  replace an earlier request for changes. When the author fixed every
  blocking finding and the round still cannot approve, `recheck-pr` dismisses
  its own earlier request.

`panel-review` posts nothing, and its `DECISION:` line follows the same rule:
`Request changes` when must-fix or should-fix has an entry, `Approve`
otherwise.

## Consequences

- On a branch that requires reviews, a request for changes blocks the merge
  until a later round approves or someone dismisses the review. A PR whose
  author never comes back stays blocked. That is the intent, and it is new:
  this tool did not block a merge before.
- A verdict needs someone who asked for it. `auto-post-panel-review-comments`
  run by itself submits a comment review, and requests changes only when the
  user says so.
- The head is fetched twice: before the verdict is decided, and again
  immediately before the review is submitted. A push in between turns an
  approval or a request for changes into a comment review.
- A should-fix (MEDIUM) finding blocks, not only a must-fix. A comment review
  now means "no decision", and a PR that the gate holds for a known fault has
  one.
- The review must be the last thing a round posts. The readback (decision 0002) takes the reviewer's latest review as the verdict, and a thread reply
  can register as a comment review of its own.
- The binaries changed in one string: the trailer instruction now names the
  three reviews. `changes requested` was already a verdict the readback, the
  summary and the view knew.
- An installed copy of the skills shadows the bundled one (decision 0013). An
  older installed copy keeps the old behavior, whatever the binary's version.
- A pending review that the user started by hand stops a round: GitHub
  accepts one pending review for each user, and the skill does not delete it.
- Dismissing a review needs write access to the repo, and on a protected
  branch the right to dismiss reviews. Without it, the earlier request stays
  in force and the report says so.
- Only `recheck-pr` withdraws a request. It knows which findings the request
  was for. A fresh `auto-review` of the same PR does not, so its comment
  review leaves an earlier request in force.
- The interactive `post-panel-review-comments` skill is not in this repo. It
  still posts each comment by itself.
