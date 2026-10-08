# CRITICAL: read first

- The engine and the view are separate. `internal/engine` imports no Charm
  package and writes nothing to the terminal. `internal/tui` runs no process
  and calls no `gh`. The linter enforces both.
- Run `go test -race ./...` and `golangci-lint run` before you call work
  done.
- v1 is the specification for behavior. It is the Rust repo
  `venabots/autoreview`. `docs/rules.md` lists the rules that carry over.
  Each rule has a test.
- This build is read-only. It reads the two PR lists and starts no review.
  `docs/plan.md` has the milestones.

## Commands

```sh
go build -o autoreview ./cmd/autoreview   # the one binary
go test -race ./...                       # every test
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
- The end-to-end tests in `cmd/autoreview` link the test binary as `gh`. A
  new `gh` call needs an answer in `fakeGh` before its test can pass.

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
- Engine tests run in `testing/synctest`, which gives virtual time. Do not
  add a clock interface.
- Commit messages are conventional (`feat:`, `fix:`, `docs:`, `test:`,
  `chore:`), one logical change each, in plain words.

## Working here

- To see the view without a person, run it in `tmux` and read the pane:
  `tmux new-session -d -x 120 -y 30 ./autoreview`, then
  `tmux capture-pane -p`.
- Do not run a build that posts against a real repo to test it. Use the
  fakes. The owner starts the first posting run.
- The argument parser is `pflag`. A flag with an optional value takes it
  with `=` only (`--watch=5`). `refuseSpacedValue` catches `--watch 5`.

# CRITICAL: read last

- The engine never draws, and the view never runs a process.
- Run `go test -race ./...` and `golangci-lint run` before you call work
  done.
- v1 is the specification. A rule from `docs/rules.md` needs a test.
