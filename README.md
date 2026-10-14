<p align="center">
  <img src="assets/logo.svg" width="96" height="96" alt="The auto mark: three rows of dots, joined across three, then two, then none">
</p>

# autoreview

One place to review your team's pull requests and handle your own.

A panel of models reviews each PR in a GitHub repo independently. autoreview
submits one GitHub review for each PR: it approves the PRs that come back clean,
and requests changes on the ones that need them. Your own PRs get a tab of their
own, where one key fixes a conflict, answers the review comments or gets CI
green again.

- **Several models, one review.** Each model reviews the diff independently. One
  more pass checks their findings against the code, and only the findings that
  hold up are posted. They land as one review, with every comment inside it.
- **Every review has a verdict.** It approves only when at least 75% of the
  panel answered and nothing above LOW remains. It requests changes when a
  finding above LOW holds up. When it cannot decide, it submits a comment review
  that says why.
- **It keeps watching.** New PRs and new pushes are reviewed as they arrive. It
  waits for CI to pass first.
- **It handles your PRs too.** The My PRs tab says what blocks each one. A key
  runs the fix in a worktree of its own, so your checkout is not touched.
- **It says when it is looking.** A PR carries an eyes reaction while its review
  runs.
- **It survives an outage.** If claude fails, codex retries the review, and the
  other way round.
- **Its results are true.** The verdict is read back from GitHub, not taken from
  the agent. The exit status is nonzero when a review failed, so it is safe to
  run from cron or CI.
- **Its costs are bounded.** Concurrency, spend per review, time per review and
  the number of passes all have limits.
- **You can reopen any review.** Each review runs in a named session. Resume it
  with `claude --resume <id>`, or press `r` in the view.

## Install

```sh
brew install venabots/tap/autoreview
```

Homebrew also installs `gh` and `dash-p`. You also need at least one agent CLI
(see [Which agents?](#which-agents)).

Run it from inside a GitHub repo:

```sh
gh auth login            # once
autoreview
```

On a terminal that opens the full-screen view and keeps looking for new PRs
until you press `q` twice.

## Upgrade

```sh
brew update && brew upgrade venabots/tap/autoreview
```

The review skills upgrade with the binary. If you installed your own copies,
upgrade those too (see [Review skills](#review-skills)).

### Coming from 0.24 or earlier

0.25 is a rewrite in Go. It is one binary with one view.

- `panel` is now `autoreview panel`. Its flags are the same.
- `review-prs` is gone, with `--pick`, `--auto` and the `gum` dependency. To
  review only some PRs, name them: `autoreview 213 240`.
- One variable replaces the reviewer: `$AUTOREVIEW_CMD`. A run stops when
  `$AUTOREVIEW_AUTO_CMD` is still set, because it would not use that command.
  Move the command to `$AUTOREVIEW_CMD`, or unset the variable.
- In the view, `w` no longer stops babysitting of your own PRs. `b` and `B` do
  that.
- The ledger and the review sessions carry over. `autoreview stats` reads the
  same file, and `--continue` finds the sessions 0.24 made.

The last Rust release is the tag `v0.24.0`.

## The view

Two tabs. `tab`, the arrow keys or a click switches between them.

**Review** lists the open PRs and where each one stands: ready, held for CI,
held under another PR, seen, or approved.

| Key   | What it does                                            |
| ----- | ------------------------------------------------------- |
| `R`   | Review the selected PR now, whatever holds it           |
| `x x` | Stop the selected review. The first `x` asks            |
| `r`   | Reopen a finished review in a new terminal tab          |
| `f`   | Type what the reviewers look at in this run (the focus) |
| `w`   | Stop or start watching for new work                     |

**My PRs** lists your own open PRs and what blocks each one: conflicts, failing
CI, requested changes, open threads. An approved PR reads `approved`, with the
number of threads that are still open.

| Key   | What it does                                                   |
| ----- | -------------------------------------------------------------- |
| `f`   | Fix the selected PR once: conflicts, comments and CI           |
| `u`   | Merge the base in and fix the conflicts                        |
| `c`   | Answer the review comments                                     |
| `b`   | Babysit the PR: fix it again each time it changes and needs it |
| `B`   | Babysit all your PRs, and any you open later                   |
| `r`   | Reopen the last task in its worktree, when it was kept         |
| `x x` | Stop the running task. The first `x` asks                      |

On both tabs, `o` opens the PR in the browser, `l` shows the run log, and `?`
lists every key. A drag in the right pane selects its text and copies it. `m`
gives the mouse back to the terminal, to select text anywhere on the screen.
`q q` quits: the first `q` asks, because a quit stops every review and task that
is running. `esc` closes the key list and answers no to a question.

Each task on your own PR runs in `<run dir>/worktrees/pr-N`. The worktree is
removed after the task, unless it holds uncommitted or unpushed work. Then it is
kept, and the view and the log name it.

`f` runs once. It merges the base in, answers the review comments and fixes
failing CI, in that order. `u` and `c` each do one part of that. `b` and `B` are
the loop: at each look, a babysat PR that needs work and has changed gets the
same fix as `f`.

While a task runs, its row says so. When it ends, the footer says how, and the
row keeps the result: `fix pushed`, `fix pushed nothing` or `fix failed`. Your
PRs are read again at once, so the row also shows what the task left. A task
that could not start says why in the footer and in the right pane.

Babysitting has three limits:

- A fix runs only after the PR changed.
- At most 3 fixes run in a row while nobody else acts on the PR.
- An approved PR gets no fix, because a push can dismiss the approval. `f`, `c`
  and `u` still work by hand, for example to answer the threads that are still
  open.

## Which agents?

Agents have two jobs in a review:

| Job              | Agents                              | Default                           | Change it with                                         |
| ---------------- | ----------------------------------- | --------------------------------- | ------------------------------------------------------ |
| **Orchestrator** | `claude`, `codex`                   | `claude`                          | `--orchestrator codex`, `--orchestrator codex:gpt-5.5` |
| **Fallback**     | the orchestrator you did not choose | the other one, if it is installed | `--fallback none`                                      |
| **Panelists**    | `claude`, `codex`, `opencode`       | every one of them on your `PATH`  | `PANEL_REVIEW_PANELISTS="claude:opus-4.8 codex"`       |

- The orchestrator runs the review. It starts the panel, merges the findings,
  and submits one review with a verdict.
- The panelists each review the diff, independently of each other.
- Approval needs at least two panelists to answer. With only one agent
  installed, reviews request changes or comment, but never approve.
- Recommended: install `claude` and `codex`. Add `opencode` for a third opinion.

## Review skills

The reviewer is a set of skills in [`skills/`](skills).

- It uses the skills you installed, if you have them: `~/.claude/skills/`, or
  the reviewed repo's `.claude/skills/`. The run prints a
  `note: ... shadow the staged copies` line when this happens.
- If you did not install them, it uses the copy built into the binary. You do
  not have to install anything.
- Exception: codex cannot use the built-in copy. With `--orchestrator codex`,
  install the skills in `~/.agents/skills/`. The run stops before it starts if
  they are missing.
- `--skills DIR` runs the skills in a directory of your own, and
  `--skills installed` stages nothing.

Use the skills yourself, outside a run:

```sh
npx skills add venabots/autoreview --skill '*' --global
```

The keys on the My PRs tab run three skills that are not in this repo:
`babysit-pr`, `sync-main` and `pr-comment-handler`. They carry your own setup,
so you install your own. A key whose skill is missing says so and starts
nothing.

## Without a terminal

On a pipe, in cron or CI, or with `--headless`, autoreview makes one pass. It
waits for checks that are still running, reviews every PR that is ready, prints
one line for each step and ends with a summary. It exits 0 only when every
review ran to its end.

```sh
autoreview --headless          # one pass, plain lines
autoreview 213 240             # only these PRs, whatever their state
autoreview --no-post           # review, and leave the PR alone
autoreview --continue          # re-check in the earlier review sessions
autoreview --babysit=15        # a pass every 15 minutes, until none is left
autoreview --watch=5           # look every 5 minutes, until it is stopped
autoreview --help              # every flag and variable
```

A reviewed PR rests 30 minutes before it is reviewed again, and gets at most 3
reviews in a run. A new push starts that count again while the run watches.

## panel

`autoreview panel` reviews one change with several models, with no PR and no
skill involved. Each panelist is blind to the others, and one more call writes
the synthesis.

```sh
autoreview panel                    # what is not committed yet
autoreview panel --staged           # the index only
autoreview panel --base main        # what this branch added
autoreview panel --tree src/pay     # audit the code as it is, with no diff
autoreview panel --help
```

## stats

Every finished review goes in a ledger, one JSON line each. `autoreview stats`
reads it back and reports how each panelist model has done.

```sh
autoreview stats --since 7d
```

## Requirements

- [`gh`](https://cli.github.com), logged in (`gh auth login`).
- [`dash-p`](https://github.com/venabots/dash-p). Every built-in review runs
  through it. `$DASHP_BIN` names another binary.
- An orchestrator CLI: `claude` by default, `codex` with `--orchestrator codex`.
  With both installed, a run has its fallback.
- `git`, for the worktrees of `panel` and of the My PRs tasks.
- `pgrep`, which stops a session being resumed while another process holds it.
  It is standard on macOS, and in `procps` on Linux.
- For `r`: [Herdr](https://herdr.dev), [cmux](https://cmux.io) or
  [Ghostty](https://ghostty.org) on macOS, to open the new tab.

## Troubleshooting

| You see                                   | Cause and fix                                                                                                                                                         |
| ----------------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `note: ... shadow the staged copies`      | Your installed skills run, not the built-in ones. They can be older. Delete them, or link them to a checkout.                                                         |
| `fallback: none (codex is not installed)` | There is no second provider to retry a failed review. Install `codex` (or `claude`).                                                                                  |
| `You've hit your session limit`           | The provider hit a usage limit. The fallback retries the review once under the other provider.                                                                        |
| A PR is not reviewed                      | Its CI is failing or pending, it sits on another open PR, or nothing changed since your last review. `--skip-wait-for-ci` and `--stacked` remove the first two holds. |
| VERDICT says `changes requested`          | A finding above LOW held up. A branch that requires reviews cannot merge until a later pass approves, which happens when the author fixes or explains every finding.  |
| VERDICT says `commented`                  | No decision: fewer than 75% of the panel answered (often one provider is down), or the head moved during the review. The first line of the review on the PR says why. |
| VERDICT says `nothing posted`             | The review finished with nothing to submit, or GitHub has no record of a submission. It is not a rejection.                                                           |
| A codex run refuses to start              | The skills are not in `~/.agents/skills/`. See [Review skills](#review-skills).                                                                                       |
| A My PRs key says a skill is missing      | `babysit-pr`, `sync-main` or `pr-comment-handler` is not installed where your orchestrator looks. See [Review skills](#review-skills).                                |
| A task says the PR is from a fork         | Its branch is not on `origin`, so no worktree can be made for it here.                                                                                                |
| You cannot select text in the view        | Drag in the right pane to copy its text. For any other text, press `m` to give the mouse back to the terminal.                                                        |
| `--continue` reviews fresh                | Another process still holds that session.                                                                                                                             |
| You need to know why a review failed      | Read `pr-N.log` in the logs directory the run prints. In the view, press `l`.                                                                                         |

## Try it without a real review

```sh
scripts/sandbox.sh              # the view, against a fake GitHub and a fake reviewer
scripts/sandbox.sh --headless   # one plain pass
```

Nothing reaches GitHub and no agent runs. The top of the script lists the
variables that change what the fakes do. The sandbox has an origin with your
PR's branch, so the My PRs keys work there too.

## Build from source

```sh
git clone git@github.com:venabots/autoreview.git
cd autoreview && go build -o autoreview ./cmd/autoreview
```

The binary is self-contained: the review skills and the panel prompts are built
in.

## Develop

```sh
go test -race $(go list ./... | grep -v /cmd/)
go test ./cmd/...
golangci-lint run
```

[AGENTS.md](AGENTS.md) has the conventions. [docs/plan.md](docs/plan.md) has the
design, and [docs/rules.md](docs/rules.md) lists each rule with the test that
pins it.
