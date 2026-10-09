# CRITICAL: read first

- The engine and the view are separate. `internal/engine` imports no Charm
  package and writes nothing to the terminal. `internal/tui` runs no process
  and calls no `gh`. The linter enforces both.
- Run `go test -race $(go list ./... | grep -v /cmd/)`, `go test ./cmd/...`
  and `golangci-lint run` before you call work done.
- v1 is the specification for behavior. It is the Rust repo
  `venabots/autoreview`. `docs/rules.md` lists the rules that carry over.
  Each rule has a test.
- The binary reviews and posts to GitHub, in the view and off a terminal.
  Never run it in a real checkout to try something: run
  `scripts/sandbox.sh`. `docs/plan.md` has the milestones.

## Commands

```sh
go build -o autoreview ./cmd/autoreview   # the one binary
go test -race $(go list ./... | grep -v /cmd/)   # the unit tests, raced
go test ./cmd/...                         # end to end; two minutes under -race
go test ./internal/tui/                   # one package
golangci-lint run                         # lint; clean today, keep it clean
golangci-lint fmt                         # gofmt and goimports
```

## Facts

- One Go module, `github.com/venabots/autoreview`, and one binary.
  `go.mod` says Go 1.26, the floor that Bubble Tea v2 sets.
- The view is Bubble Tea v2, Bubbles v2 and Lip Gloss v2, under the
  `charm.land/.../v2` import paths.
- One goroutine owns a run (`internal/engine`). It reads one inbox and
  publishes an immutable `Snapshot` after each change.
- The view waits on `Engine.Changed()` in a command and asks again after each
  snapshot. It sends commands with `Engine.Do`, which never blocks.
- `cmd/autoreview` prints the result after the alternate screen closes. On a
  pipe, `internal/plain` prints the same facts as plain lines.
- Every `gh` call goes through a `gh.Runner`, so a test hands in a function.
- Every reviewer starts through a `proc.Starter`. Engine tests hand in a fake
  whose reviews end when the test says so.
- A review's monitor goroutine reads files and asks GitHub. The engine's own
  goroutine does neither, except for the ledger append.
- The ledger is shared with v1, which still appends to it. `internal/ledger`
  writes v1's bytes exactly: call `Record.MarshalJSON()`, never
  `json.Marshal`, which escapes `<`, `>` and `&`.
- Four tests check v2 against v1 on a real ledger. They are skipped unless
  `AUTOREVIEW_LEDGER_ROUNDTRIP`, `AUTOREVIEW_STATS_ORACLE` (a copy of a
  ledger file) and `AUTOREVIEW_V1` (the v1 binary) are set. Run them after a
  change to `internal/ledger` or `internal/stats`, and never put a line of a
  real ledger in the repo.
- The end-to-end tests in `cmd/autoreview` link the test binary as `gh`,
  `dash-p`, `claude` and `codex` (`fakes_test.go`). A new `gh` call needs an
  answer in `fakeGh` before its test can pass. The panel tests add
  `opencode`, and the fake dash-p tells a panelist from a review by
  `--output-format=text` (`panel_fakes_test.go`).

## Conventions

- Do not change an argument. Return a new value. Tests check this for the
  functions that take a slice.
- Keep functions small and files under 400 lines. Split by concern.
- A doc comment states the failure that the code prevents, in prose.
- The words for a PR's state are in `internal/words`. The view adds the
  color. The view and the plain printer must use the same words.
- Count in English through `words.Count`. "1 PR(s)" is the shape to avoid.
- A PR title, a branch name and an error from `gh` are other people's text.
  Pass each one through `sanitize.Line` or `sanitize.Block` before it reaches
  the terminal.
- Nothing calls `fmt.Print*` outside `cmd/`. Write to a writer that the
  caller hands in.
- Do not put an invisible character in source. Write the escape, for example `\u202E`.
- Tests use the standard library only, in table form where cases repeat. A
  test name is a sentence that states the rule.
- View tests call `Update` and `View` directly and assert on the frame with
  the escape sequences stripped.
- The view runs no process and reads no clock of its own. A browser or a
  terminal tab is an `Actions` function, and time arrives in a tick.
- A test for color or highlight reads the escape sequences instead
  (`internal/tui/style_test.go`). Text that reads right can still be drawn in
  colors nobody can read.
- Engine tests run in `testing/synctest`, which gives virtual time. Do not
  add a clock interface.
- Commit messages are conventional (`feat:`, `fix:`, `docs:`, `test:`,
  `chore:`), one logical change each, in plain words.

## Working here

- CI runs the Go version in `go.mod`, which is older than the one on this
  machine. Run `GOTOOLCHAIN=go1.26.0 go test ./...` before a push. A test
  must not pin what the standard library does where releases differ.
- To try the binary without a real review, run `scripts/sandbox.sh`. It
  serves a repo of seven PRs from a fake `gh` and reviews them with a fake
  `dash-p`: the test binary under those names. Nothing reaches GitHub. To
  see the view without a person, run the script in `tmux` and read the pane
  with `tmux capture-pane -p`.
- The pty tests build the binary and run it under a real pty. They look only
  at what is outside the frame. What a frame says is tested on the model.
- Do not run a build that posts against a real repo to test it. Use the
  fakes. The owner starts the first posting run.
- The argument parser is `pflag`. A flag with an optional value takes it
  with `=` only (`--watch=5`). `refuseSpacedValue` catches `--watch 5`.

# CRITICAL: read last

- The engine never draws, and the view never runs a process.
- Run both test commands and `golangci-lint run` before you call work done.
- v1 is the specification. A rule from `docs/rules.md` needs a test.
