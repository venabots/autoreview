//! Cutting text to the columns a pane has. Measured by display width, not
//! bytes or chars: a title in CJK is two columns a character.

pub(crate) use crate::ui::fit;

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
