//! The My PRs tab: your own open PRs, where each one stands, and the task
//! running or last run on it.
//!
//! The review tab is about other people's work and what the run is doing
//! about it. This one is about yours, so it is built from a different list
//! (`crate::mine`) and its rows say what blocks the merge, not what the
//! sweep thinks. A task on one of them comes from the same pool as a
//! review, so its live state is read from the same jobs and archive.

use super::detail::{field, heading};
use super::list::{ROW_LINES, paint};
use super::model::Archived;
use super::text::pad;
use crate::ci::Ci;
use crate::job::{Job, JobState};
use crate::mine::{Merge, MyPr, Review};
use crate::report::sanitize_for_display;
use crate::task::{Request, Task};
use crate::ui::{cost_str, fmt_dur, result_label, verdict_label};
use ratatui::style::{Color, Style, Stylize};
use ratatui::text::{Line, Span};
use std::path::{Path, PathBuf};

/// Which list the screen shows.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum Tab {
    Review,
    Mine,
}

impl Tab {
    pub fn other(self) -> Tab {
        match self {
            Tab::Review => Tab::Mine,
            Tab::Mine => Tab::Review,
        }
    }
}

/// One of your PRs and the task on it: running now, or the last to end.
pub struct MineRow<'a> {
    pub pr: &'a MyPr,
    pub live: Option<&'a Job>,
    /// The last task to end, and the directory its files are in.
    pub last: Option<(&'a Job, PathBuf)>,
    /// Babysat, and how many fixes babysitting has run on it; None when
    /// it is not babysat.
    pub babysat: Option<u32>,
}

impl MineRow<'_> {
    /// A task on this PR that has started and not yet been read back.
    pub fn busy(&self) -> bool {
        self.live.is_some_and(|j| matches!(j.state, JobState::Queued | JobState::Running))
    }

    /// The request a key sends for `task` on this PR.
    pub fn request(&self, task: Task) -> Request {
        Request {
            pr: self.pr.number,
            task,
            title: self.pr.title.clone(),
            branch: self.pr.branch.clone(),
            cross_repo: self.pr.cross_repo,
        }
    }
}

fn finished(job: &Job) -> bool {
    matches!(job.state, JobState::Done | JobState::Failed | JobState::Timeout)
}

/// Your PRs in the order `crate::mine` ranked them, each with its task.
pub fn rows<'a>(
    mine: &'a [MyPr],
    jobs: &'a [Job],
    pass_dir: &'a Path,
    archive: &'a [Archived],
    babysat: &[(u64, u32)],
) -> Vec<MineRow<'a>> {
    mine.iter()
        .map(|pr| {
            let tasks = |j: &&Job| j.pr == pr.number && !j.task.is_review();
            let live = jobs.iter().filter(tasks).find(|j| !finished(j));
            // A task's files are in its own directory under the pass. The
            // archive records it; for the pass in progress it is joined here.
            let current = jobs
                .iter()
                .filter(tasks)
                .filter(|j| finished(j))
                .map(|j| (j, j.task.file_tag().map_or(pass_dir.to_path_buf(), |tag| pass_dir.join(tag))));
            let archived = archive.iter().filter(|a| tasks(&&a.job)).map(|a| (&a.job, a.pass_dir.clone()));
            // The pass in progress is newer than anything archived.
            let last = archived.chain(current).last();
            let babysat = babysat.iter().find(|(n, _)| *n == pr.number).map(|(_, fixes)| *fixes);
            MineRow { pr, live, last, babysat }
        })
        .collect()
}

/// How many of your tasks are running, for the quit that would stop them.
/// One that has exited still counts until it is read back: quitting then
/// loses the verdict, which only the readback knows.
pub fn running(jobs: &[Job]) -> usize {
    jobs.iter().filter(|j| !j.task.is_review() && j.state == JobState::Running).count()
}

fn icon(row: &MineRow, spinner: &'static str) -> Span<'static> {
    if row.busy() {
        return Span::raw(spinner).magenta();
    }
    match row.pr.review {
        Review::Approved => Span::raw("✓").green(),
        Review::ChangesRequested => Span::raw("✗").yellow(),
        _ => Span::raw("○").dark_gray(),
    }
}

/// The state word: the task while it runs, else what blocks the merge.
pub fn state(row: &MineRow) -> (String, Style) {
    if let Some(job) = row.live {
        let secs = job.started.map_or(0, |s| s.elapsed().as_secs());
        let word = match job.state {
            JobState::Queued => "queued".to_string(),
            _ if job.reaped => "finishing".to_string(),
            _ => format!("{} {}", job.task.doing(), fmt_dur(secs)),
        };
        return (word, Style::default().fg(Color::Magenta));
    }
    let blocker = row.pr.state_word();
    let color = match blocker.as_str() {
        "conflicts" | "CI failing" => Color::Red,
        "changes req" => Color::Yellow,
        "approved" => Color::Green,
        w if w.ends_with("thread") || w.ends_with("threads") => Color::Yellow,
        _ => Color::DarkGray,
    };
    // Babysat first: it is what the run will do about the PR on its own.
    let word = if row.babysat.is_some() { format!("babysat · {blocker}") } else { blocker };
    (word, Style::default().fg(color))
}

/// The list's lines, two a row, and the first line of the selected row.
pub fn lines(rows: &[MineRow], selected: Option<usize>, width: usize, spinner: &'static str) -> (Vec<Line<'static>>, Option<usize>) {
    let mut out = Vec::new();
    let mut at = None;
    for (i, row) in rows.iter().enumerate() {
        let picked = selected == Some(i);
        if picked {
            at = Some(out.len());
        }
        let (word, style) = state(row);
        let label = format!("#{}", row.pr.number);
        let fixed = 2 + console::measure_text_width(&label) + 3;
        let head = Line::from(vec![
            icon(row, spinner),
            Span::raw(" "),
            Span::from(label).cyan().bold(),
            Span::from(" · ").dark_gray(),
            Span::styled(pad(&word, width.saturating_sub(fixed)), style),
        ]);
        out.push(paint(head, picked, width));
        let what = if row.pr.branch.is_empty() { &row.pr.title } else { &row.pr.branch };
        let who = Line::from(vec![Span::raw("  "), Span::from(pad(&sanitize_for_display(what), width.saturating_sub(2))).dark_gray()]);
        out.push(paint(who, picked, width));
    }
    debug_assert_eq!(out.len(), rows.len() * ROW_LINES);
    (out, at)
}

fn review_words(review: Review) -> &'static str {
    match review {
        Review::Approved => "approved",
        Review::ChangesRequested => "changes requested",
        Review::Required => "review required",
        Review::None => "no decision yet",
    }
}

fn ci_words(ci: Ci) -> (&'static str, Style) {
    match ci {
        Ci::Passing => ("passing", Style::default().fg(Color::Green)),
        Ci::Failing => ("failing", Style::default().fg(Color::Red)),
        Ci::Pending => ("pending", Style::default().fg(Color::Yellow)),
        Ci::None => ("no checks", Style::default()),
    }
}

fn merge_words(merge: Merge) -> (&'static str, Style) {
    match merge {
        Merge::Clean => ("merges cleanly", Style::default()),
        Merge::Conflicts => ("conflicts with its base", Style::default().fg(Color::Red)),
        Merge::Unknown => ("not worked out yet", Style::default().fg(Color::DarkGray)),
    }
}

/// The right pane for one of your PRs.
pub fn detail(row: &MineRow) -> Vec<Line<'static>> {
    let pr = row.pr;
    let plain = Style::default();
    let mut out = vec![
        Line::from(vec![
            Span::from(format!("#{}", pr.number)).cyan().bold(),
            Span::raw(" "),
            Span::from(sanitize_for_display(&pr.title)).bold(),
        ]),
        Line::from(sanitize_for_display(&pr.branch)).dark_gray(),
        Line::default(),
    ];
    let reviewers: Vec<String> = pr.reviewers.iter().map(|(who, r)| format!("{} by @{who}", review_words(*r))).collect();
    let review = if reviewers.is_empty() { review_words(pr.review).to_string() } else { reviewers.join(", ") };
    out.push(field("review", review, plain));
    let (ci, ci_style) = ci_words(pr.ci);
    out.push(field("checks", ci, ci_style));
    let threads = match pr.open_threads {
        0 => "none open".to_string(),
        n => format!("{n} unresolved"),
    };
    out.push(field("threads", threads, plain));
    let (merge, merge_style) = merge_words(pr.merge);
    out.push(field("merge", merge, merge_style));
    if pr.draft {
        out.push(field("draft", "yes", plain));
    }
    let babysat = match row.babysat {
        Some(0) => "yes · no fix needed yet".to_string(),
        Some(1) => "yes · 1 fix so far".to_string(),
        Some(n) => format!("yes · {n} fixes so far"),
        None => "no".to_string(),
    };
    out.push(field("babysat", babysat, plain));
    out.push(Line::default());
    out.extend(task_lines(row));
    out
}

fn task_lines(row: &MineRow) -> Vec<Line<'static>> {
    let plain = Style::default();
    if let Some(job) = row.live {
        let secs = job.started.map_or(0, |s| s.elapsed().as_secs());
        let mut out = vec![heading("TASK"), field("doing", format!("{} {}", job.task.doing(), fmt_dur(secs)), plain)];
        if let Some(cwd) = &job.cwd {
            out.push(field("in", cwd.display().to_string(), plain));
        }
        out.push(Line::from("x x stops it").dark_gray());
        return out;
    }
    let hint = Line::from("f fixes it · b babysits it · u fixes conflicts · c answers comments").dark_gray();
    let Some((job, pass_dir)) = &row.last else {
        return vec![hint];
    };
    let mut out = vec![
        heading("LAST TASK"),
        field("task", job.task.doing(), plain),
        field("result", result_label(job), plain),
        field("verdict", verdict_label(job.verdict.as_deref()), plain),
        field("spent", format!("{} · {}", fmt_dur(job.elapsed_secs), cost_str(job.cost)), plain),
    ];
    if let Some(sid) = &job.sid {
        out.push(field("session", sid.clone(), plain));
    }
    let log = crate::rundir::log_file(pass_dir, job.pr);
    out.push(field("log", log.display().to_string(), plain));
    out.push(Line::default());
    out.push(hint);
    out
}

/// The tab bar: both tabs and how many rows each holds, the shown one bold.
pub fn tab_bar(shown: Tab, reviews: usize, mine: usize, babysitting: usize, width: usize) -> Line<'static> {
    let tab = |label: String, on: bool| {
        if on { Span::from(label).bold().reversed() } else { Span::from(label).dark_gray() }
    };
    let mine_label = match babysitting {
        0 => format!(" My PRs {mine} "),
        n => format!(" My PRs {mine} · babysitting {n} "),
    };
    let line = Line::from(vec![
        tab(format!(" Review {reviews} "), shown == Tab::Review),
        Span::raw("  "),
        tab(mine_label, shown == Tab::Mine),
        Span::from("   tab switches").dark_gray(),
    ]);
    super::text::fit(line, width)
}

/// Why a key that only means something on a review cannot act here.
pub fn not_a_review(pr: u64) -> String {
    format!("PR #{pr} is yours, and the run reviews only other people's work; f fixes it, b babysits it")
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::path::PathBuf;

    fn mine(n: u64) -> MyPr {
        MyPr {
            number: n,
            title: "My own work".into(),
            draft: false,
            branch: "me/my-own-work".into(),
            head: "sha4".into(),
            updated_at: "2026-10-07T10:00:00Z".into(),
            cross_repo: false,
            review: Review::Required,
            merge: Merge::Clean,
            ci: Ci::Passing,
            open_threads: 0,
            reviewers: Vec::new(),
        }
    }

    fn text(line: &Line) -> String {
        line.spans.iter().map(|s| s.content.as_ref()).collect::<String>().trim_end().to_string()
    }

    fn task(n: u64, state: JobState) -> Job {
        let mut job = Job::new(n);
        job.task = Task::Fix;
        job.state = state;
        job
    }

    #[test]
    fn a_row_says_what_blocks_the_merge_or_what_the_task_is_doing() {
        let prs = vec![mine(4), MyPr { open_threads: 2, ..mine(5) }];
        let jobs = vec![task(5, JobState::Running)];
        let rows = rows(&prs, &jobs, Path::new("/p"), &[], &[]);
        let (drawn, at) = lines(&rows, Some(1), 36, "⠋");
        let out: Vec<String> = drawn.iter().map(text).collect();
        assert_eq!(out[0], "○ #4 · awaiting review");
        assert_eq!(out[1], "  me/my-own-work");
        assert_eq!(out[2], "⠋ #5 · fixing 0s");
        assert_eq!(at, Some(2));
        assert!(rows[1].busy() && !rows[0].busy());
    }

    #[test]
    fn a_review_job_is_not_a_task() {
        // A review of PR 4 (not that one would run) must not read as a task
        // on it.
        let prs = vec![mine(4)];
        let jobs = vec![Job { state: JobState::Running, ..Job::new(4) }];
        let rows = rows(&prs, &jobs, Path::new("/p"), &[], &[]);
        assert!(rows[0].live.is_none());
        assert_eq!(running(&jobs), 0, "a running review is counted by the review list");
        assert_eq!(running(&[task(4, JobState::Running)]), 1);
        let finishing = Job { reaped: true, ..task(4, JobState::Running) };
        assert_eq!(running(&[finishing]), 1, "until its worktree is let go");
        assert_eq!(running(&[task(4, JobState::Done)]), 0);
    }

    #[test]
    fn the_detail_names_every_blocker_and_the_last_task() {
        let prs = vec![MyPr {
            review: Review::ChangesRequested,
            reviewers: vec![("alice".into(), Review::ChangesRequested)],
            open_threads: 2,
            merge: Merge::Conflicts,
            ci: Ci::Failing,
            ..mine(4)
        }];
        let mut done = task(4, JobState::Done);
        done.verdict = Some("pushed".into());
        done.elapsed_secs = 190;
        let archive = vec![Archived { job: done, pass_dir: PathBuf::from("/run/pass-1/fix") }];
        let rows = rows(&prs, &[], Path::new("/p"), &archive, &[]);
        let out: Vec<String> = detail(&rows[0]).iter().map(text).collect();
        let out = out.join("\n");
        assert!(out.contains("review    changes requested by @alice"), "{out}");
        assert!(out.contains("checks    failing"), "{out}");
        assert!(out.contains("threads   2 unresolved"), "{out}");
        assert!(out.contains("merge     conflicts with its base"), "{out}");
        assert!(out.contains("LAST TASK"), "{out}");
        assert!(out.contains("verdict   pushed"), "{out}");
        assert!(out.contains("log       /run/pass-1/fix/pr-4.log"), "{out}");
        assert!(out.contains("f fixes it · b babysits it"), "{out}");
    }

    #[test]
    fn a_task_that_ended_in_this_pass_names_its_own_log() {
        let prs = vec![mine(4)];
        let jobs = vec![task(4, JobState::Done)];
        let rows = rows(&prs, &jobs, Path::new("/run/pass-1"), &[], &[]);
        let out: Vec<String> = detail(&rows[0]).iter().map(text).collect();
        assert!(out.contains(&"log       /run/pass-1/fix/pr-4.log".to_string()), "{out:?}");
    }

    #[test]
    fn a_babysat_pr_says_so_and_how_many_fixes_it_had() {
        let prs = vec![MyPr { open_threads: 2, ..mine(4) }, mine(5)];
        let rows = rows(&prs, &[], Path::new("/p"), &[], &[(4, 2)]);
        let (drawn, _) = lines(&rows, None, 40, "⠋");
        assert_eq!(text(&drawn[0]), "○ #4 · babysat · 2 threads");
        assert_eq!(text(&drawn[2]), "○ #5 · awaiting review");
        let out: Vec<String> = detail(&rows[0]).iter().map(text).collect();
        assert!(out.contains(&"babysat   yes · 2 fixes so far".to_string()), "{out:?}");
        let out: Vec<String> = detail(&rows[1]).iter().map(text).collect();
        assert!(out.contains(&"babysat   no".to_string()), "{out:?}");
    }

    #[test]
    fn the_tab_bar_counts_both_lists() {
        let line = tab_bar(Tab::Mine, 5, 1, 0, 80);
        assert_eq!(text(&line), " Review 5    My PRs 1    tab switches");
        let line = tab_bar(Tab::Mine, 5, 3, 2, 80);
        assert_eq!(text(&line), " Review 5    My PRs 3 · babysitting 2    tab switches");
        assert_eq!(Tab::Review.other(), Tab::Mine);
    }

    #[test]
    fn a_key_asks_for_a_task_with_what_the_list_knew() {
        let prs = vec![mine(4)];
        let rows = rows(&prs, &[], Path::new("/p"), &[], &[]);
        let r = rows[0].request(Task::Comments);
        assert_eq!((r.pr, r.task, r.branch.as_str()), (4, Task::Comments, "me/my-own-work"));
    }
}
