//! What a job is for. Most jobs review somebody else's PR. A task is work on
//! one of your own PRs, started from the view: babysit it (conflicts,
//! comments, CI) or answer its review comments.
//!
//! A task is not a review, and the difference reaches every part of the run:
//! it runs a skill the person installed, in its own worktree, writes no
//! review and has no verdict to read back. Keeping the kind on the job is
//! what lets each of those places ask once instead of guessing from the
//! prompt.

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

#[cfg(test)]
mod tests {
    use super::*;

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
