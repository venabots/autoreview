//! What a job is for. Most jobs review somebody else's PR. A task is work on
//! one of your own PRs, started from the view: babysit it (conflicts,
//! comments, CI) or answer its review comments.
//!
//! A task is not a review, and the difference reaches every part of the run:
//! it runs a skill the person installed, in its own worktree, writes no
//! review and has no verdict to read back. Keeping the kind on the job is
//! what lets each of those places ask once instead of guessing from the
//! prompt.

use crate::orchestrator::Orchestrator;
use std::path::{Path, PathBuf};

/// The work a job does.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Hash, Default)]
pub enum Task {
    /// Review somebody else's PR. Everything the run did before tasks.
    #[default]
    Review,
    /// Get your own PR mergeable: conflicts, review comments and CI.
    Babysit,
    /// Answer the review comments on your own PR.
    Comments,
}

impl Task {
    /// The installed skill a task runs, or None for a review, whose skill
    /// depends on the run (see `Job::prompt`).
    pub fn skill(self) -> Option<&'static str> {
        match self {
            Task::Review => None,
            Task::Babysit => Some("babysit-pr"),
            Task::Comments => Some("pr-comment-handler"),
        }
    }

    /// The word for a task while it runs: "babysitting 1m12s".
    pub fn doing(self) -> &'static str {
        match self {
            Task::Review => "reviewing",
            Task::Babysit => "babysitting",
            Task::Comments => "fixing comments",
        }
    }

    /// The name a task's files carry, so they never overwrite a review's.
    /// None for a review, whose files keep the names they always had.
    pub fn file_tag(self) -> Option<&'static str> {
        match self {
            Task::Review => None,
            Task::Babysit => Some("babysit"),
            Task::Comments => Some("comments"),
        }
    }

    pub fn is_review(self) -> bool {
        self == Task::Review
    }
}

/// A person asked for a task on one of their PRs: everything the run needs
/// to start it, as the My PRs list knew it at the key press.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct Request {
    pub pr: u64,
    pub task: Task,
    pub title: String,
    pub branch: String,
    /// The head commit when the key was pressed, which the readback compares
    /// with to say whether the task pushed.
    pub head: String,
    pub cross_repo: bool,
}

/// Where the orchestrator finds an installed skill, in the order it reads
/// them. A task's skill is never staged, so this is the only place it can
/// come from.
pub fn skill_roots(orch: &Orchestrator, repo_root: &Path) -> Vec<PathBuf> {
    if orch.backend == "codex" {
        return crate::orchestrator::skills_roots(
            "codex",
            std::env::var("HOME").ok().as_deref(),
            std::env::var("CODEX_HOME").ok().as_deref(),
        );
    }
    vec![crate::session::config_dir().join("skills"), repo_root.join(".claude").join("skills")]
}

/// Why `task` cannot run under `orch`, or None when its skill is installed.
/// Said before anything is made: a worktree, a session and a job slot all
/// wasted on a skill that is not there is the failure this prevents.
pub fn not_installed(task: Task, roots: &[PathBuf], orch: &Orchestrator) -> Option<String> {
    let skill = task.skill()?;
    let missing = crate::orchestrator::missing_skills(roots, &[skill]);
    (!missing.is_empty()).then(|| format!("{skill} is not installed where {} looks ({})", orch.label(), orch.skills_home()))
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn a_task_whose_skill_is_missing_says_where_it_looked() {
        let dir = crate::rundir::make_unique_dir(&std::env::temp_dir(), "ar-task.").unwrap();
        let claude = Orchestrator::claude();
        let roots = vec![dir.clone()];
        assert_eq!(
            not_installed(Task::Babysit, &roots, &claude).as_deref(),
            Some("babysit-pr is not installed where claude looks (~/.claude/skills)")
        );
        std::fs::create_dir_all(dir.join("babysit-pr")).unwrap();
        std::fs::write(dir.join("babysit-pr/SKILL.md"), "").unwrap();
        assert_eq!(not_installed(Task::Babysit, &roots, &claude), None);
        assert!(not_installed(Task::Comments, &roots, &claude).is_some());
        assert_eq!(not_installed(Task::Review, &roots, &claude), None, "a review's skills are staged");
        let _ = std::fs::remove_dir_all(&dir);
    }

    #[test]
    fn claude_finds_a_task_skill_in_your_skills_or_the_repo() {
        let roots = skill_roots(&Orchestrator::claude(), Path::new("/src/app"));
        assert_eq!(roots[1], PathBuf::from("/src/app/.claude/skills"));
        assert!(roots[0].ends_with("skills"));
    }

    #[test]
    fn a_task_names_the_installed_skill_it_runs() {
        assert_eq!(Task::Review.skill(), None, "a review's skill depends on the run");
        assert_eq!(Task::Babysit.skill(), Some("babysit-pr"));
        assert_eq!(Task::Comments.skill(), Some("pr-comment-handler"));
    }

    #[test]
    fn a_task_says_what_it_is_doing() {
        assert_eq!(Task::Review.doing(), "reviewing");
        assert_eq!(Task::Babysit.doing(), "babysitting");
        assert_eq!(Task::Comments.doing(), "fixing comments");
    }

    #[test]
    fn only_a_task_renames_its_files() {
        assert_eq!(Task::Review.file_tag(), None);
        assert_eq!(Task::Babysit.file_tag(), Some("babysit"));
        assert_eq!(Task::Comments.file_tag(), Some("comments"));
        assert_eq!(Task::default(), Task::Review);
    }
}
