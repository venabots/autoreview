# A review marks its PR with a reaction

Recorded: 2026-10-01
Status: accepted

## Context

A review takes minutes, and it leaves nothing on the PR until it posts. The
author could not tell a PR that was being reviewed from one nobody had picked
up. A second reviewer could not tell either.

## Decision

- When a review starts, `autoreview` adds the eyes reaction to the PR, as the
  login `gh` is authenticated as.
- When the review ends, it removes that reaction. Every end state removes it:
  done, failed, timed out, stopped with `x`, and an interrupted run.
- A review that is retried under the fallback (decision 0019) keeps one
  reaction from the first attempt to the end of the retry.
- `--no-post` sets no reaction. That run promises to leave the PR alone.
- The reaction is not part of the result. A call that fails gives one note,
  `note: could not set or remove the eyes reaction on PR #N; the review is
  unaffected`, and the review and the exit status are not changed.
- The calls run on their own threads, each with the 15 second limit in
  `src/gh.rs`. The pass waits for the removals before it ends.
- The code is `src/reaction.rs`. The pool calls it; the reviewer skills do
  not know about it.

## Consequences

- Two more `gh api` calls for each review.
- An interrupted run waits for the removals before it gives the terminal
  back. That is about one second, and at most the limit for each call.
- A run that is killed with SIGKILL leaves the reaction on the PR. The next
  review of that PR removes it, because GitHub returns the id of a reaction
  that is already there.
- The reaction is the login's own. If the person put an eyes reaction on the
  PR by hand, the review removes it when it ends.
- `review-prs` does not set the reaction. Its reviews run in tabs that the
  binary does not wait for, so it could not remove one.
- The verdict is still read back from GitHub (decision 0002). The reaction
  says a review is running, not what it found.
