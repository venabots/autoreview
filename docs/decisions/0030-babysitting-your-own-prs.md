# Babysitting your own PRs

Recorded: 2026-10-07
Status: accepted

## Context

Decision 0028 let the view run one task on one of your PRs from a key. A
PR that keeps getting review comments, conflicts or red CI needed a key
press after each change. The My PRs tab also showed the Review tab's
footer, which listed keys that do nothing there.

## Decision

- The My PRs tab has its own keys and footer: `f` fixes the selected PR
  once (`/babysit-pr`), `u` fixes its conflicts (`/sync-main`), `c` answers
  its comments (`/pr-comment-handler`), `b` babysits it, and `B` babysits all
  of your PRs, including ones opened later. On Review, `f` still types the
  focus.
- The one-shot task is now called a fix (`Task::Fix`). "Babysit" means the
  loop.
- Babysitting lives in the run loop (`src/mine_loop.rs`), and its rule is a
  pure module (`src/babysit.rs`). At each look, a babysat PR gets a fix when
  it needs work (conflicts, failing CI, requested changes, open threads) and
  has changed since its last fix (its head, `updatedAt`, CI state or merge
  state).
- The push a fix makes is not a change: the first look after a fix records
  the PR's new state without acting on it.
- At most three fixes run in a row while nobody else acts. A new review
  thread or a changed review decision is somebody else acting, and starts
  the count again.
- Turning babysitting on makes the run keep looking for work, as `w` does.
- A babysit fix is a task like any other: refused when its skill is not
  installed or under `--no-post`, run in its own worktree, and read back
  from GitHub (decision 0028).

## Consequences

- Babysitting lasts for one run. A new run starts with nothing babysat.
- A fix that cannot resolve something is not retried until the PR changes,
  so a PR can stay red while babysat. The row and the detail pane show it.
- The fix count resets only on a new thread or review decision. A
  co-author's push without a review does not reset the cap.
- Headless runs and cron do not babysit: the keys are the only way in.
