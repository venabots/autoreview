package engine

import (
	"time"

	"github.com/venabots/autoreview/internal/mine"
	"github.com/venabots/autoreview/internal/queue"
	"github.com/venabots/autoreview/internal/repo"
)

// Phase is what the run is doing.
type Phase int

const (
	// Looking means the run is reading the PR lists from GitHub.
	Looking Phase = iota
	// Waiting means the run is idle until the next look.
	Waiting
	// Reviewing means reviews are running, or waiting for a slot.
	Reviewing
	// Stopping means the run was told to end, and waits for the reviews it
	// stopped to be collected.
	Stopping
	// Done means the run has ended and Run has returned, or is about to.
	Done
)

// Snapshot is the whole state of a run at one moment. The engine publishes
// a new one after every change and never changes one it has published, so a
// reader may keep it and read it from any goroutine.
type Snapshot struct {
	// Seq goes up by one with every snapshot.
	Seq   uint64
	Phase Phase
	Repo  repo.Context
	// Watch is the wait between looks. Zero means the run looks once.
	Watch time.Duration
	// NextLookAt is when the next look starts. It is zero unless Waiting.
	NextLookAt time.Time

	// Looked is false until a review list has arrived. Until then an empty
	// Reviews means "not read yet", not "no PRs".
	Looked  bool
	Reviews []queue.Row
	// Truncated means GitHub had more open PRs than one look reads.
	Truncated bool
	// ListErr is why the last look at the review list failed, or empty.
	// Reviews keeps what the last good look found.
	ListErr string

	// MineLooked, Mine and MineErr are the same three for your own PRs.
	MineLooked bool
	Mine       []mine.PR
	MineErr    string

	// Pass counts the passes the run has started. It is zero until the
	// first one.
	Pass int
	// Jobs is every review of the pass, in the order they start.
	Jobs []Job
	// RunDir is where the run writes its files, or empty for a run that
	// only looks.
	RunDir string
}

// Result is how a run ended.
type Result struct {
	// Code is the exit status the process ends with.
	Code int
	// Final is the last snapshot the run published.
	Final *Snapshot
}

// The exit statuses a run ends with.
const (
	// ExitOK means the run did what it was asked.
	ExitOK = 0
	// ExitFailed means something the run was asked to do did not happen.
	ExitFailed = 1
	// ExitInterrupted means a person or a signal stopped a run that would
	// have gone on.
	ExitInterrupted = 130
)
