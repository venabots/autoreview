package engine

import (
	"fmt"
	"slices"
	"time"

	"github.com/venabots/autoreview/internal/proc"
	"github.com/venabots/autoreview/internal/queue"
	"github.com/venabots/autoreview/internal/review"
	"github.com/venabots/autoreview/internal/sanitize"
	"github.com/venabots/autoreview/internal/session"
)

// exitNotStarted is the status of a review whose process never started. It
// is the shell's "command not found".
const exitNotStarted = 127

// job is one review as the engine holds it: what a reader sees, and what
// only the engine needs to supervise it.
type job struct {
	view Job
	plan session.Plan
	spec review.Spec
	proc proc.Proc
	// guard fires when the review has run past the time its harness should
	// have stopped it at.
	guard *time.Timer
	// guardTripped is true once the engine had to stop the review for
	// time. It then timed out, whatever status the stop produced.
	guardTripped bool
	// stopped is true once the run was told to end with this review alive.
	stopped bool
	exit    review.Exit
}

func views(jobs []job) []Job {
	out := make([]Job, len(jobs))
	for i, j := range jobs {
		out[i] = j.view
	}
	return out
}

// withJob is the run with one job replaced. The slice is copied: a
// snapshot already published must not see the change.
func withJob(r run, i int, j job) run {
	jobs := slices.Clone(r.jobs)
	jobs[i] = j
	r.jobs = jobs
	return r
}

func indexOf(jobs []job, pr int) int {
	return slices.IndexFunc(jobs, func(j job) bool { return j.view.PR == pr })
}

// startPass begins a pass over every PR that is ready. A look that found
// none ends the run: there is nothing to wait for.
func (e *Engine) startPass(w workers, r run) run {
	ready := []queue.Row{}
	for _, row := range r.snap.Reviews {
		if row.Wait == queue.WaitNext {
			ready = append(ready, row)
		}
	}
	if len(ready) == 0 {
		e.journal(NothingToReview{})
		r.snap.Phase = Done
		r.code = ExitOK
		return r
	}
	pass, err := e.deps.Run.Pass(r.snap.Pass + 1)
	if err != nil {
		e.journal(Noted{Text: "could not make a directory for the pass: " + err.Error()})
		r.snap.Phase = Done
		r.code = ExitFailed
		return r
	}
	r.pass = pass
	r.snap.Pass++
	r.snap.Phase = Reviewing
	r.jobs = make([]job, len(ready))
	for i, row := range ready {
		r.jobs[i] = job{view: Job{
			PR:           row.PR.Number,
			Title:        row.PR.Title,
			Author:       row.PR.Author,
			State:        JobQueued,
			Orchestrator: e.cfg.Orchestrator,
			PassDir:      pass.Dir,
		}}
	}
	e.journal(PassStarted{Pass: r.snap.Pass, Reviews: len(ready), Limit: e.cfg.Jobs, Dir: pass.Dir})
	return e.fill(w, r)
}

// fill starts queued reviews until every slot is taken.
func (e *Engine) fill(w workers, r run) run {
	for i := range r.jobs {
		if running(r.jobs) >= max(e.cfg.Jobs, 1) {
			break
		}
		if r.jobs[i].view.State == JobQueued {
			r = e.start(w, r, i)
		}
	}
	return e.endPassIfSettled(r)
}

// running counts the reviews that hold a slot. A review whose process has
// ended holds none, even while its results are still being read.
func running(jobs []job) int {
	n := 0
	for _, j := range jobs {
		if j.view.State == JobRunning {
			n++
		}
	}
	return n
}

// start runs one review. A review that cannot start fails by itself: the
// other PRs carry on.
func (e *Engine) start(w workers, r run, i int) run {
	j := r.jobs[i]
	pr := j.view.PR
	j.plan = e.planFor(pr)
	if j.plan.Note != "" {
		e.journal(Noted{Text: j.plan.Note})
	}
	request := e.request(pr, j.plan, r)
	j.spec = review.SpecFor(request)
	started, err := e.deps.Procs.Start(proc.Command{
		Path:   j.spec.Path,
		Args:   j.spec.Args,
		Env:    j.spec.Env,
		Dir:    j.spec.Dir,
		Stdout: j.spec.Stdout,
		Stderr: j.spec.Stderr,
	})
	if err != nil {
		j.view.State = JobFailed
		j.view.Outcome = review.OutcomeWord(review.Exit{Code: exitNotStarted}, false)
		j.view.Error = sanitize.Line(err.Error())
		e.journal(JobFinished{Job: j.view})
		return withJob(r, i, j)
	}
	j.proc = started
	j.view.State = JobRunning
	j.view.StartedAt = time.Now()
	j.view.Resumed = review.Skill(request) == "recheck-pr"
	if j.spec.Guard > 0 {
		j.guard = time.AfterFunc(j.spec.Guard, func() { e.in.put(guardFired{pr: pr}) })
	}
	facts := facts{view: j.view, plan: j.plan, spec: j.spec, pass: r.pass}
	w.group.Go(func() { e.monitor(w.ctx, facts, started) })
	e.journal(JobStarted{Job: j.view})
	return withJob(r, i, j)
}

// planFor decides the session a review of this PR runs in. An agent that
// takes no session gets the empty plan: a planned id that is never sent
// must not be shown, recorded or resumed.
func (e *Engine) planFor(pr int) session.Plan {
	if e.cfg.Override == "" && !e.cfg.Orchestrator.SupportsSessions() {
		return session.Plan{}
	}
	id := session.ID(e.cfg.Repo.Root, e.cfg.Repo.Owner, e.cfg.Repo.Name, pr)
	return session.PlanFor(id, pr, e.cfg.Continue, e.deps.Sessions)
}

func (e *Engine) request(pr int, plan session.Plan, r run) review.Request {
	return review.Request{
		PR:           pr,
		Orchestrator: e.cfg.Orchestrator,
		Session:      plan,
		NoPost:       e.cfg.NoPost,
		Focus:        e.cfg.Focus,
		Timeout:      e.cfg.Timeout,
		Budget:       e.cfg.Budget,
		SkillsDir:    e.deps.SkillsDir,
		Override:     e.cfg.Override,
		Dashp:        e.cfg.Dashp,
		Dir:          e.cfg.Repo.Root,
		Stdout:       r.pass.Stdout(pr),
		Stderr:       r.pass.Stderr(pr),
		Meta:         r.pass.Meta(pr),
	}
}

// reaped takes the end of a review's process. The slot is free from here,
// so the next review starts before anything this one left is read.
func (e *Engine) reaped(w workers, r run, in jobReaped) run {
	i := indexOf(r.jobs, in.pr)
	if i < 0 || r.jobs[i].view.State != JobRunning {
		return r
	}
	j := r.jobs[i]
	if j.guard != nil {
		j.guard.Stop()
	}
	j.exit = review.Exit{Code: in.exit.Code, Signaled: in.exit.Signaled}
	j.view.State = JobFinishing
	j.view.Elapsed = in.elapsed
	j.view = withMeta(j.view, j.plan, in.meta, j.spec.Override)
	r = withJob(r, i, j)
	if r.snap.Phase != Reviewing {
		return r
	}
	return e.fill(w, r)
}

// withMeta adds what the harness said about the job. It is read when the
// process ends and not later, so the summary of an interrupted run still
// has the session and the cost.
func withMeta(view Job, plan session.Plan, meta *review.Meta, override bool) Job {
	if meta != nil {
		cost := meta.CostUSD
		view.Cost = &cost
		view.Model = meta.Model
	}
	view.Session, _ = review.SummarySession(plan, meta, override)
	return view
}

// guardFired stops a review that ran past its time. The stop only begins
// here. The review is done when its process has been collected.
func (e *Engine) guardFired(r run, in guardFired) run {
	i := indexOf(r.jobs, in.pr)
	if i < 0 || r.jobs[i].view.State != JobRunning {
		return r
	}
	j := r.jobs[i]
	j.guardTripped = true
	j.proc.Stop()
	return withJob(r, i, j)
}

// settled gives a review its end state.
//
// A stop outranks the exit status. A reviewer that is killed can exit with
// the status that means "the agent failed", and a review a person stopped
// must not be read as an outage.
func (e *Engine) settled(r run, in jobSettled) run {
	i := indexOf(r.jobs, in.pr)
	if i < 0 || r.jobs[i].view.State != JobFinishing {
		return r
	}
	j := r.jobs[i]
	state := review.Classify(j.exit, j.guardTripped, j.spec.Override)
	switch {
	case j.stopped:
		j.view.State = JobFailed
		j.view.Outcome = review.OutcomeWord(j.exit, true)
	case state == review.Done:
		j.view = e.finished(j.view, in.outcome)
	case state == review.TimedOut:
		j.view.State = JobTimedOut
	default:
		j.view.State = JobFailed
		j.view.Outcome = review.OutcomeWord(j.exit, false)
		j.view.Error = in.outcome.agentError
	}
	e.journal(JobFinished{Job: j.view})
	return e.endPassIfSettled(withJob(r, i, j))
}

// finished is a review that ran to its end: what it decided, and its place
// in the ledger.
func (e *Engine) finished(view Job, out outcome) Job {
	view.State = JobDone
	view.Verdict = out.verdict
	view.Trailer = out.trailer
	for _, note := range out.notes {
		e.journal(Noted{Text: note})
	}
	if out.trailer == nil || e.deps.Record == nil {
		return view
	}
	// Only a review that reported on itself is recorded: the ledger scores
	// the panel, and a review with no trailer names no panel.
	err := e.deps.Record(Finished{Job: view, FinishedAt: time.Now(), Text: out.text, HasText: out.hasText})
	if err != nil {
		e.journal(Noted{Text: fmt.Sprintf("could not record the review of PR #%d in the ledger: %s", view.PR, sanitize.Line(err.Error()))})
	}
	return view
}

// endPassIfSettled ends the run when every review has its end state. The
// exit status is zero only when every review ran to its end.
func (e *Engine) endPassIfSettled(r run) run {
	for _, j := range r.jobs {
		if !j.view.State.Settled() {
			return r
		}
	}
	switch {
	case r.snap.Phase == Stopping:
		r.code = ExitInterrupted
	case slices.ContainsFunc(r.jobs, func(j job) bool { return j.view.State != JobDone }):
		r.code = ExitFailed
	default:
		r.code = ExitOK
	}
	r.snap.Phase = Done
	return r
}

// stopReviews ends a pass that was told to stop. Every live review is
// signalled before anything is recorded or said: a write to a terminal
// that has hung up can fail, and the reviewers must be stopping by then.
//
// A review that had not started never starts. A review whose process has
// already ended keeps its result.
func (e *Engine) stopReviews(r run) run {
	e.quitting.Store(true)
	kept := []job{}
	for _, j := range r.jobs {
		switch j.view.State {
		case JobQueued:
			continue
		case JobRunning:
			j.stopped = true
			j.proc.Stop()
		}
		kept = append(kept, j)
	}
	r.jobs = kept
	r.snap.Phase = Stopping
	e.journal(Interrupted{})
	return e.endPassIfSettled(r)
}
