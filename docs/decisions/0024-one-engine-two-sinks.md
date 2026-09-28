# One engine on its own thread, and two sinks

Recorded: 2026-09-28
Status: proposed

## Context

Decisions 0022 and 0023 left two front-ends: the full-screen view on a
terminal, and plain lines everywhere else. The engine under them was written
for plain lines, and the view was fitted around it afterwards. That shows in
five places.

- **The loop lives in the binary.** `run()` in `src/bin/autoreview.rs` is
  about 590 lines of policy: what to look at, when to wait, when to stop.
  Nothing in the library can run it, so nothing but the bash suite tests it.
- **`Ui` is three things.** It prints, it holds the view, and it is the
  control inbox: `requests`, `watch_toggle` and `focus_change` are mailboxes
  the loop polls from half a dozen places. It also keeps the run's archive,
  and `Ui::interrupted` exits the process from inside the UI.
- **The view survives the loop by workarounds.** fds 1 and 2 are pointed at
  the run log while the view is up, so the loop's prints cannot land on the
  screen. The waits between passes run in 100 ms sleeps that draw and read
  keys (`ui.wait`, `ui.hold`), and `gh` runs on a scoped thread for the same
  reason (`ui.while_busy`). `pool.rs` asks `ui.ticking()` whether to wake ten
  times a second.
- **Each mode has its own branches.** One-shot, `--watch` and `--babysit`
  each decide idling, refresh failures and holding for `R` their own way. A
  refresh failure is handled three ways, and a queued request is taken into
  an intake in three places. The mode is read from four places:
  `cfg.watch`, a local `watch`, `cfg.babysit` (changed in place) and
  `tracker.cooldown`.
- **CI has a second wait loop.** A one-shot run waits for checks in
  `ci::settle`, with its own sleep and its own refetch-failure count, before
  the main loop starts.

Each new key or mode has to be added to every one of these places. The
workarounds also constrain what can be done next: no code may ask crossterm
for the cursor while the view is up, and no key handling may block.

## Decision

The engine moves into the library and runs on its own thread. It takes
commands in and sends events out. The view and the plain printer consume the
events, and neither calls the engine.

### The engine

`src/engine/` owns the run:

- the `Queue`, which is already a pure intake state machine. It changes in
  one place (see "One loop, with a policy").
- a `Policy` (below),
- the jobs of the pass in progress, and the newest finished job per PR,
- the run directory, and the session and failure state per PR. This state
  lives in memory. The files in the run directory stay as a record, and the
  engine does not read them back.

It waits in one place, on one channel, for one of these inputs:

```rust
enum Input {
    Command(Command),
    JobReaped { idx: usize, elapsed_secs: u64 },   // from pool::Event, as today
    JobExited { idx: usize, status: ExitStatus, readback: Option<Readback> },
    Listed(Result<Looked>),                        // a PR list fetch finished
    Signal,
}

enum Command {
    ReviewNow(u64),
    StopReview(u64),
    Stop,
    SetWatching(bool),
    SetFocus(Option<String>),
}
```

The wait is `recv_timeout` until the earliest of the next poll, the next
review deadline, the next CI recheck of a held PR (every 30 seconds, as
`ci::settle` polls today) and the CI wait limit. The `gh` fetch runs on a worker thread
and posts `Listed`. The engine does not sleep, and it does not tick when
nothing is due.

`Command` replaces `tui::Action` for the variants that reach the engine. The
view keeps its own view-only intents (selection, scrolling, mouse).

### The events

```rust
enum Event {
    PassStarted { pass: u32, prs: Vec<u64>, jobs_max: u32, dir: PathBuf },
    Started(Job),
    Reaped(Job),
    Retrying(Job),
    Finished(Job),
    Line(String),   // stdout
    Note(String),   // stderr
    PassEnded(Vec<Job>),
    Listed { info: HashMap<u64, PrInfo>, ranked: Vec<u64>, waiting: Vec<(u64, Wait)> },
    Mode { line: String, looping: bool },
    Focus(Option<String>),
    Busy(Option<String>),
    Ended { why: String, code: i32 },
}
```

A `Job` in an event is an owned copy. No sink holds a reference into the
engine. Between them the events carry every state a row shows today:

- `PassStarted` names the PRs of the pass, so the view can draw them as
  queued.
- `Reaped` is sent when the review process exits. It carries the time,
  model, cost and session that `JobReaped` records now, while the GitHub
  readback is still running. The view shows it as "finishing".
- `Finished` is sent when the readback returns.
- `Line` and `Note` keep the stream each line goes to today. Progress goes
  to stderr and the report to stdout (decision 0014), and the bash suite
  reads them apart.
- `PassEnded` carries the jobs of the pass that ended.

### The sinks

- **Plain.** One writer that turns each event into the lines the bash suite
  pins today, byte for byte. It prints each pass summary on `PassEnded`, as
  a headless run does today, before the loop waits again. Headless runs
  write it to stdout and stderr, as today (decision 0014).
- **View.** The main thread runs the view's loop: draw, read crossterm for up
  to 100 ms, send any `Command`, then take the waiting events. Crossterm is
  still read on the main thread only, and nothing else reads it. While the
  view is up, a second plain sink writes to `<run>/autoreview.log`. The log
  keeps its content, pass summaries included, but a sink writes it now, and
  fds 1 and 2 stay where they are. The view prints one summary for the
  whole run on `Ended`, after it closes (decision 0020).

Only the sinks print. The 38 bare `println!`/`eprintln!` calls in the loop
become `Line` and `Note` events, each on the stream it uses today. Before
the engine starts, only the preflight and the picker print, on the normal
screen, as the startup lines do now (decision 0020). The first look and the
CI wait run in the engine, so what `prlist` and `ci` say through `Status`
today becomes `Busy`, `Line` and `Note` events.

### Following a running review

The view follows each review's transcript itself (decision 0016). `Job` has
no path fields, so the view finds a review's files from `PassStarted.dir`
and the PR number, the way `rundir` names them. It reads the transcript on
its own tick, and the engine stops polling activity.

### One loop, with a policy

```rust
struct Policy {
    poll: Option<Duration>,      // None: look once
    rest: Duration,              // per PR, after a review
    reset_cap_on_push: bool,
    ci: CiRule,
    stop: StopRule,
}
struct CiRule {
    first: CiWait,               // the first look: Hold | WaitUpTo(Duration) | Off
    later: CiWait,               // every look after it
}
struct StopRule {
    when_exhausted: bool,        // nothing left to watch ends the run
    max_idle: Option<u32>,
    max_refresh_failures: Option<u32>,
}
```

- One-shot: look once. `ci.first: WaitUpTo($AUTOREVIEW_CI_WAIT)`. Stop when
  exhausted. The CI wait becomes "hold the PR and look again" in the same
  loop, and `ci::settle` goes. For this to work, the first look moves into
  the engine too: today `select::run` calls `ci::settle` before the binary
  starts the loop.
- `--watch`: poll every 2 minutes, rest 30 minutes, never stop, back off on
  a refresh failure. `ci` is `Hold` on every look: a held PR waits for the
  next poll.
- `--babysit`: poll at the interval, rest at the interval, stop when
  exhausted, `--max-idle`, and 3 refresh failures. `ci.first` is
  `WaitUpTo($AUTOREVIEW_CI_WAIT)` and `ci.later` is `Hold`, as today.
  `src/cli.rs` gives a babysit run its CI wait, and a unit test there pins
  it. Decision 0011 names only the one-shot run as one that waits, so step
  3 corrects 0011.
- `--skip-wait-for-ci` sets both to `Off`.
- `w` replaces the policy. It does not flip flags.

Decision 0010 still applies. Today a babysit run behaves like this:

- The first pass starts at once.
- When a pass ends, the loop looks at once.
- If that look finds work, the loop waits one interval, then reviews what
  it found. The interval gives an author time to answer the review.
- If that look finds nothing, the loop looks again every interval. Work
  found on one of those looks is reviewed at once, because an interval has
  already passed.

Today the `Queue`'s `cooldown` turns on two things together: the rest per
PR, and a cap that a push resets. Only `--watch` sets it. A policy sets
them apart, as `rest` and `reset_cap_on_push`. `--watch` has both.
`--babysit` gets the rest and keeps its cap as today: a push does not reset
it. Decision 0010 says a push resets the cap, which the code does only under
`--watch`. Step 3 records which of the two is meant, in 0010 and in the
code.

A policy keeps the looks: one at once when a pass ends, then one every
`poll`. It moves the interval from the loop to the PR: `rest` stops a PR
from being reviewed again until one interval after its last review. The
difference is a PR that opens during a pass. Today it waits the interval
with the rest of the queue. Under a policy it has no rest to wait out, so
it is reviewed at the first look after the pass. A PR that was just reviewed
still waits one interval, as today.

When that difference changes a line in the bash suite, the test change goes
in the same commit as the code change.

### Passes stay

A pass is still one intake batch in `pass-N` (decision 0009). The exit status
still describes the final pass (decision 0003). A `ReviewNow` for a PR the
pass has not started still goes first, and any other PR goes first in the
next intake, as decision 0020 says. Continuous slots, where a request starts
the moment a slot frees, are out of scope here.

### Exit

The engine never calls `process::exit`. Today `Ui::interrupted` does, with
exit status 130, from inside the loop. An interrupt, `q`, or the policy
saying stop becomes `Ended { why, code }`. The main thread closes the view,
prints the summary through the plain sink and exits with the code: 0, 1 or
130, as decision 0003 says.

### Where the code goes

- `src/engine/mod.rs`: `Engine`, its run loop and `Input`
- `src/engine/policy.rs`: `Policy` and the three constructors
- `src/engine/intake.rs`: moved from the binary (`actionable_now`,
  `drop_finished`, `held_for_this_run`, `waiting_list`, the held and stacked
  reporting)
- `src/engine/pass.rs`: `pool::run_pass` without its `Ui` parameter
- `src/engine/event.rs`: `Command` and `Event`
- `src/plain.rs`: the plain sink, from the printing half of `ui.rs`
- `src/tui/`: the view's main-thread loop

`src/bin/autoreview.rs` does these things only:

- parse the flags,
- run the preflight, and the picker under `--pick` (the first automatic
  look is the engine's),
- start the engine,
- run a sink,
- exit with the code.

The target is under 200 lines. `Ui` and `src/ui/full.rs` go.

### Order of work

Each step is one PR, and `tests/run.sh` passes after each one.

1. `Event` and the plain sink, on the current thread. `Ui`'s printing
   methods send events. No behavior change.
2. The `Command` channel. The `Ui` mailboxes and `poll_input`'s filtering
   go. No behavior change.
3. The loop moves into `src/engine/` with `Policy`, and so does the first
   look. The mode branches and `ci::settle` go. The CI wait at startup now
   shows in the view as waiting rows, not as a spinner before the view
   opens. That changes one consequence of decision 0020. Any babysit timing
   lines change here, with their tests.
4. Transcript following moves into the view, and the pool stops polling
   activity. This comes before the thread split: after the split, the
   engine thread cannot update the activity of a job copy that the view
   holds. `ticking()` stays for now, because the pool still wakes every
   100 ms to redraw the view.
5. The engine goes on its own thread, and the view runs on the main thread
   with its own tick. The fd redirect, `wait`, `while_busy`, `hold`, `Woke`
   and `ticking()` go. The run log is written by a sink.
6. `Ended` replaces the `process::exit(130)` in `Ui::interrupted`.

## Consequences

- The engine can be tested without a terminal or `gh`. A test sends
  `Input`s and checks the `Event`s. The bash suite stays as the end-to-end
  check of the plain lines.
- The view becomes a function of its model and the terminal size, so most of
  `tests/tui.test.sh` can move to ratatui `TestBackend` tests. A pty smoke
  test stays for what only a real process shows: the alternate screen, the
  mouse escapes, a cooked terminal after exit, and exit codes.
- Estimated deletions: about 350 lines of workarounds (the fd redirect and
  the sleep-poll waits), about 300 to 400 lines of mode branches, and the
  mailboxes and archive in `Ui`. The events and sinks add some of that back.
  The binary goes from about 1,000 lines to under 200.
- Output order is the order the engine emits. Each sink writes from one
  thread, so plain lines cannot interleave.
- A print that bypasses the sinks, while the view is up, lands on the screen.
  The fd redirect caught this, and now nothing does. Step 5 adds a pty test
  that drives a full watch cycle and checks that the screen shows only the
  view.
- These notes stop being true, and change in the step that makes them false:
  - the "fds 1 and 2 point at the run log" convention in `AGENTS.md`
    (step 5),
  - the "loop keeps the view drawn while it waits" part of decision 0020
    (step 5),
  - the `ui.ticking()` consequence of decision 0020 (step 5),
  - the consequence of decision 0020 that the CI wait runs before the view
    opens (step 3).
- Decisions 0003, 0009, 0010, 0011 and 0020 otherwise still apply. When this
  is accepted, their mentions of `ui` and the binary's loop point here.
