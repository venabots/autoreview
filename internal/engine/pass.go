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
	// head is the commit the PR was at when its review was planned.
	head string
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

// ready is the rows that nothing holds.
func ready(rows []queue.Row) []queue.Row {
	out := []queue.Row{}
	for _, row := range rows {
		if row.Wait == queue.WaitNext {
			out = append(out, row)
		}
	}
	return out
}

// startPass begins a pass over the given PRs. With none, a run that is not
// held ends: there is nothing to wait for.
func (e *Engine) startPass(w workers, r run, rows []queue.Row) run {
	if len(rows) == 0 {
		return r
	}
	pass, err := e.deps.Run.Pass(r.snap.Pass + 1)
	if err != nil {
		e.journal(Noted{Text: "could not make a directory for the pass: " + err.Error()})
		return e.rest(r, ExitFailed)
	}
	r.snap.Earlier = withEarlier(r.snap.Earlier, views(r.jobs))
	r.pass = pass
	r.snap.Pass++
	r.snap.Phase = Reviewing
	r.jobs = make([]job, len(rows))
	for i, row := range rows {
		r.jobs[i] = job{head: row.PR.Head, view: Job{
			PR:           row.PR.Number,
			Title:        row.PR.Title,
			Author:       row.PR.Author,
			State:        JobQueued,
			Orchestrator: e.cfg.Orchestrator,
			PassDir:      pass.Dir,
		}}
	}
	e.journal(PassStarted{Pass: r.snap.Pass, Reviews: len(rows), Limit: e.cfg.Jobs, Dir: pass.Dir})
	return e.fill(w, r)
}

// rest is the run with its work done, ending with code: over, or idle when
// the run is held for a person or loops.
func (e *Engine) rest(r run, code int) run {
	r.code = code
	if e.cfg.HoldOpen || r.snap.Watching {
		r.snap.Phase = Idle
		return r
	}
	r.snap.Phase = Done
	return r
}

// counted remembers a review that has its end state: it counts against
// the PR's cap and starts its rest, whether it ran to its end or not.
func (e *Engine) counted(r run, j job) run {
	r.track = r.track.Reviewed(j.view.PR, j.head, time.Now(), j.view.Verdict == "approved", e.policy())
	return e.regate(r)
}

// withEarlier adds the reviews of a pass that has ended to the ones kept
// from before, the newest for each PR. A review that never settled is not
// kept: it has nothing to show.
func withEarlier(earlier, ended []Job) []Job {
	kept := []Job{}
	for _, old := range earlier {
		if !slices.ContainsFunc(ended, func(j Job) bool { return j.PR == old.PR && j.State.Settled() }) {
			kept = append(kept, old)
		}
	}
	for _, j := range ended {
		if j.State.Settled() {
			kept = append(kept, j)
		}
	}
	return kept
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
	return e.endPassIfSettled(w, r)
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
	j.plan = e.planFor(r, pr)
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
		return e.counted(withJob(r, i, j), j)
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
//
// A PR this run reviewed before is re-checked in the session that review
// ran in, which already holds the findings. A review that failed leaves
// nothing to re-check, so the next one starts fresh.
func (e *Engine) planFor(r run, pr int) session.Plan {
	if e.cfg.Override == "" && !e.cfg.Orchestrator.SupportsSessions() {
		return session.Plan{}
	}
	id := session.ID(e.cfg.Repo.Root, e.cfg.Repo.Owner, e.cfg.Repo.Name, pr)
	prior, ok := lastReview(r, pr)
	switch {
	case !ok:
		return session.PlanFor(id, pr, e.cfg.Continue, e.deps.Sessions)
	case prior.State != JobDone || !session.UUIDShaped(prior.Session):
		return session.PlanFor(id, pr, false, e.deps.Sessions)
	case e.deps.Sessions.InUse(prior.Session):
		return session.Plan{
			ID:   id,
			Note: fmt.Sprintf("PR #%d has a review session open in another tab or process; reviewing fresh", pr),
		}
	}
	return session.Plan{ID: prior.Session, Flag: session.FlagResume}
}

// lastReview is the newest review of pr in this run that has ended.
func lastReview(r run, pr int) (Job, bool) {
	for _, jobs := range [][]Job{views(r.jobs), r.snap.Earlier} {
		for _, j := range jobs {
			if j.PR == pr && j.State.Settled() {
				return j, true
			}
		}
	}
	return Job{}, false
}

func (e *Engine) request(pr int, plan session.Plan, r run) review.Request {
	return review.Request{
		PR:           pr,
		Orchestrator: e.cfg.Orchestrator,
		Session:      plan,
		NoPost:       e.cfg.NoPost,
		Focus:        r.snap.Focus,
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
func (e *Engine) settled(w workers, r run, in jobSettled) run {
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
	return e.endPassIfSettled(w, e.counted(withJob(r, i, j), j))
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

// endPassIfSettled ends the pass when every review has its end state. The
// exit status is zero only when every review ran to its end.
//
// A run that is held then starts the pass a person asked for meanwhile.
func (e *Engine) endPassIfSettled(w workers, r run) run {
	for _, j := range r.jobs {
		if !j.view.State.Settled() {
			return r
		}
	}
	if r.snap.Phase == Stopping {
		r.snap.Phase = Done
		r.code = ExitInterrupted
		return r
	}
	code := ExitOK
	if slices.ContainsFunc(r.jobs, func(j job) bool { return j.view.State != JobDone }) {
		code = ExitFailed
	}
	r = e.rest(r, code)
	r.passEnded = true
	if e.cfg.Loop == LoopBabysit && r.snap.Phase == Idle {
		// A run that babysits waits a whole interval after a pass, for the
		// authors to answer it.
		r = e.lookIn(w, r, e.cfg.Watch)
	}
	if r.snap.Phase != Idle || len(r.requested) == 0 {
		return r
	}
	asked := r.requested
	r.requested = nil
	return e.startPass(w, r, rowsFor(r.snap.Reviews, asked))
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
	for _, j := range r.jobs {
		if !j.view.State.Settled() {
			return r
		}
	}
	r.snap.Phase = Done
	r.code = ExitInterrupted
	return r
}

// rowsFor is the rows on the list for the given PRs, in the order asked.
func rowsFor(rows []queue.Row, prs []int) []queue.Row {
	out := []queue.Row{}
	for _, pr := range prs {
		if i := slices.IndexFunc(rows, func(row queue.Row) bool { return row.PR.Number == pr }); i >= 0 {
			out = append(out, rows[i])
		}
	}
	return out
}

// reviewNow reviews one PR a person pointed at, whatever holds it. Idle,
// the run starts a pass for it. In a pass, a review that waits goes first,
// and a PR with no review in the pass is kept for the next one. A PR that
// is being reviewed now is left alone.
func (e *Engine) reviewNow(w workers, r run, pr int) run {
	asked := rowsFor(r.snap.Reviews, []int{pr})
	if len(asked) == 0 {
		return r
	}
	switch r.snap.Phase {
	case Idle:
		return e.startPass(w, r, asked)
	case Reviewing:
		i := indexOf(r.jobs, pr)
		switch {
		case i < 0 || r.jobs[i].view.State.Settled():
			if !slices.Contains(r.requested, pr) {
				r.requested = append(slices.Clone(r.requested), pr)
			}
		case r.jobs[i].view.State == JobQueued:
			r.jobs = movedFirst(r.jobs, i)
		}
	}
	return r
}

// movedFirst puts the job at i ahead of every other review that waits.
func movedFirst(jobs []job, i int) []job {
	first := slices.IndexFunc(jobs, func(j job) bool { return j.view.State == JobQueued })
	out := slices.Clone(jobs)
	asked := out[i]
	copy(out[first+1:i+1], out[first:i])
	out[first] = asked
	return out
}

// stopJob stops one review a person pointed at. It fails as stopped, and
// the rest of the pass goes on.
func (e *Engine) stopJob(r run, pr int) run {
	i := indexOf(r.jobs, pr)
	if i < 0 || r.jobs[i].view.State != JobRunning {
		return r
	}
	j := r.jobs[i]
	j.stopped = true
	j.proc.Stop()
	e.journal(Noted{Text: fmt.Sprintf("stopped the review of PR #%d", pr)})
	return withJob(r, i, j)
}
