# The view acts on your own PRs

Recorded: 2026-10-05
Status: accepted

## Context

`autoreview` reviews other people's PRs and always hid your own. A person
running it all day also has PRs of their own that wait on something: a
conflict, red CI, review comments to answer. Skills for that work existed
(`babysit-pr`, `pr-comment-handler`), but running one meant leaving the
view, checking out the branch and starting an agent by hand.

## Decision

- The full-screen view has two tabs under the header, Review and My PRs.
  `tab` switches between them, and each keeps its own selection.
- My PRs lists your open PRs in the repo from a search by author
  (`src/mine.rs`), so none falls off the review list's page of 50. Each row
  says what blocks the merge first: conflicts, failing CI, requested
  changes, open threads, draft, pending CI, awaiting review, approved.
- `b` runs `/babysit-pr N` and `c` runs `/pr-comment-handler N` on the
  selected PR. These are a job's `Task` (`src/task.rs`), run through the
  same pool, `--jobs`, `--timeout` and `--budget` as a review.
- A task starts only from a key. It never enters the review queue: no caps,
  no rest, no watch list, and no loop starts one by itself.
- Its skill is the one the person installed. The repo is public and these
  skills carry personal setup, so none is vendored (this narrows decision
  0013 to the reviewers). A key whose skill is not installed, or any key
  under `--no-post`, says why and starts nothing.
- A task runs in a worktree of its own, `<run>/worktrees/pr-N`, on the PR's
  branch tracking `origin` (`src/task_worktree.rs`). It refuses a fork, a
  branch checked out in another worktree, and a local branch with commits
  `origin` lacks. The worktree is removed after the task unless it holds
  uncommitted or unpushed work, which is kept and named.
- The VERDICT of a task is read back from GitHub, as a review's is
  (decision 0002): `pushed` when the PR's head moved, `nothing pushed` when
  it did not, `unknown` when the readback failed.
- A failed task counts in the exit status like a failed review (decision
  0003). It is not retried under the fallback: its skill may not be
  installed for the other orchestrator.

## Consequences

- "Your own PRs are always hidden" is now true of the review list and the
  plain output only. Headless runs and `review-prs` show no My PRs list.
- Each look of a run with the view up makes one more `gh` call, for the
  search, and so does the end of each pass.
- A task pushes to your branch from a worktree. A skill that runs
  `git push --force` can rewrite what reviewers have seen; that is the
  skill's rule to keep, not this tool's.
- A run killed with SIGKILL leaves its worktree under the run directory.
  `git worktree prune` removes the record once the directory is gone.
- The list moved one row down for the tab bar. Pty tests that click by
  screen row move with it.
