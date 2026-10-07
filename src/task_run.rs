//! The parts of running a task that a review does not have: the refusals,
//! the worktree made off the event loop, the job itself, and what it did to
//! the PR, read back from GitHub.
//!
//! A task changes your PR's branch, so the pool says no before it starts
//! one for any reason that would make it run wrong: a skill that is not
//! installed, a run that promised to leave PRs alone, a PR already being
//! worked on.

use crate::cli::Config;
use crate::job::Job;
use crate::pool::Event;
use crate::report::Readback;
use crate::repo::RepoContext;
use crate::rundir::RunDir;
use crate::session::{self, SessionFlag};
use crate::task::{self, Request};
use crate::task_worktree::{self, Finished, Prepared};
use std::path::{Path, PathBuf};
use std::sync::mpsc::Sender;

/// What the pool keeps beside a task's job: its worktree, which also holds
/// the commit the PR was at when it started, and where its files go.
pub struct Running {
    pub worktree: Prepared,
    pub dir: RunDir,
}

/// Why this task must not start, or None. `busy` is whether the pass is
/// already working on that PR.
pub fn refusal(request: &Request, cfg: &Config, roots: &[PathBuf], busy: bool) -> Option<String> {
    let doing = request.task.doing();
    let why = if cfg.no_post {
        Some("--no-post leaves every PR alone".to_string())
    } else if busy {
        Some("a job on it is already running".to_string())
    } else {
        task::not_installed(request.task, roots, &cfg.orchestrator)
    };
    why.map(|why| format!("note: not {doing} PR #{}: {why}", request.pr))
}

/// Make the worktree on a thread of its own and post the result: a fetch
/// can take seconds, and the pool's loop must not stop for it.
pub fn prepare(request: Request, repo_root: PathBuf, run_root: PathBuf, tx: Sender<Event>) {
    std::thread::spawn(move || {
        let worktree = task_worktree::prepare(&repo_root, &run_root, request.pr, &request.branch, request.cross_repo);
        let _ = tx.send(Event::TaskReady { request, worktree });
    });
}

/// The job for a task whose worktree is ready.
pub fn job(request: &Request, worktree: &Prepared, cfg: &Config, ctx: &RepoContext) -> Job {
    let mut job = Job::new(request.pr);
    job.task = request.task;
    job.cwd = Some(worktree.path.clone());
    job.title = request.title.clone();
    job.author = ctx.me.clone();
    job.orchestrator = cfg.orchestrator.clone();
    // A session of its own, pinned so the view can follow the transcript
    // and `r` can reopen it. Never the PR's review session.
    if job.orchestrator.supports_sessions()
        && let Some(id) = session::fresh_id()
    {
        job.flag = SessionFlag::Pin(id.clone());
        job.sid = Some(id);
    }
    job
}

/// What the task did to the PR, in GitHub's words: whether its head moved.
/// The VERDICT column stays a fact read back from GitHub, as it is for a
/// review (decision 0002).
pub fn readback(pr: u64, before: &str) -> Readback {
    let n = pr.to_string();
    match crate::gh::output(&["pr", "view", &n, "--json", "headRefOid", "--jq", ".headRefOid"]) {
        Some(out) => Readback::Landed(pushed(before, String::from_utf8_lossy(&out).trim()).into()),
        None => Readback::Failed,
    }
}

fn pushed(before: &str, after: &str) -> &'static str {
    if after.is_empty() || after == before { "nothing pushed" } else { "pushed" }
}

/// The verdict a task's readback gives, and a note when it gave none. An
/// unread branch says "unknown" in the column: "nothing posted", the
/// default for a review, would claim something nobody checked.
pub fn verdict(pr: u64, readback: Option<Readback>) -> (Option<String>, Option<String>) {
    match readback {
        Some(Readback::Landed(word)) => (Some(word), None),
        _ => (Some("unknown".into()), Some(format!("note: could not read PR #{pr}'s branch back from GitHub; whether it pushed is unknown"))),
    }
}

/// Remove the task's worktree, or say why it is kept.
pub fn finish(repo_root: &Path, pr: u64, worktree: &Prepared) -> Option<String> {
    match task_worktree::finish(repo_root, worktree) {
        Finished::Removed => None,
        Finished::Kept(why) => Some(task_worktree::kept_note(pr, &why, &worktree.path)),
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::task::Task;

    fn request() -> Request {
        Request {
            pr: 4,
            task: Task::Babysit,
            title: "My own work".into(),
            branch: "me/my-own-work".into(),
            cross_repo: false,
        }
    }

    fn cfg() -> Config {
        let Ok(crate::cli::Parsed::Run(cfg)) = crate::cli::parse(Vec::new(), &|_| None) else {
            panic!("an empty command line parses");
        };
        *cfg
    }

    fn installed() -> (PathBuf, Vec<PathBuf>) {
        let dir = crate::rundir::make_unique_dir(&std::env::temp_dir(), "ar-taskrun.").unwrap();
        for skill in ["babysit-pr", "pr-comment-handler"] {
            std::fs::create_dir_all(dir.join(skill)).unwrap();
            std::fs::write(dir.join(skill).join("SKILL.md"), "").unwrap();
        }
        (dir.clone(), vec![dir])
    }

    #[test]
    fn a_task_is_refused_for_the_reason_that_would_make_it_run_wrong() {
        let (dir, roots) = installed();
        assert_eq!(refusal(&request(), &cfg(), &roots, false), None);
        assert_eq!(
            refusal(&request(), &cfg(), &roots, true).as_deref(),
            Some("note: not babysitting PR #4: a job on it is already running")
        );
        let no_post = Config { no_post: true, ..cfg() };
        assert_eq!(
            refusal(&request(), &no_post, &roots, false).as_deref(),
            Some("note: not babysitting PR #4: --no-post leaves every PR alone")
        );
        assert_eq!(
            refusal(&request(), &cfg(), &[], false).as_deref(),
            Some("note: not babysitting PR #4: babysit-pr is not installed where claude looks (~/.claude/skills)")
        );
        let _ = std::fs::remove_dir_all(&dir);
    }

    #[test]
    fn a_task_job_runs_in_its_worktree_in_a_session_of_its_own() {
        let ctx = RepoContext { owner: "acme".into(), name: "widgets".into(), repo_root: PathBuf::from("/src"), me: "me".into() };
        let worktree = task_worktree::tests_prepared(PathBuf::from("/run/worktrees/pr-4"), "me/my-own-work");
        let job = job(&request(), &worktree, &cfg(), &ctx);
        assert_eq!(job.task, Task::Babysit);
        assert_eq!(job.cwd.as_deref(), Some(Path::new("/run/worktrees/pr-4")));
        assert_eq!(job.author, "me");
        let Some(sid) = &job.sid else { panic!("a claude task pins a session") };
        assert_eq!(job.flag, SessionFlag::Pin(sid.clone()));
        assert_ne!(sid, &session::pr_session_id(&ctx.repo_root, "acme", "widgets", 4), "never the review's session");
    }

    #[test]
    fn the_verdict_is_whether_the_head_moved() {
        assert_eq!(pushed("sha4", "sha5"), "pushed");
        assert_eq!(pushed("sha4", "sha4"), "nothing pushed");
        assert_eq!(pushed("sha4", ""), "nothing pushed");
        assert_eq!(verdict(4, Some(Readback::Landed("pushed".into()))), (Some("pushed".into()), None));
        let (v, note) = verdict(4, Some(Readback::Failed));
        assert_eq!(v.as_deref(), Some("unknown"));
        assert_eq!(note.as_deref(), Some("note: could not read PR #4's branch back from GitHub; whether it pushed is unknown"));
    }
}
