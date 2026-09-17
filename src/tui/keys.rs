//! The keys the review screen answers to, and the second press that the two
//! costly ones need.
//!
//! Stopping a review throws away what it has spent so far, and quitting stops
//! every review. One stray key must not do either, so both arm on the first
//! press and act on the second. The arming names what it is for: the rows
//! move as reviews finish, and a second `x` that landed on a different row
//! must not stop a review nobody asked to stop.

use crossterm::event::{KeyCode, KeyEvent, KeyEventKind, KeyModifiers};
use std::time::{Duration, Instant};

/// How long a first press stays armed.
pub const CONFIRM_FOR: Duration = Duration::from_secs(3);

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum Intent {
    Up,
    Down,
    Top,
    Bottom,
    PageUp,
    PageDown,
    Resume,
    Open,
    Stop,
    ReviewNow,
    Log,
    Quit,
    /// ctrl-C: leave now, as it always has.
    Interrupt,
    /// Esc: put away the log, disarm a confirm.
    Back,
}

/// A pure table, so it is testable without a terminal.
pub fn intent(key: KeyEvent) -> Option<Intent> {
    // Release and repeat events only arrive from terminals that report them.
    if key.kind != KeyEventKind::Press {
        return None;
    }
    let ctrl = key.modifiers.contains(KeyModifiers::CONTROL);
    if ctrl {
        return match key.code {
            KeyCode::Char('c') => Some(Intent::Interrupt),
            KeyCode::Char('d') => Some(Intent::PageDown),
            KeyCode::Char('u') => Some(Intent::PageUp),
            _ => None,
        };
    }
    // Shift arrives with a capital on some terminals and without it on
    // others; the letter is what the key means.
    if !(key.modifiers - KeyModifiers::SHIFT).is_empty() {
        return None;
    }
    match key.code {
        KeyCode::Char('j') | KeyCode::Down => Some(Intent::Down),
        KeyCode::Char('k') | KeyCode::Up => Some(Intent::Up),
        KeyCode::Char('g') | KeyCode::Home => Some(Intent::Top),
        KeyCode::Char('G') | KeyCode::End => Some(Intent::Bottom),
        KeyCode::PageDown | KeyCode::Char(' ') => Some(Intent::PageDown),
        KeyCode::PageUp => Some(Intent::PageUp),
        KeyCode::Char('r') => Some(Intent::Resume),
        KeyCode::Char('o') => Some(Intent::Open),
        KeyCode::Char('x') => Some(Intent::Stop),
        KeyCode::Char('R') => Some(Intent::ReviewNow),
        KeyCode::Char('l') => Some(Intent::Log),
        KeyCode::Char('q') => Some(Intent::Quit),
        KeyCode::Esc => Some(Intent::Back),
        _ => None,
    }
}

/// What a first press armed.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum Pending {
    Stop(u64),
    Quit,
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub struct Armed {
    pub what: Pending,
    pub at: Instant,
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum Press {
    /// The first press: say what a second one will do.
    Arm(Armed),
    /// The second press, in time and for the same thing.
    Fire,
}

/// A press of a key that needs confirming, given what is armed now.
pub fn confirm(armed: Option<Armed>, want: Pending, now: Instant) -> Press {
    match armed {
        Some(a) if a.what == want && now.duration_since(a.at) <= CONFIRM_FOR => Press::Fire,
        _ => Press::Arm(Armed { what: want, at: now }),
    }
}

/// What stays armed after a key that is not the confirming one: nothing.
/// Expiry is checked on the confirming press, so a stale arm is harmless.
pub fn disarm(intent: Intent) -> bool {
    !matches!(intent, Intent::Stop | Intent::Quit)
}

#[cfg(test)]
mod tests {
    use super::*;

    fn press(code: KeyCode, modifiers: KeyModifiers) -> Option<Intent> {
        intent(KeyEvent::new(code, modifiers))
    }

    #[test]
    fn the_key_table() {
        let none = KeyModifiers::NONE;
        assert_eq!(press(KeyCode::Char('j'), none), Some(Intent::Down));
        assert_eq!(press(KeyCode::Down, none), Some(Intent::Down));
        assert_eq!(press(KeyCode::Char('k'), none), Some(Intent::Up));
        assert_eq!(press(KeyCode::Char('g'), none), Some(Intent::Top));
        assert_eq!(press(KeyCode::Char('r'), none), Some(Intent::Resume));
        assert_eq!(press(KeyCode::Char('o'), none), Some(Intent::Open));
        assert_eq!(press(KeyCode::Char('x'), none), Some(Intent::Stop));
        assert_eq!(press(KeyCode::Char('l'), none), Some(Intent::Log));
        assert_eq!(press(KeyCode::Char('q'), none), Some(Intent::Quit));
        assert_eq!(press(KeyCode::Esc, none), Some(Intent::Back));
        assert_eq!(press(KeyCode::Char('z'), none), None);
    }

    #[test]
    fn capitals_mean_the_same_with_or_without_shift() {
        for mods in [KeyModifiers::NONE, KeyModifiers::SHIFT] {
            assert_eq!(press(KeyCode::Char('R'), mods), Some(Intent::ReviewNow));
            assert_eq!(press(KeyCode::Char('G'), mods), Some(Intent::Bottom));
        }
        // A shifted r is not a capital R: the terminal would have sent one.
        assert_eq!(press(KeyCode::Char('r'), KeyModifiers::SHIFT), Some(Intent::Resume));
    }

    #[test]
    fn control_keys() {
        let ctrl = KeyModifiers::CONTROL;
        assert_eq!(press(KeyCode::Char('c'), ctrl), Some(Intent::Interrupt));
        assert_eq!(press(KeyCode::Char('d'), ctrl), Some(Intent::PageDown));
        assert_eq!(press(KeyCode::Char('u'), ctrl), Some(Intent::PageUp));
        // ctrl-q and alt-x are some other binding, not quit and stop.
        assert_eq!(press(KeyCode::Char('q'), ctrl), None);
        assert_eq!(press(KeyCode::Char('x'), KeyModifiers::ALT), None);
    }

    #[test]
    fn a_release_does_nothing() {
        let mut key = KeyEvent::new(KeyCode::Char('q'), KeyModifiers::NONE);
        key.kind = KeyEventKind::Release;
        assert_eq!(intent(key), None);
    }

    #[test]
    fn a_second_press_confirms_the_same_thing_in_time() {
        let t0 = Instant::now();
        let Press::Arm(armed) = confirm(None, Pending::Stop(9), t0) else { panic!("first press arms") };
        assert_eq!(confirm(Some(armed), Pending::Stop(9), t0 + Duration::from_secs(1)), Press::Fire);
    }

    #[test]
    fn a_second_press_for_another_pr_arms_again() {
        let t0 = Instant::now();
        let armed = Armed { what: Pending::Stop(9), at: t0 };
        assert_eq!(
            confirm(Some(armed), Pending::Stop(8), t0),
            Press::Arm(Armed { what: Pending::Stop(8), at: t0 }),
            "the rows moved; #8 was never asked about"
        );
        assert!(matches!(confirm(Some(armed), Pending::Quit, t0), Press::Arm(_)));
    }

    #[test]
    fn an_arm_expires() {
        let t0 = Instant::now();
        let armed = Armed { what: Pending::Quit, at: t0 };
        let late = t0 + CONFIRM_FOR + Duration::from_millis(1);
        assert_eq!(confirm(Some(armed), Pending::Quit, late), Press::Arm(Armed { what: Pending::Quit, at: late }));
    }

    #[test]
    fn any_other_key_disarms() {
        assert!(disarm(Intent::Down));
        assert!(disarm(Intent::Back));
        assert!(!disarm(Intent::Stop));
        assert!(!disarm(Intent::Quit));
    }
}
