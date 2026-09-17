//! What the review screen lists, worked out from what the engine knows.
//!
//! One row per PR, never one per review. A PR reviewed three times in a watch
//! run is still one piece of work, and a list that grew a row per pass would
//! bury the running reviews under a day of history. The row carries the
//! newest review that finished, whatever section the PR is in now: under
//! --watch a reviewed PR spends most of its life resting, and a list that
//! only offered the review of a PR that is finished for good would hide it
//! exactly where it is wanted.

use crate::ci::Ci;
use crate::job::{Job, JobState};
use crate::prlist::PrInfo;
use crate::stack::StackedOn;
use std::collections::HashMap;
use std::path::{Path, PathBuf};

/// A review from a pass that has ended, and the directory its files are in.
pub struct Archived {
    pub job: Job,
    pub pass_dir: PathBuf,
}

/// Why a PR this run is responsible for is not being reviewed right now.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum Wait {
    /// Held until its checks pass.
    Checks(Ci),
    /// Held until the PR underneath it lands.
    Stacked(StackedOn),
    /// It has had every review this run may give it.
    Capped,
    /// Reviewed a short while ago; it may be reviewed again from this epoch
    /// second.
    Resting { until: u64 },
    /// Nothing has changed since its last review. The next check decides.
    Quiet,
}

#[derive(Debug, Clone, Copy, PartialEq, Eq, PartialOrd, Ord)]
pub enum Section {
    Running,
    Queued,
    Waiting,
    Finished,
}

impl Section {
    pub fn title(self) -> &'static str {
        match self {
            Section::Running => "RUNNING",
            Section::Queued => "QUEUED",
            Section::Waiting => "WAITING",
            Section::Finished => "FINISHED",
        }
    }
}

/// A finished review and the pass directory its files are in.
#[derive(Debug, Clone, Copy)]
pub struct Review<'a> {
    pub job: &'a Job,
    pub pass_dir: &'a Path,
}

pub struct Row<'a> {
    pub pr: u64,
    pub section: Section,
    pub title: &'a str,
    pub author: &'a str,
    /// The review this pass has running or waiting to start.
    pub live: Option<&'a Job>,
    /// The newest review of this PR that finished in this run.
    pub last: Option<Review<'a>>,
    pub wait: Option<Wait>,
}

/// Everything the list is worked out from.
pub struct Sources<'a> {
    /// The pass in progress, or nothing between passes.
    pub jobs: &'a [Job],
    pub pass_dir: &'a Path,
    pub archive: &'a [Archived],
    pub waiting: &'a [(u64, Wait)],
    pub info: &'a HashMap<u64, PrInfo>,
}

fn finished(job: &Job) -> bool {
    matches!(job.state, JobState::Done | JobState::Failed | JobState::Timeout)
}

/// When a review ended, as an epoch second. The finished section lists the
/// newest first.
fn ended(job: &Job) -> i64 {
    job.started_epoch.saturating_add(job.elapsed_secs as i64)
}

/// The newest finished review of each PR. Later passes come later in the
/// archive and the pass in progress is newer than all of them, so the last
/// one seen wins.
fn last_reviews<'a>(src: &Sources<'a>) -> HashMap<u64, Review<'a>> {
    let archived = src.archive.iter().map(|a| Review { job: &a.job, pass_dir: a.pass_dir.as_path() });
    let current = src.jobs.iter().map(|job| Review { job, pass_dir: src.pass_dir });
    archived.chain(current).filter(|r| finished(r.job)).map(|r| (r.job.pr, r)).collect()
}

/// The list, top to bottom.
pub fn rows<'a>(src: &Sources<'a>) -> Vec<Row<'a>> {
    let last = last_reviews(src);
    let row = |pr: u64, section: Section, live: Option<&'a Job>, wait: Option<Wait>| {
        let known = src.info.get(&pr);
        let job = live.or_else(|| last.get(&pr).map(|r| r.job));
        Row {
            pr,
            section,
            title: known.map(|i| i.title.as_str()).or(job.map(|j| j.title.as_str())).unwrap_or(""),
            author: known.map(|i| i.author.as_str()).or(job.map(|j| j.author.as_str())).unwrap_or(""),
            live,
            last: last.get(&pr).copied(),
            wait,
        }
    };
    let mut out: Vec<Row<'a>> = Vec::new();
    for job in src.jobs {
        let listed = match job.state {
            JobState::Running => row(job.pr, Section::Running, Some(job), None),
            JobState::Queued => row(job.pr, Section::Queued, Some(job), None),
            _ => row(job.pr, Section::Finished, None, None),
        };
        out.push(listed);
    }
    for &(pr, wait) in src.waiting {
        if !out.iter().any(|r| r.pr == pr) {
            out.push(row(pr, Section::Waiting, None, Some(wait)));
        }
    }
    let mut rest: Vec<u64> = last.keys().copied().filter(|pr| !out.iter().any(|r| r.pr == *pr)).collect();
    rest.sort_unstable();
    for pr in rest {
        out.push(row(pr, Section::Finished, None, None));
    }
    // Stable, so running and queued keep the pass's order and waiting keeps
    // the loop's. Finished is newest first.
    out.sort_by(|a, b| {
        a.section.cmp(&b.section).then_with(|| match a.section {
            Section::Finished => {
                let end = |r: &Row| r.last.map_or(0, |l| ended(l.job));
                end(b).cmp(&end(a))
            }
            _ => std::cmp::Ordering::Equal,
        })
    });
    out
}

/// The row the selection is on: the selected PR's, or the first when that
/// PR has left the list or nothing was selected yet.
pub fn position(rows: &[Row], selected: Option<u64>) -> Option<usize> {
    if rows.is_empty() {
        return None;
    }
    Some(selected.and_then(|pr| rows.iter().position(|r| r.pr == pr)).unwrap_or(0))
}

/// The PR `delta` rows away from the selection in `order`, the list's PRs
/// top to bottom, held at the ends.
pub fn step(order: &[u64], selected: Option<u64>, delta: isize) -> Option<u64> {
    let at = selected.and_then(|pr| order.iter().position(|p| *p == pr)).unwrap_or(0);
    let to = at.saturating_add_signed(delta).min(order.len().checked_sub(1)?);
    Some(order[to])
}

/// How many rows each section holds, in section order.
pub fn counts(rows: &[Row]) -> [usize; 4] {
    let mut out = [0; 4];
    for row in rows {
        out[row.section as usize] += 1;
    }
    out
}

impl Row<'_> {
    /// A running review that has not exited yet. A reaped one is only
    /// waiting on its verdict, and its process group is gone.
    pub fn running(&self) -> bool {
        self.live.is_some_and(|j| j.state == JobState::Running && !j.reaped)
    }

    /// The review `r` would reopen, or why there is none. A running review's
    /// session is in use: two processes would write one transcript.
    pub fn resumable(&self) -> Result<Review<'_>, String> {
        if self.live.is_some_and(|j| j.state == JobState::Running) {
            return Err(format!("PR #{} is being reviewed; resume it when the review ends", self.pr));
        }
        let last = self.last.ok_or_else(|| format!("PR #{} has no finished review yet", self.pr))?;
        if last.job.sid.is_none() {
            return Err(format!("PR #{}'s review has no session to resume", self.pr));
        }
        Ok(last)
    }

    /// Whether `x` may stop this PR's review.
    pub fn stoppable(&self) -> Result<(), String> {
        if self.running() {
            Ok(())
        } else {
            Err(format!("PR #{} has no review running", self.pr))
        }
    }

    /// Whether `R` may ask for this PR now. A queued review can always be
    /// started first; anything else needs a run that looks again.
    pub fn requestable(&self, looping: bool) -> Result<(), String> {
        match self.section {
            Section::Running => Err(format!("PR #{} is being reviewed now", self.pr)),
            Section::Queued => Ok(()),
            _ if !looping => Err("this run makes one pass; review-now needs --watch or --babysit".into()),
            _ if self.last.is_some_and(|l| l.job.verdict.as_deref() == Some("approved")) => {
                Err(format!("PR #{} is approved; this run is finished with it", self.pr))
            }
            _ => Ok(()),
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    fn job(pr: u64, state: JobState, started: i64) -> Job {
        let mut job = Job::new(pr);
        job.state = state;
        job.started_epoch = started;
        job.elapsed_secs = 10;
        job
    }

    fn archived(job: Job, pass: &str) -> Archived {
        Archived { job, pass_dir: PathBuf::from(pass) }
    }

    fn prs(rows: &[Row]) -> Vec<(u64, Section)> {
        rows.iter().map(|r| (r.pr, r.section)).collect()
    }

    fn info(title: &str) -> PrInfo {
        PrInfo {
            title: title.into(),
            author: "alice".into(),
            engage: crate::prlist::Engagement::New,
            head: None,
            ci: Ci::Passing,
            stacked_on: None,
        }
    }

    #[test]
    fn sections_run_top_to_bottom_one_row_per_pr() {
        let archive = vec![archived(job(5, JobState::Done, 100), "/p1"), archived(job(6, JobState::Failed, 200), "/p1")];
        let jobs = vec![job(9, JobState::Running, 300), job(8, JobState::Queued, 0), job(7, JobState::Done, 250)];
        let waiting = vec![(4, Wait::Checks(Ci::Failing)), (5, Wait::Quiet)];
        let info = HashMap::new();
        let src = Sources { jobs: &jobs, pass_dir: Path::new("/p2"), archive: &archive, waiting: &waiting, info: &info };
        let rows = rows(&src);
        assert_eq!(
            prs(&rows),
            vec![
                (9, Section::Running),
                (8, Section::Queued),
                (4, Section::Waiting),
                (5, Section::Waiting),
                // Newest first: #7 ended at 260, #6 at 210.
                (7, Section::Finished),
                (6, Section::Finished),
            ]
        );
        assert_eq!(counts(&rows), [1, 1, 2, 2]);
    }

    #[test]
    fn a_waiting_pr_keeps_its_last_review() {
        let archive = vec![archived(job(5, JobState::Done, 100), "/p1")];
        let waiting = vec![(5, Wait::Resting { until: 900 })];
        let info = HashMap::new();
        let src = Sources { jobs: &[], pass_dir: Path::new("/p2"), archive: &archive, waiting: &waiting, info: &info };
        let rows = rows(&src);
        let last = rows[0].last.expect("the review it had");
        assert_eq!(last.pass_dir, Path::new("/p1"));
        assert_eq!(rows[0].wait, Some(Wait::Resting { until: 900 }));
    }

    #[test]
    fn a_pr_reviewed_again_shows_its_newest_review() {
        let mut first = job(5, JobState::Done, 100);
        first.verdict = Some("changes requested".into());
        let mut second = job(5, JobState::Done, 500);
        second.verdict = Some("approved".into());
        let archive = vec![archived(first, "/p1"), archived(second, "/p2")];
        let info = HashMap::new();
        let src = Sources { jobs: &[], pass_dir: Path::new("/p3"), archive: &archive, waiting: &[], info: &info };
        let rows = rows(&src);
        assert_eq!(rows.len(), 1);
        assert_eq!(rows[0].last.unwrap().job.verdict.as_deref(), Some("approved"));
    }

    #[test]
    fn a_running_pr_is_listed_once_with_its_earlier_review() {
        let archive = vec![archived(job(9, JobState::Done, 100), "/p1")];
        let jobs = vec![job(9, JobState::Running, 300)];
        let info = HashMap::new();
        let src = Sources { jobs: &jobs, pass_dir: Path::new("/p2"), archive: &archive, waiting: &[], info: &info };
        let rows = rows(&src);
        assert_eq!(prs(&rows), vec![(9, Section::Running)]);
        assert!(rows[0].last.is_some());
    }

    #[test]
    fn titles_come_from_the_newest_pr_list() {
        let mut stale = job(9, JobState::Done, 100);
        stale.title = "old title".into();
        let archive = vec![archived(stale, "/p1")];
        let info = HashMap::from([(9, info("new title"))]);
        let src = Sources { jobs: &[], pass_dir: Path::new("/p2"), archive: &archive, waiting: &[], info: &info };
        let rows = rows(&src);
        assert_eq!((rows[0].title, rows[0].author), ("new title", "alice"));
    }

    #[test]
    fn the_selection_follows_its_pr_not_its_place() {
        let jobs = vec![job(9, JobState::Running, 0), job(8, JobState::Queued, 0)];
        let info = HashMap::new();
        let src = Sources { jobs: &jobs, pass_dir: Path::new("/p"), archive: &[], waiting: &[], info: &info };
        let rows = rows(&src);
        assert_eq!(position(&rows, Some(8)), Some(1));
        assert_eq!(position(&rows, Some(42)), Some(0), "a PR that left the list");
        assert_eq!(position(&rows, None), Some(0));
        assert_eq!(position(&[], Some(8)), None);
        let order: Vec<u64> = rows.iter().map(|r| r.pr).collect();
        assert_eq!(step(&order, Some(9), 1), Some(8));
        assert_eq!(step(&order, Some(8), 1), Some(8), "held at the end");
        assert_eq!(step(&order, Some(9), -1), Some(9), "held at the start");
        assert_eq!(step(&order, Some(42), 1), Some(8), "from the top when its PR left");
        assert_eq!(step(&order, None, 5), Some(8));
        assert_eq!(step(&[], Some(8), 1), None);
    }

    fn one<'a>(live: Option<&'a Job>, last: Option<&'a Job>, section: Section) -> Row<'a> {
        let last = last.map(|job| Review { job, pass_dir: Path::new("/p") });
        Row { pr: 9, section, title: "", author: "", live, last, wait: None }
    }

    #[test]
    fn what_each_row_allows() {
        let running = job(9, JobState::Running, 0);
        let done = job(9, JobState::Done, 0);
        let mut resumable = job(9, JobState::Done, 0);
        resumable.sid = Some("7442b624-5cba-5d44-ae67-9c390cfe70a1".into());
        let mut approved = job(9, JobState::Done, 0);
        approved.verdict = Some("approved".into());
        let mut reaped = job(9, JobState::Running, 0);
        reaped.reaped = true;
        let queued = job(9, JobState::Queued, 0);

        let row = one(Some(&running), Some(&resumable), Section::Running);
        assert!(row.resumable().unwrap_err().contains("is being reviewed"));
        assert!(row.stoppable().is_ok());
        assert!(row.requestable(true).is_err());

        let row = one(None, None, Section::Waiting);
        assert!(row.resumable().unwrap_err().contains("no finished review"));
        assert!(row.stoppable().is_err());
        assert!(row.requestable(true).is_ok());
        assert!(row.requestable(false).unwrap_err().contains("--watch or --babysit"));

        assert!(one(None, Some(&done), Section::Finished).resumable().unwrap_err().contains("no session"));
        assert!(one(None, Some(&resumable), Section::Finished).resumable().is_ok());

        let row = one(Some(&reaped), None, Section::Running);
        assert!(row.stoppable().is_err(), "only its verdict is still coming");

        let row = one(Some(&queued), None, Section::Queued);
        assert!(row.requestable(false).is_ok(), "a queued review can start first");

        let row = one(None, Some(&approved), Section::Finished);
        assert!(row.requestable(true).unwrap_err().contains("approved"));
    }
}
