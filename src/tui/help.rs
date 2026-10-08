//! Every key the view answers to, for `?`.
//!
//! The footer lists the keys that fit beside the status, and the status
//! grows: a note, a long "next check" line or a narrow terminal cuts the
//! list from the right, and a key that is not on screen is a key nobody
//! finds. This list is complete at any width.

use ratatui::style::Stylize;
use ratatui::text::{Line, Span};

/// The keys and what each does, in the order a person reaches for them.
pub const KEYS: &[(&str, &str)] = &[
    ("j/k", "move between PRs (or the arrow keys)"),
    ("g/G", "first or last PR"),
    ("^d/^u", "scroll the right pane (or space and page keys)"),
    ("tab", "switch between Review and My PRs"),
    ("R", "review the selected PR now, even one that is capped or resting"),
    ("f", "My PRs: fix the selected PR once (conflicts, comments, CI)"),
    ("b", "My PRs: babysit the selected PR: fix it again when it changes"),
    ("B", "My PRs: babysit all your PRs, and any you open later"),
    ("u", "My PRs: merge the base in and fix the conflicts"),
    ("c", "My PRs: answer the selected PR's review comments"),
    ("r", "resume the selected review in a new terminal tab"),
    ("o", "open the selected PR in the browser"),
    ("x x", "stop the selected review"),
    ("w", "stop or start looking for new work"),
    ("f", "Review: type what the reviewers look at (the focus)"),
    ("l", "show the run log here; esc puts it away"),
    ("m", "give the mouse back to the terminal, to select text"),
    ("?", "show these keys; esc puts them away"),
    ("q", "quit; q again if reviews are running, which stops them"),
    ("ctrl-C", "quit now, stopping any review that is running"),
];

/// The lines the right pane shows in place of the selected PR.
pub fn lines() -> Vec<Line<'static>> {
    let width = KEYS.iter().map(|(key, _)| key.len()).max().unwrap_or(0);
    let heading = Line::from("KEYS").bold().dark_gray();
    let keys = KEYS.iter().map(|(key, what)| {
        Line::from(vec![Span::from(format!("{key:<width$}")).cyan().bold(), Span::raw("  "), Span::raw(*what)])
    });
    std::iter::once(heading).chain(std::iter::once(Line::default())).chain(keys).collect()
}

#[cfg(test)]
mod tests {
    use super::*;

    fn text(line: &Line) -> String {
        line.spans.iter().map(|s| s.content.as_ref()).collect()
    }

    #[test]
    fn every_key_is_listed_with_what_it_does() {
        let out: Vec<String> = lines().iter().map(text).collect();
        assert_eq!(out[0], "KEYS");
        assert!(out.contains(&"R       review the selected PR now, even one that is capped or resting".to_string()), "{out:?}");
        assert!(out.iter().any(|l| l.starts_with("?       show these keys")), "{out:?}");
        assert_eq!(out.len(), KEYS.len() + 2);
    }
}
