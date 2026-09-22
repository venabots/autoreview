//! The terminal while the review screen owns it.
//!
//! The screen draws on the alternate screen, through its own handle on
//! `/dev/tty`, and points fds 1 and 2 at the run log for as long as it is
//! up. The engine prints a line for everything it does, from a dozen places,
//! and any one of them landing on the screen would scribble over it. With
//! the fds moved, every one of those lines goes to the log unchanged, which
//! is also what the log view shows.
//!
//! Two things follow. Nothing may write to `stdout()` expecting a terminal:
//! crossterm's cursor query does, so no call here may ask for the cursor --
//! ratatui's `Terminal::clear`, `init` and `restore` all do, and none is
//! used. And the size comes from `/dev/tty`, which crossterm asks first, so
//! a resize is seen without the query the inline board depends on.

use crossterm::terminal::{self, Clear, ClearType, EnterAlternateScreen, LeaveAlternateScreen};
use crossterm::{cursor, execute};
use nix::unistd::{dup2_stderr, dup2_stdout};
use ratatui::Terminal;
use ratatui::backend::CrosstermBackend;
use std::fs::{File, OpenOptions};
use std::io::{self, BufWriter, Write};
use std::os::fd::{AsFd, OwnedFd};
use std::path::Path;
use std::sync::atomic::{AtomicBool, Ordering};
use std::sync::{Mutex, Once};

pub type Backend = CrosstermBackend<BufWriter<File>>;

/// True from raw mode on until the terminal is given back. The panic hook
/// reads it, and so does every draw: a panic on another thread gives the
/// terminal back, and a draw after that would paint the normal screen.
static OPEN: AtomicBool = AtomicBool::new(false);

/// The terminal's own stdout and stderr, while fds 1 and 2 point at the log.
/// Duplicated close-on-exec, so no reviewer inherits a handle on the
/// terminal it was never given.
static SAVED: Mutex<Option<(OwnedFd, OwnedFd)>> = Mutex::new(None);

pub fn is_open() -> bool {
    OPEN.load(Ordering::SeqCst)
}

/// Give the terminal back: the fds first, so whatever prints next reaches
/// the terminal, then the normal screen, then cooked mode. Safe to call
/// twice, and from the panic hook.
fn restore() {
    if !OPEN.swap(false, Ordering::SeqCst) {
        return;
    }
    // Whatever the log still holds in a buffer belongs in the log.
    let _ = io::stdout().flush();
    let _ = io::stderr().flush();
    // try_lock: the hook may run on a thread that panicked holding it.
    if let Ok(mut saved) = SAVED.try_lock()
        && let Some((out, err)) = saved.take()
    {
        let _ = dup2_stdout(&out);
        let _ = dup2_stderr(&err);
    }
    if let Ok(mut tty) = OpenOptions::new().write(true).open("/dev/tty") {
        let _ = execute!(tty, LeaveAlternateScreen, cursor::Show);
    }
    let _ = terminal::disable_raw_mode();
}

fn install_panic_hook() {
    static HOOK: Once = Once::new();
    HOOK.call_once(|| {
        let previous = std::panic::take_hook();
        std::panic::set_hook(Box::new(move |info| {
            restore();
            previous(info);
        }));
    });
}

pub struct Term {
    pub terminal: Terminal<Backend>,
}

impl Term {
    /// Take the terminal and send fds 1 and 2 to `log`. An error leaves the
    /// terminal as it was found.
    pub fn open(log: &Path) -> io::Result<Term> {
        let tty = OpenOptions::new().read(true).write(true).open("/dev/tty")?;
        let log = OpenOptions::new().create(true).append(true).open(log)?;
        install_panic_hook();
        // Anything already printed belongs on the terminal, not in the log.
        io::stdout().flush()?;
        io::stderr().flush()?;
        let saved = (io::stdout().as_fd().try_clone_to_owned()?, io::stderr().as_fd().try_clone_to_owned()?);
        terminal::enable_raw_mode()?;
        OPEN.store(true, Ordering::SeqCst);
        let taken = Self::take(tty, &log, saved);
        if taken.is_err() {
            restore();
        }
        taken
    }

    fn take(tty: File, log: &File, saved: (OwnedFd, OwnedFd)) -> io::Result<Term> {
        let mut writer = BufWriter::new(tty);
        execute!(writer, EnterAlternateScreen, cursor::Hide, Clear(ClearType::All))?;
        *SAVED.lock().unwrap_or_else(|e| e.into_inner()) = Some(saved);
        dup2_stdout(log)?;
        dup2_stderr(log)?;
        Ok(Term { terminal: Terminal::new(CrosstermBackend::new(writer))? })
    }
}

impl Drop for Term {
    fn drop(&mut self) {
        let _ = self.terminal.backend_mut().flush();
        restore();
    }
}
