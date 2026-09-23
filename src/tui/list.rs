//! The left pane: two lines per PR, and no headings.
//!
//! The first line is what the run is doing about the PR -- an icon for where
//! its reviews stand, the number plain (decision 0006), and one state word.
//! The second is whose work it is and what it is called, which is what a
//! person reads to decide whether to care. Two lines because a title beside
//! a state column leaves neither enough room in a pane this narrow.
//!
//! There are no section headings. The list is sorted so that what the run
//! will do soonest is at the top and what it will never do is at the bottom
//! (`model::priority`), and the state word says which is which.

use super::model::{Row, Section, Wait};
use super::text::pad;
use crate::ci::Ci;
use crate::job::{Job, JobState};
use crate::prlist::Decision;
use crate::report::sanitize_for_display;
use crate::ui::fmt_dur;
use ratatui::style::{Color, Modifier, Style, Stylize};
use ratatui::text::{Line, Span};

/// The lines each row draws.
pub const ROW_LINES: usize = 2;

/// The lines to draw, and the first line of the selected row.
pub fn lines(rows: &[Row], selected: Option<usize>, width: usize, spinner: &'static str, now: i64) -> (Vec<Line<'static>>, Option<usize>) {
    let mut out = Vec::new();
    let mut at = None;
    for (i, row) in rows.iter().enumerate() {
        let picked = selected == Some(i);
        if picked {
            at = Some(out.len());
        }
        out.push(head_line(row, picked, width, spinner, now));
        out.push(who_line(row, picked, width));
    }
    (out, at)
}

/// The icon at the head of a row: where the PR's reviews stand, whoever
/// wrote them. A review running now takes the spinner instead, because that
/// is the one thing more urgent than the verdict it is about to change.
///
/// It comes from the PR list, so a review this run just posted shows here
/// only once the list has been read again -- the same asymmetry the VERDICT
/// column keeps. The state word says what the run did; the icon says what
/// GitHub says about the PR.
fn icon(row: &Row, spinner: &'static str) -> Span<'static> {
    match row.section {
        Section::Running => Span::raw(spinner).magenta(),
        Section::Queued => Span::raw("·").dark_gray(),
        _ => match row.decision {
            Decision::Approved => Span::raw("✓").green(),
            Decision::ChangesRequested => Span::raw("✗").yellow(),
            Decision::None => Span::raw("○").dark_gray(),
        },
    }
}

/// A finished review's state, in a word or two.
fn finished_state(job: &Job) -> (String, Style) {
    let style = Style::default();
    match job.state {
        JobState::Done => match job.verdict.as_deref() {
            Some("approved") => ("approved".into(), style.fg(Color::Green)),
            Some("changes requested") => ("changes req".into(), style.fg(Color::Yellow)),
            Some("commented") => ("commented".into(), style.fg(Color::Cyan)),
            Some(other) if !other.is_empty() => (other.to_string(), style),
            _ => ("no verdict".into(), style.fg(Color::DarkGray)),
        },
        JobState::Timeout => ("timed out".into(), style.fg(Color::Yellow)),
        _ if job.stopped => ("stopped".into(), style.fg(Color::Red)),
        _ => ("failed".into(), style.fg(Color::Red)),
    }
}

fn wait_state(wait: Wait, now: i64) -> (String, Style) {
    let style = Style::default();
    match wait {
        Wait::Checks(Ci::Failing) => ("CI failing".into(), style.fg(Color::Red)),
        Wait::Checks(_) => ("CI pending".into(), style.fg(Color::Yellow)),
        Wait::Stacked(on) => (format!("on #{}", on.pr), style.fg(Color::Yellow)),
        Wait::Capped => ("capped".into(), style.fg(Color::DarkGray)),
        Wait::Resting { until } => {
            let left = (until as i64).saturating_sub(now).max(0) as u64;
            (format!("rest {}", fmt_dur(left)), style.fg(Color::DarkGray))
        }
        Wait::Quiet => ("quiet".into(), style.fg(Color::DarkGray)),
        Wait::Next => ("next pass".into(), style.fg(Color::DarkGray)),
        Wait::Seen => ("seen".into(), style.fg(Color::DarkGray)),
        Wait::Approved => ("approved".into(), style.fg(Color::Green)),
    }
}

/// What the state word says about a row.
pub fn state(row: &Row, now: i64) -> (String, Style) {
    let style = Style::default();
    match (row.section, row.live, row.wait, row.last) {
        (Section::Running, Some(job), _, _) if job.reaped => ("finishing".into(), style.fg(Color::Magenta)),
        (Section::Running, Some(job), _, _) => {
            let secs = job.started.map_or(0, |s| s.elapsed().as_secs());
            (format!("reviewing {}", fmt_dur(secs)), style.fg(Color::Magenta))
        }
        (Section::Queued, ..) => ("queued".into(), style.fg(Color::DarkGray)),
        (Section::Waiting, _, Some(wait), _) => wait_state(wait, now),
        (_, _, _, Some(last)) => finished_state(last.job),
        _ => (String::new(), style),
    }
}

/// `✓ #1711 · approved`
fn head_line(row: &Row, selected: bool, width: usize, spinner: &'static str, now: i64) -> Line<'static> {
    let (state, state_style) = state(row, now);
    let label = format!("#{}", row.pr);
    // The icon and a space, the number, then " · " before the state word.
    let fixed = 2 + console::measure_text_width(&label) + 3;
    let spans = vec![
        icon(row, spinner),
        Span::raw(" "),
        Span::from(label).cyan().bold(),
        Span::from(" · ").dark_gray(),
        Span::styled(pad(&state, width.saturating_sub(fixed)), state_style),
    ];
    paint(Line::from(spans), selected, width)
}

/// `  @alice Fix the ledger migration`
fn who_line(row: &Row, selected: bool, width: usize) -> Line<'static> {
    let who = if row.author.is_empty() {
        sanitize_for_display(row.title)
    } else {
        sanitize_for_display(&format!("@{} {}", row.author, row.title))
    };
    let spans = vec![Span::raw("  "), Span::from(pad(&who, width.saturating_sub(2))).dark_gray()];
    paint(Line::from(spans), selected, width)
}

/// Both lines of the selected row are drawn reversed, so the selection reads
/// as one block rather than as two rows.
fn paint(line: Line<'static>, selected: bool, width: usize) -> Line<'static> {
    let line = super::text::fit(line, width);
    if selected {
        line.patch_style(Style::default().add_modifier(Modifier::REVERSED))
    } else {
        line
    }
}

/// The first line to draw so that the selected row -- both of its lines --
/// is on screen, moving as little as possible from where the pane was.
pub fn offset(selected: Option<usize>, height: usize, previous: usize, total: usize) -> usize {
    let most = total.saturating_sub(height);
    let Some(at) = selected else { return previous.min(most) };
    let at_least = (at + ROW_LINES).saturating_sub(height);
    previous.clamp(at_least, at.max(at_least)).min(most)
}

#[cfg(test)]
mod tests {
    use super::super::model::{self, Archived, Sources};
    use super::*;
    use crate::prlist::{Engagement, PrInfo};
    use std::collections::HashMap;
    use std::path::{Path, PathBuf};

    fn text(line: &Line) -> String {
        line.spans.iter().map(|s| s.content.as_ref()).collect::<String>().trim_end().to_string()
    }

    fn done(pr: u64, verdict: &str) -> Archived {
        let mut job = Job::new(pr);
        job.state = JobState::Done;
        job.verdict = Some(verdict.into());
        job.title = "Fix the ledger".into();
        job.author = "alice".into();
        Archived { job, pass_dir: PathBuf::from("/p") }
    }

    fn info(decision: Decision) -> PrInfo {
        PrInfo {
            title: "Cache keys by tenant".into(),
            author: "erin".into(),
            engage: Engagement::Seen,
            head: None,
            ci: Ci::Passing,
            stacked_on: None,
            decision,
        }
    }

    #[test]
    fn a_row_is_two_lines_and_no_heading() {
        let mut running = Job::new(9);
        running.state = JobState::Running;
        running.title = "Add retry logic".into();
        running.author = "bob".into();
        let jobs = vec![running];
        let archive = vec![done(7, "approved")];
        let waiting = vec![(5, Wait::Checks(Ci::Failing))];
        let info = HashMap::from([(5, info(Decision::None))]);
        let src = Sources { jobs: &jobs, pass_dir: Path::new("/p"), archive: &archive, waiting: &waiting, info: &info };
        let rows = model::rows(&src);
        let (drawn, at) = lines(&rows, Some(1), 36, "⠋", 0);
        let out: Vec<String> = drawn.iter().map(text).collect();
        assert_eq!(out[0], "⠋ #9 · reviewing 0s");
        assert_eq!(out[1], "  @bob Add retry logic");
        assert_eq!(out[2], "○ #5 · CI failing");
        assert_eq!(out[3], "  @erin Cache keys by tenant");
        // GitHub has not been asked again since this run approved it, so
        // the icon is still "no decision" while the word says approved.
        assert_eq!(out[4], "○ #7 · approved");
        assert_eq!(out[5], "  @alice Fix the ledger");
        assert_eq!(at, Some(2), "the second row starts on the third line");
        for line in &drawn {
            assert!(line.width() <= 36, "{out:?}");
        }
    }

    #[test]
    fn the_icon_says_where_the_reviews_stand() {
        let waiting = vec![(5, Wait::Seen), (7, Wait::Seen), (6, Wait::Approved)];
        let info = HashMap::from([
            (5, info(Decision::None)),
            (6, info(Decision::Approved)),
            (7, info(Decision::ChangesRequested)),
        ]);
        let src = Sources { jobs: &[], pass_dir: Path::new("/p"), archive: &[], waiting: &waiting, info: &info };
        let rows = model::rows(&src);
        let (drawn, _) = lines(&rows, None, 36, "⠋", 0);
        let marks: Vec<String> = drawn.iter().step_by(ROW_LINES).map(|l| l.spans[0].content.to_string()).collect();
        assert_eq!(marks, vec!["○", "✗", "✓"], "each by its own decision, approved last");
    }

    #[test]
    fn both_lines_of_the_selected_row_are_reversed() {
        let archive = vec![done(7, "approved")];
        let info = HashMap::new();
        let src = Sources { jobs: &[], pass_dir: Path::new("/p"), archive: &archive, waiting: &[], info: &info };
        let rows = model::rows(&src);
        let (drawn, _) = lines(&rows, Some(0), 36, "⠋", 0);
        assert!(drawn.iter().all(|l| l.style.add_modifier.contains(Modifier::REVERSED)));
        assert!(drawn.iter().all(|l| l.width() == 36), "across the whole pane");
        let (drawn, _) = lines(&rows, None, 36, "⠋", 0);
        assert!(!drawn.iter().any(|l| l.style.add_modifier.contains(Modifier::REVERSED)));
    }

    #[test]
    fn a_title_cannot_draw_escape_sequences() {
        let mut hostile = done(7, "approved");
        hostile.job.title = "\u{1b}[2Jgone\u{202e}".into();
        let archive = vec![hostile];
        let info = HashMap::new();
        let src = Sources { jobs: &[], pass_dir: Path::new("/p"), archive: &archive, waiting: &[], info: &info };
        let rows = model::rows(&src);
        let (drawn, _) = lines(&rows, None, 36, "⠋", 0);
        let line = text(&drawn[1]);
        assert!(!line.contains('\u{1b}') && !line.contains('\u{202e}'), "{line:?}");
    }

    #[test]
    fn states_read_in_a_word_or_two() {
        let mut stopped = Job::new(9);
        stopped.state = JobState::Failed;
        stopped.stopped = true;
        assert_eq!(finished_state(&stopped).0, "stopped");
        stopped.stopped = false;
        assert_eq!(finished_state(&stopped).0, "failed");
        let mut quiet = Job::new(9);
        quiet.state = JobState::Done;
        assert_eq!(finished_state(&quiet).0, "no verdict");
        assert_eq!(wait_state(Wait::Resting { until: 700 }, 100).0, "rest 10m00s");
        assert_eq!(wait_state(Wait::Approved, 0).0, "approved");
        assert_eq!(wait_state(Wait::Checks(Ci::Pending), 0).0, "CI pending");
    }

    #[test]
    fn the_pane_scrolls_only_as_far_as_the_selected_row_needs() {
        // Five rows of two lines each, in a pane of four lines.
        assert_eq!(offset(Some(0), 4, 0, 10), 0);
        assert_eq!(offset(Some(2), 4, 0, 10), 0, "still on screen");
        assert_eq!(offset(Some(4), 4, 0, 10), 2, "just enough for both lines");
        assert_eq!(offset(Some(6), 4, 0, 10), 4);
        assert_eq!(offset(Some(2), 4, 6, 10), 2, "back up to it");
        assert_eq!(offset(None, 4, 9, 10), 6, "never past the end");
        assert_eq!(offset(Some(2), 4, 0, 4), 0, "a short list never scrolls");
    }
}
