//! The left pane: a heading per section and one line per PR under it.
//!
//! A line says the PR number plain (decision 0006), what state it is in, in
//! a column of its own so the eye can run down it, and whose PR it is. The
//! selected line is drawn reversed across the whole pane width.

use super::model::{Row, Section, Wait};
use super::text::{cut, pad};
use crate::ci::Ci;
use crate::job::{Job, JobState};
use crate::report::sanitize_for_display;
use crate::ui::fmt_dur;
use ratatui::style::{Color, Modifier, Style, Stylize};
use ratatui::text::{Line, Span};

/// The columns the state takes, padded, so the titles line up.
const STATE_WIDTH: usize = 12;

/// The lines to draw, and which of them is the selection.
pub fn lines(rows: &[Row], selected: Option<usize>, width: usize, spinner: &'static str, now: i64) -> (Vec<Line<'static>>, Option<usize>) {
    let mut out = Vec::new();
    let mut at = None;
    let mut section = None;
    for (i, row) in rows.iter().enumerate() {
        if section != Some(row.section) {
            section = Some(row.section);
            let count = rows.iter().filter(|r| r.section == row.section).count();
            out.push(heading(row.section, count, width));
        }
        if selected == Some(i) {
            at = Some(out.len());
        }
        out.push(row_line(row, selected == Some(i), width, spinner, now));
    }
    (out, at)
}

fn heading(section: Section, count: usize, width: usize) -> Line<'static> {
    let text = cut(&format!("{} {count}", section.title()), width);
    Line::from(Span::from(text).add_modifier(Modifier::BOLD).fg(Color::DarkGray))
}

/// The mark at the start of a line, in its colour.
fn mark(row: &Row, spinner: &'static str) -> Span<'static> {
    match row.section {
        Section::Running => Span::raw(spinner).magenta(),
        Section::Queued => Span::raw("·").dark_gray(),
        Section::Waiting => Span::raw("◷").yellow(),
        Section::Finished => match row.last.map(|l| l.job) {
            Some(job) if job.state == JobState::Done => match job.verdict.as_deref() {
                Some("approved") => Span::raw("✓").green(),
                _ => Span::raw("✓").cyan(),
            },
            Some(job) if job.state == JobState::Timeout => Span::raw("✗").yellow(),
            _ => Span::raw("✗").red(),
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
    }
}

/// What the state column says about a row.
pub fn state(row: &Row, now: i64) -> (String, Style) {
    let style = Style::default();
    match (row.section, row.live, row.wait, row.last) {
        (Section::Running, Some(job), _, _) if job.reaped => ("finishing".into(), style.fg(Color::Magenta)),
        (Section::Running, Some(job), _, _) => {
            let secs = job.started.map_or(0, |s| s.elapsed().as_secs());
            (fmt_dur(secs), style.fg(Color::Magenta))
        }
        (Section::Queued, ..) => ("queued".into(), style.fg(Color::DarkGray)),
        (Section::Waiting, _, Some(wait), _) => wait_state(wait, now),
        (_, _, _, Some(last)) => finished_state(last.job),
        _ => (String::new(), style),
    }
}

fn row_line(row: &Row, selected: bool, width: usize, spinner: &'static str, now: i64) -> Line<'static> {
    let label = format!("#{}", row.pr);
    let (state, state_style) = state(row, now);
    let who = if row.author.is_empty() {
        sanitize_for_display(row.title)
    } else {
        sanitize_for_display(&format!("@{} {}", row.author, row.title))
    };
    // A mark, the number, the state and the title, one space apart. The
    // number is padded to the widest a repo is likely to have, so the state
    // column holds still as numbers grow.
    let label = pad(&label, 6);
    let fixed = 2 + 6 + 1 + STATE_WIDTH + 1;
    let spans = vec![
        mark(row, spinner),
        Span::raw(" "),
        Span::from(label).cyan().bold(),
        Span::raw(" "),
        Span::styled(pad(&state, STATE_WIDTH), state_style),
        Span::raw(" "),
        Span::from(pad(&who, width.saturating_sub(fixed))).dark_gray(),
    ];
    let line = super::text::fit(Line::from(spans), width);
    if selected {
        line.patch_style(Style::default().add_modifier(Modifier::REVERSED))
    } else {
        line
    }
}

/// The first line to draw so that the selection is on screen, moving as
/// little as possible from where the pane was.
pub fn offset(selected: Option<usize>, height: usize, previous: usize, total: usize) -> usize {
    let most = total.saturating_sub(height);
    let Some(at) = selected else { return previous.min(most) };
    let at_least = (at + 1).saturating_sub(height);
    // One line of context above the selection, when there is one: the
    // heading of the first row in a section.
    let at_most = at.saturating_sub(1);
    previous.clamp(at_least, at_most.max(at_least)).min(most)
}

#[cfg(test)]
mod tests {
    use super::super::model::{self, Archived, Sources};
    use super::*;
    use std::collections::HashMap;
    use std::path::{Path, PathBuf};

    fn text(line: &Line) -> String {
        line.spans.iter().map(|s| s.content.as_ref()).collect()
    }

    fn done(pr: u64, verdict: &str) -> Archived {
        let mut job = Job::new(pr);
        job.state = JobState::Done;
        job.verdict = Some(verdict.into());
        job.title = "Fix the ledger".into();
        job.author = "alice".into();
        Archived { job, pass_dir: PathBuf::from("/p") }
    }

    #[test]
    fn a_section_has_a_heading_and_a_line_per_pr() {
        let mut running = Job::new(9);
        running.state = JobState::Running;
        let jobs = vec![running];
        let archive = vec![done(7, "approved"), done(6, "changes requested")];
        let waiting = vec![(5, Wait::Checks(Ci::Failing))];
        let info = HashMap::new();
        let src = Sources { jobs: &jobs, pass_dir: Path::new("/p"), archive: &archive, waiting: &waiting, info: &info };
        let rows = model::rows(&src);
        let (drawn, at) = lines(&rows, Some(1), 60, "⠋", 0);
        let text: Vec<String> = drawn.iter().map(text).collect();
        assert_eq!(text[0].trim_end(), "RUNNING 1");
        assert!(text[1].starts_with("⠋ #9     0s"), "{}", text[1]);
        assert_eq!(text[2].trim_end(), "WAITING 1");
        assert!(text[3].starts_with("◷ #5     CI failing"), "{}", text[3]);
        assert_eq!(text[4].trim_end(), "FINISHED 2");
        assert!(text[5].contains("approved") || text[5].contains("changes req"));
        assert!(text[5].contains("@alice Fix the ledger"), "{}", text[5]);
        assert_eq!(at, Some(3), "the selection is the second row, under its heading");
        // Every line fits the pane.
        for line in &drawn {
            assert!(line.width() <= 60, "{:?}", text);
        }
    }

    #[test]
    fn the_selected_line_is_reversed() {
        let archive = vec![done(7, "approved")];
        let info = HashMap::new();
        let src = Sources { jobs: &[], pass_dir: Path::new("/p"), archive: &archive, waiting: &[], info: &info };
        let rows = model::rows(&src);
        let (drawn, _) = lines(&rows, Some(0), 40, "⠋", 0);
        assert!(drawn[1].style.add_modifier.contains(Modifier::REVERSED));
        assert_eq!(drawn[1].width(), 40, "reversed across the whole pane");
        let (drawn, _) = lines(&rows, None, 40, "⠋", 0);
        assert!(!drawn[1].style.add_modifier.contains(Modifier::REVERSED));
    }

    #[test]
    fn a_title_cannot_draw_escape_sequences() {
        let mut hostile = done(7, "approved");
        hostile.job.title = "\u{1b}[2Jgone\u{202e}".into();
        let archive = vec![hostile];
        let info = HashMap::new();
        let src = Sources { jobs: &[], pass_dir: Path::new("/p"), archive: &archive, waiting: &[], info: &info };
        let rows = model::rows(&src);
        let (drawn, _) = lines(&rows, None, 60, "⠋", 0);
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
        assert_eq!(wait_state(Wait::Resting { until: 100 }, 700).0, "rest 0s");
        assert_eq!(wait_state(Wait::Checks(Ci::Pending), 0).0, "CI pending");
    }

    #[test]
    fn the_pane_scrolls_only_as_far_as_the_selection_needs() {
        // Ten lines in a pane of four.
        assert_eq!(offset(Some(0), 4, 0, 10), 0);
        assert_eq!(offset(Some(3), 4, 0, 10), 0, "still on screen");
        assert_eq!(offset(Some(5), 4, 0, 10), 2, "just enough to show it");
        assert_eq!(offset(Some(2), 4, 5, 10), 1, "back up, with a line above it");
        assert_eq!(offset(None, 4, 9, 10), 6, "never past the end");
        assert_eq!(offset(Some(1), 4, 0, 2), 0, "a short list never scrolls");
    }
}
