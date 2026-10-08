# Rules that carry over from v1

v1 is the Rust repo `venabots/autoreview`. It has 30 decision files. v2 does
not copy them. It keeps the contracts and rules below, and each one gets a
test when its milestone lands.

Status is `done` with the test that pins it, or the milestone that brings it.

## Byte-exact contracts

| Contract                                                                                       | v1 source              | Status                                             |
| ---------------------------------------------------------------------------------------------- | ---------------------- | -------------------------------------------------- |
| The PR list query has the same fields as v1                                                    | `src/prlist.rs`        | done: `internal/prs` `TestFetchCallsGhWithV1Argv`                |
| The My PRs search has the same fields as v1                                                    | `src/mine.rs`          | done: `internal/mine` `TestListSendsTheSearchToGh` |
| Session id: MD5 of `review-prs:<root>:<owner>/<name>#<n>`, shaped as a UUID. The prefix stays. | `src/session.rs:19-30` | milestone 3                                        |
| The repo root loses one trailing newline and nothing else                                      | `src/repo.rs`          | done: `internal/repo` `TestRootFrom`               |
| Ledger: v1 JSONL. Write `[]`, never `null`. `reviewed` defaults to true.                       | `src/ledger.rs`        | done: `internal/ledger` `TestALineV1WroteIsWrittenBackWithTheSameBytes`, and `TestEveryFieldIsWrittenInV1Order` |
| `dash-p` argv order, exit codes 0, 10, 20, and `--flag=value`                                  | `src/job.rs:264-339`   | milestone 3                                        |
| The trailer: a fenced `autoreview` JSON block                                                  | `src/report.rs:84`     | milestone 3                                        |
| The run directory layout                                                                       | `src/rundir.rs`        | milestone 3                                        |
| `skills/` and `prompts/`, with mode 0755 on `*.sh` after staging                               | `src/skills.rs`        | milestone 3                                        |

## Behavior rules

| Rule                                                                                                                         | Status                                                                                                  |
| ---------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------- |
| A PR title, a branch and a `gh` error are untrusted. Control and bidi characters are stripped before display.                | done: `internal/sanitize`, `internal/tui` `TestOtherPeoplesTextCannotDrawOnTheScreen`, `internal/plain` |
| UPDATED needs somebody else's event to be strictly newer than yours                                                          | done: `internal/prs`                                                                                    |
| An unknown CI state is "no checks", which is ready                                                                           | done: `internal/prs`                                                                                    |
| Stacks are worked out over every open PR, before any filter                                                                  | done: `internal/prs`                                                                                    |
| A failed look is not an empty list                                                                                           | done: `internal/prs`, `internal/mine`, `internal/engine` `TestAFailedLookKeepsWhatTheLastGoodOneFound`  |
| An empty `gh api user` login is refused                                                                                      | done: `internal/repo` `TestLoadSaysWhatIsWrong`                                                         |
| A `gh` call has a deadline, and the deadline kills its process group                                                         | done: `internal/gh` `TestADeadlineStopsGhAndWhatItStarted`                                              |
| The selection follows the PR number, not the row                                                                             | done: `internal/tui` `TestTheSelectionStaysOnItsPRWhenTheRowsMove`                                      |
| The view closes only after the engine says the run has ended                                                                 | done: `internal/tui` `TestQuitAsksTheEngineAndClosesWhenTheRunHasEnded`                                 |
| The verdict comes from GitHub `latestReviews`. The trailer is a fallback. A claim of approval is vetoed when nothing landed. | milestone 3                                                                                             |
| "The call failed" and "nothing landed" stay distinct                                                                         | milestone 3                                                                                             |
| One process group for each job. Stop is TERM, TERM, KILL.                                                                    | milestone 3                                                                                             |
| A killed job is never read as an outage and retried                                                                          | milestone 6                                                                                             |
| On quit: TERM to every group, then reactions, then worktrees, then print                                                     | milestones 3, 6, 7                                                                                      |
| "Reaped" is handled before the readback, and the slot is free then                                                           | milestone 3                                                                                             |
| A session is never pinned when its transcript exists                                                                         | milestone 3                                                                                             |
| A session is never resumed when it is open elsewhere                                                                         | milestone 6                                                                                             |
| A failed pass forces a fresh review                                                                                          | milestone 3                                                                                             |
| CI pending or failing holds a PR. A stack hold never keeps a loop alive.                                                     | milestone 5                                                                                             |
| A PR that GitHub called finished stays finished for the run                                                                  | milestone 5                                                                                             |
| Only a new head SHA resets the pass cap                                                                                      | milestone 5                                                                                             |
| The reaction is removed on every end state. Removal waits for the add.                                                       | milestone 6                                                                                             |
| Exit status 0 only when every job in the last pass ended done                                                                | milestone 3                                                                                             |
| A task runs in its own worktree and refuses a fork                                                                           | milestone 7                                                                                             |
| Babysitting follows v1 decision 0030 (`src/babysit.rs`)                                                                      | milestone 7                                                                                             |

## Go safeguards that v1 did not need

| Safeguard                                                     | Status                                                                                  |
| ------------------------------------------------------------- | --------------------------------------------------------------------------------------- |
| SIGPIPE is ignored, so a closed pipe does not end the process | done: `cmd/autoreview/main.go`                                                          |
| SIGINT, SIGTERM and SIGHUP all become one `Quit`              | done: `cmd/autoreview/main.go`                                                          |
| A panic in a look becomes that look's error                   | done: `internal/engine` `TestALookThatPanicsAnswersWithAnError`                         |
| A panic on the engine goroutine gives the terminal back first | done: `cmd/autoreview/screen.go`                                                        |
| The engine never blocks on a reader                           | done: `internal/engine` `TestChangedHoldsOneTokenHoweverManySnapshots`                  |
| Unread input is discarded when the view closes                | done: `cmd/autoreview/flush.go` (no automated test yet; milestone 4 adds the pty cases) |
| Nothing prints outside `cmd/`                                 | done: `forbidigo` in `.golangci.yml`                                                    |
