//! Which open PRs sit on top of other open PRs.
//!
//! Two shapes, and each needs its own test.
//!
//! **A declared stack.** The PR's base is another open PR's branch, which is
//! what GitHub's stacked PRs and every stacking tool produce. GitHub diffs it
//! from that branch, so its diff is clean -- but its code only makes sense on
//! top of the PR below it, and reviewing it means reading that PR's work for
//! the integration anyway. It is found by the branch names, and it is the one
//! case the base branch does answer.
//!
//! **An undeclared stack.** The branch was cut from another open PR's branch
//! while it was in flight, and still says `base: main`. GitHub then serves the
//! diff from where the two branches parted, so that PR's commits are inside
//! this one's diff and reviewing both reads the same code twice. Measured on
//! this repo: #18's diff was 314KB across 23 files, of which 12KB across 5
//! files was its own work -- the other 96% was #15, open at the same time.
//! Nothing in `baseRefName` shows this. Both PRs said `base: main`. What shows
//! it is the commits, which the PR list already fetches.
//!
//! Each group of related PRs keeps exactly one reviewable member: the PR the
//! others are built on, or, when no single PR is underneath all of them, the
//! one opened first. Everything above it waits until it lands.

use crate::prlist::PrNode;
use std::collections::{HashMap, HashSet};

/// Why a PR is being left alone: the open PR underneath it, and how the two
/// were found to be related -- which is also how the held line reads.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Hash)]
pub struct StackedOn {
    pub pr: u64,
    pub why: Why,
}

#[derive(Debug, Clone, Copy, PartialEq, Eq, Hash)]
pub enum Why {
    /// This PR's base is that PR's branch: it is stacked on it by design.
    Base,
    /// This PR's diff carries that many of that PR's commits.
    Commits(usize),
}

impl StackedOn {
    /// The parenthesis after the PR number in the held line.
    fn detail(&self) -> String {
        match self.why {
            Why::Base => format!("based on #{}", self.pr),
            Why::Commits(n) => {
                format!("{} also in #{}", crate::ui::count(n, "commit"), self.pr)
            }
        }
    }
}

/// One PR reduced to what the two tests need.
struct Branch<'a> {
    number: u64,
    /// The branch this PR merges into. Named in the base repository, so it is
    /// only comparable with the head branch of a PR from that same repository.
    base_ref: Option<&'a str>,
    /// The branch this PR is, and None when it lives in a fork: a fork's
    /// branch name means nothing to another PR's base, and two forks both
    /// called `main` would otherwise read as a stack.
    head_ref: Option<&'a str>,
    /// The tip. A PR whose tip is a commit *inside* another PR is the one
    /// underneath it, which is what makes the relation directional.
    head: Option<&'a str>,
    /// The commits GitHub counts as this PR's own: base..head, so anything
    /// already in the base branch is not in here. Two PRs sharing one is two
    /// PRs whose diffs overlap.
    commits: HashSet<&'a str>,
}

/// Every PR sitting on top of another open PR, and which PR that is.
///
/// Every open PR counts, including drafts, approved ones, bots and your own.
/// A PR is held by the shape of the branches, not by whether this tool would
/// have reviewed the PR underneath it: a colleague's branch cut from your own
/// unmerged work carries your commits in its diff whether or not anyone
/// reviews yours.
pub fn parents(prs: &[PrNode]) -> HashMap<u64, StackedOn> {
    let branches: Vec<Branch> = prs.iter().map(branch).collect();
    let mut held = HashMap::new();
    for group in groups(&branches) {
        if group.len() < 2 {
            continue;
        }
        let keeper = keeper(&branches, &group);
        for &i in &group {
            if i == keeper {
                continue;
            }
            let on = nearest_below(&branches, &group, i).unwrap_or(keeper);
            // Guard against naming itself: only two PRs with the same tip can
            // get here, and "stacked on itself" would be nonsense to print.
            let on = if on == i { keeper } else { on };
            held.insert(branches[i].number, stacked_on(&branches, i, on));
        }
    }
    held
}

fn branch(pr: &PrNode) -> Branch<'_> {
    Branch {
        number: pr.number,
        base_ref: pr.base_ref_name.as_deref(),
        head_ref: pr.head_ref_name.as_deref().filter(|_| !pr.is_cross_repository),
        head: pr.head_ref_oid.as_deref(),
        commits: pr.commits.nodes.iter().filter_map(|c| c.commit.oid.as_deref()).collect(),
    }
}

/// How PR `above` was found to be sitting on PR `below`. The declared
/// relation is preferred: it is what the author said, and a stack whose
/// branches also happen to share commits is still a stack.
fn stacked_on(branches: &[Branch], above: usize, below: usize) -> StackedOn {
    let pr = branches[below].number;
    if based_on(branches, above, below) {
        return StackedOn { pr, why: Why::Base };
    }
    let shared = branches[above].commits.intersection(&branches[below].commits).count();
    StackedOn { pr, why: Why::Commits(shared) }
}

/// Does `above` merge into `below`'s branch?
fn based_on(branches: &[Branch], above: usize, below: usize) -> bool {
    above != below
        && branches[below].head_ref.is_some()
        && branches[above].base_ref == branches[below].head_ref
}

/// Is `above` on top of `below`, by either test? Directional: the declared
/// base points one way, and carrying the other PR's tip points one way.
/// Commit overlap alone is not directional, which is what `keeper` settles.
fn sits_on(branches: &[Branch], above: usize, below: usize) -> bool {
    based_on(branches, above, below)
        || (above != below
            && branches[below].head.is_some_and(|tip| branches[above].commits.contains(tip)))
}

/// The PRs of each group of related PRs, as indexes into `branches`. Union-find
/// over both relations, so a stack three deep and a pair of branches cut from
/// one commit each come out as a single group.
fn groups(branches: &[Branch]) -> Vec<Vec<usize>> {
    let mut parent: Vec<usize> = (0..branches.len()).collect();
    // Overlap, via the commits: two PRs holding the same commit are related,
    // and so is anything related to either of them.
    let mut owner: HashMap<&str, usize> = HashMap::new();
    for (i, b) in branches.iter().enumerate() {
        for oid in &b.commits {
            match owner.get(oid) {
                Some(&j) => union(&mut parent, i, j),
                None => {
                    owner.insert(oid, i);
                }
            }
        }
    }
    // The declared relation, via the branch names.
    for i in 0..branches.len() {
        for j in 0..branches.len() {
            if based_on(branches, i, j) {
                union(&mut parent, i, j);
            }
        }
    }
    let mut grouped: HashMap<usize, Vec<usize>> = HashMap::new();
    for i in 0..branches.len() {
        grouped.entry(find(&mut parent, i)).or_default().push(i);
    }
    // Sorted, so the held list comes out in a stable order rather than a hash
    // one: the same repo must produce the same line twice.
    let mut out: Vec<Vec<usize>> = grouped.into_values().collect();
    for group in &mut out {
        group.sort_by_key(|&i| branches[i].number);
    }
    out.sort_by_key(|g| g.first().map(|&i| branches[i].number));
    out
}

fn find(parent: &mut Vec<usize>, mut i: usize) -> usize {
    while parent[i] != i {
        parent[i] = parent[parent[i]];
        i = parent[i];
    }
    i
}

fn union(parent: &mut Vec<usize>, a: usize, b: usize) {
    let (ra, rb) = (find(parent, a), find(parent, b));
    if ra != rb {
        parent[ra] = rb;
    }
}

/// The one member of a group that is still reviewed: the PR with nothing
/// underneath it, and the lowest-numbered such PR when a group has several
/// (two branches cut from one unmerged commit, which is the shape the base
/// branch cannot see). A group where every member sits on another -- two PRs
/// sharing a tip, or a base cycle -- falls back to the lowest number, so a
/// group can never hold all of its members.
fn keeper(branches: &[Branch], group: &[usize]) -> usize {
    group
        .iter()
        .copied()
        .find(|&i| !group.iter().any(|&j| sits_on(branches, i, j)))
        .unwrap_or(group[0])
}

/// The PR immediately below this one: the branch it is based on if it declared
/// one, otherwise the largest PR whose tip it carries, which is the closest.
/// Naming the nearest rather than the bottom is what makes a three-deep stack
/// read as a chain instead of three PRs all pointing at the same one.
///
/// Both tests are directional, so a PR can never be told it is stacked on one
/// above it. Two branches cut from the same commit answer neither, and fall
/// back to the group's keeper.
fn nearest_below(branches: &[Branch], group: &[usize], i: usize) -> Option<usize> {
    if let Some(declared) = group.iter().copied().find(|&j| based_on(branches, i, j)) {
        return Some(declared);
    }
    group
        .iter()
        .copied()
        .filter(|&j| sits_on(branches, i, j))
        .max_by_key(|&j| (branches[j].commits.len(), std::cmp::Reverse(branches[j].number)))
}

/// The sweep's line for the PRs it is leaving alone this pass, shaped like the
/// CI one: what is held, why, and the flag that reviews them anyway.
pub fn held_line(held: &[(u64, StackedOn)]) -> String {
    let list: Vec<String> =
        held.iter().map(|(n, on)| format!("#{n} ({})", on.detail())).collect();
    let them = if held.len() == 1 { "it" } else { "them" };
    format!(
        "holding {} stacked on another PR: {}; --stacked reviews {them} anyway",
        crate::ui::count(held.len(), "PR"),
        list.join(" ")
    )
}

#[cfg(test)]
mod tests {
    use super::*;

    /// A PR with the commits named, the last of which is its tip, merging
    /// into `base` from a branch of its own named "pr<number>".
    fn pr(number: u64, base: &str, commits: &[&str]) -> PrNode {
        let nodes: Vec<String> = commits
            .iter()
            .map(|oid| format!(r#"{{"commit":{{"oid":"{oid}"}}}}"#))
            .collect();
        let head = commits.last().copied().unwrap_or("");
        let json = format!(
            r#"{{"number":{number},"title":"t","isDraft":false,
              "updatedAt":"2026-08-10T10:00:00Z","reviewDecision":null,
              "headRefOid":"{head}","baseRefName":"{base}","headRefName":"pr{number}",
              "author":{{"login":"alice"}},
              "comments":{{"nodes":[]}},"reviews":{{"nodes":[]}},
              "commits":{{"nodes":[{}]}}}}"#,
            nodes.join(",")
        );
        serde_json::from_str(&json).unwrap()
    }

    fn held_on(prs: &[PrNode], n: u64) -> Option<StackedOn> {
        parents(prs).get(&n).copied()
    }

    #[test]
    fn a_pr_based_on_another_open_prs_branch_is_held_on_it() {
        // The declared stack. GitHub diffs #16 from #15's branch, so their
        // commits do not overlap at all -- the branch names are the only
        // thing that says these two are one piece of work.
        let prs = vec![pr(15, "main", &["a", "b"]), pr(16, "pr15", &["c", "d"])];
        assert_eq!(held_on(&prs, 16), Some(StackedOn { pr: 15, why: Why::Base }));
        assert_eq!(held_on(&prs, 15), None, "the one underneath is still reviewed");
    }

    #[test]
    fn a_branch_cut_from_another_open_prs_branch_is_held_on_it() {
        // The undeclared stack, which is the expensive one: both say
        // "base: main", and #18's diff is mostly #15's work.
        let prs = vec![pr(15, "main", &["a", "b", "c", "d"]), pr(18, "main", &["a", "b", "e"])];
        assert_eq!(held_on(&prs, 18), Some(StackedOn { pr: 15, why: Why::Commits(2) }));
        assert_eq!(held_on(&prs, 15), None);
    }

    #[test]
    fn a_pr_that_carries_another_prs_tip_is_the_one_held() {
        // Overlap alone says which two PRs are related but not which is
        // underneath. Carrying the other's tip says it outright, whatever the
        // PR numbers are -- here the top of the stack was opened first.
        let prs = vec![pr(30, "main", &["a", "b", "c"]), pr(31, "main", &["a", "b"])];
        assert_eq!(held_on(&prs, 30).map(|s| s.pr), Some(31), "31 is underneath");
        assert_eq!(held_on(&prs, 31), None);
    }

    #[test]
    fn a_three_deep_stack_names_the_pr_directly_below_each_one() {
        let prs = vec![
            pr(19, "main", &["a", "b"]),
            pr(20, "pr19", &["c"]),
            pr(21, "pr20", &["d"]),
        ];
        let held = parents(&prs);
        assert_eq!(held.get(&20), Some(&StackedOn { pr: 19, why: Why::Base }));
        assert_eq!(held.get(&21), Some(&StackedOn { pr: 20, why: Why::Base }), "nearest");
        assert_eq!(held.get(&19), None, "one keeper for the whole stack");
    }

    #[test]
    fn a_three_deep_undeclared_stack_names_the_nearest_too() {
        let prs = vec![
            pr(19, "main", &["a", "b"]),
            pr(20, "main", &["a", "b", "c"]),
            pr(21, "main", &["a", "b", "c", "d"]),
        ];
        let held = parents(&prs);
        assert_eq!(held.get(&20), Some(&StackedOn { pr: 19, why: Why::Commits(2) }));
        assert_eq!(held.get(&21), Some(&StackedOn { pr: 20, why: Why::Commits(3) }));
        assert_eq!(held.get(&19), None);
    }

    #[test]
    fn two_prs_off_one_parent_are_both_held_and_the_parent_is_not() {
        let prs = vec![
            pr(19, "main", &["a", "b"]),
            pr(20, "pr19", &["c"]),
            pr(21, "pr19", &["d"]),
        ];
        let held = parents(&prs);
        assert_eq!(held.get(&20).map(|s| s.pr), Some(19));
        assert_eq!(held.get(&21).map(|s| s.pr), Some(19));
        assert_eq!(held.get(&19), None);
    }

    #[test]
    fn unrelated_prs_are_never_held() {
        // Different work, and the ordinary case of several PRs all merging
        // into main: a shared base branch is not a stack.
        let prs = vec![
            pr(9, "main", &["a", "b"]),
            pr(8, "main", &["c"]),
            pr(6, "develop", &["d", "e"]),
        ];
        assert!(parents(&prs).is_empty());
    }

    #[test]
    fn a_pr_with_no_commit_data_is_left_alone() {
        // Every fixture in this repo's suite predates the oid field, and each
        // must stay reviewable: no commits means no overlap anyone can prove.
        let prs = vec![pr(9, "main", &[]), pr(8, "main", &[]), pr(7, "main", &["a"])];
        assert!(parents(&prs).is_empty());
    }

    #[test]
    fn two_forks_with_the_same_branch_name_are_not_a_stack() {
        // `headRefName` is a name in the head repository. Two contributors
        // working on their own fork's `patch-1`, both merging into main,
        // would otherwise each read as based on the other.
        let mut prs = vec![pr(9, "patch-1", &["a"]), pr(8, "main", &["b"])];
        prs[1].head_ref_name = Some("patch-1".into());
        prs[1].is_cross_repository = true;
        assert!(parents(&prs).is_empty(), "a fork branch names nothing here");

        // The same two branches in the base repository are a stack.
        prs[1].is_cross_repository = false;
        assert_eq!(held_on(&prs, 9).map(|s| s.pr), Some(8));
    }

    #[test]
    fn a_group_always_keeps_one_reviewable_pr() {
        // Two PRs on the same tip sit on each other, so neither is a keeper by
        // the ancestry rule. Holding both would leave work nobody reviews.
        let prs = vec![pr(9, "main", &["a", "b"]), pr(8, "main", &["a", "b"])];
        let held = parents(&prs);
        assert_eq!(held.len(), 1, "exactly one of the two is held: {held:?}");
        assert!(held.contains_key(&9), "the later one waits");
    }

    #[test]
    fn a_base_cycle_still_keeps_one() {
        // Two PRs each based on the other's branch. Nonsense, and GitHub
        // would refuse the second -- but a rule that deadlocked on it would
        // silently stop reviewing both.
        let mut prs = vec![pr(9, "pr8", &["a"]), pr(8, "pr9", &["b"])];
        prs[0].head_ref_name = Some("pr9".into());
        prs[1].head_ref_name = Some("pr8".into());
        assert_eq!(parents(&prs).len(), 1);
    }

    #[test]
    fn the_held_line_names_the_relation_and_the_way_out() {
        assert_eq!(
            held_line(&[(18, StackedOn { pr: 15, why: Why::Commits(8) })]),
            "holding 1 PR stacked on another PR: #18 (8 commits also in #15); --stacked reviews it anyway"
        );
        assert_eq!(
            held_line(&[
                (16, StackedOn { pr: 15, why: Why::Base }),
                (18, StackedOn { pr: 15, why: Why::Commits(1) }),
            ]),
            "holding 2 PRs stacked on another PR: #16 (based on #15) #18 (1 commit also in #15); --stacked reviews them anyway"
        );
    }
}
