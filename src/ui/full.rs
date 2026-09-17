//! The Ui's side of the full-screen view: opening it, telling it what the
//! loop knows, keeping it drawn while the loop waits, and giving the
//! terminal back with a summary of the whole run.
//!
//! The loop between passes has no tick of its own: it sleeps an interval or
//! waits on `gh`. With the view up, both wait in tenths of a second instead,
//! drawing and reading keys, so the screen never freezes for as long as a
//! network call takes.

use super::Ui;
use crate::activity::Tail;
use crate::board::Action;
use crate::job::{Job, JobState};
use crate::pool;
use crate::prlist::PrInfo;
use crate::tui::{Archived, Header, Screen, Wait};
use std::collections::HashMap;
use std::path::Path;
use std::sync::mpsc::Receiver;
use std::time::{Duration, Instant};

/// How often the view is drawn while the loop waits.
const TICK: Duration = Duration::from_millis(100);

/// How a wait between passes ended.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum Woke {
    /// The time ran out.
    Elapsed,
    /// A person asked for a review now.
    Requested,
}

/// The newest review of each PR, first seen first. A review in the pass in
/// progress counts once it has started.
fn latest(archive: &[Archived], jobs: &[Job]) -> Vec<Job> {
    let mut out: Vec<Job> = Vec::new();
    let newer = archive.iter().map(|a| &a.job).chain(jobs.iter().filter(|j| j.state != JobState::Queued));
    for job in newer {
        match out.iter_mut().find(|j| j.pr == job.pr) {
            Some(slot) => *slot = job.clone(),
            None => out.push(job.clone()),
        }
    }
    out
}

impl Ui {
    /// Open the full-screen view over `run_root`'s log. Off a terminal there
    /// is nothing to open it on, and the run prints its plain lines as ever.
    pub fn open_screen(&mut self, header: Header, run_root: &Path) {
        if !self.terminal {
            eprintln!("note: --tui needs a terminal; printing plain lines");
            return;
        }
        match Screen::open(header) {
            Ok(screen) => {
                self.screen = Some(screen);
                self.run_root = Some(run_root.to_path_buf());
                // The plain lines, from here on, are the log's.
                self.tty = false;
            }
            Err(e) => eprintln!("note: could not open the full-screen view ({e}); using the ordinary output"),
        }
    }

    pub fn has_screen(&self) -> bool {
        self.screen.is_some()
    }

    /// Keep a finished pass for the view and the summary at the end. Its
    /// activity is dropped: a day of watching would otherwise keep every
    /// event of every review.
    pub fn archive(&mut self, jobs: Vec<Job>) {
        if self.run_root.is_none() {
            return;
        }
        let pass_dir = self.pass_dir.clone();
        self.archive.extend(jobs.into_iter().map(|mut job| {
            job.activity = Tail::silent("the review has ended");
            Archived { job, pass_dir: pass_dir.clone() }
        }));
    }

    /// What the loop is waiting on, as of its latest look.
    pub fn waiting(&mut self, waiting: Vec<(u64, Wait)>) {
        if let Some(screen) = &mut self.screen {
            screen.set_waiting(waiting);
        }
    }

    /// What the latest PR list said about each PR: titles and authors.
    pub fn know(&mut self, info: &HashMap<u64, PrInfo>) {
        if let Some(screen) = &mut self.screen {
            screen.know(info);
        }
    }

    pub fn has_requests(&self) -> bool {
        !self.requests.is_empty()
    }

    /// Draw once and read the keys, between passes. A request is kept for
    /// the loop; what else the keys asked for is returned.
    fn tick_idle(&mut self) -> Vec<Action> {
        let Some(screen) = &mut self.screen else { return Vec::new() };
        let actions = screen.events();
        screen.draw(&[], &self.pass_dir, &self.archive);
        let mut out = Vec::new();
        for action in actions {
            match action {
                Action::ReviewNow(pr) => self.request(pr),
                other => out.push(other),
            }
        }
        out
    }

    /// One tick between passes: leave the way ctrl-C does if a key or a
    /// signal asked to.
    fn idle_step(&mut self, rx: &Receiver<pool::Event>) {
        if self.tick_idle().contains(&Action::Stop) {
            self.interrupted(&[]);
        }
        // Anything else on the channel is a stale event from a pass that
        // has already ended.
        if let Ok(pool::Event::Signal) = rx.try_recv() {
            self.interrupted(&[]);
        }
    }

    /// Wait `dur` between passes. `wake_on_request` ends the wait early when
    /// a person asks for a review; the waits after a failure do not pass it,
    /// or every key press would repeat the call that just failed.
    pub fn wait(&mut self, dur: Duration, rx: &Receiver<pool::Event>, wake_on_request: bool) -> Woke {
        let deadline = Instant::now() + dur;
        let asked = self.requests.len();
        let next = super::epoch_now() + dur.as_secs() as i64;
        if let Some(screen) = &mut self.screen {
            screen.set_next_check(Some(next));
        }
        let woke = loop {
            let left = deadline.saturating_duration_since(Instant::now());
            if left.is_zero() {
                break Woke::Elapsed;
            }
            if self.screen.is_none() {
                match rx.recv_timeout(left) {
                    Ok(pool::Event::Signal) => self.interrupted(&[]),
                    Ok(_) => continue,
                    Err(_) => break Woke::Elapsed,
                }
            }
            self.idle_step(rx);
            if wake_on_request && self.requests.len() > asked {
                break Woke::Requested;
            }
            std::thread::sleep(left.min(TICK));
        };
        if let Some(screen) = &mut self.screen {
            screen.set_next_check(None);
        }
        woke
    }

    /// Run `work`, which blocks, while the view stays drawn. Without the
    /// view it is just a call.
    pub fn while_busy<T: Send>(&mut self, what: &str, rx: &Receiver<pool::Event>, work: impl FnOnce() -> T + Send) -> T {
        if self.screen.is_none() {
            return work();
        }
        if let Some(screen) = &mut self.screen {
            screen.set_busy(Some(what));
        }
        let done = std::thread::scope(|scope| {
            let handle = scope.spawn(work);
            while !handle.is_finished() {
                self.idle_step(rx);
                std::thread::sleep(TICK);
            }
            handle.join()
        });
        if let Some(screen) = &mut self.screen {
            screen.set_busy(None);
        }
        done.unwrap_or_else(|panic| std::panic::resume_unwind(panic))
    }

    /// The run is over. With the view up it stays, saying why, until `q`;
    /// without it this returns at once.
    pub fn hold(&mut self, why: &str, rx: &Receiver<pool::Event>) {
        let Some(screen) = &mut self.screen else { return };
        screen.set_ended(why);
        loop {
            if self.tick_idle().contains(&Action::Stop) {
                return;
            }
            if let Ok(pool::Event::Signal) = rx.try_recv() {
                return;
            }
            std::thread::sleep(TICK);
        }
    }

    /// Give the terminal back: the board, the view, and the plain lines to
    /// stdout. Safe to call twice.
    pub fn shutdown(&mut self) {
        self.end_pass();
        if self.screen.take().is_some() {
            self.tty = self.terminal;
        }
    }

    /// The summary a run ends with. A run that had the view gets one table
    /// for the whole run, the newest review of each PR in it; any other run
    /// gets the pass summary it has always had, which only an interrupted
    /// pass still owes.
    pub fn print_final(&self, jobs: &[Job]) {
        match &self.run_root {
            Some(root) => {
                let reviews = latest(&self.archive, jobs);
                if reviews.is_empty() {
                    println!("logs: {}", root.display());
                } else {
                    self.print_summary(&reviews, root);
                }
            }
            None if !jobs.is_empty() => self.print_summary(jobs, &self.pass_dir),
            None => {}
        }
    }

    /// Stop now, as ctrl-C does: the terminal back, the summary, exit 130.
    /// The caller has already stopped whatever was running.
    pub fn interrupted(&mut self, jobs: &[Job]) -> ! {
        self.shutdown();
        println!();
        eprintln!("interrupted; stopping running reviews");
        self.print_final(jobs);
        self.show_cursor();
        std::process::exit(130);
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::path::PathBuf;

    fn job(pr: u64, state: JobState, verdict: &str) -> Job {
        let mut job = Job::new(pr);
        job.state = state;
        job.verdict = Some(verdict.into());
        job
    }

    #[test]
    fn the_final_summary_has_the_newest_review_of_each_pr() {
        let archive = vec![
            Archived { job: job(9, JobState::Done, "changes requested"), pass_dir: PathBuf::from("/p1") },
            Archived { job: job(8, JobState::Done, "commented"), pass_dir: PathBuf::from("/p1") },
            Archived { job: job(9, JobState::Done, "approved"), pass_dir: PathBuf::from("/p2") },
        ];
        let jobs = vec![job(8, JobState::Running, ""), job(7, JobState::Queued, "")];
        let out = latest(&archive, &jobs);
        let seen: Vec<(u64, JobState, Option<&str>)> =
            out.iter().map(|j| (j.pr, j.state, j.verdict.as_deref())).collect();
        assert_eq!(
            seen,
            vec![(9, JobState::Done, Some("approved")), (8, JobState::Running, Some(""))],
            "a review that never started is not the newest"
        );
    }

    #[test]
    fn without_the_view_waiting_is_a_plain_sleep() {
        let mut ui = Ui::new(String::new());
        let (_tx, rx) = std::sync::mpsc::channel();
        let started = Instant::now();
        assert_eq!(ui.wait(Duration::from_millis(30), &rx, true), Woke::Elapsed);
        assert!(started.elapsed() >= Duration::from_millis(30));
        assert_eq!(ui.while_busy("working", &rx, || 42), 42);
        ui.hold("done", &rx);
    }
}
