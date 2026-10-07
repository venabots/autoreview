//! A worktree for a task on one of your PRs: the PR's branch, checked out
//! somewhere of its own, so the task can edit, commit and push without
//! touching the checkout you are working in.
//!
//! The local branch has the PR's own name and tracks it on `origin`, so the
//! plain `git push` the skills run pushes to the PR. Two things would make
//! that unsafe, and both are refused rather than worked around: the branch
//! checked out in another worktree (it would move under that checkout), and
//! a local branch of that name holding commits `origin` does not have
//! (starting the task from `origin` would throw them away).

use std::path::{Path, PathBuf};
use std::process::{Command, Stdio};
use std::time::{Duration, Instant};

/// The process groups of fetches running now. An interrupt stops them: a
/// fetch left running after the run exits keeps going with no deadline.
static LIVE: std::sync::Mutex<Vec<i32>> = std::sync::Mutex::new(Vec::new());

/// Stop every fetch that is running. For an interrupt.
pub fn stop_fetches() {
    let groups = LIVE.lock().map(|g| g.clone()).unwrap_or_default();
    for group in groups {
        let _ = nix::sys::signal::killpg(nix::unistd::Pid::from_raw(group), nix::sys::signal::Signal::SIGKILL);
    }
}

/// How long a fetch of the PR's branch may take. A remote that stalls must
/// not hold the pass open: the pass waits for every task's worktree.
const FETCH_TIMEOUT: Duration = Duration::from_secs(120);

/// A worktree made for one task.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct Prepared {
    pub path: PathBuf,
    pub branch: String,
    pub pr: u64,
    /// The commit the worktree starts at: the PR's head on `origin` after
    /// the fetch. Whether the task pushed is read against this, not against
    /// a list that may be minutes old.
    pub head: String,
    /// Whether `prepare` made the local branch. Only then is it removed
    /// with the worktree: a branch that was there before is yours.
    created_branch: bool,
}

/// What became of a worktree after its task.
#[derive(Debug, Clone, PartialEq, Eq)]
pub enum Finished {
    Removed,
    /// Kept, because removing it would lose work. Says what work.
    Kept(String),
}

/// Git that never asks for anything: the view owns the terminal, and a
/// prompt there would draw over it and wait for keys nobody types for it.
///
/// Git's own prompts are off, and the child runs in a session of its own,
/// with no controlling terminal: ssh asks for a passphrase or a host key on
/// `/dev/tty`, not on stdin, and without a terminal it fails at once. The
/// session is also a process group, so a fetch past its deadline is stopped
/// with everything it started.
fn command(dir: &Path, args: &[&str]) -> Command {
    use std::os::unix::process::CommandExt;
    let mut c = Command::new("git");
    c.arg("-C").arg(dir).args(args).stdin(Stdio::null()).env("GIT_TERMINAL_PROMPT", "0");
    // SAFETY: setsid is async-signal-safe and touches no memory of ours.
    unsafe {
        c.pre_exec(|| nix::unistd::setsid().map(|_| ()).map_err(std::io::Error::from));
    }
    c
}

fn git(dir: &Path, args: &[&str]) -> Result<String, String> {
    let out = command(dir, args)
        .output()
        .map_err(|e| format!("could not run git: {e}"))?;
    if out.status.success() {
        Ok(String::from_utf8_lossy(&out.stdout).trim().to_string())
    } else {
        Err(String::from_utf8_lossy(&out.stderr).trim().to_string())
    }
}

/// Fetch the PR's branch, or say why not, within `FETCH_TIMEOUT`. The
/// reason is git's own (`reason`): with prompts off, a failed login is the
/// usual cause, and "exit 128" would not say so.
fn fetch(repo_root: &Path, refspec: &str) -> Result<(), String> {
    let mut child = command(repo_root, &["fetch", "--quiet", "origin", refspec])
        .stdout(Stdio::null())
        .stderr(Stdio::piped())
        .spawn()
        .map_err(|e| format!("could not run git: {e}"))?;
    let group = child.id() as i32;
    if let Ok(mut live) = LIVE.lock() {
        live.push(group);
    }
    let status = wait_for_fetch(&mut child);
    if let Ok(mut live) = LIVE.lock() {
        live.retain(|&g| g != group);
    }
    status
}

fn wait_for_fetch(child: &mut std::process::Child) -> Result<(), String> {
    use std::io::Read;
    // Drained on its own thread, so a child that writes a lot is never
    // blocked against our wait.
    let mut stderr = child.stderr.take();
    let said = std::thread::spawn(move || {
        let mut text = String::new();
        if let Some(err) = stderr.as_mut() {
            let _ = err.read_to_string(&mut text);
        }
        text
    });
    let deadline = Instant::now() + FETCH_TIMEOUT;
    let status = loop {
        match child.try_wait() {
            Ok(Some(status)) => break status,
            Ok(None) if Instant::now() >= deadline => {
                let group = nix::unistd::Pid::from_raw(child.id() as i32);
                let _ = nix::sys::signal::killpg(group, nix::sys::signal::Signal::SIGKILL);
                let _ = child.wait();
                return Err(format!("git fetch took longer than {}s", FETCH_TIMEOUT.as_secs()));
            }
            Ok(None) => std::thread::sleep(Duration::from_millis(50)),
            Err(e) => return Err(format!("could not wait for git: {e}")),
        }
    };
    if status.success() {
        return Ok(());
    }
    let text = said.join().unwrap_or_default();
    Err(reason(&text).unwrap_or_else(|| format!("git fetch failed ({status})")))
}

/// The line of git's stderr that says what went wrong: its `fatal:` line,
/// which comes before the hints it adds, else its first line.
fn reason(stderr: &str) -> Option<String> {
    let lines: Vec<&str> = stderr.lines().map(str::trim).filter(|l| !l.is_empty()).collect();
    match lines.iter().position(|l| l.starts_with("fatal:")) {
        // The line before it is often the cause, an ssh "Permission denied".
        Some(at) if at > 0 => Some(format!("{} {}", lines[at - 1], lines[at])),
        Some(at) => Some(lines[at].to_string()),
        None => lines.first().map(|l| l.to_string()),
    }
}

/// Where a worktree has `branch` checked out, if anywhere.
///
/// A record whose directory is gone, from a worktree this tool made, is
/// removed: one left by an earlier run would otherwise block the PR for
/// ever. Only that record goes. Any other missing worktree may be on a disk
/// that is not mounted right now, and is not this run's to drop.
fn checked_out_at(repo_root: &Path, branch: &str) -> Result<Option<PathBuf>, String> {
    let list = git(repo_root, &["worktree", "list", "--porcelain"])?;
    let wanted = format!("branch refs/heads/{branch}");
    let mut path = None;
    for line in list.lines() {
        if let Some(p) = line.strip_prefix("worktree ") {
            path = Some(PathBuf::from(p));
        } else if line == wanted {
            return match path {
                Some(p) if !p.exists() && made_here(&p) => {
                    let gone = p.display().to_string();
                    git(repo_root, &["worktree", "remove", "--force", &gone])?;
                    Ok(None)
                }
                Some(p) if !p.exists() => Err(format!(
                    "{branch} is checked out at {}, which is not there; if it is gone for good, run git worktree prune",
                    p.display()
                )),
                found => Ok(found),
            };
        }
    }
    Ok(None)
}

/// Whether `path` is a task worktree this tool made: `run-*/worktrees/pr-N`,
/// in a run directory (`rundir::RunDir::new`). A worktree of yours that
/// happens to be called `pr-12` is not.
fn made_here(path: &Path) -> bool {
    let name = |p: Option<&Path>| p.and_then(|p| p.file_name()).and_then(|n| n.to_str()).map(str::to_string);
    let own = name(Some(path)).unwrap_or_default();
    let parent = name(path.parent());
    let run = name(path.parent().and_then(Path::parent));
    parent.as_deref() == Some("worktrees")
        && run.is_some_and(|r| r.starts_with("run-"))
        && own.strip_prefix("pr-").is_some_and(|n| !n.is_empty() && n.bytes().all(|b| b.is_ascii_digit()))
}

/// Where a task on PR `pr` runs, under the run directory.
pub fn path_for(run_root: &Path, pr: u64) -> PathBuf {
    run_root.join("worktrees").join(format!("pr-{pr}"))
}

/// Make the worktree for a task on PR `pr`, whose branch is `branch`, or say
/// why not. The message is for the footer, so it names the PR.
pub fn prepare(repo_root: &Path, run_root: &Path, pr: u64, branch: &str, cross_repo: bool) -> Result<Prepared, String> {
    if cross_repo {
        return Err(format!("PR #{pr} is from a fork; its branch is not on origin"));
    }
    if branch.is_empty() || branch.starts_with('-') {
        return Err(format!("PR #{pr} has no branch this run can check out"));
    }
    let path = path_for(run_root, pr);
    if path.exists() {
        return Err(format!("PR #{pr}'s worktree from an earlier task is still at {}", path.display()));
    }
    let refspec = format!("+refs/heads/{branch}:refs/remotes/origin/{branch}");
    fetch(repo_root, &refspec).map_err(|e| format!("could not fetch PR #{pr}'s branch {branch}: {e}"))?;
    if let Some(at) = checked_out_at(repo_root, branch)? {
        return Err(format!("PR #{pr}'s branch {branch} is checked out at {}; switch it away first", at.display()));
    }
    let remote = format!("origin/{branch}");
    let local = format!("refs/heads/{branch}");
    let exists = git(repo_root, &["rev-parse", "--verify", "--quiet", &local]).is_ok();
    if exists && git(repo_root, &["merge-base", "--is-ancestor", &local, &remote]).is_err() {
        return Err(format!("your local branch {branch} has commits that are not on origin; push or move them first"));
    }
    // A branch of yours that tracks somewhere else would push the task's
    // work there, and changing what it tracks is not this run's to do.
    let tracks = git(repo_root, &["rev-parse", "--abbrev-ref", &format!("{branch}@{{upstream}}")]).ok();
    if let Some(upstream) = tracks.as_deref().filter(|u| *u != remote) {
        return Err(format!("your local branch {branch} tracks {upstream}, not {remote}"));
    }
    let where_to = path.display().to_string();
    let made = if exists {
        git(repo_root, &["worktree", "add", "--quiet", &where_to, branch])
    } else {
        git(repo_root, &["worktree", "add", "--quiet", "--track", "-b", branch, &where_to, &remote])
    };
    // A hook can fail after the worktree is made; either way, nothing is
    // left behind that would refuse the next task on this PR.
    if let Err(e) = made {
        undo(repo_root, &path, branch, !exists);
        return Err(format!("could not make a worktree for PR #{pr}: {e}"));
    }
    match finish_setup(&path, branch, &remote, exists, tracks.is_none()) {
        Ok(head) => Ok(Prepared { path, branch: branch.to_string(), pr, head, created_branch: !exists }),
        Err(e) => {
            undo(repo_root, &path, branch, !exists);
            Err(format!("could not make a worktree for PR #{pr}: {e}"))
        }
    }
}

/// Bring a worktree made on an existing branch up to origin, and read the
/// commit it starts at.
fn finish_setup(path: &Path, branch: &str, remote: &str, existed: bool, untracked: bool) -> Result<String, String> {
    if existed {
        // Behind origin, never ahead (checked before), so this only moves it
        // forward to the commit the PR is at.
        git(path, &["merge", "--quiet", "--ff-only", remote]).map_err(|e| format!("could not bring {branch} up to origin: {e}"))?;
        if untracked {
            git(path, &["branch", "--quiet", "--set-upstream-to", remote]).map_err(|e| format!("could not track {remote}: {e}"))?;
        }
    }
    git(path, &["rev-parse", "HEAD"]).map_err(|e| format!("could not read its head: {e}"))
}

/// Take back a worktree that `prepare` made and could not finish, and the
/// branch with it when `prepare` made that too.
fn undo(repo_root: &Path, path: &Path, branch: &str, created_branch: bool) {
    let where_is = path.display().to_string();
    let _ = git(repo_root, &["worktree", "remove", "--force", &where_is]);
    // Lowercase -d: a branch that appeared between the check and the add is
    // not ours, and -d refuses one that holds anything origin lacks.
    if created_branch {
        let _ = git(repo_root, &["branch", "--quiet", "-d", branch]);
    }
}

/// Remove the worktree when the task left nothing behind, and keep it when
/// it did: uncommitted changes, or commits it never pushed.
pub fn finish(repo_root: &Path, prepared: &Prepared) -> Finished {
    let path = &prepared.path;
    match git(path, &["status", "--porcelain"]) {
        Ok(changes) if !changes.is_empty() => return Finished::Kept("uncommitted changes".into()),
        Ok(_) => {}
        Err(e) => return Finished::Kept(format!("git could not read it: {e}")),
    }
    match git(path, &["rev-list", "--count", "@{upstream}..HEAD"]) {
        Ok(n) if n != "0" => return Finished::Kept("commits that are not pushed".into()),
        Ok(_) => {}
        Err(e) => return Finished::Kept(format!("git could not compare it with origin: {e}")),
    }
    let where_is = path.display().to_string();
    if let Err(e) = git(repo_root, &["worktree", "remove", &where_is]) {
        return Finished::Kept(format!("git could not remove it: {e}"));
    }
    if prepared.created_branch {
        // -d, not -D: it refuses a branch that is not fully pushed, which the
        // checks above already ruled out, so a refusal here loses nothing.
        let _ = git(repo_root, &["branch", "--quiet", "-d", &prepared.branch]);
    }
    Finished::Removed
}

/// A worktree as `prepare` would describe one, for the tests of the code
/// that is handed one.
#[cfg(test)]
pub(crate) fn tests_prepared(path: PathBuf, branch: &str) -> Prepared {
    Prepared { path, branch: branch.to_string(), pr: 4, head: "sha4".into(), created_branch: true }
}

/// The line a kept worktree gets, for the log and the view.
pub fn kept_note(pr: u64, why: &str, path: &Path) -> String {
    format!("note: PR #{pr}'s worktree has {why}: {}", path.display())
}

#[cfg(test)]
mod tests {
    use super::*;

    /// Git with no global or system config, so a developer's signing,
    /// hooks or default branch cannot change what the test sees.
    fn run(dir: &Path, args: &[&str]) -> String {
        let out = Command::new("git")
            .arg("-C")
            .arg(dir)
            .args(["-c", "user.name=t", "-c", "user.email=t@example.com", "-c", "commit.gpgsign=false", "-c", "init.defaultBranch=main"])
            .args(args)
            .env("GIT_CONFIG_GLOBAL", "/dev/null")
            .env("GIT_CONFIG_NOSYSTEM", "1")
            .output()
            .unwrap();
        assert!(out.status.success(), "git {args:?}: {}", String::from_utf8_lossy(&out.stderr));
        String::from_utf8_lossy(&out.stdout).trim().to_string()
    }

    /// A clone of a bare `origin` that has `main` and the PR's branch, and a
    /// run directory beside them.
    fn setup() -> (PathBuf, PathBuf, PathBuf) {
        let base = crate::rundir::make_unique_dir(&std::env::temp_dir(), "ar-wt.").unwrap();
        let origin = base.join("origin.git");
        let seed = base.join("seed");
        let clone = base.join("clone");
        std::fs::create_dir_all(&seed).unwrap();
        run(&base, &["init", "--quiet", "--bare", origin.to_str().unwrap()]);
        run(&seed, &["init", "--quiet"]);
        run(&seed, &["commit", "--quiet", "--allow-empty", "-m", "init"]);
        run(&seed, &["remote", "add", "origin", origin.to_str().unwrap()]);
        run(&seed, &["push", "--quiet", "origin", "main"]);
        run(&seed, &["switch", "--quiet", "-c", "me/work"]);
        run(&seed, &["commit", "--quiet", "--allow-empty", "-m", "work"]);
        run(&seed, &["push", "--quiet", "origin", "me/work"]);
        run(&base, &["clone", "--quiet", origin.to_str().unwrap(), clone.to_str().unwrap()]);
        let run_root = base.join("run");
        std::fs::create_dir_all(&run_root).unwrap();
        (base, clone, run_root)
    }

    fn origin_head(clone: &Path, branch: &str) -> String {
        run(clone, &["rev-parse", &format!("origin/{branch}")])
    }

    #[test]
    fn a_task_gets_the_pr_branch_tracking_origin_in_a_worktree_of_its_own() {
        let (base, clone, run_root) = setup();
        let p = prepare(&clone, &run_root, 4, "me/work", false).unwrap();
        assert_eq!(p.path, run_root.join("worktrees/pr-4"));
        assert_eq!(run(&p.path, &["rev-parse", "--abbrev-ref", "HEAD"]), "me/work");
        assert_eq!(run(&p.path, &["rev-parse", "--abbrev-ref", "@{upstream}"]), "origin/me/work");
        assert_eq!(run(&p.path, &["rev-parse", "HEAD"]), origin_head(&clone, "me/work"));
        assert_eq!(p.head, origin_head(&clone, "me/work"), "the head the readback compares with");
        assert_eq!(p.pr, 4);
        assert_eq!(run(&clone, &["rev-parse", "--abbrev-ref", "HEAD"]), "main", "your checkout is not touched");
        // A plain push from the worktree lands on the PR's branch.
        run(&p.path, &["commit", "--quiet", "--allow-empty", "-m", "fix"]);
        run(&p.path, &["push", "--quiet"]);
        assert_eq!(run(&p.path, &["rev-parse", "HEAD"]), origin_head(&clone, "me/work"));
        assert_eq!(finish(&clone, &p), Finished::Removed);
        assert!(!p.path.exists());
        assert!(run(&clone, &["branch", "--list", "me/work"]).is_empty(), "the branch it made goes with it");
        let _ = std::fs::remove_dir_all(&base);
    }

    #[test]
    fn a_branch_checked_out_elsewhere_is_refused() {
        let (base, clone, run_root) = setup();
        run(&clone, &["switch", "--quiet", "me/work"]);
        let e = prepare(&clone, &run_root, 4, "me/work", false).unwrap_err();
        assert!(e.contains("PR #4's branch me/work is checked out at"), "{e}");
        assert!(!run_root.join("worktrees/pr-4").exists());
        let _ = std::fs::remove_dir_all(&base);
    }

    #[test]
    fn a_local_branch_with_unpushed_commits_is_refused() {
        let (base, clone, run_root) = setup();
        run(&clone, &["branch", "--quiet", "me/work", "origin/me/work"]);
        let wt = base.join("side");
        run(&clone, &["worktree", "add", "--quiet", wt.to_str().unwrap(), "me/work"]);
        run(&wt, &["commit", "--quiet", "--allow-empty", "-m", "mine, not pushed"]);
        run(&clone, &["worktree", "remove", wt.to_str().unwrap()]);
        let e = prepare(&clone, &run_root, 4, "me/work", false).unwrap_err();
        assert!(e.contains("has commits that are not on origin"), "{e}");
        let _ = std::fs::remove_dir_all(&base);
    }

    #[test]
    fn a_local_branch_behind_origin_is_brought_up_and_kept_afterwards() {
        let (base, clone, run_root) = setup();
        run(&clone, &["branch", "--quiet", "me/work", "main"]);
        let p = prepare(&clone, &run_root, 4, "me/work", false).unwrap();
        assert_eq!(run(&p.path, &["rev-parse", "HEAD"]), origin_head(&clone, "me/work"));
        assert_eq!(finish(&clone, &p), Finished::Removed);
        assert!(!run(&clone, &["branch", "--list", "me/work"]).is_empty(), "a branch that was yours stays");
        let _ = std::fs::remove_dir_all(&base);
    }

    #[test]
    fn a_missing_worktree_that_is_not_ours_is_refused() {
        let (base, clone, run_root) = setup();
        let gone = base.join("gone");
        run(&clone, &["worktree", "add", "--quiet", gone.to_str().unwrap(), "-b", "me/work", "origin/me/work"]);
        std::fs::rename(&gone, base.join("moved-away")).unwrap();
        // Not one of ours: it may be on a disk that is not mounted.
        let e = prepare(&clone, &run_root, 4, "me/work", false).unwrap_err();
        assert!(e.contains("which is not there; if it is gone for good, run git worktree prune"), "{e}");
        let _ = std::fs::remove_dir_all(&base);
    }

    #[test]
    fn a_local_branch_that_tracks_somewhere_else_is_refused() {
        let (base, clone, run_root) = setup();
        run(&clone, &["branch", "--quiet", "--track", "me/work", "origin/main"]);
        let e = prepare(&clone, &run_root, 4, "me/work", false).unwrap_err();
        assert_eq!(e, "your local branch me/work tracks origin/main, not origin/me/work");
        let _ = std::fs::remove_dir_all(&base);
    }

    #[test]
    fn work_left_behind_keeps_the_worktree() {
        let (base, clone, run_root) = setup();
        let p = prepare(&clone, &run_root, 4, "me/work", false).unwrap();
        std::fs::write(p.path.join("half.txt"), "edit").unwrap();
        assert_eq!(finish(&clone, &p), Finished::Kept("uncommitted changes".into()));
        run(&p.path, &["add", "half.txt"]);
        run(&p.path, &["commit", "--quiet", "-m", "done, not pushed"]);
        assert_eq!(finish(&clone, &p), Finished::Kept("commits that are not pushed".into()));
        assert!(p.path.exists());
        assert_eq!(
            kept_note(4, "commits that are not pushed", &p.path),
            format!("note: PR #4's worktree has commits that are not pushed: {}", p.path.display())
        );
        let _ = std::fs::remove_dir_all(&base);
    }

    #[test]
    fn the_reason_is_git_s_fatal_line_not_its_hints() {
        let ssh = "git@github.com: Permission denied (publickey).\nfatal: Could not read from remote repository.\n\nPlease make sure you have the correct access rights\nand the repository exists.\n";
        assert_eq!(
            reason(ssh).as_deref(),
            Some("git@github.com: Permission denied (publickey). fatal: Could not read from remote repository.")
        );
        assert_eq!(reason("error: something\n").as_deref(), Some("error: something"));
        assert_eq!(reason("\n\n"), None);
    }

    #[test]
    fn only_a_stale_worktree_this_tool_made_is_removed() {
        assert!(made_here(Path::new("/tmp/run-x/worktrees/pr-4")));
        assert!(!made_here(Path::new("/Volumes/usb/work")));
        assert!(!made_here(Path::new("/Volumes/usb/worktrees/pr-12")), "yours, by the same name");
        assert!(!made_here(Path::new("/tmp/run-x/worktrees/pr-")));
        let (base, clone, run_root) = setup();
        let mine = base.join("run-old").join("worktrees").join("pr-9");
        run(&clone, &["worktree", "add", "--quiet", mine.to_str().unwrap(), "-b", "me/work", "origin/me/work"]);
        std::fs::remove_dir_all(&mine).unwrap();
        run(&clone, &["branch", "--quiet", "--unset-upstream", "me/work"]);
        assert!(prepare(&clone, &run_root, 4, "me/work", false).is_ok(), "an old task worktree of ours is cleared");
        let _ = std::fs::remove_dir_all(&base);
    }

    #[test]
    fn a_failed_hook_after_the_worktree_is_made_leaves_nothing_behind() {
        let (base, clone, run_root) = setup();
        let hooks = clone.join(".git/hooks");
        std::fs::write(hooks.join("post-checkout"), "#!/bin/sh\nexit 1\n").unwrap();
        use std::os::unix::fs::PermissionsExt;
        std::fs::set_permissions(hooks.join("post-checkout"), std::fs::Permissions::from_mode(0o755)).unwrap();
        let e = prepare(&clone, &run_root, 4, "me/work", false).unwrap_err();
        assert!(e.starts_with("could not make a worktree for PR #4"), "{e}");
        assert!(!run_root.join("worktrees/pr-4").exists());
        assert!(run(&clone, &["branch", "--list", "me/work"]).is_empty(), "the branch it made is gone too");
        let _ = std::fs::remove_dir_all(&base);
    }

    #[test]
    fn a_failed_fetch_says_why_in_git_s_words() {
        let (base, clone, run_root) = setup();
        let e = prepare(&clone, &run_root, 4, "me/not-there", false).unwrap_err();
        let git_said = e.strip_prefix("could not fetch PR #4's branch me/not-there: ").unwrap_or_else(|| panic!("{e}"));
        assert!(git_said.starts_with("fatal:"), "git's own words: {git_said}");
        assert!(git_said.contains("me/not-there"), "naming the ref it could not find: {git_said}");
        let _ = std::fs::remove_dir_all(&base);
    }

    #[test]
    fn a_fork_or_a_missing_branch_is_refused_before_git_runs() {
        let dir = Path::new("/nonexistent");
        assert!(prepare(dir, dir, 4, "me/work", true).unwrap_err().contains("is from a fork"));
        assert!(prepare(dir, dir, 4, "", false).unwrap_err().contains("has no branch"));
        assert!(prepare(dir, dir, 4, "--upload-pack=x", false).unwrap_err().contains("has no branch"));
    }
}
