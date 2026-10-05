//! Where the panes go, and the lines above and below them.

use super::text::{cut, fit};
use ratatui::layout::Rect;
use ratatui::style::{Color, Modifier, Stylize};
use ratatui::text::{Line, Span};

/// Below this width the list goes above the detail instead of beside it:
/// two panes side by side would leave neither room for a title.
const SIDE_BY_SIDE: u16 = 90;

pub struct Areas {
    pub header: Rect,
    /// The tab bar, under the header: Review and My PRs.
    pub tabs: Rect,
    pub list: Rect,
    pub detail: Rect,
    pub footer: Rect,
}

pub fn areas(size: Rect) -> Areas {
    let row = |y: u16| Rect { x: size.x, y, width: size.width, height: 1.min(size.height) };
    let header = row(size.y);
    let footer = row(size.y + size.height.saturating_sub(1));
    // No room for a tab bar under the header and above the footer: none is
    // drawn, rather than one drawn over the footer.
    let tabs = if size.height >= 3 { row(size.y + 1) } else { Rect { height: 0, ..row(size.y) } };
    let body = Rect {
        x: size.x,
        y: size.y + 2,
        width: size.width,
        height: size.height.saturating_sub(3),
    };
    if size.width >= SIDE_BY_SIDE {
        let list_width = (size.width * 3 / 10).clamp(30, 44);
        Areas {
            header,
            tabs,
            list: Rect { width: list_width, ..body },
            detail: Rect { x: body.x + list_width, width: body.width - list_width, ..body },
            footer,
        }
    } else {
        let list_height = (body.height * 2 / 5).max(3).min(body.height);
        Areas {
            header,
            tabs,
            list: Rect { height: list_height, ..body },
            detail: Rect { y: body.y + list_height, height: body.height - list_height, ..body },
            footer,
        }
    }
}

pub fn header(repo: &str, mode: &str, focus: Option<&str>, log: &str, width: usize) -> Line<'static> {
    let mut line = vec![
        Span::from("autoreview").bold().magenta(),
        Span::raw(" · "),
        Span::from(repo.to_string()).bold(),
        Span::raw(" · "),
        Span::from(mode.to_string()),
    ];
    // Before the log path, which is the part worth losing to a narrow
    // terminal: what the reviewers are being told is not.
    if let Some(focus) = focus {
        line.push(Span::raw(" · "));
        line.push(Span::from(format!("focus: {focus}")).yellow());
    }
    line.push(Span::from(format!(" · log {log}")).dark_gray());
    fit(Line::from(line), width)
}

/// The line a focus is typed on, with the cursor drawn where it sits. The
/// terminal's own cursor stays hidden: one that moved with the pane's
/// scrolling would be a second, wrong cursor.
pub fn prompt(text: &str, cursor: usize, width: usize) -> Line<'static> {
    let mut spans = vec![Span::from("focus ").yellow().bold()];
    let chars: Vec<char> = text.chars().collect();
    let before: String = chars[..cursor.min(chars.len())].iter().collect();
    let at: String = chars.get(cursor).copied().unwrap_or(' ').to_string();
    let after: String = chars.iter().skip(cursor + 1).collect();
    spans.push(Span::raw(before));
    spans.push(Span::from(at).add_modifier(Modifier::REVERSED));
    spans.push(Span::raw(after));
    spans.push(Span::from("   enter to apply · esc to leave it").dark_gray());
    fit(Line::from(spans), width)
}

/// The keys, most useful first; the footer shows as many as fit.
const HINTS: &[(&str, &str)] = &[
    ("q", "quit"),
    ("j/k", "move"),
    ("r", "resume"),
    ("o", "open"),
    ("R", "review now"),
    ("w", "watch"),
    ("tab", "my PRs"),
    ("m", "mouse"),
    ("f", "focus"),
    ("x", "stop"),
    ("l", "log"),
    ("^d/^u", "scroll"),
];

/// The hint that ends the footer at every width: the key for the full list.
const HELP_KEY: &str = "?";
const HELP_WHAT: &str = "keys";

/// The line under the panes: what the run is doing, or the latest message,
/// and at the right edge the keys that fit.
pub fn footer(status: &str, message: Option<&str>, width: usize) -> Line<'static> {
    let left = match message {
        Some(m) => Span::from(cut(m, width)).yellow(),
        None => Span::from(cut(status, width)).dark_gray(),
    };
    let used = left.width();
    // `? keys` is the last hint and is never the one cut: whatever else does
    // not fit, it says where the full list is.
    let help = format!("{HELP_KEY} {HELP_WHAT}");
    let help_width = console::measure_text_width(&help);
    let mut hints: Vec<Span<'static>> = Vec::new();
    let mut hints_width = 0;
    for (key, what) in HINTS {
        let piece = format!("{}{key} {what}", if hints.is_empty() { "" } else { "  " });
        let w = console::measure_text_width(&piece);
        // Two columns of air between the status and the keys, at least.
        if used + 2 + hints_width + w + 2 + help_width > width {
            break;
        }
        hints_width += w;
        hints.push(Span::from(piece).fg(Color::DarkGray));
    }
    let help = if hints.is_empty() { help } else { format!("  {help}") };
    if used + 2 + hints_width + console::measure_text_width(&help) <= width {
        hints_width += console::measure_text_width(&help);
        hints.push(Span::from(help).fg(Color::DarkGray));
    }
    let mut spans = vec![left];
    if !hints.is_empty() {
        spans.push(Span::raw(" ".repeat(width - used - hints_width)));
        spans.extend(hints);
    }
    Line::from(spans)
}

#[cfg(test)]
mod tests {
    use super::*;

    fn text(line: &Line) -> String {
        line.spans.iter().map(|s| s.content.as_ref()).collect()
    }

    #[test]
    fn the_header_says_what_the_reviewers_are_told() {
        let plain = text(&header("acme/app", "one pass", None, "/tmp/log", 80));
        assert_eq!(plain, "autoreview · acme/app · one pass · log /tmp/log");
        let with = text(&header("acme/app", "one pass", Some("the ledger"), "/tmp/log", 80));
        assert!(with.contains("· focus: the ledger · log"), "{with}");
    }

    #[test]
    fn a_focus_is_typed_on_a_line_with_a_cursor() {
        let line = prompt("be strict", 0, 80);
        assert!(text(&line).starts_with("focus be strict"), "{}", text(&line));
        assert_eq!(under_cursor(&line), "b");
        assert_eq!(under_cursor(&prompt("be strict", 3, 80)), "s");
        // Past the last character the cursor is a blank cell of its own.
        assert_eq!(under_cursor(&prompt("ab", 2, 80)), " ");
        assert!(text(&prompt("ab", 2, 80)).contains("enter to apply"));
    }

    /// The one cell the prompt draws in reverse: its cursor.
    fn under_cursor(line: &Line) -> String {
        line.spans
            .iter()
            .filter(|s| s.style.add_modifier.contains(Modifier::REVERSED))
            .map(|s| s.content.to_string())
            .collect()
    }

    #[test]
    fn wide_terminals_put_the_panes_side_by_side() {
        let a = areas(Rect::new(0, 0, 120, 40));
        assert_eq!((a.header.y, a.tabs.y, a.footer.y), (0, 1, 39));
        assert_eq!((a.list.y, a.list.x, a.list.width, a.list.height), (2, 0, 36, 37));
        assert_eq!((a.detail.x, a.detail.width), (36, 84));
    }

    #[test]
    fn narrow_terminals_stack_them() {
        let a = areas(Rect::new(0, 0, 60, 30));
        assert_eq!((a.list.y, a.list.height, a.list.width), (2, 10, 60));
        assert_eq!((a.detail.y, a.detail.height), (12, 17));
    }

    #[test]
    fn a_tiny_terminal_does_not_underflow() {
        let a = areas(Rect::new(0, 0, 10, 1));
        assert_eq!(a.list.height + a.detail.height, 0);
        assert_eq!(a.tabs.height, 0, "no tab bar over the footer");
        let a = areas(Rect::new(0, 0, 10, 2));
        assert_eq!(a.tabs.height + a.list.height + a.detail.height, 0);
        let a = areas(Rect::new(0, 0, 0, 0));
        assert_eq!(a.header.height, 0);
    }

    #[test]
    fn the_footer_shows_the_keys_that_fit() {
        let wide = text(&footer("2 running", None, 120));
        assert!(wide.starts_with("2 running"));
        assert!(wide.contains("R review now"), "{wide}");
        assert!(wide.ends_with("? keys"), "the full list is always named last: {wide}");
        assert_eq!(console::measure_text_width(&wide), 120);
        let narrow = text(&footer("2 running", None, 30));
        assert!(narrow.contains("q quit") && !narrow.contains("resume"), "{narrow}");
        assert!(narrow.ends_with("? keys"), "cut keys, but never this one: {narrow}");
        assert!(console::measure_text_width(&narrow) <= 30);
        // A long note still leaves room for it.
        let noted = text(&footer("2 running", Some("note: could not set the eyes reaction on PR #9"), 60));
        assert!(noted.ends_with("? keys"), "{noted}");
        let tiny = text(&footer("2 running · 1 queued", None, 12));
        assert_eq!(tiny, "2 running ·…");
    }

    #[test]
    fn a_message_takes_the_status_place() {
        let line = footer("2 running", Some("press x again to stop PR #9"), 80);
        assert!(text(&line).starts_with("press x again to stop PR #9"));
    }
}
