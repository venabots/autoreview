# autoreview

An automated code review system. It watches a repo's pull requests, reviews
each one with a panel of independent models, posts what they found, and
approves the ones that come back clean — then keeps watching, so a PR opened or
pushed to while it runs gets picked up too.

You can also drive every part of it by hand.

| Command                     | Reviews                    | Use when                                                                 |
| --------------------------- | -------------------------- | ------------------------------------------------------------------------ |
| [`autoreview`](#autoreview) | a repo's PRs, headlessly   | the default: no terminal needed, and an exit status that means something |
| [`panel`](#panel)           | one change, several models | you want independent second opinions on a single diff                    |
| [`review-prs`](#review-prs) | a repo's PRs, one tab each | you want to watch a review happen and steer it mid-flight                |

## How a review happens

```
autoreview             picks every PR that is NEW or UPDATED, once CI passes
  └─ dash-p → claude   one headless agent per PR, --jobs at a time
       └─ /auto-review a panel of models reviews the diff independently,
                       their findings are synthesized, verified, and posted
  ← verdict            read back from GitHub, not taken from the agent's word
  ↻ fallback → codex   the agent's provider failed; the other one retries it
  ↻ --babysit          watch on an interval: new PRs join, fixed ones leave
```

The panel in the middle is the [`auto-review`](skills/auto-review) skill,
which each agent runs against its own PR. [`panel`](#panel) is that same idea
as a binary you can point at any diff, with no PR and no skill involved.

Four things that shape the whole design:

- **The exit status means the reviews succeeded**, not that the processes
  started. A cron job or a CI step can tell a finished sweep from a broken one.
- **The verdict is read back from GitHub.** An agent that believed its own
  report would show "approved" for an approval that never landed.
- **A review runs in a session you can reopen.** Losing the terminal loses live
  steering, not access: every review's session id is printed, and
  `claude --resume <id>` picks it up.
- **Unattended means bounded.** `--jobs` caps concurrency, `--budget` caps each
  review's spend, `--timeout` stops a wedged one, `--max-passes` stops a
  conversation becoming a loop, and `--max-idle` stops a quiet PR keeping a
  cron-started process alive forever.
- **One provider being down does not stop the sweep.** The agent driving a
  review is a choice (`--orchestrator`), and a review that fails because its
  provider did is retried under the other one. See
  [Orchestrators and the fallback](#orchestrators-and-the-fallback).

## Requirements

- [`gh`](https://cli.github.com) — authenticated (`gh auth login`)
- [`gum`](https://github.com/charmbracelet/gum) — the interactive picker only.
  A sweep (`review-prs --auto`, plain `autoreview`) never reaches it.
- [`dash-p`](https://github.com/venabots/dash-p) — **`autoreview` only**: the
  built-in reviewer runs through it (`brew install venabots/tap/dash-p`; set
  `$DASHP_BIN` to point elsewhere). Not needed when `$AUTOREVIEW_CMD` replaces
  the reviewer.
- An orchestrator CLI — **`autoreview` only**: `claude` by default, `codex`
  with `--orchestrator codex`. Installing both is what gives a run its
  fallback, and is the recommended setup: see
  [Orchestrators and the fallback](#orchestrators-and-the-fallback).
- `pgrep` — refuses to resume a review session another process still holds;
  without it `--continue` loses that guard in both tools, and `autoreview`
  requires it. Standard on macOS; `procps` on slim Linux images.
- A supported terminal for spawning tabs — **`review-prs` only**:
  - [Herdr](https://herdr.dev) (preferred; detected via `HERDR_ENV`, drives new
    tabs over its socket API via the `herdr` CLI), or
  - [cmux](https://cmux.io) (detected via `CMUX_SURFACE_ID`), or
  - [Ghostty](https://ghostty.org) 1.3+ on macOS (detected via `TERM_PROGRAM`,
    drives new tabs through AppleScript — needs Accessibility permission)

## Install

### Homebrew

```sh
brew install venabots/tap/autoreview
```

### Manual

```sh
git clone git@github.com:venabots/autoreview.git
cargo install --path autoreview
```

Either way you get all three binaries. All three are self-contained: nothing is
read from the checkout at runtime, so a copy or a symlink anywhere on `$PATH`
works.

### Skills

The reviewer each agent runs is a set of skills, and they live in this repo
under [`skills/`](skills). **The binaries carry them**: each build compiles
the tree in, writes it out for a run, and hands every reviewer the directory
with `--add-dir`. Nothing is installed, and the skill a review ran is the one
the binary was built with.

Skills you install yourself still win. Claude Code resolves a name from your
own skills directory, and from the reviewed repo's `.claude/skills`, before
any directory a run adds. So a copy in either place is the override. Both
binaries say when that is happening:

```
note: auto-review, panel-review under /Users/you/.claude/skills shadow the staged copies; the installed skills run
```

`--skills` chooses the source for a run, and both binaries take it (or
`$AUTOREVIEW_SKILLS` / `$REVIEW_PRS_SKILLS`). Exactly one source per run,
printed as `skills: ...` when the run starts:

| Value       | What runs                                                                                             |
| ----------- | ----------------------------------------------------------------------------------------------------- |
| unset       | the skills this binary was built with, staged for the run                                             |
| a directory | the `<name>/SKILL.md` under it, staged in place of the bundle: a fork, or a repo's own tuned reviewer |
| `installed` | nothing staged; the reviewer finds what is installed, which is what every run did before              |

A skill you installed still wins over a staged one, as above, and the note
says so. `--skills` does not reach a command override, which finds its own
skills; the run notes that too.

To use the skills interactively, outside a run, install them for every agent
that supports the [Agent Skills](https://agentskills.io) layout:

```sh
npx skills add venabots/autoreview --skill '*' --global
```

Or point a skills directory at the checkout. Each agent reads its own:

| Agent  | Reads                                                   |
| ------ | ------------------------------------------------------- |
| claude | `~/.claude/skills/`                                     |
| codex  | `~/.agents/skills/` (and `~/.codex/skills`, deprecated) |

```sh
ln -s "$PWD/skills/"* ~/.claude/skills/
ln -s "$PWD/skills/"* ~/.agents/skills/
```

**Staging reaches claude only, so a codex orchestrator needs the second line.**
What a run stages is a `.claude/skills` directory handed over with
`--add-dir`. Claude Code resolves a skill from it; codex does not, because it
reads its skills from its own roots and from its project root, never from a
directory added at run time. So under
[`--orchestrator codex`](#orchestrators-and-the-fallback) the bundled copy
never arrives, and the skills have to be installed where codex looks. A run
checks before it spends anything and refuses if they are missing.

| Skill                                                                       | Run by                                                                                         |
| --------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------- |
| [`auto-review`](skills/auto-review)                                         | `autoreview` unattended: the sweep, `--babysit`, `--watch`                                     |
| [`panel-review`](skills/panel-review)                                       | `autoreview --pick`, and `--no-post` (even with `--continue`); the first step of `auto-review` |
| [`recheck-pr`](skills/recheck-pr)                                           | `autoreview --continue`, and every later `--babysit` or `--watch` pass                         |
| [`auto-post-panel-review-comments`](skills/auto-post-panel-review-comments) | `auto-review`, to post what the panel found                                                    |
| [`approve-pr`](skills/approve-pr)                                           | `auto-review` and `recheck-pr`, when the gate passes                                           |
| [`pr-review-tab`](skills/pr-review-tab)                                     | `review-prs --auto` and `review-prs --babysit`, to close its own tab                           |

They are versioned with the binaries because they change with them: a flag the
binary passes down is a flag the skill has to understand.

## autoreview

The default way to run all this. Each PR is reviewed by a
[dash-p](https://github.com/venabots/dash-p) subprocess driving claude
headlessly, `--jobs` at a time. The run shows a live per-PR board, reads each
review's verdict back from GitHub when it finishes, and ends with a summary of
verdicts, findings, models and cost. It exits nonzero if any review failed,
which is what makes it safe to put in cron or CI.

```sh
autoreview                  # review every NEW/UPDATED PR
autoreview --jobs 3         # ...three at a time (default 2)
autoreview --pick           # picker, then review each selection headlessly
autoreview --continue       # resume earlier sessions for a second look
autoreview --babysit=15     # re-run every 15 min, picking up new PRs as they open
autoreview --skip-wait-for-ci # review a PR whatever its checks say
autoreview --stacked        # review PRs stacked on another open PR too
autoreview --tui --watch    # full screen: every PR on the left, its review on the right
autoreview --help           # usage
```

**Sweeping is the default, and the picker is the flag.** There are no tabs to
choose between here, so the ordinary run is "review whatever is actionable" and
`--pick` is for the times you want a subset. ([`review-prs`](#review-prs) is
the other way round: it opens a tab per PR, so choosing which tabs to open is
the point.) `--auto` / `-A` still parse —
an old alias or cron line keeps working — they just name the default now.

It takes the same selection flags as `review-prs` (`--continue`, `--all`,
`--dependabot`, `--stacked`, `--skip-wait-for-ci`, `--babysit`) plus thirteen
of its own: `--pick`, `--watch`, `--focus`, `--no-post`, `--jobs`,
`--orchestrator`, `--fallback`, `--timeout`, `--budget`, `--log-dir`,
`--max-passes`, `--max-idle` and `--tui`.

**`--focus` steers a run.** It reaches every panelist as the reviewer focus:

```sh
autoreview --focus "be strict about the ledger migration"
```

Use it for what you care about today. Anything the repo always cares about
belongs in its `CLAUDE.md`, which the panelists read anyway.

**`--no-post` leaves the PR alone.** No comments, no approval — the reviews go
to `pr-N.review.md` and the summary says nothing was posted:

```sh
autoreview --no-post
```

This works by choosing the reviewer, not by asking one to hold back: the run
gets the skill that has no posting step. That is also why it is refused
alongside `$AUTOREVIEW_AUTO_CMD`, which decides for itself what it posts. The
session stays resumable, so a later `--continue` run can pick the review up
and post it.

On a terminal the pass is a live board -- finished reviews settle into
permanent result lines, running ones spin, and a progress bar tracks the
pass:

```
2 PRs to review: #9 #8
reviewing 2 PRs · logs: /tmp/autoreview.k3Xq8p/run-Qszknc/pass-1

  ✓ #9 approved · risk LOW · 4m12s · $0.51  @alice Add retry logic
  ⠹ #8 @bob Fix flaky test · rechecking 1m47s
  ━━━━━━━━━━━━╸───────────  1/2 · 1 running
```

Each row says who opened the PR, and whether this is a first look
(`reviewing`) or a second one against the findings already in that session
(`rechecking`) — both questions you would otherwise open the PR to answer.

The board redraws itself when the terminal is resized, in place: the rows
refit to the new width, and the header and the reviews that already finished
stay where they are. `q` (or ctrl-C) stops the pass, stops every running
review, and prints the summary of what finished.

A running row is not the whole story, so `space` (or `enter`) opens every
running row: the session the review runs in, how many turns and tool calls
it has made, and the last few things it did -- read from the transcript
Claude Code writes as it works. `1` to `9` open one row by its position,
`esc` closes them all. When the terminal is wide enough the row itself ends
with the tool the review is in right now:

```
  ⠹ #8 @bob Fix flaky test · reviewing 1m47s · Bash
      session fa5ced7b-32dd-578b-a3b9-d4d23195dce1
      14 turns · 9 tool calls
        40s ago  Read    pool.rs
        12s ago  said    The retry path never re-arms the deadline.
         3s ago  Bash    cargo test --quiet
```

Only the built-in reviewer in a session this run named has a transcript to
follow. A session claude named itself is found when the review ends, and a
command override has no transcript at all; its row follows its stderr.

The header only mentions concurrency when it actually holds reviews back: with
five PRs and `--jobs 2` it reads `reviewing 5 PRs, 2 at a time`.

The summary is a pair of tables -- what each review concluded, and which models
did the reviewing. Every `#N` is an OSC 8 hyperlink to the PR, so a terminal
that supports them (Ghostty, iTerm2, WezTerm, kitty, VS Code) opens it on
cmd-click. Piped output, `$NO_COLOR` and `TERM=dumb` get plain text instead:

```
╭────┬────────┬────────────────┬────────┬──────────────┬───────┬───────┬────────────────╮
│ PR ┆ RESULT ┆ VERDICT        ┆ RISK   ┆ FINDINGS     ┆ TIME  ┆ COST  ┆ MODEL          │
╞════╪════════╪════════════════╪════════╪══════════════╪═══════╪═══════╪════════════════╡
│ #9 ┆ done   ┆ approved       ┆ LOW    ┆ 1 polish     ┆ 4m12s ┆ $0.51 ┆ claude-fable-5 │
│ #8 ┆ done   ┆ commented      ┆ MEDIUM ┆ 2 should-fix ┆ 6m03s ┆ $0.88 ┆ claude-fable-5 │
│ #7 ┆ done   ┆ nothing posted ┆ LOW    ┆ none         ┆ 2m10s ┆ $0.31 ┆ claude-fable-5 │
╰────┴────────┴────────────────┴────────┴──────────────┴───────┴───────┴────────────────╯
╭────┬─────────────────┬──────────┬──────────┬────────╮
│ PR ┆ MODEL           ┆ STATUS   ┆ FINDINGS ┆ TOP    │
╞════╪═════════════════╪══════════╪══════════╪════════╡
│ #9 ┆ gpt-5.5         ┆ answered ┆ 1        ┆ LOW    │
│ #9 ┆ claude-opus-4.7 ┆ answered ┆ 0        ┆ -      │
│ #8 ┆ gpt-5.5         ┆ answered ┆ 3        ┆ MEDIUM │
│ #8 ┆ claude-opus-4.7 ┆ failed   ┆ -        ┆ -      │
╰────┴─────────────────┴──────────┴──────────┴────────╯
reopen any review with: claude --resume <SESSION>
  #9  cc10f740-28c3-58c6-ae64-d9ff37df22a7
  #8  fa5ced7b-32dd-578b-a3b9-d4d23195dce1
logs: /tmp/autoreview.k3Xq8p/run-Qszknc/pass-1
```

Two columns worth reading carefully:

- **RESULT** is the review process: `done`, `timed out`, `failed (exit 10)`.
- **VERDICT** is what landed on the PR: `approved`, `commented`,
  `changes requested`, or `nothing posted` when the review finished without
  leaving a review behind. `nothing posted` is not a rejection; it means the
  reviewer had nothing to submit, or that GitHub has no record of a submission.

A failed review is followed by one `error` line that says why, in the
harness's own words -- `error #9 #8: You've hit your session limit · resets
12pm (America/New_York)`. The reason comes from dash-p's envelope: claude
reports a usage limit or an API error as its answer, and the exit code alone
is only a number. Reviews that failed for the same reason share one line.

The panel table's **STATUS** answers only "did this panelist come back with a
review", not "did it like the PR" — `answered`, `failed`, or `-` when the
reviewer did not say. The panelist's CLI name is dropped: the model identifies
the row, and a panelist that never reported one falls back to its name
(`opencode`).

Without a TTY -- cron, CI, piped output -- the board becomes one plain line
per state change and the summary a plain aligned table with the same columns,
plus one `panel #N:` line per PR with panel data, which keeps both the CLI
name and the model: `panel #9: codex (gpt-5.5) 1 finding, top LOW`. A failed
review names its reason on its line: `FAILED  #9 (exit 10, 3s): You've hit
your session limit · resets 12pm (America/New_York)`.

### Orchestrators and the fallback

The **orchestrator** is the agent that runs the review: one session that reads
the skill, fans the panel out, synthesizes the findings, posts them, and
approves. It is not the panel. The panel is chosen by the
[`panel-review`](skills/panel-review) skill and is several models either way —
`--orchestrator` picks the one driving them.

```sh
autoreview --orchestrator codex             # codex runs the reviews
autoreview --orchestrator codex:gpt-5.5     # ...on a named model
autoreview --fallback none                  # never retry under another provider
```

Two backends can orchestrate, because both take an explicit skill invocation
and both read the Agent Skills layout:

| Orchestrator | Prompt form      | Sessions           | `--budget`   |
| ------------ | ---------------- | ------------------ | ------------ |
| `claude`     | `/auto-review N` | pinned and resumed | enforced     |
| `codex`      | `$auto-review N` | fresh every review | not enforced |

Those last two columns are dash-p's doing, not a preference: it forwards
`--session-id`, `--resume` and `--max-budget-usd` to claude alone. So flags
that cannot reach a codex run are **not sent** rather than silently dropped,
and a run says so at startup instead of leaving you to notice:

```
note: codex cannot resume a review session; every pass reviews fresh
note: --budget is not enforced under codex; dash-p forwards the cap to claude alone
```

A codex run loses `--continue` and the second-look pass that `--babysit` does
after the first interval — each pass reviews from scratch. Nothing else
changes: the panel, the posting, the approval gate and the GitHub verdict
readback are all the same, because none of them is the orchestrator's job.

**The fallback is what an outage costs you.** `dash-p` exits **10** when the
orchestrator itself fails — the provider is down, a usage limit was hit, the
turn came back `is_error`. Nothing about the PR caused that, so the review is
worth trying again somewhere else. By default the other backend retries it,
once:

```
  ↻ #9 claude exit 10 · retrying with codex
  ✓ #9 approved · risk LOW · 5m01s · via codex
```

and the summary says what happened, so a green run never hides a provider
that fell over:

```
PR #9: claude failed (exit 10); reviewed with codex instead
```

Five things worth knowing about it:

- **Only exit 10 is retried.** A timeout already had the whole allowance and
  retrying would spend it twice. A signal-death was somebody's decision. An
  overridden reviewer (`$AUTOREVIEW_AUTO_CMD`) is judged by its exit status
  alone, with no dash-p behind it to say what that status means.
- **Once.** The retry is one more chance, not a promise. If both providers
  fail, the run still fails and the summary names both attempts.
- **A retry is always a fresh review.** The failed attempt's session belongs
  to the provider that just failed, and cannot be handed to another backend.
- **The failed attempt is kept.** Its log is set aside as
  `pr-N.<orchestrator>.log`, so the outage still explains itself after the
  retry has written the plain names.
- **It respects `--jobs`.** A retry is a whole review, so it waits for a slot
  like anything else.

The default is `auto`: whichever of codex and claude is not the orchestrator,
if it is installed. A run says which on its first line, and says why when
there is none, because installing the other CLI is the fix:

```
orchestrator: claude · fallback: codex
orchestrator: claude · fallback: none (codex is not installed)
```

`--fallback none` turns it off. A fallback named by hand must be installed —
you asked for that retry, so a run that quietly has none is not the run you
asked for. `$AUTOREVIEW_ORCHESTRATOR` and `$AUTOREVIEW_FALLBACK` set both from
the environment, which is where a cron line usually wants them.

**During an outage you get reviews, not approvals.** The fallback rescues the
*orchestrator*, but the panel is drawn from the same CLIs — so a provider
being down also costs you its panelist. With three panelists and one down,
coverage is 2/3, under the 75% the approval gate needs. The reviews still run,
the findings still post; the stamp waits for a human or for the next
`--babysit` pass once the provider is back. That is the intended trade: an
approval is the one thing that should not be given on thin coverage.

**A failing panelist is a different thing entirely, and is already handled.**
A panelist that crashes or times out is not exit 10 and does not fail the
review: [`panel-review`](skills/panel-review) marks it failed in its report,
the synthesis is told not to count it toward consensus, and
[`auto-review`](skills/auto-review)'s gate withholds approval unless at least
**75% of the launched panel returned** and at least two did. So a panel that
comes back short posts its findings and declines to approve, rather than
approving on thin coverage. The panel table in the summary is where you see
it:

```
│ #8 ┆ claude-opus-4.7 ┆ failed   ┆ -        ┆ -      │
```

### Verdicts and models

The VERDICT column is read back from GitHub, not taken from the agent's
report: after a review finishes, autoreview asks `gh` whether your login
submitted a review on that PR since the job started -- approved, commented,
or changes requested. An agent that believed its own report would show
"approved" for an approval that never landed.

Everything GitHub cannot know comes from the reviewer itself. A system-prompt
instruction sent with every built-in review asks the agent to end its final
reply with a fenced `autoreview` code block: its decision, the synthesized
risk, the finding counts per bucket, and every launched panelist with its
self-reported model, result, finding count and top severity. The block is
best-effort -- a review that never writes it costs a `-` in those columns,
nothing more -- and it is also the verdict fallback when GitHub has nothing
to say.

MODEL is dash-p's accounting (`model_resolved` in its envelope): the model
that drove the review session. The panel table lists the models the
panelists ran on, as they reported them to the session that fanned them out.

### Reaching into a review

There is no tab to interrupt, which loses live steering but not access. Every
review runs in a session with a derived id, and the summary prints one per PR:

```sh
claude --resume cc10f740-28c3-58c6-ae64-d9ff37df22a7
```

That reopens the review as it was, findings and all. (The id comes from the
result envelope, so it names the session the review actually ran in, even when
the run let Claude Code allocate its own.) Intervention becomes on-demand
rather than up-front — which is the trade that makes an unattended sweep
possible at all.

The same trade buys three other things a tab cannot give you:

- **It runs anywhere.** No terminal required, so ssh, cron and CI are all fine.
  ([`review-prs`](#review-prs) needs herdr, cmux or Ghostty and refuses without
  one.)
- **Bounded concurrency.** Twelve PRs is twelve tabs the other way; here it is
  `--jobs 2`. Keep that number low — a panel review is itself several agents,
  so `--jobs 4` can mean a dozen concurrent processes.
- **Per-PR accounting.** dash-p's meta envelope gives cost per PR, and
  `--budget` caps each review's spend (claude's `--max-budget-usd`).

The exit status is the fourth, and it is the one that makes the whole thing
automatable: dash-p's codes are the signal, so an `is_error` turn and garbage
output are both `agent-error`, and a review that overruns `--timeout` counts as
failed too.

### The full-screen view

`--tui` draws the run full screen instead of the inline board. Every open PR is
on the left, two lines each, most pressing first. The selected PR's details
are on the right.

The screen opens whenever there is a terminal, including on a repo with
nothing to review: the PRs are all there, each saying why it is being left
alone, and `R` reviews any of them on the spot.

`--tui` is a view, not a mode: on its own it makes one pass and then waits.
`w` is what turns a run into a watching one, so `autoreview --tui` and
`autoreview --tui --watch=2` differ only in where you decide.

```
autoreview · acme/widgets · watching every 2m · a reviewed PR rests 30m · log …
⠸ #1711 · reviewing 1m12s         │ #1711 Fix the ledger migration
  @alice Fix the ledger migration │ @alice
· #1702 · queued                  │
  @dan Bump the client deps       │ reviewing 1m12s · claude
✗ #1650 · CI failing              │ session 3fcba529-a817-5cea-aea7-9c6bdf065a96
  @erin Cache keys by tenant      │ 14 turns · 9 tool calls
○ #1698 · rest 28m04s             │
  @bob Add retry logic            │ ACTIVITY
✗ #1640 · changes req             │    40s ago  Read     pool.rs
  @carol Refactor the client      │    12s ago  Bash     cargo test --quiet
✓ #1633 · approved                │
  @frank Drop the old gateway     │
1 running · 1 queued · 3 waiting · 1 finished · next check in 1m40s   q quit  j/k move
```

The list has no headings. It is ordered by what the run will do next: the
reviews running, then the ones about to start, then the PRs it is waiting on
(soonest first: due next pass, held for their checks, stacked, resting,
capped), then the ones it has reviewed, and last the PRs it has nothing to do
about -- seen, then approved.

A PR has one row of two lines, whatever has happened to it: the icon and one
state word say where its reviews stand and what the run is doing, and the
second line says whose work it is. The row keeps the PR's newest finished
review wherever it sits, so a PR resting under `--watch` still shows its
verdict, its "not approved yet" block and the review text.

The icon is GitHub's word, not the run's: `✓` approved, `✗` changes
requested, `○` nothing decided yet, and the spinner while a review of it is
running. A review this run just posted shows up there once the PR list has
been read again -- the same asymmetry the VERDICT column keeps.

| Key               | Does                                                          |
| ----------------- | ------------------------------------------------------------- |
| `j` `k`, arrows   | move the selection                                            |
| `g` `G`           | first and last row                                            |
| `ctrl-d` `ctrl-u` | scroll the right pane                                         |
| `r`               | open the selected review in a new herdr, cmux or Ghostty tab  |
| `o`               | open the PR in the browser                                    |
| `x` `x`           | stop the selected running review                              |
| `R`               | review the selected PR now                                    |
| `w`               | start or stop looking for work, as `--watch` does             |
| `l`               | show the run log in the right pane; `esc` puts it away        |
| `q`               | quit; a second `q` when reviews are running, which stops them |

`r` runs `cd <repo> && claude --resume <session>` in the new tab, or
`codex resume <session>` for a review codex drove. It refuses a review that
is still running. `x` ends a review as a failure, reported as "stopped"; the
fallback does not retry it, and the next pass reviews that PR from scratch.
`w` starts the run looking for work on its own, on the `--watch` interval
(`$AUTOREVIEW_WATCH_INTERVAL`, default 2m), resting each reviewed PR for the
`--babysit` one (default 30m). `w` again stops it: the reviews running finish
and the screen waits for `q`. A `--babysit` run turned off and on again comes
back watching, because the key asks one question -- keep looking for work? --
and watching is the answer that suits somebody at a screen.

`R` reviews the selected PR now, whatever the rest, the cap or the sweep say.
It wakes the run and reviews that PR on its own; under `--babysit` the other
PRs keep their interval. A one-shot run that has finished its pass starts
another for it, so one screen can work through a repo by hand. Only a review
already running refuses, and, under a run that looks again, a PR it has
dropped as approved or closed.

While the view is up, everything the run would have printed goes to
`autoreview.log` in the run directory, byte for byte, and `l` shows it. A run
that ends keeps the view until `q`, then prints one summary table for the
whole run. Off a terminal, `--tui` says so and the run prints its plain lines.

### Prompts

Skills are invoked by name rather than in prose — an unattended one-shot has
no human to correct a prompt that failed to trigger the skill. Each backend
has its own explicit form: Claude Code takes a slash command, and codex
reserves `/` for its own built-in commands and takes `$name` instead.

| Run                                                  | claude            | codex             |
| ---------------------------------------------------- | ----------------- | ----------------- |
| The default sweep, and `--babysit`                   | `/auto-review N`  | `$auto-review N`  |
| A first review under `--pick`                        | `/panel-review N` | `$panel-review N` |
| `--continue`, and every babysit pass after the first | `/recheck-pr N`   | `$recheck-pr N`   |

Reaching for `--pick` is the one thing that proves somebody is watching, so it
is what marks a run attended — and a `--babysit` loop outlives that person
either way, so it stays unattended.

There is no `pr-review-tab` here: that skill exists to close its own tab and to
run an in-session `/loop`, and a headless process needs neither.

### Babysit, headless

`--babysit` re-runs the pass on an interval until there is nothing left to do.
Each interval the queue is **rebuilt**, not just shrunk:

- **PRs leave** when `gh pr view` says they are approved or closed. Approval is
  read back from GitHub rather than inferred from what the agent said — the
  review either landed or it did not. A PR merged without an approving review
  leaves too; waiting for an approval that is never coming would re-review it
  every interval forever.
- **PRs join** when the sweep now ranks them actionable — a PR opened while the
  last pass was running, or one the author has just pushed to. Before this, the
  queue was fixed when the run started and a PR opened a minute later waited
  for a whole new invocation.

The two directions come from different sources on purpose. Leaving is decided
per PR by `gh pr view`, which is authoritative. Joining is decided by the sweep,
which is a snapshot and can lag by a poll — so a PR that has left is finished
for the run, and a stale list cannot put it back into the queue that just
dropped it.

Nothing new is needed to decide "should I look again": a review autoreview
posts becomes _your_ latest activity on that PR, so an author pushing a fix
afterwards flips it back to `UPDATED` on its own. The sweep filter already
means "actionable now".

A PR is queued **only while the sweep ranks it actionable**. After a review,
that review is your own latest activity, so an untouched PR goes quiet by
itself and costs nothing until its author moves — and comes back the moment
they push.

`--pick --babysit` stays inside what you picked. A run told to watch two PRs
does not quietly grow to five.

### The two bounds

Both exist because this runs unattended, and both end the run cleanly:

- `--max-passes` (default 3, or `$AUTOREVIEW_MAX_PASSES`) — every review is
  activity on the PR, so an author who answers one makes it actionable again,
  which would make autoreview review it again for as long as the loop runs.
  After that many passes on one PR, it says so and leaves it alone.
- `--max-idle` (default 3, or `$AUTOREVIEW_MAX_IDLE`) — how many checks in a
  row may find nothing to do. A PR nobody is touching should not keep a process
  alive forever, least of all one cron started, where the next run would pile
  on top of this one. The check straight after a pass does not count: your own
  review is the newest thing on every PR you just reviewed, so that one is idle
  by construction.

A transient API error does not end the loop and does not silently narrow it
either: the refresh keeps the current queue and tries again next interval,
giving up only after three consecutive failures.

The loop is the autoreview process itself, not an in-session `/loop` inside a
tab, so an interval that never converges is one process you can see and kill.
Interrupting it stops
the running reviews too, along with their children: an orphan keeps spending and
keeps holding its session open, which would make the next `--continue` refuse to
resume it.

### Logs

Each pass writes to `$log_dir/run-<random>/pass-N/`, printed at the start of every
run and again in the summary. The per-run directory is what lets two runs share
a `--log-dir` — ordinary under cron, where the default hour-long timeout outlasts
most intervals — without reading each other's results:

- `pr-N.review.md` — the review as text, which is the one to open. The
  built-in reviewer ends it with one of `DECISION: Approve`,
  `DECISION: Comment`, `DECISION: Request changes`, or `DECISION: No action`:
  what the reviewer did to the PR, or what the panel recommends when the
  reviewer was `panel-review` (`--no-post`, or a first review under `--pick`).
  An override is not bound to these strings.
- `pr-N.json` — dash-p's answer (`{"answer": ..., "metadata": ...}`)
- `pr-N.meta.json` — the metadata envelope (session id, cost, exit status);
  written even when a timeout or interrupt leaves stdout empty
- `pr-N.log` — stderr, which is where a failure explains itself
- `pr-N.<orchestrator>.log` — the same three files for an attempt the
  [fallback](#orchestrators-and-the-fallback) took over, set aside under the
  name of the orchestrator that failed. Only written when a review was retried.

Beside the passes, `run-<random>/agent/` holds the bundled skills as the
reviewers saw them, so a review can be read against the exact instructions it
ran under.

`--log-dir` pins the location; the default is a fresh temp directory per run.

### Stats

The log directory is per run, and the OS clears temp directories in days. What
each model found, and whether it held up, is only worth anything as a series --
so every review that reports the fenced trailer is also appended to a
**ledger** that outlives the run: one JSON line per review under
`~/.local/state/autoreview/ledger.jsonl`
(`$XDG_STATE_HOME` is honored; `$AUTOREVIEW_LEDGER` names a different file, and
`off` records nothing). `panel` writes to the same ledger.

`autoreview stats` reads it back per model:

```sh
autoreview stats                 # every recorded review
autoreview stats --since 2w      # or a date: --since 2026-08-01
autoreview stats --repo widgets  # one repo, by substring
autoreview stats --json          # the same numbers for another tool
autoreview stats --import        # read past reviews out of Claude Code's transcripts first
```

```
320 reviews from 2026-08-21 to 2026-09-09 across 4 repos

MODEL             BACKEND   RUNS  AVAIL          RAW/RUN  KEPT  UNIQUE  HIGH+  DROPPED  KEEP RATE    SAMPLE
claude-opus-5     claude    89    100% (96-100)  4.6      234   180     21     5        56% (51-61)  proven
claude-fable-5    claude    136   100% (97-100)  1.8      226   172     19     11       88% (83-91)  proven
glm-5.3           opencode  273   97% (94-98)    1.1      206   118     28     12       68% (62-73)  proven
gpt-5.6-sol       codex     220   97% (94-99)    0.9      136   78      32     18       72% (65-78)  proven
fable             claude    26    0% (0-13)      -        0     0       0      0        -            thin

decisions: 181 commented, 81 approved, 55 none, 3 changes-requested
needs attention: fable on claude answered 0 of 26 launches
```

A row is one model on one backend, however the trailer spelled it:
`xai/grok-4.6` and `grok-4.6` are one row, and so are `codex` and
`codex-gpt-5.6-sol`. The columns:

- **RUNS** is how often the model was launched on a panel, and **AVAIL** how
  often it came back with a review. A model that never answers is a
  misconfigured alias, and the `needs attention` line says so.
- **RAW/RUN** is what the model itself reported, per answered run. More is not
  better: a panelist that reports five findings a run and has one kept is
  costing the synthesizer time.
- **KEPT** is the number that matters: findings the synthesis kept after
  verifying them against the code, that name this model in their `Flagged by`
  line. **UNIQUE** is the subset no other panelist raised -- what the panel
  would have missed without it. **HIGH+** is the kept findings at HIGH or
  CRITICAL.
- **DROPPED** is the other side: findings the synthesis listed under
  Disagreements because verification overruled or downgraded them.
- **KEEP RATE** is kept over reported, run by run -- the closest thing the
  pipeline has to precision, since the synthesis is its only verification
  step. It is an estimate: the synthesis merges duplicate findings, and a
  panelist's own count is self-reported.
- **SAMPLE** says how much to trust the row: `proven` after 30 answered runs,
  `emerging` after 10, `thin` below that.

Every rate carries its 95% Wilson interval, because ten runs and three hundred
do not deserve the same confidence and a bare percentage hides which is which.

`--import` is for the history from before the ledger existed. Every review
autoreview ran left a Claude Code session transcript behind, with the synthesis
and the trailer in it, and the import reads those into the ledger once. It is
safe to repeat: a review already recorded is skipped, and so is any session
autoreview recorded live. It takes a few seconds per gigabyte of transcripts.

### Overrides

`$AUTOREVIEW_AUTO_CMD` for unattended runs (the default sweep and `--babysit`),
and `$AUTOREVIEW_CMD` for `--pick` runs, replace the built-in reviewer. Same
substitution rules as `$REVIEW_PRS_CMD` — the PR number replaces the first `{}`,
or is appended if there is no placeholder:

```sh
AUTOREVIEW_AUTO_CMD='my-review' autoreview
AUTOREVIEW_CMD='gh pr checkout {} && my-review {}' autoreview --pick
```

An override replaces the orchestrator, so `--orchestrator` and the fallback do
not apply to it: there is no dash-p behind it to say what its exit status
means, and its failures are its own to retry.

An override owns its own session handling and receives
`$REVIEW_PRS_SESSION_ID` and `$REVIEW_PRS_SESSION_RESUME` — the same contract
`review-prs` uses, so one wrapper works with both. Here they arrive in the
child's real environment; `review-prs` can only reach a new tab through a
command string, so it exports them there instead. Cost, session and model are dash-p's accounting, so the
summary shows `-` for them under an overridden reviewer -- it owns its own
sessions -- and the trailer is not read either. The verdict column still
works: it is read back from GitHub, which does not care who reviewed.

## panel

`panel` reviews **one change with several models at once**. Every backend CLI
on `PATH` (codex, claude, opencode) reviews the same diff in parallel through
dash-p, blind to the others. Their reports print as they land. One more model
call then reads all of them, verifies the questionable claims against the code,
and writes the report.

```sh
panel                         # review what you have not committed yet
panel --base main             # review what this branch added
panel --panelist codex --panelist claude:opus-4.8
panel --focus "the retry path"
panel --no-synthesis          # the raw reports, no synthesis
panel --help
```

### Why it is shaped this way

The same review used to be an agent skill: a coordinator model read 656 lines
of instructions and drove a 1094-line bash script that fanned the panelists
out. That put a model in charge of work that needs no judgment at all —
spawning processes, polling them, retrying a quota blip, collecting output —
and made the fan-out only as reliable as the coordinator's willingness to
follow instructions.

So the mechanical half is a program and the judgment half is one model call:

```
panel  ─┬─ dash-p → codex     ─┐
        ├─ dash-p → claude    ─┼─→ dash-p → claude (synthesis, read-only, in the repo)
        └─ dash-p → opencode  ─┘
```

The synthesis runs **in the repository**, not on the text alone. Verifying a
finding means reading the code it is about — a synthesizer handed only the
panelists' prose can merge their claims but cannot check any of them.

It is told two things a naive pipe would drop: which panelists failed, so their
silence is not read as agreement, and how many answered, so "flagged by 2 of 3"
means what it says.

### Isolation

A committed target (`--base`) gives each panelist its own throwaway git
worktree pinned to the same commit, with write and exec: panelists run the test
suite, grep for callers, and edit files to investigate, and nothing survives
the run. One worktree each rather than one shared, because parallel reviewers
racing on `target/` and `node_modules` produce flaky findings and leak edits
into each other's reading.

Uncommitted work has no ref to pin, so panelists read your actual working tree
with `--perms read-only` and change nothing.

Either way the worktrees are removed when the run ends — including on ctrl-C,
which stops the panelists first.

### What it does not do yet

`--pr` (review a GitHub PR by number, with panelists fetching it themselves),
review approaches (`/decompose`), and `$PANEL_REVIEW_PANELISTS`-style env
configuration all still live in the skill. This is the common path only.

## review-prs

The same PR list, fanned into one terminal tab per PR instead of a headless
process. Reach for it when you want to watch a review happen and interrupt it;
reach for [`autoreview`](#autoreview) for everything else.

Its tabs run `claude`, and there is no `--orchestrator` here: a tab has no
exit status to read, so there is nothing for a fallback to act on — you are
sitting in front of it, which is the point. To drive a different agent, use
`$REVIEW_PRS_CMD` (see [The review command](#the-review-command)).

Run from inside any GitHub repo:

```sh
review-prs              # open, non-draft, unapproved PRs (excludes yours + bots)
review-prs --auto       # skip the picker; auto-review every NEW/UPDATED PR
review-prs --babysit    # re-check non-approvable PRs on an interval until approved
review-prs --babysit=15 # ...every 15 minutes (default 30)
review-prs --continue   # resume an earlier review session instead of starting over
review-prs --all        # also include PRs already marked APPROVED
review-prs --dependabot # also include Dependabot PRs (shown dimmed)
review-prs --stacked    # also fan out PRs stacked on another open PR
review-prs --auto --skip-wait-for-ci # fan out a PR whatever its checks say
review-prs --help       # usage
```

In the picker: `space` toggles a PR, `enter` confirms. Each selected PR opens in
a fresh tab.

### The review command

Each spawned tab `cd`s to the repo root and runs a review command for the PR.
By default that is a non-interactive [Claude Code](https://claude.com/claude-code)
panel review:

```sh
claude --dangerously-skip-permissions --add-dir=<staged skills> --session-id <uuid> "panel review <number>"
```

(The `--session-id` is what makes [`--continue`](#continue-mode) possible later.)

Override it with the `REVIEW_PRS_CMD` environment variable. The PR number is
substituted for the first `{}` placeholder, or appended if there is no
placeholder:

```sh
# Append form — runs `review 123` in each tab (e.g. a shell function/alias):
REVIEW_PRS_CMD='review' review-prs

# Placeholder form — substitute the number anywhere in the command:
REVIEW_PRS_CMD='gh pr checkout {} && my-reviewer' review-prs
```

Note that `REVIEW_PRS_CMD` must be on the spawned tab's `PATH` (or be a shell
function/alias defined in its startup files) — the command runs in a fresh
shell, not the one you launched `review-prs` from.

An overridden command owns its own session handling. It still receives the PR's
session id as `$REVIEW_PRS_SESSION_ID`, along with `$REVIEW_PRS_SESSION_RESUME`
(`1` when `--continue` matched an existing session, `0` otherwise), so it can
wire up resumption however it likes.

### Auto mode

`--auto` skips the picker entirely: it fans out **every `NEW` and `UPDATED` PR**
(the actionable ones) and runs an auto-review command in each tab. `SEEN` PRs
are skipped on purpose — nothing has changed since you last engaged, so there's
no reason to re-review them. A PR whose checks have not passed is held, and
pending checks are waited for before the tabs open (see
[Waiting for CI](#waiting-for-ci)). Combine with `--all` / `--dependabot` to
widen the set, and `--skip-wait-for-ci` to fan out regardless of checks.

The per-tab command is `REVIEW_PRS_AUTO_CMD` (same `{}`/append substitution as
`REVIEW_PRS_CMD`), defaulting to the [`pr-review-tab`](skills/pr-review-tab)
skill:

```sh
claude --dangerously-skip-permissions --add-dir=<staged skills> --session-id <uuid> "pr-review-tab <number>"
```

That skill runs an auto-review and, **when the PR is approved, closes its tab**
so a finished review cleans up after itself. (Tabs are closed via the enclosing
multiplexer — `herdr tab close` / `cmux close-surface` — from inside the tab,
which is why the behavior lives in the skill, not in review-prs.)

### Babysit mode

`--babysit` keeps a not-yet-approvable PR's tab open and **re-checks it on an
interval until it can be approved**, then closes the tab — so a fix pushed
overnight gets stamped without you re-running anything. The interval defaults to
30 minutes; set it with `--babysit=MINUTES` or `$REVIEW_PRS_BABYSIT_INTERVAL`. A
bare number is minutes, and suffixed durations (`30m`, `1h`, `2d`) work too;
anything else is rejected up front rather than seeded into the tab.

It uses the same unattended command as `--auto`, so it composes with both the
sweep (`review-prs --auto --babysit`) and the picker (`review-prs --babysit`,
then choose which PRs to babysit). Under the hood the `pr-review-tab` skill
starts an in-session `/loop` that re-runs the
[`recheck-pr`](skills/recheck-pr) skill each interval;
`recheck-pr`'s fast path makes a no-change cycle cheap, and the loop ends when
the tab closes on approval.

### Continue mode

`--continue` (`-C`) reopens the review session a PR already had on this machine
instead of reviewing it from scratch. The resumed tab still holds the earlier
findings, so it takes a **second look** — did the author fix them? — rather than
re-deriving a review the author has already answered.

```sh
review-prs --continue            # picker; RESUMABLE rows reopen their session
review-prs --auto --continue     # sweep, resuming wherever a session exists
```

Each PR gets a session id derived from the repo directory plus
`owner/name#number`, so the same PR in the same checkout maps to the same
session on every run. There is no state file: nothing to sync, nothing to go
stale when a PR closes. A first review pins the id with `--session-id`;
`--continue` reopens it with `--resume` and swaps the prompt:

| Run                    | Prompt                        |
| ---------------------- | ----------------------------- |
| First review           | `panel review <N>`            |
| `--continue`           | `recheck-pr <N>`              |
| `--auto` / `--babysit` | `pr-review-tab <N>`           |
| ...with `--continue`   | `pr-review-tab <N> --recheck` |

PRs with a session show `RESUMABLE` in the picker, so you can see what would be
resumed before you choose. Without `--continue` those PRs review from scratch in
a fresh session, exactly as before.

Two limits worth knowing:

- **A session belongs to one checkout.** Sessions live under
  `~/.claude/projects`, keyed to the repo root the tab `cd`s into, and the id
  hashes that path in. A second clone or a `git worktree` of the same repo
  therefore starts its own session for the same PR rather than reopening the
  other one. Another machine will not find them at all.
- **Sessions grow.** A PR resumed many times accumulates context and eventually
  auto-compacts. That is fine for a second look; it is not a substitute for a
  fresh review when a PR has been rewritten.
- **Only the first review is addressable.** A PR keeps exactly one derived id.
  Reviewing a PR again _without_ `-C` deliberately starts an unnamed session, so
  a later `-C` reopens the first review, not that one. Treat a no-`-C` re-review
  as a throwaway; use `-C` for the thread you want to keep.
- **One tab at a time.** `-C` will not reopen a session another tab still holds
  open — a babysit tab, typically. It says so and reviews fresh instead.

Your own PRs are always excluded — this tool is for reviewing others' work.
Dependabot PRs are hidden by default; pass `--dependabot` to include them, where
they appear dimmed to mark them as lower-priority. (The bot match is one
anchored prefix in `src/prlist.rs` — extend it as more AI coding bots show up.)

### Naming

Under herdr and cmux, review sessions label themselves so a screenful of tabs
stays readable:

- **Workspace** — renamed to `REVIEW_PRS_WORKSPACE` (default `pr reviews`). Set
  it empty (`REVIEW_PRS_WORKSPACE=`) to leave your workspace title alone.
- **Tabs** — named for the PR and what the tab is doing: `PR 27 Review`,
  `PR 27 Auto-Review` (`--auto`), `PR 27 Recheck` (a resumed `--continue`
  session), or `PR 27 Babysit` (`--babysit`, which wins over the others).

Both names are sticky: they survive the terminal-title escapes the review
command emits while it runs, which would otherwise replace them with a generic
agent-generated summary.

The workspace rename and the cmux tab rename are best-effort — a failure there
is ignored. Under herdr the label is applied at tab-create time, so a rejected
label fails that tab's spawn; the sweep warns and continues with the next PR.

Ghostty tabs are left unnamed. It has no sticky-title API, so any title set at
spawn time would be overwritten by the review command within seconds.

## Picking the PRs

`review-prs` and `autoreview` start the same way:

1. List the current repo's open, non-draft PRs (via the GitHub GraphQL API).
2. Annotate each with an engagement badge, a review-state flag, and a relative
   "last activity" time, then sort the most actionable to the top.
3. Take every `NEW` and `UPDATED` PR whose checks have passed and which is not
   [sitting on another open PR](#stacked-prs), or let you multi-select with
   [gum](https://github.com/charmbracelet/gum).

Then they diverge: `review-prs` opens a terminal tab per PR and runs the
[review command](#the-review-command) in it; `autoreview` runs a headless
subprocess per PR instead. `panel` does none of this — it takes one diff and
asks several models about it.

### Waiting for CI

A PR opened a minute ago has its linter still running, and a review of code the
author is about to fix is a review wasted. So the sweep reads the checks on
each PR's head commit (from the same GraphQL call, so nothing extra is fetched)
and **holds a PR until they pass**:

- **Pending** checks are waited for. A one-shot run — `autoreview`,
  `autoreview --babysit`, `review-prs --auto` — has no next poll, so it waits
  right there, polling every 30 seconds for up to 30 minutes
  (`$AUTOREVIEW_CI_WAIT` / `$REVIEW_PRS_CI_WAIT`, in the `--babysit` shapes:
  `45`, `1h`). The wait is announced, counted up on the spinner, and a PR
  still pending at the limit is held rather than reviewed.
- **Failing** checks hold the PR with no wait: failing is settled, and the
  author's next push is what changes it. The run says which PRs it is holding
  and why.
- A `--watch` run, and the refresh between `--babysit` passes, never wait: they
  look again on their next poll, and a held PR joins the queue the moment its
  checks go green. A loop names a held PR once per state, not once per poll.
  A `--babysit` run that finds only held PRs keeps checking on its interval,
  within `--max-idle`, rather than exiting with nothing reviewed.
- The wait tolerates a refresh that fails, up to three in a row, and is
  bounded by the limit either way. The limit is validated only by a run that
  will wait, so a bad value in a shell profile does not refuse a `--watch` run
  or the picker.
- A PR with **no checks at all** is never held. A repo without CI is not a
  repo that waits for ever.

`--skip-wait-for-ci` turns all of this off and reviews whatever the checks
say. The picker never holds a pick — it shows the checks in a
[CI column](#ci) so you choose knowing, and reviews what you chose. A loop
after a pick (`--pick --babysit`, `--pick --watch`) holds like any loop on its
later polls: a picked PR that is pushed to is reviewed again once its checks
pass.

### Stacked PRs

A PR sitting on top of another open PR is held until that PR lands, so a stack
is reviewed once, from the bottom, as it merges. Two shapes are held, and each
is found a different way.

**A declared stack** — the PR's base is another open PR's branch, which is what
GitHub's stacked PRs and every stacking tool produce. GitHub diffs it from that
branch, so its diff is clean; but its code only makes sense on top of the PR
below it, and reviewing it means reading that PR's work for the integration
anyway. Found by the branch names, which is the one case the base branch does
answer.

Branch names alone would mistake a long-lived branch for a stack, so two guards
keep integration branches out. The repository's default branch is never a stack
tip — a PR that merges `main` back into a release branch would otherwise hold
every PR that merges into `main`. And a stack parent creates the branch its
children merge into, so no open PR was already merging into that branch before
the parent was opened: on a git-flow repo the PRs merging into `develop` predate
the PR that merges `develop` onward, and none of them is stacked on it.

**An undeclared stack** — the branch was cut from another open PR's branch
while it was in flight, and still says `base: main`. GitHub then serves the
diff from where the two branches parted, so that PR's commits sit inside this
one's diff and reviewing both reads the same code twice. Measured on this
repo's own PRs: #18's diff was 314KB across 23 files, of which 12KB across 5
files was its own work. The other 96% was #15, open at the same time and
already reviewed — and both PRs said `base: main`, so nothing in the base
branch showed it. Found in the commits, whose `oid`s ride along on a list the
same GraphQL call already fetches.

The sweep names each held PR and how it was found:

```
holding 2 PRs stacked on another PR: #16 (based on #15) #18 (8 commits also in #15); --stacked reviews them anyway
```

A held PR is released by the PR underneath it landing. Every hold names one PR
directly underneath and rests on evidence about those two PRs alone: a declared
base, a carried tip commit, or commits the two diffs share. Relatedness is never
passed along a chain — two PRs that share nothing are not related because a
third one carries both, and a PR held on work it does not share would wait for a
merge that changes nothing about it. A relation that pointed in a circle would
leave every PR in it waiting, so the oldest member of a circle is freed.

Every open PR counts for this, including drafts, approved ones, bots and your
own: a colleague's branch cut from your unmerged work carries your commits
whether or not this tool would ever review yours. So the relation is worked out
before any filter runs.

Unlike a CI hold, this is **not** a reason for a run to keep waiting. Checks
settle by themselves in minutes; a stack moves when a person merges something.
So a `--watch` or `--babysit` run names a stacked PR once and drops it from its
watch list, and a one-shot run never spends its
[CI wait](#waiting-for-ci) on a PR the stack gate will hold anyway. A dropped PR
rejoins as new work once the stack clears.

`--stacked` / `-s` turns the hold off and reviews every PR in the stack. The
picker never holds a pick — it marks those rows `(stacked on #15)` so you
choose knowing — and a loop after a pick (`--pick --babysit`, `--pick --watch`)
keeps a picked PR even when it starts to sit on another open PR.

## Columns

```
#NUM   ENGAGEMENT   REVIEW   CI   SESSION   TIME   AUTHOR   TITLE
```

### Engagement

How the PR stands relative to your own comments and reviews:

| Badge     | Meaning                                                       |
| --------- | ------------------------------------------------------------- |
| `NEW`     | You have not commented or reviewed this PR.                   |
| `UPDATED` | New comments, reviews, or commits since your last engagement. |
| `SEEN`    | You engaged and nothing has changed since.                    |

### Review

The PR's overall review decision:

| Flag       | Meaning                                                       |
| ---------- | ------------------------------------------------------------- |
| `CHANGES`  | Reviewers requested changes (`CHANGES_REQUESTED`).            |
| `APPROVED` | Already approved. Hidden by default; shown only with `--all`. |
| `-`        | No decision yet.                                              |

### CI

The checks on the PR's head commit, which decide whether the sweep reviews it
now (see [Waiting for CI](#waiting-for-ci)):

| Flag      | Meaning                                                     |
| --------- | ----------------------------------------------------------- |
| `PASSING` | Every check passed. The sweep reviews it.                   |
| `PENDING` | Checks still running. The sweep waits for them.             |
| `FAILING` | A check failed or errored. The sweep holds it until a push. |
| `-`       | No checks on this commit. Never held.                       |

### Session

Whether this machine already has a review session for the PR:

| Flag        | Meaning                                                    |
| ----------- | ---------------------------------------------------------- |
| `RESUMABLE` | An earlier review session exists; `--continue` reopens it. |
| `-`         | No session yet; a review starts from scratch.              |

The column only appears when at least one PR is resumable — on a repo you have
never reviewed, every row would read `-`.

PRs are sorted `NEW` first, then `UPDATED`, then `SEEN`, with most recent
activity breaking ties.

## While it waits

Three network calls stand between launch and the first thing worth showing:
`gh repo view`, `gh api user`, then one GraphQL call for the PR list. The two
PR tools say what they are doing through them:

```
reading the repo
fetching open PRs from acme/widgets
found 40 open PRs, 3 to consider
3 PRs to review: #9 (new) #8 (updated) #6 (new)
```

The two counts are deliberate. The second is what is left after your own PRs,
bots and approved ones are filtered out — and on a repo where most of the open
PRs are yours, "found 3" on its own reads as a broken query rather than a
working filter.

The `found` line stays. The steps around it do not: on a terminal they are one
spinner line that rewrites itself and leaves nothing behind, so the report
still starts at the top. Under cron and CI, or with stderr redirected, each
step is a plain line instead.

**stderr decides**, because that is where all of this goes — a run whose stdout
is piped to a file still has a terminal to spin on, and still leaves that file
holding only the report.

`panel` reports its own three waits the same way, and the last two count up
rather than sitting still:

```
reading the repo
building the diff
materializing worktree 3 of 4
3 panelists still reviewing, 4m12s
synthesizing with claude, 1m30s
```

Materializing a checkout per panelist is the longest thing a panel run does
before a model is asked anything, and the synthesis is the most expensive
silence in the run to mistake for a hang — every panelist has already been
paid for by the time it starts.

## Notes

- The GraphQL query inspects up to the 100 most recent comments, reviews, and
  commits per PR — ample for typical PRs, and always inclusive of the latest
  activity that drives the engagement badge.
- Tabs are spawned serially; Ghostty gets a small delay between tabs to avoid
  racing the new-tab keystroke.

## Development

```sh
bash tests/run.sh
```

`AGENTS.md` holds the rules for working in this repo, for people and for
coding agents alike (`CLAUDE.md` points at it). The design decisions behind
the code, and what each one rules out, are one file each under
[`docs/decisions/`](docs/decisions). A PR that decides something adds one.

### Layout

One crate, one library, two binaries:

```
src/lib.rs         the shared core all three binaries are built on
src/bin/           the three entry points: review-prs, autoreview, panel

src/prlist.rs      the GraphQL query, engagement ranking, the sweep
src/ci.rs          the head commit's checks, and the wait for them
src/stack.rs       which PRs sit on top of another open PR
src/picker.rs      the gum picker
src/select.rs      fetch, rank, then sweep or pick
src/session.rs     derived session ids, and how a PR attaches to one
src/skills.rs      the review skills, compiled in and staged for each run
src/repo.rs        dependency checks, repo and user context
src/interval.rs    babysit-interval parsing
src/cli.rs         autoreview's flags
src/orchestrator.rs which agent drives a review, and what retries it

src/tabs/          review-prs: cli, per-tab command, terminal spawners
src/panel/         panel: cli, target, worktrees, fan-out, synthesis
prompts/           the panelist and synthesis prompts, compiled in
skills/            the review skills the agents run, one directory each
src/pool.rs        autoreview: the event-driven job pool
src/job.rs         autoreview: one review, spawned and classified
src/report.rs      autoreview: verdict readback and the agent's trailer
src/rundir.rs      autoreview: what one run writes under --log-dir
src/ui.rs          autoreview: what every board row and summary says
src/board.rs       autoreview: the live area, an inline viewport in raw mode
src/tui/           autoreview --tui: the list, the detail pane, keys, the terminal
src/findings.rs    the findings a synthesized review attributes to each panelist
src/ledger.rs      the append-only record of finished reviews, across runs
src/stats/         autoreview stats: cli, per-model folding, rendering, import
```

The two tools have to agree on what counts as an actionable PR and which
session it belongs to. That is not maintained — it is structural: they select
and derive through the same code. What differs is everything below the
selection, and it is what each binary is: `review-prs` spawns tabs,
`autoreview` runs an event-driven job pool over dash-p subprocesses.

### Tests

`tests/run.sh` builds the crate, runs `cargo test` (the unit layer: interval
parsing, session goldens, ranking, CLI validation, argv and tab-command
shapes), then runs the bash suites — the real binaries against fake `gh`,
`gum`, `cmux` and `dash-p` on `PATH`, inside a throwaway git repo, with
`$CLAUDE_CONFIG_DIR` pointed at a throwaway session store. They never touch
your repos, your Claude Code sessions, or GitHub. Two files,
`tests/board.test.sh` and `tests/tui.test.sh`, run autoreview on a pty rather
than a pipe, through `tests/pty.py`: a driver that answers the board's cursor
query, resizes the terminal mid-pass and presses keys. They are the only times
the live board and the full-screen view are drawn under test, and they need
`python3`; without one they say so and skip.
It finishes with `bash -n`
and `shellcheck` over the suite itself and over the scripts in `skills/`, which
is all the bash in the repo.

The suite takes about a minute and a half, most of it one test: whether a
babysit pass resumes the session the previous pass ran in can only be shown by
running a second pass, and the shortest interval the tool accepts is a minute.

CI runs the same command on macOS and Linux. macOS is the primary target;
Linux catches anything that quietly depended on it.

## License

MIT
