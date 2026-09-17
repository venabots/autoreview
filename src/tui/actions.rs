//! What the screen's keys do outside the engine: reopen a review in a new
//! tab, and open a PR in the browser.
//!
//! Both run a program, and both run it off the main thread: the Ghostty path
//! sleeps a third of a second per tab and `gh` waits on the network, and a
//! screen that froze for either would read as a hung run. Whatever the
//! program writes to stderr lands in the run log, because that is where fd 2
//! points while the screen is up.

use crate::job::Job;
use crate::tabs::command::shell_quote;
use crate::tabs::spawner;
use std::path::Path;
use std::process::{Command, Stdio};
use std::sync::mpsc::Sender;

/// The shell line that reopens a finished review, in the review's own repo.
///
/// Built from the orchestrator's reopen command, the same one the summary
/// prints, so the two cannot drift apart. A review the fallback took over
/// names the fallback's CLI: a codex thread handed to `claude --resume`
/// opens nothing.
pub fn resume_line(job: &Job, repo_root: &Path) -> Result<String, String> {
    let sid = job
        .sid
        .as_deref()
        .ok_or_else(|| format!("PR #{}'s review has no session to resume", job.pr))?;
    let reopen = job.orchestrator.reopen_command().replace("<SESSION>", &shell_quote(sid));
    Ok(format!("cd {} && {reopen}", shell_quote(&repo_root.display().to_string())))
}

/// The label a resumed review's tab carries, in the shape review-prs names
/// its own tabs.
pub fn tab_label(pr: u64) -> String {
    format!("PR {pr} Resume")
}

/// The first line of a message, for a footer that has one line to give it.
fn first_line(msg: &str) -> String {
    msg.lines().next().unwrap_or("").trim().to_string()
}

/// Open `line` in a new terminal tab and say how it went on `done`.
pub fn resume_in_tab(pr: u64, line: String, done: Sender<String>) {
    std::thread::spawn(move || {
        let said = match spawner::detect().and_then(|s| spawner::spawn(s, &line, &tab_label(pr)).map(|()| s)) {
            Ok(s) => format!("opened PR #{pr}'s review in a new {} tab", s.name()),
            Err(e) => format!("{} (see the log: l)", first_line(&e)),
        };
        let _ = done.send(said);
    });
}

/// The `gh` arguments that open a PR in the browser.
pub fn open_args(pr: u64, repo: &str) -> Vec<String> {
    ["pr", "view", &pr.to_string(), "--web", "--repo", repo].map(String::from).to_vec()
}

/// Open PR `pr` in the browser and say how it went on `done`. stdin is
/// closed: the terminal is in raw mode, and a child that read it would take
/// the keys meant for the screen.
pub fn open_in_browser(pr: u64, repo: String, done: Sender<String>) {
    std::thread::spawn(move || {
        let ran = Command::new("gh")
            .args(open_args(pr, &repo))
            .stdin(Stdio::null())
            .stdout(Stdio::null())
            .status();
        let said = match ran {
            Ok(s) if s.success() => format!("opened PR #{pr} in the browser"),
            Ok(_) => format!("gh could not open PR #{pr} (see the log: l)"),
            Err(e) => format!("could not run gh: {e}"),
        };
        let _ = done.send(said);
    });
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::orchestrator::Orchestrator;

    const SID: &str = "7442b624-5cba-5d44-ae67-9c390cfe70a1";

    fn reviewed(orchestrator: Orchestrator) -> Job {
        let mut job = Job::new(9);
        job.sid = Some(SID.into());
        job.orchestrator = orchestrator;
        job
    }

    #[test]
    fn a_claude_review_reopens_with_claude_in_its_repo() {
        let line = resume_line(&reviewed(Orchestrator::claude()), Path::new("/src/app")).unwrap();
        assert_eq!(line, format!("cd /src/app && claude --resume {SID}"));
    }

    #[test]
    fn a_codex_review_reopens_with_codex() {
        let codex = Orchestrator::parse("codex").unwrap();
        let line = resume_line(&reviewed(codex), Path::new("/src/app")).unwrap();
        assert_eq!(line, format!("cd /src/app && codex resume {SID}"));
    }

    #[test]
    fn a_path_with_a_space_stays_one_word() {
        let line = resume_line(&reviewed(Orchestrator::claude()), Path::new("/src/my app")).unwrap();
        assert!(line.starts_with("cd '/src/my app' && "), "{line}");
    }

    #[test]
    fn a_session_id_cannot_add_words_to_the_line() {
        let mut job = reviewed(Orchestrator::claude());
        job.sid = Some("x; rm -rf ~".into());
        let line = resume_line(&job, Path::new("/src")).unwrap();
        assert_eq!(line, "cd /src && claude --resume 'x; rm -rf ~'");
    }

    #[test]
    fn no_session_is_no_line() {
        let mut job = reviewed(Orchestrator::claude());
        job.sid = None;
        assert_eq!(
            resume_line(&job, Path::new("/src")).unwrap_err(),
            "PR #9's review has no session to resume"
        );
    }

    #[test]
    fn the_browser_is_asked_through_gh_for_this_repo() {
        assert_eq!(open_args(9, "acme/app"), ["pr", "view", "9", "--web", "--repo", "acme/app"]);
    }

    #[test]
    fn a_footer_gets_the_first_line() {
        assert_eq!(first_line("error: no supported terminal detected\n  expected ..."), "error: no supported terminal detected");
        assert_eq!(first_line(""), "");
    }
}
