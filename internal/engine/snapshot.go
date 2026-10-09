package engine

import (
	"slices"
	"time"

	"github.com/venabots/autoreview/internal/mine"
	"github.com/venabots/autoreview/internal/queue"
	"github.com/venabots/autoreview/internal/repo"
)

// Phase is what the run is doing with reviews. Reading the PR lists is a
// separate matter: a run reads them while it reviews, and between reviews.
type Phase int

const (
	// Idle means no review is running. The run has not started one yet,
	// or the last pass has ended and the run stays for the person.
	Idle Phase = iota
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
	// Looking is true while the run reads the PR lists from GitHub.
	Looking bool
	// NextLookAt is when the next look starts. It is zero while the run is
	// looking, and in a run that looks once.
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
	// Jobs is every review of the newest pass, in the order they start.
	Jobs []Job
	// Earlier is the newest review of each PR from the passes before it.
	Earlier []Job
	// Focus is what the reviewers are told to look at, or empty.
	Focus string
	// Watching says the run reviews what becomes ready at each look.
	// Rest is how long a reviewed PR waits before it may be reviewed again.
	Watching bool
	Rest     time.Duration
	// At is when the snapshot was published: the time a wait is measured
	// from.
	At time.Time
	// RunDir is where the run writes its files, or empty for a run that
	// only looks.
	RunDir string
}

// Reviewed is the newest review of each PR in the whole run: the reviews
// kept from earlier passes, then the newest pass.
func (s *Snapshot) Reviewed() []Job {
	out := []Job{}
	for _, old := range s.Earlier {
		if !slices.ContainsFunc(s.Jobs, func(j Job) bool { return j.PR == old.PR }) {
			out = append(out, old)
		}
	}
	return append(out, s.Jobs...)
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
