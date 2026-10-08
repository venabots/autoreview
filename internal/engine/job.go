package engine

import (
	"time"

	"github.com/venabots/autoreview/internal/review"
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
)

// Settled says the review has an end state and nothing more will change.
func (s JobState) Settled() bool {
	return s == JobDone || s == JobFailed || s == JobTimedOut
}

// Job is one review as a reader sees it. The engine publishes these in
// each snapshot. Like the snapshot, a published Job is never changed.
type Job struct {
	PR     int
	Title  string
	Author string
	State  JobState
	// Orchestrator is the agent that ran, or runs, the review.
	Orchestrator review.Orchestrator
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
}
