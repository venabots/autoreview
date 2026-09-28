//! Cutting text to the columns a pane has. Measured by display width, not
//! bytes or chars: a title in CJK is two columns a character.

use ratatui::text::{Line, Span};

/// Cut a styled line to the width it is drawn in. A line wider than its
/// pane wraps into the row below, which in a list of fixed-height rows is
/// the next PR's row.
///
/// The cut keeps every span's style up to the column it stops at, and ends
/// in an ellipsis styled like the span it cut.
pub(crate) fn fit(line: Line<'static>, width: usize) -> Line<'static> {
    if width == 0 {
        return Line::default();
    }
    if line.width() <= width {
        return line;
    }
    // Measured on the plain text, so a wide character at the boundary is
    // counted the way the terminal draws it; then the same number of
    // characters is taken back out of the spans.
    let plain: String = line.spans.iter().map(|s| s.content.as_ref()).collect();
    let cut = console::truncate_str(&plain, width, "…");
    let mut keep = cut.chars().count().saturating_sub(1);
    let mut spans = Vec::new();
    for span in line.spans {
        let chars = span.content.chars().count();
        if chars <= keep {
            keep -= chars;
            spans.push(span);
            continue;
        }
        let head: String = span.content.chars().take(keep).collect();
        spans.push(Span::styled(format!("{head}…"), span.style));
        break;
    }
    Line::from(spans)
}

/// Cut to `width` columns, ending in an ellipsis when something was cut.
pub fn cut(s: &str, width: usize) -> String {
    if width == 0 {
        return String::new();
    }
    console::truncate_str(s, width, "…").into_owned()
}

/// Cut or pad to exactly `width` columns.
pub fn pad(s: &str, width: usize) -> String {
    let cut = cut(s, width);
    let used = console::measure_text_width(&cut);
    format!("{cut}{}", " ".repeat(width.saturating_sub(used)))
}

/// Tabs as spaces. A terminal cell holds one character, and a tab drawn
/// into one moves the cursor past the cells after it.
pub fn expand_tabs(line: &str) -> String {
    let mut out = String::with_capacity(line.len());
    let mut col = 0;
    for c in line.chars() {
        if c == '\t' {
            let n = 4 - col % 4;
            out.push_str(&" ".repeat(n));
            col += n;
        } else {
            out.push(c);
            col += console::measure_text_width(c.encode_utf8(&mut [0; 4]));
        }
    }
    out
}

#[cfg(test)]
mod tests {
    use super::*;
    use ratatui::style::Stylize;

    fn text(line: &Line<'_>) -> String {
        line.spans.iter().map(|s| s.content.as_ref()).collect()
    }

    #[test]
    fn fit_cuts_to_the_width_it_is_given() {
        assert_eq!(fit(Line::from("hello"), 80).width(), 5);
        let cut = fit(Line::from("hello world, this is long"), 10);
        assert_eq!(cut.width(), 10);
        assert!(text(&cut).ends_with('…'));
        // Colour is not width: a styled line is cut by what it draws, and the
        // cut keeps the style of the span it fell in.
        let styled = Line::from(vec![Span::raw("hello ").green(), Span::raw("world").red()]);
        let cut = fit(styled, 8);
        assert_eq!(cut.width(), 8);
        assert_eq!(text(&cut), "hello w…");
        assert_eq!(cut.spans.len(), 2);
        assert_eq!(cut.spans[1].style, Span::raw("").red().style);
        // Nothing fits in nothing, and not an ellipsis either.
        assert_eq!(fit(Line::from("hello"), 0).width(), 0);
    }

    #[test]
    fn cut_and_pad_measure_columns() {
        assert_eq!(cut("abcdef", 4), "abc…");
        assert_eq!(cut("abc", 4), "abc");
        assert_eq!(cut("abc", 0), "");
        assert_eq!(pad("ab", 4), "ab  ");
        assert_eq!(pad("abcdef", 4), "abc…");
        assert_eq!(console::measure_text_width(&pad("日本語", 5)), 5);
    }

    #[test]
    fn tabs_stop_every_four_columns() {
        assert_eq!(expand_tabs("\tx"), "    x");
        assert_eq!(expand_tabs("ab\tx"), "ab  x");
        assert_eq!(expand_tabs("abcd\tx"), "abcd    x");
    }
}
