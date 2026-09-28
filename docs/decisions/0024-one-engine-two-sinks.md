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

- the `Queue` (unchanged: it is already a pure intake state machine),
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
review deadline and the CI wait limit. The `gh` fetch runs on a worker thread
and posts `Listed`. The engine does not sleep, and it does not tick when
nothing is due.

`Command` replaces `tui::Action` for the variants that reach the engine. The
view keeps its own view-only intents (selection, scrolling, mouse).

### The events

```rust
enum Event {
    PassStarted { pass: u32, total: usize, jobs_max: u32, dir: PathBuf },
    Started(Job),
    Retrying(Job),
    Finished(Job),
    Note(String),
    Listed { info: HashMap<u64, PrInfo>, ranked: Vec<u64>, waiting: Vec<(u64, Wait)> },
    Mode { line: String, looping: bool },
    Focus(Option<String>),
    Busy(Option<String>),
    Ended { why: String, code: i32 },
}
```

A `Job` in an event is an owned copy. No sink holds a reference into the
engine.

### The sinks

- **Plain.** One writer that turns each event into the lines the bash suite
  pins today, byte for byte, and prints the summary on `Ended`. Headless runs
  write it to stdout and stderr, as today (decision 0014).
- **View.** The main thread runs the view's loop: draw, read crossterm for up
  to 100 ms, send any `Command`, then take the waiting events. Crossterm is
  still read on the main thread only, and nothing else reads it. While the
  view is up, a second plain sink writes to `<run>/autoreview.log`. The log
  keeps its content, but a sink writes it now, and fds 1 and 2 stay where
  they are.

Only the sinks print. The 38 bare `println!`/`eprintln!` calls in the loop
become `Note` events. Library code that prints today (`prlist`, `ci` through
`Status`) runs either before the engine starts, as the startup lines do now
(decision 0020), or through `Note`.

### Following a running review

The view follows each review's transcript itself (decision 0016). `Started`
carries the job and its log paths, and the view reads the transcript on its
own tick. The engine stops polling activity, and `ticking()` goes.

### One loop, with a policy

```rust
struct Policy {
    poll: Option<Duration>,      // None: look once
    rest: Duration,              // per PR, after a review
    ci: CiWait,                  // Hold | WaitUpTo(Duration) | Off
    stop: StopRule,
}
struct StopRule {
    when_exhausted: bool,        // nothing left to watch ends the run
    max_idle: Option<u32>,
    max_refresh_failures: Option<u32>,
}
```

- One-shot: look once. `ci: WaitUpTo($AUTOREVIEW_CI_WAIT)`. Stop when
  exhausted. The CI wait becomes "hold the PR and look again" in the same
  loop, and `ci::settle` goes.
- `--watch`: poll every 2 minutes, rest 30 minutes, never stop, back off on
  a refresh failure.
- `--babysit`: poll at the interval, rest at the interval, stop when
  exhausted, `--max-idle`, and 3 refresh failures.
- `w` replaces the policy. It does not flip flags.

Decision 0010 still applies. A babysit run looks every interval, and a PR
found on a look is reviewed on that look. Today a babysit run sleeps the
interval before every pass, so the first review can start up to one interval
sooner. When that difference changes a line in the bash suite, the test
change goes in the same commit as the code change.

### Passes stay

A pass is still one intake batch in `pass-N` (decision 0009). The exit status
still describes the final pass (decision 0003). A `ReviewNow` for a PR the
pass has not started still goes first, and any other PR goes first in the
next intake, as decision 0020 says. Continuous slots, where a request starts
the moment a slot frees, are out of scope here.

### Exit

The engine never calls `process::exit`. An interrupt, `q`, or the policy
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
- run the preflight and the first selection,
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
3. The loop moves into `src/engine/` with `Policy`. The mode branches and
   `ci::settle` go. Any babysit timing lines change here, with their tests.
4. The engine goes on its own thread, and the view runs on the main thread.
   The fd redirect, `wait`, `while_busy`, `hold` and `Woke` go. The run log
   is written by a sink.
5. Transcript following moves into the view. `ticking()` goes.
6. `Ended` replaces the two `process::exit` calls inside the loop.

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
  The fd redirect caught this, and now nothing does. Step 4 adds a pty test
  that drives a full watch cycle and checks that the screen shows only the
  view.
- These notes stop being true, and change in the step that makes them false:
  - the "fds 1 and 2 point at the run log" convention in `CLAUDE.md`,
  - the "loop keeps the view drawn while it waits" part of decision 0020,
  - the `ui.ticking()` consequence of decision 0020.
- Decisions 0003, 0009, 0010, 0011 and 0020 otherwise still apply. When this
  is accepted, their mentions of `ui` and the binary's loop point here.
