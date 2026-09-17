//! The pass engine: a bounded pool of review subprocesses, driven by events
//! rather than polling. Each job gets a monitor thread whose entire body is
//! `child.wait()` -- which is what makes a job killed from outside ordinary
//! rather than a special case: it is a wait() that returns a signal-death
//! status, and the slot frees immediately.

use crate::cli::Config;
use crate::job::{self, GUARD_GRACE_SECS, Job, JobState};
use crate::ledger;
use crate::orchestrator::Fallback;
use crate::report;
use crate::repo::RepoContext;
use crate::rundir::RunDir;
use crate::session::{self, SessionFlag};
use crate::activity::Tail;
use crate::board::Action;
use crate::ui::Ui;
use nix::sys::signal::{Signal, killpg};
use nix::unistd::Pid;
use std::collections::{HashMap, VecDeque};
use std::sync::mpsc::{Receiver, RecvTimeoutError, Sender};
use std::time::{Duration, Instant, SystemTime, UNIX_EPOCH};

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
fn interrupt(jobs: &[Job], ui: &mut Ui) -> ! {
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
    // Then the terminal: the board or the full-screen view holds it in raw
    // mode, and the summary must land on a terminal that has been given back.
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
    cfg: &Config,
    ctx: &RepoContext,
    rundir: &RunDir,
    dashp: &str,
    tx: &Sender<Event>,
    ui: &mut Ui,
) -> bool {
    let is_override = cfg.review_cmd.is_some();
    match job::spawn(&jobs[idx], cfg, ctx, rundir, dashp) {
        Ok(child) => {
            let started = Instant::now();
            jobs[idx].pgid = Some(child.id() as i32);
            jobs[idx].started = Some(started);
            jobs[idx].started_epoch = SystemTime::now()
                .duration_since(UNIX_EPOCH)
                .map(|d| d.as_secs() as i64)
                .unwrap_or(0);
            jobs[idx].state = JobState::Running;
            jobs[idx].activity = follow(&jobs[idx], is_override, rundir);
            deadlines[idx] = deadline_for(cfg, is_override).map(|d| Deadline { at: Instant::now() + d });
            ui.note_transition(&jobs[idx]);
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
                let readback = status.success().then(|| report::github_verdict(pr, &me, started_epoch));
                let _ = tx.send(Event::JobExited { idx, status, readback });
            });
            true
        }
        Err(e) => {
            ui.note(format!("error: could not start the review for PR #{}: {e}", jobs[idx].pr));
            jobs[idx].state = JobState::Failed;
            jobs[idx].exit_code = Some(127);
            // The failed marker too, like the exit path: the next babysit
            // pass must review fresh, not re-check a stale session for a PR
            // this run never reviewed.
            rundir.mark_failed(jobs[idx].pr);
            ui.note_transition(&jobs[idx]);
            false
        }
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
    !job.stopped
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
    cfg: &Config,
    ctx: &RepoContext,
    rundir: &RunDir,
    dashp: &str,
    rx: &Receiver<Event>,
    tx: &Sender<Event>,
    ui: &mut Ui,
) -> Vec<Job> {
    let is_override = cfg.review_cmd.is_some();
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
    let total = jobs.len();
    let jobs_max = cfg.jobs as usize;

    ui.begin_pass(total, cfg.jobs, &rundir.pass_dir);

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

    while finished < total {
        // Fill free slots in queue order -- the order the tests (and eyes)
        // expect the starts to happen.
        while running < jobs_max && (!retries.is_empty() || !order.is_empty()) {
            let idx = match (retries.is_empty(), order.pop_front()) {
                (true, Some(idx)) => {
                    plan_job(&mut jobs[idx], cfg, ctx, rundir, ui);
                    idx
                }
                // Already planned: a retry is always a fresh, unpinned review.
                _ => retries.remove(0),
            };
            if launch(idx, &mut jobs, &mut deadlines, cfg, ctx, rundir, dashp, tx, ui) {
                running += 1;
            } else {
                finished += 1;
            }
        }

        // A spawn failure can finish the last job right here, with nothing
        // running and nothing to receive -- waiting on the channel then would
        // stall the end of the pass for the full receive timeout.
        if finished >= total {
            break;
        }

        // Ten frames a second on a terminal: the tick is what turns the
        // spinner now, and one turn a second is what a spinner looks like.
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
                let job = &mut jobs[idx];
                deadlines[idx] = None;
                job.reaped = true;
                job.elapsed_secs = elapsed_secs;
                job.pgid = None;
                running -= 1;
                // The meta envelope is complete once the child is reaped, so
                // even a summary printed mid-readback -- an interrupt --
                // hands back this review's session id and cost.
                let meta = job::read_meta(rundir, job.pr);
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
            Ok(Event::JobExited { idx, status, readback }) => {
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
            Ok(Event::Signal) => interrupt(&jobs, ui),
            Err(RecvTimeoutError::Timeout) => {}
            Err(RecvTimeoutError::Disconnected) => break,
        }

        // With the board up the terminal is in raw mode, so ctrl-C is a key
        // rather than a signal. Read here, on this thread, after every wake:
        // the board must never own a reader thread (see src/board.rs).
        for action in ui.poll_input() {
            match action {
                Action::Stop => interrupt(&jobs, ui),
                Action::StopReview(pr) => stop_review(&mut jobs, &mut deadlines, pr, ui),
                // Started next if this pass has it waiting; otherwise the
                // loop takes it into the next pass.
                Action::ReviewNow(pr) if !move_to_front(&mut order, &jobs, pr) => ui.request(pr),
                _ => {}
            }
        }

        // What each running review is doing, for the board. One stat per
        // running job per tick; nothing at all off a terminal, where no row
        // would show it.
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
    ui.end_pass();
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
