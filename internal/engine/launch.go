package engine

import (
	"fmt"

	"github.com/venabots/autoreview/internal/proc"
	"github.com/venabots/autoreview/internal/review"
	"github.com/venabots/autoreview/internal/sanitize"
)

// launch starts a review's process, once the skills it reads are in place.
//
// A reviewer that does not find its skill does not stop. It reviews by its
// own lights, posts nothing, and the run counts a review with no verdict
// and runs it again until the PR is capped. A review whose skills cannot
// be staged is refused here instead, with the reason, and no reviewer
// starts.
func (e *Engine) launch(request review.Request, spec review.Spec) (proc.Proc, error) {
	if e.deps.Restage != nil && request.ReadsStagedSkills() {
		if err := e.deps.Restage(); err != nil {
			return nil, fmt.Errorf("staging the review skills again: %w", err)
		}
	}
	return e.deps.Procs.Start(proc.Command{
		Path:   spec.Path,
		Args:   spec.Args,
		Env:    spec.Env,
		Dir:    spec.Dir,
		Stdout: spec.Stdout,
		Stderr: spec.Stderr,
	})
}

// notStarted fails the review at i, whose process never started. It counts
// against the PR's cap like any review that ended, and the other reviews
// carry on.
func (e *Engine) notStarted(r run, i int, j job, err error) run {
	j.view.State = JobFailed
	j.view.Outcome = review.OutcomeWord(review.Exit{Code: exitNotStarted}, false)
	j.view.Error = sanitize.Line(err.Error())
	e.journal(JobFinished{Job: j.view})
	e.clearMark(j.view.PR)
	return e.counted(withJob(r, i, j), j)
}
