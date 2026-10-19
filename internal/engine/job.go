package engine

import (
	"time"

	"github.com/venabots/autoreview/internal/review"
	"github.com/venabots/autoreview/internal/task"
)

// JobState is where one review stands.
type JobState int

const (
	// JobQueued means the review waits for a free slot.
	JobQueued JobState = iota
	// JobRunning means its process is alive.
	JobRunning
	// JobFinishing means its process has ended and its slot is free. What it
	// left behind is being read, and GitHub is being asked what landed.
	JobFinishing
	// JobDone means the review ran to its end. It says nothing about the
	// verdict: a review that requested changes is done.
	JobDone
	// JobFailed means the review did not run to its end.
	JobFailed
	// JobTimedOut means the review ran past its time limit and was stopped.
	JobTimedOut
	// JobPreparing means a task's worktree is being made.
	JobPreparing
)

// Settled says the review has an end state and nothing more will change.
func (s JobState) Settled() bool {
	return s == JobDone || s == JobFailed || s == JobTimedOut
}

// Job is one review as a reader sees it. The engine publishes these in
// each snapshot. Like the snapshot, a published Job is never changed.
type Job struct {
	PR int
	// Task is the work: a review, or a task on one of your own PRs.
	Task   task.Kind
	Title  string
	Author string
	State  JobState
	// Orchestrator is the agent that ran, or runs, the review.
	Orchestrator review.Orchestrator
	// Verdicts is which events the review was allowed to submit.
	Verdicts review.Verdicts
	// Resumed is true for a second look in an earlier session.
	Resumed bool
	// StartedAt is zero until the review starts.
	StartedAt time.Time
	// Elapsed is how long the process ran. It is set when the process ends.
	Elapsed time.Duration
	// Outcome says how a failed review ended: "exit 10", "no result" or
	// "stopped".
	Outcome string
	// Error is why the harness said the review failed, or empty.
	Error string
	// Verdict is what landed on the PR: "approved", "changes requested" or
	// "commented". Empty means nothing was posted.
	Verdict string
	// Trailer is what the reviewer reported about its own review, or nil.
	Trailer *review.Trailer
	// Cost is what the review spent, or nil when the harness did not say.
	Cost *float64
	// Model is the model the harness ran, or empty.
	Model string
	// Session is the session the review ran in, or empty when it is not
	// known. It is what a person resumes.
	Session string
	// PassDir holds the files the review wrote.
	PassDir string
	// First is the attempt that failed before the fallback took over, or
	// nil when this is the first attempt.
	First *Attempt
	// Follow is the session whose transcript shows what the review is
	// doing, or empty when none is known while it runs. FollowFromEnd says
	// the session is resumed: what it held before belongs to earlier
	// reviews.
	Follow        string
	FollowFromEnd bool
	// Worktree is where a task runs. Kept says why it was kept after the
	// task, "uncommitted changes" or "commits that are not pushed", or is
	// empty when it was removed. Yours says Worktree is a worktree of
	// yours that the task borrowed: it stays after the task even when Kept
	// is empty, so the task can be reopened in it.
	Worktree string
	Kept     string
	Yours    bool
	// Reopening says the task's worktree, which was removed, is being made
	// again so its session can be resumed. Reopens counts the tries that
	// ended, so a reader can tell a new answer from an old one, and
	// ReopenError says why the last one failed, or is empty.
	Reopening   bool
	Reopens     int
	ReopenError string
	// Stderr is the file the reviewer writes its stderr to. FollowStderr
	// says it is the only live record: an override keeps no transcript.
	Stderr       string
	FollowStderr bool
}

// Attempt is what is kept of an orchestrator that failed with exit 10
// before the fallback took over: enough to say what happened.
type Attempt struct {
	Orchestrator review.Orchestrator
	Outcome      string
	// Error is why the harness said it failed: a usage limit, an API error.
	// The exit status says a provider failed; this says which way.
	Error   string
	Elapsed time.Duration
}

// Shortfall is a clean review that too few of the panel answered, or false.
// Under comments only no review approves, so a short panel is not why this
// one did not, and saying so would send the person to look for more
// reviewers.
func (j Job) Shortfall() (review.Shortfall, bool) {
	if !j.Verdicts.Approves() {
		return review.Shortfall{}, false
	}
	return review.CleanButShort(j.Verdict, j.Trailer)
}

// ReasonsHeader opens the list of the blockers the review named. Under
// comments only nothing is approved, so the list is only what it found.
func (j Job) ReasonsHeader() string {
	if !j.Verdicts.Approves() {
		return "the review found:"
	}
	return review.ReasonsHeader
}
