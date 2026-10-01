//! The eyes reaction a review leaves on its PR while it runs.
//!
//! A review takes minutes and says nothing on the PR until it posts. Without
//! a mark the author cannot tell a PR that is being read from one nobody has
//! picked up, and a second reviewer cannot tell either. The reaction goes on
//! when the review starts and comes off when it ends, however it ends: the
//! verdict is the review's to leave, and a mark that outlived it would say a
//! review is running when none is.
//!
//! Every call here runs on its own thread. A slow GitHub must not stall the
//! pass, and a reaction that could not be set is a note, never a failed
//! review.

use crate::gh;
use std::collections::HashMap;
use std::thread::JoinHandle;

const CONTENT: &str = "eyes";

fn reactions_path(owner: &str, name: &str, pr: u64) -> String {
    format!("repos/{owner}/{name}/issues/{pr}/reactions")
}

fn add_args(owner: &str, name: &str, pr: u64) -> Vec<String> {
    vec![
        "api".into(),
        reactions_path(owner, name, pr),
        "--method".into(),
        "POST".into(),
        "-f".into(),
        format!("content={CONTENT}"),
        "--jq".into(),
        ".id".into(),
    ]
}

fn remove_args(owner: &str, name: &str, pr: u64, id: u64) -> Vec<String> {
    vec![
        "api".into(),
        format!("{}/{id}", reactions_path(owner, name, pr)),
        "--method".into(),
        "DELETE".into(),
    ]
}

fn run(args: &[String]) -> Option<Vec<u8>> {
    let args: Vec<&str> = args.iter().map(String::as_str).collect();
    gh::output(&args)
}

/// Put the reaction on, and say which reaction it is. GitHub answers the
/// same way when this login's reaction is already there, so asking twice is
/// how a later call learns the id of one it did not set.
fn add(owner: &str, name: &str, pr: u64) -> Option<u64> {
    let out = run(&add_args(owner, name, pr))?;
    String::from_utf8_lossy(&out).trim().parse().ok()
}

fn remove(owner: &str, name: &str, pr: u64, id: u64) -> bool {
    run(&remove_args(owner, name, pr, id)).is_some()
}

/// The marks one pass has out. `show` and `clear` return at once; `settle`
/// waits for what they started.
pub struct Marks {
    owner: String,
    name: String,
    /// Off under --no-post, which promises to leave the PR alone.
    enabled: bool,
    /// A mark that is on, or on its way: the reaction's id once known.
    shown: HashMap<u64, JoinHandle<Option<u64>>>,
    /// A mark on its way off: whether it came off.
    clearing: Vec<(u64, JoinHandle<bool>)>,
}

impl Marks {
    pub fn new(owner: &str, name: &str, enabled: bool) -> Marks {
        Marks {
            owner: owner.to_string(),
            name: name.to_string(),
            enabled,
            shown: HashMap::new(),
            clearing: Vec::new(),
        }
    }

    /// Mark the PR as being reviewed. A PR already marked is left alone: a
    /// review retried under the fallback is still the same review.
    pub fn show(&mut self, pr: u64) {
        if !self.enabled || self.shown.contains_key(&pr) {
            return;
        }
        let (owner, name) = (self.owner.clone(), self.name.clone());
        self.shown.insert(pr, std::thread::spawn(move || add(&owner, &name, pr)));
    }

    /// Take the mark off. It waits for the mark to land first, on its own
    /// thread: a review that failed in its first second would otherwise
    /// remove a reaction that arrives a moment later and then stays.
    ///
    /// A mark that never reported an id is asked for again before it is
    /// given up on. A call that timed out may still have landed, and the
    /// reaction it left would say a review is running for as long as nobody
    /// looked.
    pub fn clear(&mut self, pr: u64) {
        let Some(shown) = self.shown.remove(&pr) else { return };
        let (owner, name) = (self.owner.clone(), self.name.clone());
        let cleared = std::thread::spawn(move || {
            let landed = shown.join().ok().flatten();
            match landed.or_else(|| add(&owner, &name, pr)) {
                Some(id) => remove(&owner, &name, pr, id),
                None => false,
            }
        });
        self.clearing.push((pr, cleared));
    }

    /// Take every mark off: the pass is being interrupted, and no review it
    /// started will reach its own end.
    pub fn clear_all(&mut self) {
        let prs: Vec<u64> = self.shown.keys().copied().collect();
        for pr in prs {
            self.clear(pr);
        }
    }

    /// Whether a mark is still on its way off.
    pub fn clearing(&self) -> bool {
        self.clearing.iter().any(|(_, cleared)| !cleared.is_finished())
    }

    /// Wait for every mark that is on its way off, and name the PRs whose
    /// mark could not be set or could not be removed. The process may end
    /// right after a pass, and a thread it did not wait for is a reaction
    /// left behind.
    pub fn settle(&mut self) -> Vec<u64> {
        let mut failed: Vec<u64> = self
            .clearing
            .drain(..)
            .filter_map(|(pr, cleared)| (!cleared.join().unwrap_or(false)).then_some(pr))
            .collect();
        failed.sort_unstable();
        failed
    }
}

/// What the run says about a mark that failed. One line: the review itself
/// is unaffected, and the PR may still carry the reaction.
pub fn failed_note(pr: u64) -> String {
    format!("note: could not set or remove the eyes reaction on PR #{pr}; the review is unaffected")
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn the_reaction_goes_on_the_pr_as_an_issue_reaction() {
        // A PR is an issue to this endpoint. --jq .id is what lets the
        // removal name the reaction it is removing.
        assert_eq!(
            add_args("acme", "widgets", 9).join(" "),
            "api repos/acme/widgets/issues/9/reactions --method POST -f content=eyes --jq .id"
        );
    }

    #[test]
    fn the_removal_names_one_reaction() {
        // Never the collection: that path with DELETE is not an endpoint,
        // and somebody else's reaction is not ours to remove.
        assert_eq!(
            remove_args("acme", "widgets", 9, 77).join(" "),
            "api repos/acme/widgets/issues/9/reactions/77 --method DELETE"
        );
    }

    #[test]
    fn a_run_that_leaves_the_pr_alone_marks_nothing() {
        let mut marks = Marks::new("acme", "widgets", false);
        marks.show(9);
        marks.clear(9);
        marks.clear_all();
        assert!(!marks.clearing());
        assert!(marks.settle().is_empty(), "nothing was started, so nothing failed");
    }

    #[test]
    fn clearing_a_pr_that_was_never_marked_does_nothing() {
        // A review that could not be spawned has no mark to take off.
        let mut marks = Marks::new("acme", "widgets", true);
        marks.clear(9);
        assert!(marks.settle().is_empty());
    }

    #[test]
    fn the_note_names_the_pr_and_spares_the_review() {
        assert_eq!(
            failed_note(9),
            "note: could not set or remove the eyes reaction on PR #9; the review is unaffected"
        );
    }
}
