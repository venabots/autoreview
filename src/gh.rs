//! One `gh` call with a deadline, for the calls a run makes off its event
//! loop. A hung network call must not hold a pool slot, or the end of a
//! pass, for ever -- least of all in a tool built for cron.

use std::io::Read;
use std::process::{Command, Stdio};
use std::time::{Duration, Instant};

/// How long one call may take before it is killed.
pub const TIMEOUT_SECS: u64 = 15;

/// What `gh` printed, or None when it failed, could not be started, or ran
/// past the deadline. Stdout is drained on its own thread so a child blocked
/// writing can never deadlock against our wait.
pub fn output(args: &[&str]) -> Option<Vec<u8>> {
    let mut child = Command::new("gh")
        .args(args)
        .stdin(Stdio::null())
        .stdout(Stdio::piped())
        .stderr(Stdio::null())
        .spawn()
        .ok()?;
    let mut stdout = child.stdout.take()?;
    let (tx, rx) = std::sync::mpsc::channel();
    std::thread::spawn(move || {
        let mut buf = Vec::new();
        let _ = stdout.read_to_end(&mut buf);
        let _ = tx.send(buf);
    });
    let deadline = Instant::now() + Duration::from_secs(TIMEOUT_SECS);
    loop {
        match child.try_wait() {
            Ok(Some(status)) if status.success() => {
                return rx.recv_timeout(Duration::from_secs(1)).ok();
            }
            Ok(Some(_)) => return None,
            Ok(None) => {
                if Instant::now() >= deadline {
                    let _ = child.kill();
                    let _ = child.wait();
                    return None;
                }
                std::thread::sleep(Duration::from_millis(50));
            }
            Err(_) => {
                let _ = child.kill();
                let _ = child.wait();
                return None;
            }
        }
    }
}
