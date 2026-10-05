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
use std::process::Command;

/// A worktree made for one task.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct Prepared {
    pub path: PathBuf,
    pub branch: String,
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

fn git(dir: &Path, args: &[&str]) -> Result<String, String> {
    let out = Command::new("git")
        .arg("-C")
        .arg(dir)
        .args(args)
        .output()
        .map_err(|e| format!("could not run git: {e}"))?;
    if out.status.success() {
        Ok(String::from_utf8_lossy(&out.stdout).trim().to_string())
    } else {
        Err(String::from_utf8_lossy(&out.stderr).trim().to_string())
    }
}

/// Where a worktree has `branch` checked out, if anywhere.
fn checked_out_at(repo_root: &Path, branch: &str) -> Result<Option<PathBuf>, String> {
    let list = git(repo_root, &["worktree", "list", "--porcelain"])?;
    let wanted = format!("branch refs/heads/{branch}");
    let mut path = None;
    for line in list.lines() {
        if let Some(p) = line.strip_prefix("worktree ") {
            path = Some(PathBuf::from(p));
        } else if line == wanted {
            return Ok(path);
        }
    }
    Ok(None)
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
    git(repo_root, &["fetch", "--quiet", "origin", &refspec])
        .map_err(|e| format!("could not fetch PR #{pr}'s branch {branch}: {e}"))?;
    if let Some(at) = checked_out_at(repo_root, branch)? {
        return Err(format!("PR #{pr}'s branch {branch} is checked out at {}; switch it away first", at.display()));
    }
    let remote = format!("origin/{branch}");
    let local = format!("refs/heads/{branch}");
    let exists = git(repo_root, &["rev-parse", "--verify", "--quiet", &local]).is_ok();
    if exists && git(repo_root, &["merge-base", "--is-ancestor", &local, &remote]).is_err() {
        return Err(format!("your local branch {branch} has commits that are not on origin; push or move them first"));
    }
    let where_to = path.display().to_string();
    if exists {
        git(repo_root, &["worktree", "add", "--quiet", &where_to, branch]).map_err(|e| format!("could not make a worktree for PR #{pr}: {e}"))?;
        // Behind origin, never ahead (checked above), so this only moves it
        // forward to the commit the PR is at.
        git(&path, &["merge", "--quiet", "--ff-only", &remote]).map_err(|e| format!("could not bring {branch} up to origin: {e}"))?;
        git(&path, &["branch", "--quiet", "--set-upstream-to", &remote]).map_err(|e| format!("could not track {remote}: {e}"))?;
    } else {
        git(repo_root, &["worktree", "add", "--quiet", "--track", "-b", branch, &where_to, &remote])
            .map_err(|e| format!("could not make a worktree for PR #{pr}: {e}"))?;
    }
    Ok(Prepared { path, branch: branch.to_string(), created_branch: !exists })
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
    fn a_fork_or_a_missing_branch_is_refused_before_git_runs() {
        let dir = Path::new("/nonexistent");
        assert!(prepare(dir, dir, 4, "me/work", true).unwrap_err().contains("is from a fork"));
        assert!(prepare(dir, dir, 4, "", false).unwrap_err().contains("has no branch"));
        assert!(prepare(dir, dir, 4, "--upload-pack=x", false).unwrap_err().contains("has no branch"));
    }
}
