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
  runs the fix in a worktree of its own, or in your clean worktree of that PR's
  branch. A checkout with changes of yours is not touched.
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
- In the view, `w` no longer stops the autofix of your own PRs. `a` and `A` do
  that. Babysitting is now called autofix, on `a` and `A` in place of `b` and
  `B`. `F` fixes all your PRs once. `c` and `u` are gone: `f` does both.
- The ledger and the review sessions carry over. `autoreview stats` reads the
  same file, and `--continue` finds the sessions 0.24 made.

The last Rust release is the tag `v0.24.0`.

## The view

Two tabs. `tab`, the arrow keys or a click switches between them.

**Review** lists the open PRs and where each one stands: ready, held for CI,
held under another PR, seen, or approved. With `--author=alice` it lists
only the PRs alice opened, and the tab says `by @alice`. Give the flag
again, or split logins with commas, for more than one person.
With `--dependabot`, a Dependabot PR that conflicts, or whose checks fail after
its base moved on, is not reviewed: the run comments `@dependabot rebase` once
for each head, and reviews the PR Dependabot pushes next.

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

| Key   | What it does                                                     |
| ----- | ---------------------------------------------------------------- |
| `f`   | Fix the selected PR once: conflicts, comments and CI             |
| `F`   | Fix each of your PRs that needs it, once                         |
| `a`   | Autofix the PR: fix it again each time it changes and needs it   |
| `A`   | Autofix all your PRs, and any you open later. On at the start    |
| `r`   | Reopen the last task in its worktree, if it was kept or is yours |
| `x x` | Stop the running task. The first `x` asks                        |

On both tabs, `/` finds a PR: type its number, or words from its title, branch
or author, and the selection moves as you type. Typing a number starts it too,
so `2734` goes to PR #2734. `enter` keeps the PR, and `esc` goes back. `o` opens
the PR in the browser, `l` shows the run log, and `?` lists every key. A drag in the right pane selects its text and copies it. `m`
gives the mouse back to the terminal, to select text anywhere on the screen.
`q q` quits: the first `q` asks, because a quit stops every review and task that
is running. `esc` closes the key list and answers no to a question.

Each task on your own PR runs in `<run dir>/worktrees/pr-N`. The worktree is
removed after the task, unless it holds uncommitted changes to tracked files or
unpushed commits. Then it is kept, and the view and the log name it. Untracked
files, such as a `__pycache__`, do not keep it. The next task on the PR removes
a kept worktree when that work is gone, and is refused while it is there. When you have the PR's branch checked
out somewhere else with no changes, the task runs there instead. It brings the
branch up to `origin` first, and the worktree stays after the task. When that
checkout has changes of yours, or the branch tracks somewhere else, the task
works on a branch of its own, `autoreview/<run>/pr-N`, and pushes to the PR from
it. A local branch with commits that are not on `origin` is refused: push them
first.

`f` runs once. It merges the base in, answers the review comments and fixes
failing CI, in that order, and skips each step that has nothing to do. `F` does
the same for each of your PRs that needs it. `a` and `A` are the loop: at each
look, an autofixed PR that needs work and has changed gets the same fix as `f`.

Autofix is on for all your PRs when the view opens, as watching is. Press `A` to
turn it off, or `a` to turn it off for one PR. `--once` opens the view with
autofix off, and `--no-post` refuses it.

While a task runs, its row says so. When it ends, the footer says how, and the
row keeps the result: `fix pushed`, `fix pushed nothing` or `fix failed`. Your
PRs are read again at once, so the row also shows what the task left. A task
that could not start says why in the footer and in the right pane.

Autofix has three limits. `f` and `F` are keys you press, so they keep to none
of them:

- A fix runs only after the PR changed.
- At most 3 fixes run in a row while nobody else acts on the PR
  (`mine.autofix_cap` in the [settings](#settings)).
- An approved PR gets only a fix of its conflicts, because a push can dismiss
  the approval. A PR with conflicts cannot merge, so that push loses nothing.
  Open threads and a failing build on an approved PR stay for you: `f` or `F`
  fixes them when you press it. The PR's right pane, and `a` when it turns
  autofix on, say so.

## Settings

The first run in the view asks for the settings that you are most likely to
change, and writes them to `~/.config/autoreview/config.toml`
(`$XDG_CONFIG_HOME/autoreview/config.toml` when that variable is set). Every run
reads the file. Run `autoreview setup` to change the settings, or edit the file:
each key has a comment.

The form asks one question on each step, and says what the answer changes and
its default. `enter` keeps the answer shown, and `shift+tab` goes back one step.
The panel is a checklist of the models it found: the file's panel, the one in
`$PANEL_REVIEW_PANELISTS`, the models this machine has run before (with how
each did), and each backend on `PATH`. A later step adds models that the list
does not have. The last step shows every answer: save them, go back, or leave
without saving.

- A flag wins over its `$AUTOREVIEW_*` variable, and a variable wins over the
  file. A question says so when your shell sets its variable.
- A panel kept from `$PANEL_REVIEW_PANELISTS` is written to the file, so you
  can remove the variable. A panel kept as `PATH` gave it is not written, so a
  backend you install later still joins.
- A bad value or an unknown key stops the run, and the error names the key.
- The file never makes a run loop by itself. Off a terminal, only `--watch` and
  `--babysit` do that.
- A run off a terminal never asks. With no file, it uses the built-in defaults.
- `$AUTOREVIEW_CONFIG` names another file, or `off` reads none.

| Key                                                                     | What it sets                                                                                              |
| ----------------------------------------------------------------------- | --------------------------------------------------------------------------------------------------------- |
| `review.orchestrator`, `fallback`, `jobs`, `timeout`, `budget`, `focus` | The same as `--orchestrator`, `--fallback`, `--jobs`, `--timeout`, `--budget` and `--focus`               |
| `review.verdicts`                                                       | `approve-and-request-changes` (the default), `approve-only` or `comment-only`                             |
| `loop.watch`, `rest`, `max_passes`, `max_idle`, `ci_wait`               | `--watch`, `--babysit`, `--max-passes`, `--max-idle` and `$AUTOREVIEW_CI_WAIT`                            |
| `prs.approved`, `dependabot`, `stacked`, `skip_ci`                      | `--all`, `--dependabot`, `--stacked` and `--skip-wait-for-ci`. `--all=false` turns one off for a run      |
| `mine.autofix`                                                          | Whether the view starts with autofix on for all your PRs (default `true`)                                 |
| `mine.autofix_cap`                                                      | The most fixes in a row on one of your PRs while nobody else acts on it (default 3)                       |
| `panel.panelists`                                                       | The models that review together, such as `["claude:opus-4.8", "codex"]`. Empty is every backend on `PATH` |
| `panel.synth`                                                           | The model that writes the synthesis of `autoreview panel`                                                 |

**Verdicts.** `approve-only` makes a review that would request changes a comment
review with the same findings. `comment-only` makes every review a comment
review. The setting reaches `auto-review` and `recheck-pr` as a `--verdicts`
option. If you installed your own copies of those skills, they need the option
too: a run with `approve-only` or `comment-only` stops before it starts and
names the copy that does not know it.

**Panel models.** A review hands `panel.panelists` to `panel-review` through
`$PANEL_REVIEW_PANELISTS`, unless you set that variable yourself. Name at least
2, because `auto-review` approves only when several models review. With codex as
the orchestrator, a codex `shell_environment_policy` of `core` can drop the
variable.

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

The keys on the My PRs tab run two skills that are not in this repo:
`babysit-pr` and `sync-main`. `babysit-pr` calls `pr-comment-handler` and
`fix-ci` in turn. They carry your own setup, so you install your own. A key
whose skill is missing says so and starts nothing.

## Without a terminal

On a pipe, in cron or CI, or with `--headless`, autoreview makes one pass. It
waits for checks that are still running, reviews every PR that is ready, prints
one line for each step and ends with a summary. It exits 0 only when every
review ran to its end.

```sh
autoreview --headless          # one pass, plain lines
autoreview 213 240             # only these PRs, whatever their state
autoreview --author=alice      # only the PRs alice opened
autoreview --no-post           # review, and leave the PR alone
autoreview --continue          # re-check in the earlier review sessions
autoreview --babysit=15        # a pass every 15 minutes, until none is left
autoreview --watch=5           # look every 5 minutes, until it is stopped
autoreview --help              # every flag and variable
```

A reviewed PR rests 30 minutes before it is reviewed again, and gets at most 3
reviews in a run. A new push starts that count again while the run watches. The
[settings](#settings) change both numbers: `loop.rest` and `loop.max_passes`.

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

## doctor

A review that loses a panelist still finishes. It posts a comment instead of a
verdict, and nothing on the screen turns red. `autoreview doctor` finds this:

- It asks each panelist one short question now, through `dash-p`, as a review
  does. A panelist that does not answer shows the reason in its own words: no
  credits, a usage limit, or a flag its CLI no longer takes.
- It reads the last day of the ledger: how often each model answered, what the
  reviews cost, and which PRs got no verdict while a panelist was missing.
- It lists the version of `gh`, `dash-p`, `git` and each agent CLI.

It exits 1 when it finds a problem, so cron can tell you.

```sh
autoreview doctor              # ask the panel, read the last day
autoreview doctor --since 7d   # a longer window
autoreview doctor --no-probe   # read the ledger only; spends nothing
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
| A My PRs key says a skill is missing      | `babysit-pr` or `sync-main` is not installed where your orchestrator looks. See [Review skills](#review-skills).                                                      |
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
