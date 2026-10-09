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
| Session id: MD5 of `review-prs:<root>:<owner>/<name>#<n>`, shaped as a UUID. The prefix stays. | `src/session.rs:19-30` | done: `internal/session` `TestIDIsWhatV1FiledReviewsUnder` |
| The repo root loses one trailing newline and nothing else                                      | `src/repo.rs`          | done: `internal/repo` `TestRootFrom`               |
| Ledger: v1 JSONL. Write `[]`, never `null`. `reviewed` defaults to true.                       | `src/ledger.rs`        | done: `internal/ledger` `TestALineV1WroteIsWrittenBackWithTheSameBytes`, and `TestEveryFieldIsWrittenInV1Order` |
| `dash-p` argv order, exit codes 0, 10, 20, and `--flag=value`                                  | `src/job.rs:264-339`   | done: `internal/review` `TestTheDashpArgvIsV1s`, `TestClassifySaysWhatAnExitMeans`, `TestAFlagDashpDoesNotKnowIsOneToken` |
| The trailer: a fenced `autoreview` JSON block                                                  | `src/report.rs:84`     | done: `internal/review` `TestTheInstructionIsV1sByteForByte`, `TestTheLastBlockThatParsesWins` |
| The run directory layout                                                                       | `src/rundir.rs`        | done: `internal/rundir` `TestThePathsOfAPass` |
| `skills/` and `prompts/`, with mode 0755 on `*.sh` after staging                               | `src/skills.rs`        | done for `skills/`: `internal/skills` `TestStageWritesTheBundleWhereTheAgentLooks`, and `embed_test.go`. `prompts/` comes with `panel`. |

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
| Stopping a review and quitting with reviews alive need a second press, and the first press names its PR | done: `internal/tui` `TestXStopsARunningReviewOnTheSecondPress`, `TestASecondXOnAnotherRowOrTooLateStopsNothing`, `TestQStopsRunningReviewsOnlyOnTheSecondPress` |
| The terminal is given back cooked, and the summary is printed after the view closes | done: `cmd/autoreview` `TestOnATerminalTheViewOpensAndQGivesTheTerminalBack` |
| Quit while the run is held ends with the pass's status. Quit during a pass is 130. | done: `internal/engine` `TestAHeldRunStaysOpenAfterItsPassAndEndsWithItsStatus`, `cmd/autoreview` `TestQTwiceStopsARunningReviewAndExits130` |
| A focus the flag would refuse cannot reach a prompt from the view | done: `internal/tui` `TestAFocusAPromptCannotCarryKeepsTheEditorOpen` |
| The verdict comes from GitHub `latestReviews`. The trailer is a fallback. A claim of approval is vetoed when nothing landed. | done: `internal/review` `TestTheVerdictPrefersGitHubToTheAgent`, `internal/engine` `TestTheVerdictIsGitHubsAndNotTheReviewersOwn` |
| "The call failed" and "nothing landed" stay distinct                                                                         | done: `internal/review` `TestACallThatFailedAndNothingLandedStayDistinct` |
| One process group for each job. Stop is TERM, TERM, KILL.                                                                    | done: `internal/proc` `TestEachProcessLeadsItsOwnGroup`, `TestStopEndsAGrandchildThatIgnoresTERM` |
| A killed job is never read as an outage and retried                                                                          | done: `internal/engine` `TestOnlyAFailedAgentIsRetried` |
| Exit 10 is retried once under the fallback, as a fresh review, ahead of the reviews that wait. The failed attempt's files are set aside. | done: `internal/engine` `TestExit10IsRetriedOnceUnderTheFallback`, `TestARetryGoesAheadOfTheReviewsThatWait`; `cmd/autoreview` `TestAReviewWhoseAgentFailsIsRetriedUnderTheOther` |
| On quit: TERM to every group, then reactions, then worktrees, then print                                                     | done for the kills and the reactions: `internal/engine` `TestQuitStopsTheRunningReviewsAndNeverStartsTheQueued`, `TestQuitStopsTheReviewsThenTakesTheMarksOff`. Worktrees are milestone 7. |
| "Reaped" is handled before the readback, and the slot is free then                                                           | done: `internal/engine` `TestASlotIsFreeWhenAProcessEndsNotWhenItsResultIsRead` |
| A session is never pinned when its transcript exists                                                                         | done: `internal/session` `TestPlanFor`, `internal/review` `TestAtMostOneSessionFlagIsSentAndOnlyToClaude` |
| A session is never resumed when it is open elsewhere                                                                         | done: `internal/session` `TestPlanFor`, `cmd/autoreview` `TestContinueResumesTheEarlierSession` |
| A failed pass forces a fresh review                                                                                          | done: `internal/engine` `TestAReviewAfterAFailedOneStartsFresh` |
| CI pending or failing holds a PR. A stack hold never keeps a loop alive.                                                     | done: `internal/engine` `TestABabysitRunStaysForChecksThatAreRunning`, `TestARunThatLooksOnceWaitsForChecksThatAreRunning`, `TestFailingChecksAreNotWaitedFor`; `heldByChecks` |
| A PR that GitHub called finished stays finished for the run                                                                  | done: `internal/queue` `TestAPRThisRunApprovedStaysFinished`, `internal/engine` `TestAPRTheRunApprovedIsNotReviewedAgain`. A closed PR leaves the list. No call for each PR is needed. |
| Only a new head SHA resets the pass cap                                                                                      | done: `internal/queue` `TestOnlyANewHeadStartsTheCountAgain` |
| A later pass re-checks a PR in the session of its last review | done: `internal/engine` `TestAReviewedPRRestsAndIsThenRecheckedInItsSession` |
| A run that babysits ends when nothing is left, after too many idle looks, or after three failed looks. The look straight after a pass is not idle. | done: `internal/engine` `TestABabysitRunEndsWhenEveryPRItReviewedIsDone`, `TestABabysitRunEndsAfterTooManyIdleLooks`, `TestABabysitRunGivesUpAfterThreeFailedLooks` |
| A run that watches never gives up on failed looks | done: `internal/engine` `TestAWatchingRunKeepsLookingThroughFailedLooks` |
| The reaction is removed on every end state. Removal waits for the add.                                                       | done: `internal/marks` `TestTheRemovalWaitsForTheAddToLand`, `internal/engine` `TestAReviewMarksItsPRWhileItRuns`, `TestQuitStopsTheReviewsThenTakesTheMarksOff` |
| Exit status 0 only when every job in the last pass ended done                                                                | done: `internal/engine` `TestAFailedReviewFailsTheRunAndSaysWhy`, `TestAPassReviewsEveryReadyPRAndEndsClean` |
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
| Unread input is discarded when the view closes                | done: `cmd/autoreview` `TestCtrlCQuitsAtOnceAndLeavesNoInputForTheShell` |
| The plain printer never makes the engine wait | done: `internal/plain` `TestThePrinterNeverMakesTheEngineWait` |
| Nothing prints outside `cmd/`                                 | done: `forbidigo` in `.golangci.yml`                                                    |
