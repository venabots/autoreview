//! One line of text, edited in place.
//!
//! The focus is the one thing a person changes mid-run, and it is a sentence
//! rather than a flag, so it needs somewhere to type. This is that, and no
//! more: a line, a cursor, and the keys a person expects of a prompt. There
//! is no selection, no history and no wrapping, because a focus is one line
//! and everything past that is a text editor nobody asked for.
//!
//! Characters are kept as `char`s, not bytes: a cursor that moved by bytes
//! would land inside a letter the moment somebody pastes an em dash.

use crossterm::event::{KeyCode, KeyEvent, KeyEventKind, KeyModifiers};

/// What a key did to the line.
#[derive(Debug, Clone, PartialEq, Eq)]
pub enum Edit {
    /// Still typing.
    Typing,
    /// Enter: take what is in the line.
    Done(String),
    /// Esc: leave it as it was.
    Cancelled,
}

#[derive(Debug, Default, Clone)]
pub struct Input {
    chars: Vec<char>,
    /// Where the next character goes, counted in characters.
    cursor: usize,
}

impl Input {
    /// An editor opened on `text`, with the cursor at its end, which is
    /// where somebody who pressed the key to edit expects to carry on.
    pub fn at_end(text: &str) -> Input {
        let chars: Vec<char> = text.chars().collect();
        Input { cursor: chars.len(), chars }
    }

    pub fn text(&self) -> String {
        self.chars.iter().collect()
    }

    pub fn cursor(&self) -> usize {
        self.cursor
    }

    /// One key. Anything this does not know is ignored rather than typed:
    /// a function key must not leave `F5` in the middle of a sentence.
    pub fn key(&mut self, key: KeyEvent) -> Edit {
        if key.kind != KeyEventKind::Press {
            return Edit::Typing;
        }
        let ctrl = key.modifiers.contains(KeyModifiers::CONTROL);
        match key.code {
            KeyCode::Enter => return Edit::Done(self.text()),
            KeyCode::Esc => return Edit::Cancelled,
            // The shell's own line keys, which fingers reach for first.
            KeyCode::Char('u') if ctrl => {
                self.chars.clear();
                self.cursor = 0;
            }
            KeyCode::Char('a') if ctrl => self.cursor = 0,
            KeyCode::Char('e') if ctrl => self.cursor = self.chars.len(),
            KeyCode::Char('w') if ctrl => self.rub_out_word(),
            KeyCode::Char(c) if !ctrl && !key.modifiers.contains(KeyModifiers::ALT) => {
                self.chars.insert(self.cursor, c);
                self.cursor += 1;
            }
            KeyCode::Backspace => {
                if self.cursor > 0 {
                    self.cursor -= 1;
                    self.chars.remove(self.cursor);
                }
            }
            KeyCode::Delete => {
                if self.cursor < self.chars.len() {
                    self.chars.remove(self.cursor);
                }
            }
            KeyCode::Left => self.cursor = self.cursor.saturating_sub(1),
            KeyCode::Right => self.cursor = (self.cursor + 1).min(self.chars.len()),
            KeyCode::Home => self.cursor = 0,
            KeyCode::End => self.cursor = self.chars.len(),
            _ => {}
        }
        Edit::Typing
    }

    /// ctrl-w: the run of spaces before the cursor, then the word.
    fn rub_out_word(&mut self) {
        while self.cursor > 0 && self.chars[self.cursor - 1].is_whitespace() {
            self.cursor -= 1;
            self.chars.remove(self.cursor);
        }
        while self.cursor > 0 && !self.chars[self.cursor - 1].is_whitespace() {
            self.cursor -= 1;
            self.chars.remove(self.cursor);
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    fn press(input: &mut Input, code: KeyCode) -> Edit {
        input.key(KeyEvent::new(code, KeyModifiers::NONE))
    }

    fn ctrl(input: &mut Input, c: char) -> Edit {
        input.key(KeyEvent::new(KeyCode::Char(c), KeyModifiers::CONTROL))
    }

    fn typed(text: &str) -> Input {
        let mut input = Input::default();
        for c in text.chars() {
            press(&mut input, KeyCode::Char(c));
        }
        input
    }

    #[test]
    fn typing_and_leaving() {
        let mut input = typed("be strict");
        assert_eq!(input.text(), "be strict");
        assert_eq!(input.cursor(), 9);
        assert_eq!(press(&mut input, KeyCode::Enter), Edit::Done("be strict".into()));
        assert_eq!(press(&mut input, KeyCode::Esc), Edit::Cancelled);
    }

    #[test]
    fn it_opens_on_what_is_there_already() {
        let mut input = Input::at_end("the ledger");
        assert_eq!(input.cursor(), 10);
        press(&mut input, KeyCode::Char('!'));
        assert_eq!(input.text(), "the ledger!");
    }

    #[test]
    fn the_cursor_moves_and_the_edits_follow_it() {
        let mut input = typed("be strict");
        press(&mut input, KeyCode::Home);
        press(&mut input, KeyCode::Char('!'));
        assert_eq!(input.text(), "!be strict");
        press(&mut input, KeyCode::End);
        press(&mut input, KeyCode::Backspace);
        assert_eq!(input.text(), "!be stric");
        press(&mut input, KeyCode::Left);
        press(&mut input, KeyCode::Delete);
        assert_eq!(input.text(), "!be stri");
        // Neither end runs away.
        for _ in 0..20 {
            press(&mut input, KeyCode::Left);
        }
        press(&mut input, KeyCode::Backspace);
        assert_eq!(input.text(), "!be stri");
        assert_eq!(input.cursor(), 0);
    }

    #[test]
    fn the_shell_keys_work_as_fingers_expect() {
        let mut input = typed("be strict about the ledger");
        ctrl(&mut input, 'w');
        assert_eq!(input.text(), "be strict about the ");
        ctrl(&mut input, 'w');
        assert_eq!(input.text(), "be strict about ");
        ctrl(&mut input, 'a');
        assert_eq!(input.cursor(), 0);
        ctrl(&mut input, 'e');
        assert_eq!(input.cursor(), 16);
        ctrl(&mut input, 'u');
        assert_eq!(input.text(), "");
        assert_eq!(input.cursor(), 0);
        ctrl(&mut input, 'w');
        assert_eq!(input.text(), "", "nothing to rub out");
    }

    #[test]
    fn what_it_refuses_to_type() {
        let mut input = typed("ok");
        press(&mut input, KeyCode::F(5));
        input.key(KeyEvent::new(KeyCode::Char('x'), KeyModifiers::ALT));
        let mut release = KeyEvent::new(KeyCode::Char('z'), KeyModifiers::NONE);
        release.kind = KeyEventKind::Release;
        input.key(release);
        assert_eq!(input.text(), "ok");
    }

    #[test]
    fn a_cursor_counts_characters_not_bytes() {
        let mut input = typed("café —");
        press(&mut input, KeyCode::Backspace);
        press(&mut input, KeyCode::Backspace);
        assert_eq!(input.text(), "café");
        press(&mut input, KeyCode::Left);
        press(&mut input, KeyCode::Char('f'));
        assert_eq!(input.text(), "caffé");
    }
}
