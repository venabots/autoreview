package engine

// Event is one thing the run did, for a reader who follows along: the
// plain printer, and the run's own log. Events are a journal. Nothing
// rebuilds the run's state from them; the snapshot is the state.
type Event interface{ event() }

// Looked says the run has read the PR lists for the first time.
type Looked struct {
	Snapshot *Snapshot
}

// NothingToReview says the look found no PR that is ready.
type NothingToReview struct{}

// PassStarted says a pass has begun.
type PassStarted struct {
	Pass int
	// Reviews is how many PRs the pass reviews.
	Reviews int
	// Limit is how many run at once.
	Limit int
	// Dir is where the pass writes its files.
	Dir string
}

// JobStarted says a review's process is running.
type JobStarted struct {
	Job Job
}

// JobFinished says a review has its end state.
type JobFinished struct {
	Job Job
}

// Noted is something a person should know that is not a result: a session
// that was open elsewhere, a claim that GitHub contradicted.
type Noted struct {
	Text string
}

// Interrupted says the run was told to stop while reviews were running.
type Interrupted struct{}

func (Looked) event()          {}
func (NothingToReview) event() {}
func (PassStarted) event()     {}
func (JobStarted) event()      {}
func (JobFinished) event()     {}
func (Noted) event()           {}
func (Interrupted) event()     {}
