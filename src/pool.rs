//! The pass engine: a bounded pool of review subprocesses, driven by events
//! rather than polling. Each job gets a monitor thread whose entire body is
//! `child.wait()` -- which is what makes a job killed from outside ordinary
//! rather than a special case: it is a wait() that returns a signal-death
//! status, and the slot frees immediately.

use crate::cli::Config;
use crate::job::{self, GUARD_GRACE_SECS, Job, JobState};
use crate::ledger;
use crate::orchestrator::Fallback;
use crate::reaction::{self, Marks};
use crate::report;
use crate::repo::RepoContext;
use crate::rundir::RunDir;
use crate::session::{self, SessionFlag};
use crate::task::{self, Request};
use crate::task_run;
use crate::task_worktree::Prepared;
use crate::activity::Tail;
use crate::tui::Action;
use crate::ui::Ui;
use nix::sys::signal::{Signal, killpg};
use nix::unistd::Pid;
use std::collections::{HashMap, VecDeque};
use std::path::PathBuf;
use std::sync::mpsc::{Receiver, RecvTimeoutError, Sender};
use std::time::{Duration, Instant};

pub enum Event {
    /// The child was reaped. Sent the moment wait() returns, before the
    /// GitHub readback delays JobExited: the deadline guard must never fire
    /// for a review that already exited, and the recorded time must not
    /// include the readback's duration.
    JobReaped { idx: usize, elapsed_secs: u64 },
    JobExited {
        idx: usize,
        status: std::process::ExitStatus,
        /// The GitHub verdict readback, run on the monitor thread so a slow
        /// answer never blocks the event loop. None when the job did not
        /// exit cleanly -- an unfinished review has no verdict to read.
        readback: Option<report::Readback>,
    },
    /// A task's worktree is ready, or could not be made. Made on a thread of
    /// its own, because a fetch can take seconds.
    TaskReady {
        request: crate::task::Request,
        worktree: Result<crate::task_worktree::Prepared, String>,
    },
    Signal,
}

/// Stop one job: every process in its group, TERM first so a well-behaved
/// reviewer can clean up, then KILL, which cannot be ignored -- a reviewer
/// that shrugs off TERM must not outlive its timeout, because the pass waits
/// on it. One killpg covers everything the job spawned: the group was created
/// at spawn, so there is no tree to walk and no reparenting race to lose.
pub fn stop_group(pgid: i32) {
    let pg = Pid::from_raw(pgid);
    let _ = killpg(pg, Signal::SIGTERM);
    std::thread::sleep(Duration::from_millis(200));
    let _ = killpg(pg, Signal::SIGTERM);
    std::thread::sleep(Duration::from_millis(200));
    let _ = killpg(pg, Signal::SIGKILL);
}

/// Ctrl-C (or a dropped ssh session) must not leave reviews running, and the
/// reviews that already finished are still worth reopening -- so hand back
/// their session ids on the way out rather than dropping them.
fn interrupt(jobs: &[Job], marks: &mut Marks, ui: &mut Ui) -> ! {
    // The reviews before anything prints. A hangup leaves no terminal to
    // print to, and a print that fails panics, which would end the process
    // with the reviewers still running and still spending.
    for job in jobs {
        if let Some(pgid) = job.pgid
            && !matches!(job.state, JobState::Done | JobState::Failed | JobState::Timeout)
        {
            stop_group(pgid);
        }
    }
    // Then the marks, while the process is still here to remove them: a
    // review that was stopped is not being read any more, and nothing later
    // would take its reaction off the PR.
    marks.clear_all();
    marks.settle();
    // Then the terminal: the full-screen view holds it in raw mode, and the
    // summary must land on a terminal that has been given back.
    ui.interrupted(jobs)
}

struct Deadline {
    at: Instant,
}

/// Decide how PR #n attaches to a session for this pass. The recorded id --
/// the session an earlier pass of this run actually reviewed in -- outranks
/// the derived one: where a session already existed before the run, pass 1
/// reviewed under an id claude allocated, and the derived id names something
/// older. A failed pass leaves nothing to re-check, so it reviews fresh;
/// fresh still pins the derived id when no transcript exists, so the review
/// stays reachable afterwards.
fn plan_job(job: &mut Job, cfg: &Config, ctx: &RepoContext, rundir: &RunDir, ui: &mut Ui) {
    job.orchestrator = cfg.orchestrator.clone();
    // dash-p hands a session flag to claude alone. Any other orchestrator
    // reviews fresh, in a session it allocates itself -- said once at
    // startup rather than here, once per PR per pass.
    if !job.orchestrator.supports_sessions() {
        job.sid = None;
        job.flag = SessionFlag::None;
        job.resume = false;
        return;
    }
    let mut prior = if cfg.continue_sessions { rundir.recorded_session(job.pr) } else { None };
    let mut busy = false;
    if let Some(p) = &prior
        && session::session_in_use(p)
    {
        ui.note(format!(
            "note: PR #{} has its review session open elsewhere; reviewing fresh",
            job.pr
        ));
        prior = None;
        busy = true;
    }
    let fresh = rundir.last_pass_failed(job.pr);

    if busy || fresh {
        let derived = session::pr_session_id(&ctx.repo_root, &ctx.owner, &ctx.name, job.pr);
        if !session::session_exists(&derived) {
            job.sid = Some(derived.clone());
            job.flag = SessionFlag::Pin(derived);
        } else {
            job.sid = None;
            job.flag = SessionFlag::None;
        }
        job.resume = false;
    } else if let Some(p) = prior {
        job.sid = Some(p.clone());
        job.flag = SessionFlag::Resume(p);
        job.resume = true;
    } else {
        let planned = session::plan_session(
            &ctx.repo_root,
            &ctx.owner,
            &ctx.name,
            job.pr,
            cfg.continue_sessions,
        );
        if let Some(note) = planned.note {
            ui.note(note);
        }
        job.sid = planned.sid;
        job.flag = planned.flag;
        job.resume = planned.resume;
    }
}

/// Where a review's live activity can be read from, if anywhere. The
/// built-in reviewer runs in a session whose transcript claude writes as it
/// goes -- when this run chose the session id. A session claude named itself
/// is only found when the review ends, and an override reviewer has no
/// transcript at all; both leave the stderr log, which is what they write.
fn follow(job: &Job, is_override: bool, rundir: &RunDir) -> Tail {
    if is_override {
        return Tail::plain(rundir.log_path(job.pr));
    }
    match &job.sid {
        // A resumed session's transcript already holds every earlier pass;
        // this run's activity starts at its end.
        Some(sid) if job.resume => Tail::transcript(sid.clone(), job.started_epoch).from_end(),
        Some(sid) => Tail::transcript(sid.clone(), job.started_epoch),
        None => Tail::silent("claude named this session itself; its transcript is found when the review ends"),
    }
}

/// The tasks of one pass: the ones whose worktree is still being made, and
/// what each running one needs beside its job.
struct Tasks {
    /// Where the orchestrator finds an installed skill, read once a pass.
    roots: Vec<PathBuf>,
    /// PRs whose worktree is being made. The pass does not end while any is.
    preparing: Vec<u64>,
    running: HashMap<usize, task_run::Running>,
}

impl Tasks {
    fn new(cfg: &Config, ctx: &RepoContext) -> Tasks {
        Tasks { roots: task::skill_roots(&cfg.orchestrator, &ctx.repo_root), preparing: Vec::new(), running: HashMap::new() }
    }

    fn preparing(&self) -> bool {
        !self.preparing.is_empty()
    }

    /// Where job `idx` writes its files: the task's own directory, or the
    /// pass for a review.
    fn dir<'a>(&'a self, idx: usize, rundir: &'a RunDir) -> &'a RunDir {
        self.running.get(&idx).map_or(rundir, |t| &t.dir)
    }

    /// The PR's head before task `idx` started, which the readback compares
    /// with. None for a review.
    fn head(&self, idx: usize) -> Option<String> {
        self.running.get(&idx).map(|t| t.head.clone())
    }

    /// A person asked for a task. Refused with a note, or its worktree is
    /// started on a thread of its own.
    #[allow(clippy::too_many_arguments)]
    fn ask(&mut self, request: Request, jobs: &[Job], cfg: &Config, ctx: &RepoContext, rundir: &RunDir, tx: &Sender<Event>, ui: &mut Ui) {
        let pr = request.pr;
        let busy = self.preparing.contains(&pr)
            || jobs.iter().any(|j| j.pr == pr && matches!(j.state, JobState::Queued | JobState::Running));
        if let Some(note) = task_run::refusal(&request, cfg, &self.roots, busy) {
            ui.note(note);
            return;
        }
        self.preparing.push(pr);
        task_run::prepare(request, ctx.repo_root.clone(), rundir.root.clone(), tx.clone());
    }

    /// A worktree is ready, or could not be made. The new job's index when
    /// the task joins the pass.
    #[allow(clippy::too_many_arguments)]
    fn ready(
        &mut self,
        request: Request,
        worktree: Result<Prepared, String>,
        jobs: &mut Vec<Job>,
        cfg: &Config,
        ctx: &RepoContext,
        rundir: &RunDir,
        ui: &mut Ui,
    ) -> Option<usize> {
        self.preparing.retain(|&pr| pr != request.pr);
        let refused = |why: String| format!("note: not {} PR #{}: {why}", request.task.doing(), request.pr);
        let worktree = match worktree {
            Ok(w) => w,
            Err(why) => {
                ui.note(refused(why));
                return None;
            }
        };
        let dir = match rundir.for_task(request.task) {
            Ok(dir) => dir,
            Err(e) => {
                ui.note(refused(format!("could not make its log directory: {e}")));
                if let Some(note) = task_run::finish(&ctx.repo_root, request.pr, &worktree) {
                    ui.note(note);
                }
                return None;
            }
        };
        let idx = jobs.len();
        jobs.push(task_run::job(&request, &worktree, cfg, ctx));
        self.running.insert(idx, task_run::Running { worktree, head: request.head, dir });
        Some(idx)
    }

    /// Task `job` ended. What it did to the PR is GitHub's word, read back
    /// on its monitor thread; its worktree goes unless it holds work.
    #[allow(clippy::too_many_arguments)]
    fn finished(
        &mut self,
        idx: usize,
        job: &mut Job,
        state: JobState,
        code: Option<i32>,
        readback: Option<report::Readback>,
        ctx: &RepoContext,
        ui: &mut Ui,
    ) {
        job.state = state;
        job.exit_code = code;
        let Some(running) = self.running.get(&idx) else { return };
        if state == JobState::Failed && !job.stopped {
            job.error = report::read_agent_error(&running.dir.stdout_path(job.pr));
        }
        let (verdict, note) = task_run::verdict(job.pr, readback);
        job.verdict = verdict;
        for note in note.into_iter().chain(task_run::finish(&ctx.repo_root, job.pr, &running.worktree)) {
            ui.note(note);
        }
    }
}

/// Whether this job runs the review override. Only a review does: a task
/// runs its own skill through dash-p whatever the override says.
fn overridden(job: &Job, cfg: &Config) -> bool {
    cfg.review_cmd.is_some() && job.task.is_review()
}

/// What a job is, for the one message that names it.
fn what(job: &Job) -> &'static str {
    if job.task.is_review() { "review" } else { "task" }
}

fn deadline_for(cfg: &Config, is_override: bool) -> Option<Duration> {
    if cfg.timeout_secs == 0 {
        return None;
    }
    // dash-p enforces the real cap itself; the grace only covers its one
    // known hang hole. An override has no inner enforcement, so its deadline
    // is exact.
    let secs = if is_override { cfg.timeout_secs } else { cfg.timeout_secs + GUARD_GRACE_SECS };
    Some(Duration::from_secs(secs))
}

/// Start the review for `jobs[idx]` and its monitor thread. True when it is
/// running; false when it could not even be spawned, in which case the job
/// is already marked failed -- a reviewer that cannot start is a failed
/// review, not a dead run, and the other PRs still get theirs.
#[allow(clippy::too_many_arguments)]
fn launch(
    idx: usize,
    jobs: &mut [Job],
    deadlines: &mut [Option<Deadline>],
    marks: &mut Marks,
    cfg: &Config,
    ctx: &RepoContext,
    rundir: &RunDir,
    dashp: &str,
    tx: &Sender<Event>,
    ui: &mut Ui,
    task_head: Option<String>,
) -> bool {
    let is_override = overridden(&jobs[idx], cfg);
    match job::spawn(&jobs[idx], cfg, ctx, rundir, dashp) {
        Ok(child) => {
            let started = Instant::now();
            jobs[idx].pgid = Some(child.id() as i32);
            jobs[idx].started = Some(started);
            jobs[idx].started_epoch = crate::clock::epoch_secs();
            jobs[idx].state = JobState::Running;
            jobs[idx].activity = follow(&jobs[idx], is_override, rundir);
            deadlines[idx] = deadline_for(cfg, is_override).map(|d| Deadline { at: Instant::now() + d });
            ui.note_transition(&jobs[idx]);
            if jobs[idx].task.is_review() {
                marks.show(jobs[idx].pr);
            }
            let tx = tx.clone();
            let pr = jobs[idx].pr;
            let me = ctx.me.clone();
            let started_epoch = jobs[idx].started_epoch;
            std::thread::spawn(move || {
                let mut child = child;
                // A wait() error (ECHILD, if anything else reaped the
                // child) still frees the slot: a dropped event would
                // hang the pass forever, in a tool built for cron.
                let status = child
                    .wait()
                    .unwrap_or_else(|_| std::os::unix::process::ExitStatusExt::from_raw(127 << 8));
                // Reap first, then read back: JobReaped disarms the
                // deadline and pins the elapsed time, so the readback
                // below -- off the event loop, bounded by its own
                // deadline -- delays only this slot's release, never
                // the guard, the clock, or the other jobs.
                let _ = tx.send(Event::JobReaped { idx, elapsed_secs: started.elapsed().as_secs() });
                // A task is read back whether or not it exited cleanly: one
                // that failed late may still have pushed, and the reader
                // must know.
                let readback = match &task_head {
                    Some(before) => Some(crate::task_run::readback(pr, before)),
                    None => status.success().then(|| report::github_verdict(pr, &me, started_epoch)),
                };
                let _ = tx.send(Event::JobExited { idx, status, readback });
            });
            true
        }
        Err(e) => {
            ui.note(format!("error: could not start the {} for PR #{}: {e}", what(&jobs[idx]), jobs[idx].pr));
            jobs[idx].state = JobState::Failed;
            jobs[idx].exit_code = Some(127);
            // The failed marker too, like the exit path: the next babysit
            // pass must review fresh, not re-check a stale session for a PR
            // this run never reviewed.
            if jobs[idx].task.is_review() {
                rundir.mark_failed(jobs[idx].pr);
            }
            ui.note_transition(&jobs[idx]);
            // Only a retry has a mark by now, from its first attempt.
            marks.clear(jobs[idx].pr);
            false
        }
    }
}

/// The next review to start, and whether it still needs planning: a retry
/// first, then the queue. The queue is only touched when no retry waits. A
/// review taken off it and not started is never started, and the pass would
/// wait for it for ever.
fn next_start(retries: &mut Vec<usize>, order: &mut VecDeque<usize>) -> Option<(usize, bool)> {
    if retries.is_empty() {
        order.pop_front().map(|idx| (idx, true))
    } else {
        Some((retries.remove(0), false))
    }
}

/// Start a review this pass has not started yet before the others. False
/// when the pass has no such review waiting.
fn move_to_front(order: &mut VecDeque<usize>, jobs: &[Job], pr: u64) -> bool {
    let Some(pos) = order.iter().position(|&idx| jobs[idx].pr == pr) else {
        return false;
    };
    if let Some(idx) = order.remove(pos) {
        order.push_front(idx);
    }
    true
}

/// What the run says when the focus changes, in the log and on the screen.
pub fn focus_note(focus: Option<&str>) -> String {
    match focus {
        Some(focus) => format!("note: the reviewers are now told: {focus}"),
        None => "note: the reviewers are no longer told anything in particular".to_string(),
    }
}

/// Stop one running review because a person asked. It ends as a failure,
/// like a review killed from outside, and the next pass reviews the PR from
/// scratch. A reaped review has nothing left to stop, and its process group
/// id may already belong to something else.
fn stop_review(jobs: &mut [Job], deadlines: &mut [Option<Deadline>], pr: u64, ui: &mut Ui) {
    let Some(idx) = jobs.iter().position(|j| j.pr == pr) else { return };
    let job = &mut jobs[idx];
    let pgid = job.pgid.filter(|_| job.state == JobState::Running && !job.reaped);
    let Some(pgid) = pgid else {
        ui.note(format!("note: PR #{pr} has no review running; nothing to stop"));
        return;
    };
    job.stopped = true;
    deadlines[idx] = None;
    stop_group(pgid);
    ui.note(format!("note: stopped the review of PR #{pr}"));
}

/// Whether a finished attempt is one the fallback should retry: the
/// orchestrator itself failed (dash-p's exit 10 -- an outage, a usage limit,
/// an is_error turn), there is a fallback to retry under, and this was the
/// first attempt. Nothing else is retried. A timeout already ran for the
/// whole allowance; a signal-death was somebody's decision; and an override
/// is judged by its exit status alone, with no dash-p behind it to have
/// said what the status means.
fn should_fall_back(job: &Job, state: JobState, code: Option<i32>, cfg: &Config) -> bool {
    // A task's skill is the one the person installed for the orchestrator
    // they chose; the other one may not have it.
    job.task.is_review()
        && !job.stopped
        && state == JobState::Failed
        && code == Some(10)
        && cfg.review_cmd.is_none()
        && matches!(cfg.fallback, Fallback::Spec(_))
        && !job.fell_back()
}

#[allow(clippy::too_many_arguments)]
pub fn run_pass(
    queue: &[u64],
    info: &HashMap<u64, crate::prlist::PrInfo>,
    cfg: &mut Config,
    ctx: &RepoContext,
    rundir: &RunDir,
    dashp: &str,
    rx: &Receiver<Event>,
    tx: &Sender<Event>,
    ui: &mut Ui,
) -> Vec<Job> {
    let mut jobs: Vec<Job> = queue
        .iter()
        .map(|&n| {
            let mut job = Job::new(n);
            if let Some(meta) = info.get(&n) {
                job.title = meta.title.clone();
                job.author = meta.author.clone();
            }
            job
        })
        .collect();
    let mut deadlines: Vec<Option<Deadline>> = queue.iter().map(|_| None).collect();
    // The eyes reaction each PR carries while its review runs. Not under
    // --no-post: that run promised to leave the PR alone.
    let mut marks = Marks::new(&ctx.owner, &ctx.name, !cfg.no_post);
    let mut total = jobs.len();
    let jobs_max = cfg.jobs as usize;
    let mut tasks = Tasks::new(cfg, ctx);
    let asked = ui.take_tasks();

    ui.begin_pass(total, asked.len(), cfg.jobs, &rundir.pass_dir);
    for request in asked {
        tasks.ask(request, &jobs, cfg, ctx, rundir, tx, ui);
    }

    let mut running = 0usize;
    // The reviews not started yet, in the order they start. Queue order
    // unless a person asks for one first.
    let mut order: VecDeque<usize> = (0..total).collect();
    let mut finished = 0usize;
    // Reviews waiting to be retried under the fallback. They go ahead of the
    // queue -- a PR that has already had one attempt is closer to done than
    // one that has had none -- but not ahead of the cap: a retry is a whole
    // review, and --jobs is the promise about how many run at once.
    let mut retries: Vec<usize> = Vec::new();

    // A task whose worktree is still being made is part of the pass: it
    // joins the queue when its worktree is ready.
    while finished < total || tasks.preparing() {
        // Before anything is started: a focus typed while the run waited
        // belongs to the reviews it was waiting to start, and one typed
        // mid-pass belongs to whatever has not started yet.
        if let Some(focus) = ui.take_focus() {
            cfg.focus = focus;
            ui.note(focus_note(cfg.focus.as_deref()));
            ui.show_focus(cfg.focus.as_deref());
        }
        // Fill free slots in queue order -- the order the tests (and eyes)
        // expect the starts to happen.
        while running < jobs_max {
            let Some((idx, unplanned)) = next_start(&mut retries, &mut order) else { break };
            // A retry is already planned: it is always a fresh, unpinned
            // review.
            if unplanned && jobs[idx].task.is_review() {
                plan_job(&mut jobs[idx], cfg, ctx, rundir, ui);
            }
            let dir = tasks.dir(idx, rundir);
            let head = tasks.head(idx);
            if launch(idx, &mut jobs, &mut deadlines, &mut marks, cfg, ctx, dir, dashp, tx, ui, head) {
                running += 1;
            } else {
                finished += 1;
            }
        }

        // A spawn failure can finish the last job right here, with nothing
        // running and nothing to receive -- waiting on the channel then would
        // stall the end of the pass for the full receive timeout.
        if finished >= total && !tasks.preparing() {
            break;
        }

        // Ten frames a second with the view up: the tick is what turns the
        // spinner, and one turn a second is what a spinner looks like.
        let wait = if ui.ticking() {
            Duration::from_millis(100)
        } else {
            // Event-driven: sleep to the nearest deadline, or just wait for
            // an exit. The cap keeps a wrong deadline from wedging the loop.
            deadlines
                .iter()
                .flatten()
                .map(|d| d.at.saturating_duration_since(Instant::now()))
                .min()
                .unwrap_or(Duration::from_secs(60))
                .min(Duration::from_secs(60))
                .max(Duration::from_millis(10))
        };

        match rx.recv_timeout(wait) {
            Ok(Event::JobReaped { idx, elapsed_secs }) => {
                // The child is gone. Neither the guard nor the interrupt
                // path may killpg its dead (possibly recycled) pgid, the
                // clock stops at the real exit, and the slot frees for the
                // next queued review -- only the readback remains, and it
                // belongs to no process group.
                let dir = tasks.dir(idx, rundir);
                let job = &mut jobs[idx];
                let is_override = overridden(job, cfg);
                deadlines[idx] = None;
                job.reaped = true;
                job.elapsed_secs = elapsed_secs;
                job.pgid = None;
                running -= 1;
                // The meta envelope is complete once the child is reaped, so
                // even a summary printed mid-readback -- an interrupt --
                // hands back this review's session id and cost.
                let meta = job::read_meta(dir, job.pr);
                if let Some(m) = &meta
                    && m.total_cost_usd > 0.0
                    && !is_override
                {
                    job.cost = Some(m.total_cost_usd);
                }
                if let Some(m) = &meta
                    && !m.model_resolved.is_empty()
                    && !is_override
                {
                    job.model = Some(m.model_resolved.clone());
                }
                job.sid = job::summary_sid(job, meta.as_ref(), is_override);
            }
            Ok(Event::TaskReady { request, worktree }) => {
                if let Some(idx) = tasks.ready(request, worktree, &mut jobs, cfg, ctx, rundir, ui) {
                    deadlines.push(None);
                    order.push_back(idx);
                    total += 1;
                }
            }
            Ok(Event::JobExited { idx, status, readback }) if !jobs[idx].task.is_review() => {
                let (state, code) = if jobs[idx].stopped {
                    (JobState::Failed, None)
                } else {
                    job::classify(status, jobs[idx].guard_tripped, false)
                };
                finished += 1;
                tasks.finished(idx, &mut jobs[idx], state, code, readback, ctx, ui);
                ui.note_transition(&jobs[idx]);
            }
            Ok(Event::JobExited { idx, status, readback }) => {
                let is_override = overridden(&jobs[idx], cfg);
                // A stopped review is a failure with no result, whatever the
                // kill made the reviewer exit with: dash-p may answer TERM
                // with 10, which would otherwise read as an outage and be
                // retried, or with 20, which would read as a timeout.
                let (state, code) = if jobs[idx].stopped {
                    (JobState::Failed, None)
                } else {
                    job::classify(status, jobs[idx].guard_tripped, is_override)
                };
                if should_fall_back(&jobs[idx], state, code, cfg)
                    && let Fallback::Spec(fallback) = &cfg.fallback
                {
                    // The failed attempt's files are kept under its own
                    // name: its log is where the failure explains itself,
                    // and the retry would otherwise write over it.
                    jobs[idx].exit_code = code;
                    rundir.archive_attempt(jobs[idx].pr, &jobs[idx].orchestrator.backend);
                    jobs[idx].retry_under(fallback.clone());
                    ui.note_retry(&jobs[idx]);
                    retries.push(idx);
                    continue;
                }
                let job = &mut jobs[idx];
                job.state = state;
                job.exit_code = code;
                // A failed built-in review leaves its reason in the envelope:
                // claude's own usage-limit notice, an API error. Exit 10
                // without it is a number the reader has to go and decode.
                if job.state == JobState::Failed && !is_override && !job.stopped {
                    job.error = report::read_agent_error(&rundir.stdout_path(job.pr));
                }
                // The slot and the deadline were released at JobReaped;
                // this event only finishes the bookkeeping.
                finished += 1;
                // The review is over, whichever way it went. A retry never
                // reaches here, so its mark stays on between the attempts.
                marks.clear(job.pr);

                let ok = job.state == JobState::Done;
                if ok {
                    rundir.clear_failed(job.pr);
                } else {
                    rundir.mark_failed(job.pr);
                }
                // Cost, model and sid were read at JobReaped. Only a review
                // that finished is worth resuming, and only an envelope id
                // names the session it actually ran in.
                // A session is recorded only when the next pass could hand
                // it back: a codex thread id resumed under claude would
                // fail, and fall back, every interval.
                if ok && !is_override
                    && job.orchestrator.supports_sessions()
                    && let Some(m) = job::read_meta(rundir, job.pr)
                    && session::is_uuid_shaped(&m.session_id)
                {
                    let _ = rundir.record_session(job.pr, &m.session_id);
                }
                // What the finished review concluded: the agent's trailer,
                // then GitHub's readback (carried in from the monitor
                // thread), which outranks it. Only a review that completed
                // has a conclusion to read, and only the built-in reviewer
                // promises the envelope the trailer lives in -- but the
                // GitHub readback works under any reviewer.
                if ok && let Some(gh) = readback {
                    // dash-p's answer is only the reviewer's last message,
                    // so the transcript is where a review that was followed
                    // by a sign-off still lives.
                    let transcript =
                        job.sid.as_deref().and_then(crate::session::transcript_path);
                    if !is_override {
                        job.trailer = report::read_trailer(
                            &rundir.stdout_path(job.pr),
                            transcript.as_deref(),
                            job.started_epoch,
                        );
                    }
                    // The review in a form a person can open. Best effort: a
                    // review that ran is not spoiled by a file that could not
                    // be written, and pr-N.json still holds the original.
                    let review = report::read_review(
                        &rundir.stdout_path(job.pr),
                        transcript.as_deref(),
                        job.started_epoch,
                    );
                    if let Some(review) = &review {
                        let _ = std::fs::write(rundir.review_path(job.pr), review);
                    }
                    // A --no-post reviewer ran the skill with no posting
                    // step, so its own claim about what landed cannot be
                    // true. GitHub alone decides, which is also what makes
                    // the "nothing was posted" line under the summary safe
                    // to print. The rest of the trailer -- risk, findings,
                    // the panel -- is still worth reading.
                    let claimed = (!cfg.no_post).then_some(job.trailer.as_ref()).flatten();
                    job.verdict = report::resolve_verdict(&gh, claimed);
                    if let Some(claim) = report::vetoed_claim(&gh, job.trailer.as_ref()) {
                        ui.note(format!(
                            "note: PR #{}'s reviewer reported \"{claim}\" but GitHub shows no such review landed",
                            job.pr
                        ));
                    }
                    // Into the ledger, where it outlives the log directory.
                    // Only a review that reported a panel has anything to
                    // say about the models; the verdict alone is GitHub's.
                    if let Some(trailer) = &job.trailer
                        && let Some(path) = ledger::path(&crate::cli::real_env)
                    {
                        let run = ledger::autoreview_run(ledger::Reviewed {
                            repo: &format!("{}/{}", ctx.owner, ctx.name),
                            pr: job.pr,
                            session: job.sid.as_deref(),
                            started_epoch: job.started_epoch,
                            verdict: job.verdict.as_deref(),
                            trailer,
                            review: review.as_deref(),
                            cost_usd: job.cost,
                            elapsed_secs: job.elapsed_secs,
                            driver_model: job.model.as_deref(),
                        });
                        if let Err(e) = ledger::append(&path, &run) {
                            ui.note(format!(
                                "note: could not record PR #{} in the ledger at {}: {e}",
                                job.pr,
                                path.display()
                            ));
                        }
                    }
                    if gh == report::Readback::Failed {
                        // Say what actually fills the column: the agent's own
                        // report only when a trailer decision resolved one.
                        let tail = if job.verdict.is_some() {
                            "the verdict is the agent's own report"
                        } else {
                            "its verdict is unknown"
                        };
                        ui.note(format!(
                            "note: could not read PR #{}'s review back from GitHub; {tail}",
                            job.pr
                        ));
                    }
                }
                ui.note_transition(job);
            }
            Ok(Event::Signal) => interrupt(&jobs, &mut marks, ui),
            Err(RecvTimeoutError::Timeout) => {}
            Err(RecvTimeoutError::Disconnected) => break,
        }

        // With the view up the terminal is in raw mode, so ctrl-C is a key
        // rather than a signal. Read here, on this thread, after every wake.
        for action in ui.poll_input() {
            match action {
                Action::Stop => interrupt(&jobs, &mut marks, ui),
                Action::StopReview(pr) => stop_review(&mut jobs, &mut deadlines, pr, ui),
                // Started next if this pass has it waiting; otherwise the
                // loop takes it into the next pass.
                Action::ReviewNow(pr) if !move_to_front(&mut order, &jobs, pr) => ui.request(pr),
                Action::RunTask(request) => tasks.ask(request, &jobs, cfg, ctx, rundir, tx, ui),
                _ => {}
            }
        }

        // What each running review is doing, for the view. One stat per
        // running job per tick; nothing at all without the view, where no
        // row would show it.
        if ui.ticking() {
            for job in jobs.iter_mut().filter(|j| j.state == JobState::Running && !j.reaped) {
                job.activity.poll();
            }
        }

        // Trip the guard on anything past its deadline. The job stays Running
        // until its monitor reports the exit the KILL guarantees, so there is
        // no state to race with.
        let now = Instant::now();
        for (idx, slot) in deadlines.iter_mut().enumerate() {
            if let Some(d) = slot
                && now >= d.at
            {
                if let Some(pgid) = jobs[idx].pgid {
                    stop_group(pgid);
                }
                jobs[idx].guard_tripped = true;
                *slot = None;
            }
        }

        ui.render(&jobs);
    }

    ui.render(&jobs);
    // The last review's mark is still coming off. The view keeps drawing
    // while it does, so a slow GitHub reads as a wait and not a hang.
    while ui.ticking() && marks.clearing() {
        std::thread::sleep(Duration::from_millis(100));
        ui.render(&jobs);
    }
    for pr in marks.settle() {
        ui.note(reaction::failed_note(pr));
    }
    jobs
}

pub fn failures(jobs: &[Job]) -> usize {
    jobs.iter().filter(|j| j.state != JobState::Done).count()
}

#[cfg(test)]
mod tests {
    use super::*;

    fn jobs(prs: &[u64]) -> Vec<Job> {
        prs.iter().map(|&n| Job::new(n)).collect()
    }

    #[test]
    fn a_requested_review_starts_before_the_rest() {
        let jobs = jobs(&[9, 8, 7]);
        let mut order: VecDeque<usize> = (0..3).collect();
        assert!(move_to_front(&mut order, &jobs, 7));
        assert_eq!(order, VecDeque::from([2, 0, 1]));
        // Already started, or never in this pass: the loop takes it.
        order.pop_front();
        assert!(!move_to_front(&mut order, &jobs, 7));
        assert!(!move_to_front(&mut order, &jobs, 12));
        assert_eq!(order, VecDeque::from([0, 1]), "the rest keep queue order");
    }

    #[test]
    fn a_waiting_retry_takes_nothing_from_the_queue() {
        let mut retries = vec![0];
        let mut order: VecDeque<usize> = VecDeque::from([1, 2]);
        assert_eq!(next_start(&mut retries, &mut order), Some((0, false)));
        assert_eq!(order, VecDeque::from([1, 2]), "the queue is untouched");
        assert_eq!(next_start(&mut retries, &mut order), Some((1, true)));
        assert_eq!(next_start(&mut retries, &mut order), Some((2, true)));
        assert_eq!(next_start(&mut retries, &mut order), None);
    }

    fn cfg() -> Config {
        let Ok(crate::cli::Parsed::Run(cfg)) = crate::cli::parse(Vec::new(), &|_| None) else {
            panic!("an empty command line parses");
        };
        Config { fallback: Fallback::Spec(crate::orchestrator::Orchestrator::parse("codex").unwrap()), ..*cfg }
    }

    #[test]
    fn a_stopped_review_is_never_retried() {
        let cfg = cfg();
        let mut job = Job::new(9);
        assert!(should_fall_back(&job, JobState::Failed, Some(10), &cfg), "an outage is retried");
        job.stopped = true;
        assert!(!should_fall_back(&job, JobState::Failed, Some(10), &cfg), "a person stopped it");
    }
}
