//! Everything the user reads.
//!
//! Two front-ends over the same pass. With the full-screen view up
//! (`crate::tui`), the view draws the run and every plain line goes to the
//! run log. Without it -- cron, CI, piped output, or `--headless` on a
//! terminal -- state changes print one plain line each.
//!
//! The plain strings are a contract: the test suite greps for them verbatim,
//! and so do people's eyes -- keep them byte-identical across refactors.

use crate::tui::{Action, Screen};
use crate::job::{Job, JobState};
use crate::report::{Panelist, Trailer};
use crate::task::Task;
use crate::{quorum, why};
use std::io::IsTerminal;
use std::path::PathBuf;

mod full;

pub use full::Woke;

/// The frames the spinner turns through. The view indexes this slice
/// directly, so every frame has to draw something: a blank in the cycle
/// blanks the row once a turn, which reads as a flash rather than as motion.
pub const SPINNER_FRAMES: &[&str] = &["⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"];
/// The same frames for indicatif, which takes the last string it is handed as
/// the one it leaves on the line when the bar stops. The blank on the end is
/// that parting frame, so a finished step erases its spinner; it is not part
/// of the animation, and handing it to the view would flash the row.
pub fn spinner_ticks() -> Vec<&'static str> {
    SPINNER_FRAMES.iter().copied().chain([" "]).collect()
}

/// Whether output has a terminal to draw on. Asked before the Ui exists, by
/// a run deciding whether it has a screen to open.
pub fn on_a_terminal() -> bool {
    std::io::stdout().is_terminal()
}

pub fn fmt_dur(s: u64) -> String {
    if s >= 3600 {
        format!("{}h{:02}m", s / 3600, (s % 3600) / 60)
    } else if s >= 60 {
        format!("{}m{:02}s", s / 60, s % 60)
    } else {
        format!("{s}s")
    }
}

pub fn cost_str(cost: Option<f64>) -> String {
    match cost {
        Some(c) if c >= 0.0 => format!("${c:.2}"),
        _ => "-".into(),
    }
}

/// "1 PR" / "3 PRs". Every count the user reads goes through here: "1 PR(s)"
/// is the shape that made someone stop and reread the line.
pub fn count(n: usize, singular: &str) -> String {
    if n == 1 { format!("{n} {singular}") } else { format!("{n} {singular}s") }
}

/// The pass header. The concurrency is only worth saying when it actually
/// holds reviews back -- "1 PR, 2 at a time" describes nothing.
fn pass_headline(reviews: usize, tasks: usize, jobs_max: u32) -> String {
    let subject = match (reviews, tasks) {
        (_, 0) => format!("reviewing {}", count(reviews, "PR")),
        (0, _) => format!("running {}", count(tasks, "task")),
        _ => format!("reviewing {} and running {}", count(reviews, "PR"), count(tasks, "task")),
    };
    if (jobs_max as usize) < reviews + tasks {
        format!("{subject}, {jobs_max} at a time")
    } else {
        subject
    }
}

/// The RESULT cell, in the summary and the view. A reaped job's review
/// already exited; only its verdict readback is still in flight, and an
/// interrupt summary must not report it as a review that was cut short.
pub(crate) fn result_label(job: &Job) -> String {
    match job.state {
        JobState::Done => "done".to_string(),
        JobState::Timeout => "timed out".to_string(),
        JobState::Failed => format!("failed ({})", job.outcome()),
        JobState::Queued => "queued".to_string(),
        JobState::Running if job.reaped => "finishing".to_string(),
        JobState::Running => "running".to_string(),
    }
}

/// The FINDINGS cell: only the non-zero buckets, "none" for a clean report,
/// "-" when the review never said.
pub fn findings_label(trailer: Option<&Trailer>) -> String {
    let Some(f) = trailer.and_then(|t| t.findings.as_ref()) else {
        return "-".into();
    };
    let mut parts = Vec::new();
    for (n, word) in [(f.must_fix, "must-fix"), (f.should_fix, "should-fix"), (f.polish, "polish")] {
        match n {
            Some(0) | None => {}
            Some(n) => parts.push(format!("{n} {word}")),
        }
    }
    if parts.is_empty() {
        // "none" is a claim about all three buckets; a report that omitted
        // one has not made it.
        if [f.must_fix, f.should_fix, f.polish].iter().all(|n| *n == Some(0)) {
            "none".into()
        } else {
            "-".into()
        }
    } else {
        parts.join(", ")
    }
}

/// What landed on the PR, or the fact that nothing did. "-" read as a verdict
/// of its own -- a refusal to approve -- when it only ever meant "no review
/// was posted".
pub fn verdict_label(verdict: Option<&str>) -> &str {
    verdict.filter(|v| !v.is_empty()).unwrap_or("nothing posted")
}

/// One panelist, in words: "codex (gpt-5.5) 3 findings, top MEDIUM".
pub fn panelist_label(p: &Panelist) -> String {
    let name = p.name.as_deref().unwrap_or("?");
    let model = p.model.as_deref().unwrap_or("unknown");
    let mut s = format!("{name} ({model})");
    if p.ok == Some(false) {
        s.push_str(" failed");
        return s;
    }
    match p.findings {
        Some(0) => s.push_str(" clean"),
        Some(1) => s.push_str(" 1 finding"),
        Some(n) => s.push_str(&format!(" {n} findings")),
        None => {}
    }
    if let Some(top) = p.top.as_deref()
        && p.findings.unwrap_or(0) > 0
    {
        s.push_str(&format!(", top {top}"));
    }
    s
}

/// Failed reviews and why the harness said they failed, one line per
/// distinct reason. Identical reasons group -- a usage limit hits every PR
/// in the pass, and reading the same notice five times is noise.
/// The same block off a TTY: the header and its bullets, indented under the
/// eight-column label that opens every plain line.
fn plain_why_lines(job: &Job) -> Vec<String> {
    let clean = quorum::clean_but_short(job).map(|s| quorum::line(&s));
    let reasons = why::reasons(job);
    let block = (!reasons.is_empty()).then(|| std::iter::once(why::HEADER.to_string()).chain(reasons));
    clean
        .into_iter()
        .chain(block.into_iter().flatten())
        .map(|line| format!("        {line}"))
        .collect()
}

/// The same block in the end-of-run summary, where the PR number has to ride
/// the header: the table above it holds every PR, not just this one.
///
/// A clean review that too few of the panel answered says so first: it is
/// the one unapproved PR a reader may only need to look over.
fn summary_why_lines(job: &Job) -> Vec<String> {
    let clean = quorum::clean_but_short(job).map(|s| format!("#{} {}", job.pr, quorum::line(&s)));
    let reasons = why::reasons(job);
    let block = (!reasons.is_empty()).then(|| {
        std::iter::once(format!("#{} {}", job.pr, why::HEADER)).chain(reasons.into_iter().map(|r| format!("  {r}")))
    });
    clean.into_iter().chain(block.into_iter().flatten()).collect()
}

pub fn error_lines(jobs: &[Job]) -> Vec<String> {
    let mut groups: Vec<(&str, Vec<u64>)> = Vec::new();
    for job in jobs {
        if job.state != JobState::Failed {
            continue;
        }
        let Some(why) = job.error.as_deref() else { continue };
        match groups.iter_mut().find(|(reason, _)| *reason == why) {
            Some((_, prs)) => prs.push(job.pr),
            None => groups.push((why, vec![job.pr])),
        }
    }
    groups
        .into_iter()
        .map(|(why, prs)| {
            let names: Vec<String> = prs.iter().map(|n| format!("#{n}")).collect();
            format!("error {}: {}", names.join(" "), why)
        })
        .collect()
}

fn opt_label(v: Option<&str>) -> String {
    v.filter(|s| !s.is_empty()).unwrap_or("-").to_string()
}

#[derive(Default)]
pub struct Ui {
    /// PRs a person asked to have reviewed now that the current pass could
    /// not start, for the loop to put in the next one.
    requests: Vec<u64>,
    /// Tasks a person asked for on their own PRs, waiting for a pass to
    /// start them. Apart from `requests`: a task never enters the review
    /// queue, its caps or its watch list.
    tasks: Vec<crate::task::Request>,
    /// Whether a person asked the run to start or stop looking for work,
    /// waiting for the loop to reach a point where it can.
    watch_toggle: Option<bool>,
    /// What a person typed as the new focus, waiting to be taken.
    focus_change: Option<Option<String>>,
    /// The full-screen view, while it is up.
    screen: Option<crate::tui::Screen>,
    /// The run directory, once the full-screen view has opened. The summary
    /// at the end covers the whole run, and says where its files are.
    run_root: Option<PathBuf>,
    /// Every review of the run, kept for the full-screen view and the
    /// summary at its end. Empty when the view never opened.
    archive: Vec<crate::tui::Archived>,
    /// The pass directory the current or last pass writes to.
    pass_dir: PathBuf,
    /// A line the full-screen run's summary ends with.
    final_notes: Vec<String>,
}

impl Ui {
    pub fn new() -> Ui {
        Ui::default()
    }

    /// Whether the pass should tick: redraw ten times a second and follow
    /// each running review's activity. Only the full-screen view shows
    /// either; the plain lines only need to hear when something changed.
    pub fn ticking(&self) -> bool {
        self.screen.is_some()
    }

    /// Keep a request for the next pass. Asked twice is asked once.
    pub fn request(&mut self, pr: u64) {
        if !self.requests.contains(&pr) {
            self.requests.push(pr);
        }
    }

    /// Every request kept since the last call, in the order asked.
    pub fn take_requests(&mut self) -> Vec<u64> {
        std::mem::take(&mut self.requests)
    }

    /// Keep a task for the next pass. A second ask for a PR that already
    /// has one waiting replaces it: the last key pressed is what was meant.
    pub fn run_task(&mut self, request: crate::task::Request) {
        self.tasks.retain(|t| t.pr != request.pr);
        self.tasks.push(request);
    }

    /// Every task kept since the last call, in the order asked.
    pub fn take_tasks(&mut self) -> Vec<crate::task::Request> {
        std::mem::take(&mut self.tasks)
    }

    /// Whether a task is waiting for a pass, without taking it.
    pub fn has_tasks(&self) -> bool {
        !self.tasks.is_empty()
    }

    /// A note the user should see now: spawn failures, session fallbacks.
    /// The view flashes it; it also goes to stderr, which is the run log
    /// while the view is up.
    pub fn note(&mut self, note: String) {
        if let Some(screen) = &mut self.screen {
            screen.flash(note.clone());
        }
        eprintln!("{note}");
    }

    /// One plain line per state change. While the view is up these land in
    /// the run log, which is where fd 1 points.
    pub fn note_transition(&mut self, job: &Job) {
        let n = job.pr;
        // Who opened it, and whether this is a first look or a second one.
        // A log that only says "start #9" makes you open the PR to learn
        // either.
        let who = if job.author.is_empty() { String::new() } else { format!(" @{}", job.author) };
        match job.state {
            JobState::Running => {
                let verb = match job.task {
                    Task::Review if job.resume => "rechecking",
                    task => task.doing(),
                };
                let via = if job.fell_back() { format!(" with {}", job.orchestrator.backend) } else { String::new() };
                println!("start   #{n}{who} ({verb}{via})");
            }
            JobState::Done => {
                println!("done    #{n} ({})", fmt_dur(job.elapsed_secs));
                // Indented under the line they explain, on the same stream,
                // so a cron log keeps each reason with its PR.
                for line in plain_why_lines(job) {
                    println!("{line}");
                }
            }
            JobState::Failed => match &job.error {
                // The reason rides the same line, not a note below it: a
                // failure the reader has to decode is the gap this closes.
                Some(why) => {
                    println!("FAILED  #{n} ({}, {}): {why}", job.outcome(), fmt_dur(job.elapsed_secs))
                }
                None => println!("FAILED  #{n} ({}, {})", job.outcome(), fmt_dur(job.elapsed_secs)),
            },
            JobState::Timeout => println!("TIMEOUT #{n} ({})", fmt_dur(job.elapsed_secs)),
            JobState::Queued => {}
        }
    }

    /// The orchestrator gave up and the fallback is taking the review over.
    /// Not a finish: this PR still has a review to come. Called after the
    /// job was reset for the retry, so the job names the stand-in and its
    /// first attempt names the failure.
    pub fn note_retry(&mut self, job: &Job) {
        let Some(first) = &job.first_attempt else { return };
        let why = match &first.error {
            // The harness's own words, where it gave any: "exit 10" alone
            // does not separate a usage limit from an outage.
            Some(why) => format!("{} {}: {why}", first.orchestrator.label(), first.outcome()),
            None => format!("{} {}", first.orchestrator.label(), first.outcome()),
        };
        println!("RETRY   #{} ({why} · retrying with {})", job.pr, job.orchestrator.label());
    }

    /// Print the pass header.
    pub fn begin_pass(&mut self, reviews: usize, tasks: usize, jobs_max: u32, pass_dir: &std::path::Path) {
        self.pass_dir = pass_dir.to_path_buf();
        println!("{}", pass_headline(reviews, tasks, jobs_max));
        println!("logs: {}\n", pass_dir.display());
    }

    /// Redraw the view, if it is up. Called on the pool's tick, which is
    /// also what turns the spinner.
    pub fn render(&mut self, jobs: &[Job]) {
        if let Some(screen) = &mut self.screen {
            screen.draw(jobs, &self.pass_dir, &self.archive);
        }
    }

    /// What the keys pressed since the last tick ask for. The loop's own
    /// requests are kept here; the rest are handed back for the pass to act
    /// on. Nothing without the view: there is nothing to press a key at.
    pub fn poll_input(&mut self) -> Vec<Action> {
        // The watch key is the loop's, not the pass's: kept here until the
        // loop reaches a point where it can change what it does.
        let actions = self.screen.as_mut().map(Screen::events).unwrap_or_default();
        actions
            .into_iter()
            .filter(|action| match action {
                Action::Watch(on) => {
                    self.watch_toggle = Some(*on);
                    false
                }
                // The focus reaches the pass itself: a review that has not
                // started yet is told what the person just typed.
                Action::Focus(focus) => {
                    self.focus_change = Some(focus.clone());
                    false
                }
                _ => true,
            })
            .collect()
    }

    /// The summary of a pass or a run: one aligned table, then the lines
    /// that explain it. The same text on a terminal and in a log, so what a
    /// person reads is what the suite pins.
    pub fn print_summary(&self, jobs: &[Job], pass_dir: &std::path::Path) {
        let mut rows: Vec<Vec<String>> = vec![
            ["PR", "RESULT", "VERDICT", "RISK", "FINDINGS", "TIME", "COST", "MODEL", "SESSION"]
                .map(String::from)
                .to_vec(),
        ];
        for job in jobs {
            rows.push(vec![
                format!("#{}", job.pr),
                result_label(job),
                verdict_label(job.verdict.as_deref()).to_string(),
                opt_label(job.trailer.as_ref().and_then(|t| t.risk.as_deref())),
                findings_label(job.trailer.as_ref()),
                fmt_dur(job.elapsed_secs),
                cost_str(job.cost),
                opt_label(job.model.as_deref()),
                job.sid.clone().unwrap_or_else(|| "-".into()),
            ]);
        }
        println!();
        print!("{}", align(&rows));
        for job in jobs {
            for line in summary_why_lines(job) {
                println!("{line}");
            }
        }
        for job in jobs {
            if let Some(t) = &job.trailer
                && !t.panel.is_empty()
            {
                let panelists: Vec<String> = t.panel.iter().map(panelist_label).collect();
                println!("panel #{}: {}", job.pr, panelists.join("; "));
            }
        }
        for line in jobs.iter().filter_map(fallback_line) {
            println!("{line}");
        }
        for line in error_lines(jobs) {
            println!("{line}");
        }
        println!("\nlogs: {}", pass_dir.display());
        for hint in reopen_hints(jobs) {
            println!("{hint}");
        }
    }
}

/// What the summary owes about a review the fallback took over: which
/// orchestrator gave up and how, and whether the stand-in finished the job.
/// None for a review that ran on its first attempt.
pub(crate) fn fallback_line(job: &Job) -> Option<String> {
    let first = job.first_attempt.as_ref()?;
    let to = job.orchestrator.label();
    // The harness's own words where it gave any. `error_lines` reports a
    // reason only for a job that ended Failed, so a review the fallback
    // rescued would otherwise lose the reason it was rescued from -- which
    // is the case most worth reading, because nothing else records it.
    let from = match &first.error {
        Some(why) => format!("{} failed ({}: {why})", first.orchestrator.label(), first.outcome()),
        None => format!("{} failed ({})", first.orchestrator.label(), first.outcome()),
    };
    Some(match job.state {
        JobState::Done => format!("PR #{}: {from}; reviewed with {to} instead", job.pr),
        JobState::Timeout => format!("PR #{}: {from}, then {to} timed out", job.pr),
        _ => format!("PR #{}: {from}, then {to} failed ({})", job.pr, job.outcome()),
    })
}

/// How to reopen the reviews in this summary, one line per backend that ran
/// one. Every session id is printed against the CLI that can take it: a
/// codex thread id handed to `claude --resume` opens nothing.
fn reopen_hints(jobs: &[Job]) -> Vec<String> {
    let mut commands: Vec<(&str, &'static str)> = Vec::new();
    for job in jobs.iter().filter(|j| j.sid.is_some()) {
        let entry = (job.orchestrator.backend.as_str(), job.orchestrator.reopen_command());
        if !commands.contains(&entry) {
            commands.push(entry);
        }
    }
    if commands.is_empty() {
        commands.push(("claude", crate::orchestrator::Orchestrator::claude().reopen_command()));
    }
    commands
        .iter()
        .enumerate()
        .map(|(i, (backend, cmd))| {
            if i == 0 {
                format!("reopen any review with: {cmd}")
            } else {
                format!("  or, for a {backend} review: {cmd}")
            }
        })
        .collect()
}

/// A `?` that returns early, or a panic that unwinds, must not leave the
/// terminal in raw mode on the alternate screen.
impl Drop for Ui {
    fn drop(&mut self) {
        self.shutdown();
    }
}

/// What `column -t` did, natively: pad each column to its widest cell with a
/// two-space gutter, last column ragged. `column` lives in util-linux and the
/// boxes this tool is built for -- slim CI images -- routinely ship without
/// it; a summary must never die on formatting. Widths are display widths,
/// not byte counts: the verdict/risk/model columns carry agent-authored text
/// that may be multibyte.
pub fn align(rows: &[Vec<String>]) -> String {
    let cols = rows.iter().map(|r| r.len()).max().unwrap_or(0);
    let mut widths = vec![0usize; cols];
    for row in rows {
        for (i, cell) in row.iter().enumerate() {
            widths[i] = widths[i].max(console::measure_text_width(cell));
        }
    }
    let mut out = String::new();
    for row in rows {
        let mut line = String::new();
        for (i, cell) in row.iter().enumerate() {
            line.push_str(cell);
            if i + 1 < row.len() {
                let pad = widths[i] - console::measure_text_width(cell) + 2;
                line.push_str(&" ".repeat(pad));
            }
        }
        out.push_str(line.trim_end());
        out.push('\n');
    }
    out
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::job::Job;
    use crate::report::parse_trailer;

    #[test]
    fn durations_read_as_written() {
        assert_eq!(fmt_dur(3), "3s");
        assert_eq!(fmt_dur(63), "1m03s");
        assert_eq!(fmt_dur(252), "4m12s");
        assert_eq!(fmt_dur(3600), "1h00m");
        assert_eq!(fmt_dur(3900), "1h05m");
    }

    #[test]
    fn a_review_the_fallback_took_over_says_so_in_the_summary() {
        use crate::orchestrator::Orchestrator;
        let mut job = Job::new(9);
        assert_eq!(fallback_line(&job), None, "a first-attempt review has nothing to add");
        job.exit_code = Some(10);
        job.error = Some("usage limit reached".into());
        job.retry_under(Orchestrator::parse("codex").unwrap());
        // The reason travels with the attempt that failed, and is cleared
        // from the job: it does not belong to the review that succeeded.
        assert!(job.error.is_none());
        job.state = JobState::Done;
        // On a rescued review this line is the only record of why the
        // first attempt gave up -- error_lines reports a reason only for a
        // job that ended Failed, and this one ended Done.
        assert_eq!(
            fallback_line(&job).unwrap(),
            "PR #9: claude failed (exit 10: usage limit reached); reviewed with codex instead"
        );
        job.first_attempt.as_mut().unwrap().error = None;
        assert_eq!(
            fallback_line(&job).unwrap(),
            "PR #9: claude failed (exit 10); reviewed with codex instead"
        );
        job.state = JobState::Failed;
        job.exit_code = Some(10);
        assert_eq!(
            fallback_line(&job).unwrap(),
            "PR #9: claude failed (exit 10), then codex failed (exit 10)"
        );
        job.state = JobState::Timeout;
        assert_eq!(
            fallback_line(&job).unwrap(),
            "PR #9: claude failed (exit 10), then codex timed out"
        );
    }

    #[test]
    fn reopen_hints_name_the_cli_that_can_take_each_session() {
        use crate::orchestrator::Orchestrator;
        // No sessions at all: the claude line still prints, so the reader
        // learns how a review is reopened even when none of these can be.
        assert_eq!(reopen_hints(&[Job::new(9)]), vec!["reopen any review with: claude --resume <SESSION>"]);
        let mut claude = Job::new(9);
        claude.sid = Some("7442b624-5cba-5d44-ae67-9c390cfe70a1".into());
        let mut codex = Job::new(8);
        codex.orchestrator = Orchestrator::parse("codex").unwrap();
        codex.sid = Some("0199c4a1-4a2b-7c3d-8e4f-5a6b7c8d9e0f".into());
        assert_eq!(
            reopen_hints(&[claude, codex]),
            vec![
                "reopen any review with: claude --resume <SESSION>",
                "  or, for a codex review: codex resume <SESSION>",
            ]
        );
        // A run that was all codex leads with codex.
        let mut only = Job::new(8);
        only.orchestrator = Orchestrator::parse("codex").unwrap();
        only.sid = Some("0199c4a1-4a2b-7c3d-8e4f-5a6b7c8d9e0f".into());
        assert_eq!(reopen_hints(&[only]), vec!["reopen any review with: codex resume <SESSION>"]);
    }

    #[test]
    fn costs() {
        assert_eq!(cost_str(Some(0.42)), "$0.42");
        assert_eq!(cost_str(Some(1.005)), "$1.00");
        assert_eq!(cost_str(None), "-");
    }

    #[test]
    fn summary_alignment() {
        let rows = vec![
            vec!["PR".into(), "RESULT".into(), "SESSION".into()],
            vec!["#9".into(), "done".into(), "abc".into()],
            vec!["#123".into(), "failed (no result)".into(), "-".into()],
        ];
        let out = align(&rows);
        let lines: Vec<&str> = out.lines().collect();
        assert_eq!(lines[0], "PR    RESULT              SESSION");
        assert_eq!(lines[1], "#9    done                abc");
        assert_eq!(lines[2], "#123  failed (no result)  -");
    }

    #[test]
    fn alignment_pads_by_display_width_not_bytes() {
        // "LÅG" is three columns wide but four bytes; byte padding would
        // shift every later column of its row.
        let rows = vec![
            vec!["RISK".into(), "NEXT".into(), "END".into()],
            vec!["LÅG".into(), "x".into(), "y".into()],
        ];
        let lines = align(&rows);
        let lines: Vec<&str> = lines.lines().collect();
        assert_eq!(lines[0], "RISK  NEXT  END");
        assert_eq!(lines[1], "LÅG   x     y");
    }

    #[test]
    fn a_reaped_running_job_reads_as_finishing() {
        let mut job = Job::new(9);
        job.state = JobState::Running;
        assert_eq!(result_label(&job), "running");
        job.reaped = true;
        assert_eq!(result_label(&job), "finishing");
    }

    #[test]
    fn transition_outcomes_render_in_the_failed_line() {
        let mut job = Job::new(9);
        job.exit_code = None;
        assert_eq!(job.outcome(), "no result");
        job.exit_code = Some(10);
        assert_eq!(format!("FAILED  #{} ({}, {})", job.pr, job.outcome(), fmt_dur(3)), "FAILED  #9 (exit 10, 3s)");
    }

    #[test]
    fn identical_failure_reasons_group_into_one_line() {
        let failed = |pr: u64, why: Option<&str>| {
            let mut job = Job::new(pr);
            job.state = JobState::Failed;
            job.exit_code = Some(10);
            job.error = why.map(String::from);
            job
        };
        let limit = "You've hit your session limit · resets 12pm (America/New_York)";
        let mut jobs = vec![
            failed(1759, Some(limit)),
            failed(1756, Some(limit)),
            failed(8, Some("API Error: 500")),
            failed(7, None),
        ];
        jobs.push({
            let mut done = Job::new(6);
            done.state = JobState::Done;
            done
        });
        let lines = error_lines(&jobs);
        assert_eq!(
            lines,
            vec![
                "error #1759 #1756: You've hit your session limit · resets 12pm (America/New_York)"
                    .to_string(),
                "error #8: API Error: 500".to_string(),
            ]
        );
    }

    #[test]
    fn findings_cells() {
        assert_eq!(findings_label(None), "-");
        let t = parse_trailer("```autoreview\n{\"findings\":{\"must_fix\":1,\"should_fix\":0,\"polish\":2}}\n```");
        assert_eq!(findings_label(t.as_ref()), "1 must-fix, 2 polish");
        let clean = parse_trailer("```autoreview\n{\"findings\":{\"must_fix\":0,\"should_fix\":0,\"polish\":0}}\n```");
        assert_eq!(findings_label(clean.as_ref()), "none");
        let unknown = parse_trailer("```autoreview\n{\"decision\":\"approved\"}\n```");
        assert_eq!(findings_label(unknown.as_ref()), "-");
        // A report that omitted a bucket has not claimed "none".
        let partial = parse_trailer("```autoreview\n{\"findings\":{\"must_fix\":0}}\n```");
        assert_eq!(findings_label(partial.as_ref()), "-");
    }

    /// A review that commented on #9 over one blocker.
    fn unapproved_job() -> Job {
        let trailer = parse_trailer(
            "```autoreview\n{\"decision\":\"commented\",\"blockers\":[{\"severity\":\"MEDIUM\",\"domain\":\"money\",\"reversible\":false,\"location\":\"src/pay.rs:88\",\"gist\":\"a retried checkout charges twice\"}]}\n```",
        );
        let mut job = Job::new(9);
        job.verdict = Some("commented".into());
        job.trailer = trailer;
        job
    }

    #[test]
    fn the_plain_block_sits_under_the_line_it_explains() {
        // Eight columns, the width of the "done    " label every plain line
        // opens with, so the reason hangs under the PR it belongs to.
        assert_eq!(
            plain_why_lines(&unapproved_job()),
            vec![
                "        not approved yet because:".to_string(),
                "        - [MEDIUM] (money, irreversible) src/pay.rs:88 — a retried checkout charges twice"
                    .to_string(),
            ]
        );
    }

    #[test]
    fn the_summary_block_names_its_pr() {
        // The table above it holds every PR of the pass, so a bare header
        // would belong to none of them.
        let lines = summary_why_lines(&unapproved_job());
        assert_eq!(lines[0], "#9 not approved yet because:");
        assert!(lines[1].starts_with("  - [MEDIUM] (money, irreversible)"));
    }

    #[test]
    fn an_approved_pr_draws_no_block_on_any_path() {
        let mut job = unapproved_job();
        job.verdict = Some("approved".into());
        assert!(plain_why_lines(&job).is_empty());
        assert!(summary_why_lines(&job).is_empty());
    }

    #[test]
    fn every_spinner_frame_draws_something() {
        // The view walks these frames in order, so a blank one empties the
        // lead for a tick and the row flashes. The blank belongs only at the
        // end of the indicatif ticks, where it is what a finished bar leaves
        // behind.
        for frame in SPINNER_FRAMES {
            assert!(!frame.trim().is_empty(), "blank frame {frame:?} in the cycle");
        }
        let ticks = spinner_ticks();
        assert_eq!(ticks.len(), SPINNER_FRAMES.len() + 1);
        assert_eq!(ticks.last(), Some(&" "));
    }

    #[test]
    fn counts_read_as_english() {
        assert_eq!(count(1, "PR"), "1 PR");
        assert_eq!(count(0, "PR"), "0 PRs");
        assert_eq!(count(3, "review"), "3 reviews");
    }

    #[test]
    fn the_pass_header_only_claims_a_limit_that_binds() {
        assert_eq!(pass_headline(1, 0, 2), "reviewing 1 PR");
        assert_eq!(pass_headline(2, 0, 2), "reviewing 2 PRs");
        assert_eq!(pass_headline(5, 0, 2), "reviewing 5 PRs, 2 at a time");
        // A task is not a review, and the headline does not call it one.
        assert_eq!(pass_headline(0, 1, 2), "running 1 task");
        assert_eq!(pass_headline(2, 1, 2), "reviewing 2 PRs and running 1 task, 2 at a time");
    }

    #[test]
    fn an_empty_verdict_says_nothing_landed() {
        // A bare "-" read as a verdict of its own; it never was one.
        assert_eq!(verdict_label(None), "nothing posted");
        assert_eq!(verdict_label(Some("")), "nothing posted");
        assert_eq!(verdict_label(Some("approved")), "approved");
    }

    #[test]
    fn panelist_lines() {
        let t = parse_trailer(
            "```autoreview\n{\"panel\":[{\"name\":\"codex\",\"model\":\"gpt-5.5\",\"ok\":true,\"findings\":3,\"top\":\"MEDIUM\"},{\"name\":\"claude\",\"model\":\"claude-opus-4.7\",\"ok\":true,\"findings\":0},{\"name\":\"opencode\",\"ok\":false}]}\n```",
        )
        .unwrap();
        assert_eq!(panelist_label(&t.panel[0]), "codex (gpt-5.5) 3 findings, top MEDIUM");
        assert_eq!(panelist_label(&t.panel[1]), "claude (claude-opus-4.7) clean");
        assert_eq!(panelist_label(&t.panel[2]), "opencode (unknown) failed");
    }
}
