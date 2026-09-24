//! A review's markdown, drawn as lines a person can follow.
//!
//! What a reviewer writes is markdown meant for a PR comment: headings,
//! bullets, fenced code, `file.rs:88` in backticks, and blank lines wherever
//! the model felt like one. Drawn raw it reads as a wall with `##` and
//! backticks in it, which is what the pane looked like before this module.
//!
//! This is not a markdown renderer. It is the handful of shapes a review
//! actually uses, and every other line passes through as itself -- a review
//! is somebody else's text, and a parser that got clever with it would hide
//! the one line the reader needed.

use ratatui::style::{Color, Modifier, Style};
use ratatui::text::{Line, Span};

/// How far a fenced block is indented, so code sits apart from prose.
const CODE_INDENT: &str = "  ";

/// The lines of a review, styled. `lines` is the review as it was read:
/// sanitized, tabs expanded, one entry per line.
pub fn render(lines: &[String]) -> Vec<Line<'static>> {
    let mut out: Vec<Line<'static>> = Vec::new();
    let mut in_code = false;
    for raw in lines {
        let line = raw.trim_end();
        if line.trim_start().starts_with("```") {
            // The fence itself says nothing; what it encloses is drawn as
            // code, so the boundary is visible without the backticks.
            in_code = !in_code;
            continue;
        }
        if in_code {
            out.push(Line::from(format!("{CODE_INDENT}{line}")).style(code()));
            continue;
        }
        // Blank lines are the model's paragraph breaks, and it often uses
        // several. One is a break; three are a hole in the pane.
        if line.trim().is_empty() {
            // Nothing before it, or a break already drawn: skip.
            let already = out.last().is_none_or(|last| last.width() == 0);
            if !already {
                out.push(Line::default());
            }
            continue;
        }
        out.push(prose(line));
    }
    // A trailing blank line would push the last line of a scrolled pane off
    // its end for nothing.
    while out.last().is_some_and(|l| l.width() == 0) {
        out.pop();
    }
    out
}

fn code() -> Style {
    Style::default().fg(Color::Cyan)
}

fn heading() -> Style {
    Style::default().add_modifier(Modifier::BOLD)
}

/// One line that is not inside a fence.
fn prose(line: &str) -> Line<'static> {
    let trimmed = line.trim_start();
    let indent = &line[..line.len() - trimmed.len()];
    // A rule between sections, as wide as the pane allows it to be drawn.
    if is_rule(trimmed) {
        return Line::from("─".repeat(24)).style(Style::default().fg(Color::DarkGray));
    }
    if let Some(text) = heading_text(trimmed) {
        return Line::from(inline(text)).style(heading());
    }
    if let Some((marker, text)) = bullet(trimmed) {
        let mut spans = vec![
            Span::raw(indent.to_string()),
            Span::styled(marker, Style::default().fg(Color::DarkGray)),
        ];
        spans.extend(inline(text));
        return Line::from(spans);
    }
    let mut spans = vec![Span::raw(indent.to_string())];
    spans.extend(inline(trimmed));
    Line::from(spans)
}

fn is_rule(line: &str) -> bool {
    let bar = line.trim_end();
    bar.len() >= 3 && (bar.bytes().all(|b| b == b'-') || bar.bytes().all(|b| b == b'*'))
}

/// The text of an ATX heading, without its hashes.
fn heading_text(line: &str) -> Option<&str> {
    let rest = line.trim_start_matches('#');
    let hashes = line.len() - rest.len();
    (1..=6).contains(&hashes).then(|| rest.trim_start())
}

/// A list item's marker, drawn as a bullet, and the text after it. Numbered
/// items keep their number: it is what the reviewer referred to elsewhere.
fn bullet(line: &str) -> Option<(String, &str)> {
    for mark in ["- ", "* ", "+ "] {
        if let Some(text) = line.strip_prefix(mark) {
            return Some(("• ".to_string(), text));
        }
    }
    let digits: String = line.chars().take_while(char::is_ascii_digit).collect();
    if !digits.is_empty() && digits.len() <= 3 {
        for mark in [". ", ") "] {
            if let Some(text) = line[digits.len()..].strip_prefix(mark) {
                return Some((format!("{digits}{} ", mark.trim_end()), text));
            }
        }
    }
    None
}

/// The spans of one line: `code` in backticks and **bold** picked out, and
/// everything else as it was written. An unclosed marker is text, because a
/// review that mentions a lone backtick still has to read as it was typed.
pub fn inline(text: &str) -> Vec<Span<'static>> {
    let mut out: Vec<Span<'static>> = Vec::new();
    let mut plain = String::new();
    let mut rest = text;
    while let Some(at) = rest.find(['`', '*']) {
        let (marker, style) = match rest.as_bytes()[at] {
            b'`' => ("`", code()),
            _ if rest[at..].starts_with("**") => ("**", heading()),
            _ => {
                // A single star is a star: emphasis is rare in a review and
                // a bullet's own marker is not emphasis.
                plain.push_str(&rest[..=at]);
                rest = &rest[at + 1..];
                continue;
            }
        };
        let after = &rest[at + marker.len()..];
        let Some(end) = after.find(marker) else {
            break;
        };
        plain.push_str(&rest[..at]);
        if !plain.is_empty() {
            out.push(Span::raw(std::mem::take(&mut plain)));
        }
        out.push(Span::styled(after[..end].to_string(), style));
        rest = &after[end + marker.len()..];
    }
    plain.push_str(rest);
    if !plain.is_empty() {
        out.push(Span::raw(plain));
    }
    out
}

#[cfg(test)]
mod tests {
    use super::*;

    fn lines(text: &str) -> Vec<String> {
        text.lines().map(str::to_string).collect()
    }

    fn drawn(text: &str) -> Vec<String> {
        render(&lines(text))
            .iter()
            .map(|l| l.spans.iter().map(|s| s.content.as_ref()).collect())
            .collect()
    }

    #[test]
    fn a_heading_loses_its_hashes_and_keeps_its_weight() {
        let out = render(&lines("## Findings\ntext"));
        assert_eq!(out[0].spans[0].content, "Findings");
        assert!(out[0].style.add_modifier.contains(Modifier::BOLD));
        assert!(!out[1].style.add_modifier.contains(Modifier::BOLD));
        // Six is a heading; seven is somebody's ascii art.
        assert_eq!(drawn("###### deep"), vec!["deep"]);
        assert_eq!(drawn("####### wat"), vec!["####### wat"]);
    }

    #[test]
    fn bullets_become_bullets_and_numbers_keep_their_number() {
        assert_eq!(
            drawn("- one\n  - nested\n* two\n1. first\n2) second"),
            vec!["• one", "  • nested", "• two", "1. first", "2) second"]
        );
        // Not a list: a sentence that happens to start with a dash or a year.
        assert_eq!(drawn("-not a bullet"), vec!["-not a bullet"]);
        assert_eq!(drawn("2026 was the year"), vec!["2026 was the year"]);
    }

    #[test]
    fn a_fence_is_drawn_as_code_without_its_backticks() {
        let out = drawn("before\n```rust\nlet x = 1;\n```\nafter");
        assert_eq!(out, vec!["before", "  let x = 1;", "after"]);
    }

    #[test]
    fn runs_of_blank_lines_collapse_and_the_last_one_goes() {
        assert_eq!(drawn("one\n\n\n\ntwo\n\n\n"), vec!["one", "", "two"]);
    }

    #[test]
    fn a_rule_is_a_rule() {
        assert_eq!(drawn("---"), vec!["─".repeat(24)]);
        assert_eq!(drawn("- - -"), vec!["• - -"], "a bullet, not a rule");
    }

    #[test]
    fn code_and_bold_lose_their_markers_and_keep_their_style() {
        let spans = inline("see `src/pay.rs:88` and **fix it** now");
        let text: Vec<String> = spans.iter().map(|s| s.content.to_string()).collect();
        assert_eq!(text, vec!["see ", "src/pay.rs:88", " and ", "fix it", " now"]);
        assert_eq!(spans[1].style.fg, Some(Color::Cyan));
        assert!(spans[3].style.add_modifier.contains(Modifier::BOLD));
    }

    #[test]
    fn an_unclosed_marker_is_text() {
        let text: Vec<String> =
            inline("a ` b ** c").iter().map(|s| s.content.to_string()).collect();
        assert_eq!(text, vec!["a ` b ** c"]);
        let text: Vec<String> =
            inline("2 * 3 * 4").iter().map(|s| s.content.to_string()).collect();
        assert_eq!(text, vec!["2 * 3 * 4"]);
    }
}
