//! The right pane: what the selected PR's review is doing, why the PR is
//! waiting, and what its last review found.
//!
//! Built as plain lines and left to the paragraph to wrap, so a review's text
//! reads at the pane's width. Everything that came from a model or a person
//! is sanitized on the way in: a review quoting an escape sequence must not
//! repaint the screen it is shown on.

use super::actions;
use super::model::{Review, Row, Section, Wait};
use crate::job::{Job, JobState};
use crate::report::sanitize_for_display;
use crate::ui::{cost_str, count, fallback_line, findings_label, fmt_dur, result_label, verdict_label};
use crate::{rundir, why};
use ratatui::style::{Color, Modifier, Style, Stylize};
use ratatui::text::{Line, Span};
use std::path::Path;

/// The columns a field's name takes.
const KEY_WIDTH: usize = 10;

pub struct Context<'a> {
    pub now: i64,
    pub repo_root: &'a Path,
    /// The last review's text, one entry a line, as the screen read it.
    pub review: Option<&'a [String]>,
    /// When the loop looks for work again, while it is waiting.
    pub next_check: Option<i64>,
    /// Whether `R` would do anything, so the pane only offers it then.
    pub can_request: bool,
}

pub fn lines(row: &Row, ctx: &Context) -> Vec<Line<'static>> {
    let mut out = title(row);
    match (row.section, row.live) {
        (Section::Running, Some(job)) => out.extend(running(job, ctx.now)),
        (Section::Queued, Some(job)) => out.push(queued(job)),
        (Section::Waiting, _) => out.extend(waiting(row.wait, ctx)),
        _ => {}
    }
    if let Some(last) = row.last {
        out.push(Line::default());
        out.extend(review(last, ctx));
    } else if row.section != Section::Running {
        out.push(Line::default());
        out.push(Line::from("no review has finished for this PR in this run").dark_gray());
    }
    out
}

fn heading(text: &str) -> Line<'static> {
    Line::from(Span::from(text.to_string()).add_modifier(Modifier::BOLD).fg(Color::DarkGray))
}

fn field(key: &str, value: impl Into<String>, style: Style) -> Line<'static> {
    Line::from(vec![
        Span::from(format!("{key:<KEY_WIDTH$}")).dark_gray(),
        Span::styled(sanitize_for_display(&value.into()), style),
    ])
}

fn title(row: &Row) -> Vec<Line<'static>> {
    let mut out = vec![Line::from(vec![
        Span::from(format!("#{}", row.pr)).cyan().bold(),
        Span::raw(" "),
        Span::from(sanitize_for_display(row.title)).bold(),
    ])];
    if !row.author.is_empty() {
        out.push(Line::from(format!("@{}", sanitize_for_display(row.author))).dark_gray());
    }
    out.push(Line::default());
    out
}

fn running(job: &Job, now: i64) -> Vec<Line<'static>> {
    let (verb, secs) = if job.reaped {
        ("finishing", job.elapsed_secs)
    } else {
        (if job.resume { "rechecking" } else { "reviewing" }, job.started.map_or(0, |s| s.elapsed().as_secs()))
    };
    let via = match &job.first_attempt {
        Some(first) => format!(" · {} after {} failed", job.orchestrator.label(), first.orchestrator.label()),
        None => format!(" · {}", job.orchestrator.label()),
    };
    let tail = &job.activity;
    let counts = if tail.turns == 0 {
        "waiting for the first turn".to_string()
    } else {
        format!("{} · {}", count(tail.turns as usize, "turn"), count(tail.tool_calls as usize, "tool call"))
    };
    let mut out = vec![
        Line::from(format!("{verb} {}{via}", fmt_dur(secs))).magenta(),
        Line::from(sanitize_for_display(&tail.source_label())).dark_gray(),
        Line::from(counts).dark_gray(),
    ];
    if !tail.events.is_empty() {
        out.push(Line::default());
        out.push(heading("ACTIVITY"));
    }
    for event in &tail.events {
        let when = match event.at {
            Some(at) => format!("{:>9}", format!("{} ago", fmt_dur(now.saturating_sub(at).max(0) as u64))),
            None => " ".repeat(9),
        };
        let name = event.tool.clone().unwrap_or_else(|| "said".to_string());
        out.push(Line::from(vec![
            Span::from(when).dark_gray(),
            Span::raw("  "),
            Span::from(format!("{name:<8}")).cyan(),
            Span::raw(" "),
            Span::from(event.what.clone()),
        ]));
    }
    out
}

fn queued(job: &Job) -> Line<'static> {
    match &job.first_attempt {
        Some(first) => Line::from(format!(
            "queued · {} failed; waiting for a slot to retry with {}",
            first.orchestrator.label(),
            job.orchestrator.label()
        ))
        .yellow(),
        None => Line::from("queued · starts when a review slot frees").dark_gray(),
    }
}

/// Why a PR is waiting, in the words the log would use.
fn wait_reason(wait: Wait, ctx: &Context) -> String {
    let next = ctx
        .next_check
        .map(|at| format!("; next check in {}", fmt_dur(at.saturating_sub(ctx.now).max(0) as u64)))
        .unwrap_or_default();
    match wait {
        Wait::Checks(ci) => format!("held until its checks pass (checks {}){next}", ci.reason()),
        Wait::Stacked(on) => format!("held until #{} lands ({})", on.pr, on.detail()),
        Wait::Capped => "has had every review this run gives one PR (--max-passes)".into(),
        Wait::Resting { until } => format!(
            "resting after its review; it may be reviewed again in {}",
            fmt_dur((until as i64).saturating_sub(ctx.now).max(0) as u64)
        ),
        Wait::Quiet => format!("nothing new since its last review{next}"),
        Wait::Next => format!("reviewed at the next check{next}"),
    }
}

fn waiting(wait: Option<Wait>, ctx: &Context) -> Vec<Line<'static>> {
    let Some(wait) = wait else { return Vec::new() };
    let mut out = vec![Line::from(wait_reason(wait, ctx)).yellow()];
    if ctx.can_request {
        out.push(Line::from("R reviews it now").dark_gray());
    }
    out
}

fn result_style(job: &Job) -> Style {
    match job.state {
        JobState::Done => Style::default().fg(Color::Green),
        JobState::Timeout => Style::default().fg(Color::Yellow),
        _ => Style::default().fg(Color::Red),
    }
}

fn verdict_style(verdict: Option<&str>) -> Style {
    match verdict {
        Some("approved") => Style::default().fg(Color::Green).add_modifier(Modifier::BOLD),
        Some("changes requested") => Style::default().fg(Color::Yellow),
        Some("commented") => Style::default().fg(Color::Cyan),
        _ => Style::default().fg(Color::DarkGray),
    }
}

fn review(last: Review, ctx: &Context) -> Vec<Line<'static>> {
    let job = last.job;
    let plain = Style::default();
    let mut out = vec![heading("LAST REVIEW")];
    out.push(field("result", result_label(job), result_style(job)));
    if let Some(error) = &job.error {
        out.push(field("error", error.clone(), Style::default().fg(Color::Red)));
    }
    out.push(field("verdict", verdict_label(job.verdict.as_deref()), verdict_style(job.verdict.as_deref())));
    if let Some(risk) = job.trailer.as_ref().and_then(|t| t.risk.as_deref()) {
        out.push(field("risk", risk, plain));
    }
    out.push(field("findings", findings_label(job.trailer.as_ref()), plain));
    let mut spent = vec![fmt_dur(job.elapsed_secs), cost_str(job.cost)];
    spent.extend(job.model.clone());
    out.push(field("spent", spent.join(" · "), plain));
    if let Some(line) = fallback_line(job) {
        out.push(field("fallback", line, Style::default().fg(Color::Yellow)));
    }
    if let Some(sid) = &job.sid {
        out.push(field("session", sid.clone(), plain));
    }
    // The command on a line of its own, so it wraps at the pane's edge
    // rather than under the field names, and copies as one piece.
    if let Ok(line) = actions::resume_line(job, ctx.repo_root) {
        out.push(field("resume", "r opens it in a new tab, or run:", Style::default().fg(Color::DarkGray)));
        out.push(Line::from(line));
    }
    if job.state != JobState::Done {
        let log = rundir::log_file(last.pass_dir, job.pr);
        out.push(field("log", log.display().to_string(), plain));
    }
    let reasons = why::reasons(job);
    if !reasons.is_empty() {
        out.push(Line::default());
        out.push(Line::from(why::HEADER).yellow());
        out.extend(reasons.into_iter().map(|r| Line::from(format!("  {}", sanitize_for_display(&r)))));
    }
    if job.state == JobState::Done {
        out.push(Line::default());
        out.push(heading("REVIEW"));
        match ctx.review {
            Some(text) => out.extend(text.iter().map(|l| review_line(l))),
            None => {
                let path = rundir::review_file(last.pass_dir, job.pr);
                out.push(Line::from(format!("no review text at {}", path.display())).dark_gray());
            }
        }
    }
    out
}

/// One line of a review's markdown, with its headings picked out. The text
/// was sanitized and its tabs expanded when it was read.
fn review_line(line: &str) -> Line<'static> {
    if line.starts_with('#') {
        Line::from(line.to_string()).bold()
    } else if line.trim_start().starts_with("```") {
        Line::from(line.to_string()).dark_gray()
    } else {
        Line::from(line.to_string())
    }
}

#[cfg(test)]
mod tests {
    use super::super::model::{self, Archived, Sources};
    use super::*;
    use crate::activity::Tail;
    use crate::ci::Ci;
    use crate::report::{Blocker, Trailer};
    use std::collections::HashMap;
    use std::path::PathBuf;

    fn text(lines: &[Line]) -> String {
        lines
            .iter()
            .map(|l| l.spans.iter().map(|s| s.content.as_ref()).collect::<String>())
            .collect::<Vec<_>>()
            .join("\n")
    }

    fn ctx<'a>(review: Option<&'a [String]>) -> Context<'a> {
        Context { now: 1_000, repo_root: Path::new("/src/app"), review, next_check: Some(1_090), can_request: true }
    }

    fn reviewed() -> Job {
        let mut job = Job::new(9);
        job.state = JobState::Done;
        job.title = "Fix the ledger".into();
        job.author = "alice".into();
        job.verdict = Some("changes requested".into());
        job.sid = Some("7442b624-5cba-5d44-ae67-9c390cfe70a1".into());
        job.cost = Some(0.42);
        job.elapsed_secs = 192;
        job.trailer = Some(Trailer {
            risk: Some("MEDIUM".into()),
            blockers: vec![Blocker { gist: Some("a retried checkout charges twice".into()), ..Blocker::default() }],
            ..Trailer::default()
        });
        job
    }

    fn draw(archive: &[Archived], waiting: &[(u64, Wait)], jobs: &[Job], ctx: &Context) -> String {
        let info = HashMap::new();
        let src = Sources { jobs, pass_dir: Path::new("/p2"), archive, waiting, info: &info };
        let rows = model::rows(&src);
        text(&lines(&rows[0], ctx))
    }

    #[test]
    fn a_finished_review_says_what_landed_and_how_to_reopen_it() {
        let archive = vec![Archived { job: reviewed(), pass_dir: PathBuf::from("/p1") }];
        let body = vec!["## Findings".to_string(), "- the retry path".to_string()];
        let out = draw(&archive, &[], &[], &ctx(Some(&body)));
        assert!(out.starts_with("#9 Fix the ledger\n@alice"), "{out}");
        assert!(out.contains("verdict   changes requested"), "{out}");
        assert!(out.contains("risk      MEDIUM"), "{out}");
        assert!(out.contains("spent     3m12s · $0.42"), "{out}");
        assert!(out.contains("resume    r opens it in a new tab, or run:\ncd /src/app && claude --resume 7442b624"), "{out}");
        assert!(out.contains(why::HEADER), "{out}");
        assert!(out.contains("REVIEW\n## Findings\n- the retry path"), "{out}");
    }

    #[test]
    fn a_review_with_no_text_says_where_it_looked() {
        let archive = vec![Archived { job: reviewed(), pass_dir: PathBuf::from("/p1") }];
        let out = draw(&archive, &[], &[], &ctx(None));
        assert!(out.contains("no review text at /p1/pr-9.review.md"), "{out}");
    }

    #[test]
    fn a_failed_review_names_its_error_and_log() {
        let mut job = reviewed();
        job.state = JobState::Failed;
        job.exit_code = Some(10);
        job.error = Some("You've hit your limit".into());
        let archive = vec![Archived { job, pass_dir: PathBuf::from("/p1") }];
        let out = draw(&archive, &[], &[], &ctx(None));
        assert!(out.contains("result    failed (exit 10)"), "{out}");
        assert!(out.contains("error     You've hit your limit"), "{out}");
        assert!(out.contains("log       /p1/pr-9.log"), "{out}");
        assert!(!out.contains("\nREVIEW\n"), "a failed review has no text to show: {out}");
    }

    #[test]
    fn a_waiting_pr_says_why_and_keeps_its_last_review() {
        let archive = vec![Archived { job: reviewed(), pass_dir: PathBuf::from("/p1") }];
        let waiting = vec![(9, Wait::Quiet)];
        let out = draw(&archive, &waiting, &[], &ctx(None));
        assert!(out.contains("nothing new since its last review; next check in 1m30s"), "{out}");
        assert!(out.contains("LAST REVIEW"), "{out}");
        let out = draw(&[], &[(9, Wait::Checks(Ci::Failing))], &[], &ctx(None));
        assert!(out.contains("held until its checks pass (checks failing)"), "{out}");
        assert!(out.contains("R reviews it now"), "{out}");
        assert!(out.contains("no review has finished for this PR"), "{out}");
        let ended = Context { can_request: false, ..ctx(None) };
        let out = draw(&[], &[(9, Wait::Next)], &[], &ended);
        assert!(out.contains("reviewed at the next check"), "{out}");
        assert!(!out.contains("R reviews it now"), "nothing would review it: {out}");
    }

    #[test]
    fn a_running_review_shows_what_it_is_doing() {
        let mut job = Job::new(9);
        job.state = JobState::Running;
        job.sid = Some("7442b624-5cba-5d44-ae67-9c390cfe70a1".into());
        let mut tail = Tail::transcript_at(PathBuf::from("/nonexistent"), 0);
        let line = serde_json::json!({
            "type": "assistant",
            "timestamp": "1970-01-01T00:15:40Z",
            "message": {"content": [{"type": "tool_use", "name": "Bash", "input": {"command": "cargo test"}}]}
        });
        tail.feed(&format!("{line}\n"));
        job.activity = tail;
        let out = draw(&[], &[], &[job], &ctx(None));
        assert!(out.contains("reviewing 0s · claude"), "{out}");
        assert!(out.contains("1 turn · 1 tool call"), "{out}");
        assert!(out.contains("ACTIVITY"), "{out}");
        assert!(out.contains("1m00s ago  Bash     cargo test"), "{out}");
    }

    #[test]
    fn a_review_cannot_draw_escape_sequences() {
        let mut job = reviewed();
        job.error = Some("\u{1b}]52;c;evil\u{7}".into());
        job.state = JobState::Failed;
        let archive = vec![Archived { job, pass_dir: PathBuf::from("/p1") }];
        let out = draw(&archive, &[], &[], &ctx(None));
        assert!(!out.contains('\u{1b}') && !out.contains('\u{7}'), "{out:?}");
    }
}
