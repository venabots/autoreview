# The TUI is a full-screen view over the same engine

Recorded: 2026-09-17
Status: accepted

## Context

The inline board (decision 0015) shows the reviews that are running. A review
that finishes becomes one line and scrolls away. To learn what it found you
open `pr-N.review.md`, and to talk to it you copy a session id out of the
summary into `claude --resume`. A watch run that lasts all day leaves a
day of results in scrollback, and nothing shows the PRs it is waiting on.

What was wanted was a place to go back to: every PR the run is responsible
for, what each review is doing or found, and a key that reopens a review in a
new terminal tab.

## Decision

`autoreview --tui` draws the run full screen, in `src/tui/`. Without the
flag nothing changes: the inline board, the plain lines and the summary are
as they were.

- **A flag, not a fourth binary.** The view needs everything the babysit and
  watch loop does, and that loop lives in `src/bin/autoreview.rs`. A second
  binary would copy it or force it into the library first. The flag uses the
  same loop, the same flags and the same exit status.
- **The alternate screen, full size.** A full-screen ratatui viewport takes
  its size from `/dev/tty` and redraws on a resize without asking where the
  cursor is, which is the query the inline board is built around.
- **fds 1 and 2 go to the run log while the view is up.** The loop prints a
  line for everything it does, from a dozen places, and one of them landing
  on the screen would scribble over it. With the fds on
  `<run>/autoreview.log`, every existing line goes there unchanged, the plain
  form included, since `ui.tty` is off while the view is up. The view draws
  through its own handle on `/dev/tty`. The saved fds are duplicated
  close-on-exec, so no reviewer inherits the terminal.
- **Nothing asks crossterm for the cursor while the view is up.** The query
  writes to `stdout()`, which is the log file then. So the view never calls
  ratatui's `Terminal::clear`, `init` or `restore`; it enters, clears and
  leaves the alternate screen itself.
- **One row per PR, not one per review.** Sections run top to bottom: running,
  queued, waiting, finished. Under a run that looks again, every PR it still
  watches is listed as waiting, including the ones the next pass will review,
  so a finished row is a PR the run has dropped. Every row carries the newest
  review that finished, whatever its section: under `--watch` a reviewed PR
  spends most of its life resting, and a list that offered its review only
  once the PR was finished for good would hide it where it is wanted.
- **Keys act on the frame the person saw.** Selection follows a PR, not a
  place, because rows move as reviews finish. Stopping a review and quitting
  mid-pass take a second press, and the arming names its PR, so a second `x`
  on another row stops nothing.
- **`r` opens `claude --resume <id>` in a new tab** through the spawner
  review-prs uses, built from the orchestrator's reopen command so it cannot
  drift from the summary's hint. It refuses a review that is running: two
  processes would write one transcript.
- **A stopped review is a failure.** It ends with the outcome "stopped", the
  fallback never retries it, the run exits 1, and the next pass reviews the
  PR from scratch.
- **The view opens whenever there is a terminal.** A run with nothing to
  review used to print one line and exit; asking for a screen and getting
  that is the wrong shape. The list holds every open PR, not only the ones
  the run is responsible for, each saying why it is being left alone. A
  quiet repo is the state the view is most worth opening in.
- **`R` asks for a review now.** A queued review starts next. Any other PR
  goes first in the next intake, past the rest, the cap and the sweep's own
  view: those bound a loop nobody watches, and a key press is somebody
  watching. A one-shot run whose pass has ended starts another, so one
  screen can work through a repo by hand. Only a review already running
  refuses, and, under a run that looks again, a PR it has dropped. A request
  the queue refuses is said.
- **A request wakes the loop's wait, and brings only itself.** When
  `--babysit` waits out its interval before a pass, a request runs a pass of
  that PR alone; the others keep their interval, because it is what gives
  their authors time to answer. The waits after a failure do not wake: a key
  press there would repeat the call that just failed. A wake a request
  caused is not an idle check.
- **`w` starts and stops the run's own looking for work.** The mode was a
  startup choice, and a screen is where a person changes their mind: they
  open one pass, see a PR worth watching for, and want the run to stay. The
  key asks one question -- keep looking for work? -- so a `--babysit` run
  turned off and on comes back watching, which is the answer that suits
  somebody sitting in front of it. The toggle is applied only where the loop
  decides what it does next, never mid-pass, and a wait it interrupts leaves
  the loop to decide again with nothing queued.
- **The loop keeps the view drawn while it waits.** Its sleeps and its `gh`
  calls run in tenths of a second, drawing and reading keys; the calls run on
  a scoped thread.
- **A run that ends keeps the view until `q`,** saying why it ended, and
  exits with the status it would have had. The summary printed after it is
  one table for the whole run, the newest review of each PR.

## Consequences

- The plain output contract is untouched. The view's own words are not a
  contract in the same way; `tests/tui.test.sh` pins the ones that matter by
  driving it on a pty and reading the run log.
- A tab opened with `r` holds the session. A later pass sees it in use and
  reviews that PR fresh, as it does for any session open elsewhere.
- The pass asks two questions where it asked one: `ui.ticking()` is whether
  it animates and follows activity, `ui.tty` whether output is styled for a
  terminal. The view needs the first without the second.
- `Job` and `Tail` are `Clone`, for the summary at the end. An archived
  review drops its activity, so a day of watching does not keep every event.
- The detail pane uses ratatui's `unstable-rendered-line-info` feature to
  count wrapped lines. An upgrade of ratatui has to check it still exists.
- The picker, the CI wait and the startup lines all run before the view
  opens, on the normal screen, and stay above the summary when it closes.
  That includes the sweep's own "no NEW or UPDATED PRs to review", which a
  quiet run still prints before the screen takes over.
- A `--tui` run with nothing to review still makes a run directory and
  stages its skills, because `R` may ask for a review at any moment. Without
  the flag such a run still exits before either.
- The intervals `w` turns on are resolved when the flags are parsed, and
  leniently: a bad `$AUTOREVIEW_WATCH_INTERVAL` still refuses a `--watch`
  run, but it must not refuse a run that only might become one.
