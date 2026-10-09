package panel

import (
	"context"
	"time"

	"github.com/venabots/autoreview/internal/proc"
)

// The reasons a run gives for a process it ended itself.
const (
	stoppedInterrupted = "interrupted"
	stoppedTimedOut    = "timed out and had to be killed"
)

// ended is how a supervised process finished.
type ended struct {
	exit proc.Exit
	// stopped is why the run ended the process, or "" when it ended by
	// itself.
	stopped string
}

// supervise waits for p, and stops its group at the deadline or when ctx
// ends.
//
// dash-p enforces the real timeout. The deadline covers its one known gap,
// a wait without limit after the agent closes stdout and does not exit:
// without it, one wedged panelist would hang the run for as long as the
// terminal stays open, with every worktree still registered.
//
// A process that has already ended when ctx ends keeps its own exit: one
// that wrote a whole report must not be marked interrupted, which would hide
// its exit code and print FAILED over a review that succeeded.
func supervise(ctx context.Context, p proc.Proc, limit time.Duration) ended {
	exited := make(chan proc.Exit, 1)
	go func() { exited <- p.Wait() }()
	timer := time.NewTimer(limit)
	defer timer.Stop()
	select {
	case exit := <-exited:
		return ended{exit: exit}
	case <-timer.C:
		p.Stop()
		return ended{exit: <-exited, stopped: stoppedTimedOut}
	case <-ctx.Done():
		select {
		case exit := <-exited:
			return ended{exit: exit}
		default:
		}
		p.Stop()
		return ended{exit: <-exited, stopped: stoppedInterrupted}
	}
}
