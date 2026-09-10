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
//! Every hold names one PR directly underneath, and rests on evidence about
//! those two PRs alone. Relatedness is never passed along a chain: a PR that
//! shares no work with the one below it would wait for a merge that changes
//! nothing about it.

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
/// Every hold rests on a direct relation between two PRs. Relatedness is not
/// passed along a chain: two PRs that share nothing are not related because a
/// third one carries both, and a PR held on something it shares no work with
/// would never be reviewed until an unrelated PR landed.
///
/// Every open PR counts, including drafts, approved ones, bots and your own.
/// A PR is held by the shape of the branches, not by whether this tool would
/// have reviewed the PR underneath it: a colleague's branch cut from your own
/// unmerged work carries your commits in its diff whether or not anyone
/// reviews yours.
///
/// `default_branch` is the repository's own default branch, which is never a
/// stack: see `based_on`.
pub fn parents(prs: &[PrNode], default_branch: Option<&str>) -> HashMap<u64, StackedOn> {
    let branches: Vec<Branch> = prs.iter().map(branch).collect();
    let mut on: Vec<Option<usize>> =
        (0..branches.len()).map(|i| below(&branches, default_branch, i)).collect();
    break_circles(&branches, &mut on);
    branches
        .iter()
        .enumerate()
        .filter_map(|(i, b)| {
            on[i].map(|j| (b.number, stacked_on(&branches, default_branch, i, j)))
        })
        .collect()
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

/// The one PR this one sits on, or None when it sits on nothing and is
/// reviewed. Three tests, strongest evidence first.
fn below(branches: &[Branch], default_branch: Option<&str>, i: usize) -> Option<usize> {
    let all = || 0..branches.len();
    // 1. The PR says so: its base is another open PR's branch. Two open PRs
    //    can share a head branch, so the lowest number wins rather than
    //    whichever the API listed first -- the same repo must answer the same
    //    way twice.
    if let Some(j) = all()
        .filter(|&j| based_on(branches, default_branch, i, j))
        .min_by_key(|&j| branches[j].number)
    {
        return Some(j);
    }
    // 2. It carries another PR's tip, so it is built on that PR's work. Of
    //    the PRs it is built on, the one with the most commits is the nearest,
    //    which makes a three-deep stack read as a chain.
    if let Some(j) = all()
        .filter(|&j| carries_tip(branches, i, j))
        .max_by_key(|&j| (branches[j].commits.len(), std::cmp::Reverse(branches[j].number)))
    {
        return Some(j);
    }
    // 3. Neither is built on the other, but their diffs overlap: two branches
    //    cut from the same unmerged commit. The older PR is reviewed and this
    //    one waits, because this one came second. A PR that sits on *this* one
    //    is never the answer -- the evidence there points the other way.
    all()
        .filter(|&j| {
            branches[j].number < branches[i].number
                && shared(branches, i, j) > 0
                && !carries_tip(branches, j, i)
                && !based_on(branches, default_branch, j, i)
        })
        .max_by_key(|&j| (shared(branches, i, j), std::cmp::Reverse(branches[j].number)))
}

/// How PR `above` was found to sit on PR `below`. The declared relation is
/// preferred: it is what the author said, and a stack whose branches also
/// share commits is still a stack.
fn stacked_on(
    branches: &[Branch],
    default_branch: Option<&str>,
    above: usize,
    below: usize,
) -> StackedOn {
    let pr = branches[below].number;
    if based_on(branches, default_branch, above, below) {
        return StackedOn { pr, why: Why::Base };
    }
    // At least one: both remaining tests need a commit in common, and a PR's
    // tip is one of its own commits.
    StackedOn { pr, why: Why::Commits(shared(branches, above, below)) }
}

/// How many commits the two PRs serve in both diffs.
fn shared(branches: &[Branch], a: usize, b: usize) -> usize {
    branches[a].commits.intersection(&branches[b].commits).count()
}

/// Does `above` merge into `below`'s branch, in a way that means a stack?
///
/// The branch names alone are not enough, because a long-lived branch is a
/// base too. Two guards keep an integration branch out:
///
/// - the repository's default branch is never a stack tip. A PR that merges
///   `main` back into a release branch would otherwise hold every PR that
///   merges into `main`.
/// - a stack parent creates the branch its children merge into, so no open PR
///   was already merging into that branch before the parent was opened. On a
///   git-flow repo the PRs merging into `develop` predate the PR that merges
///   `develop` onward, and none of them is stacked on it.
///
/// The second guard asks about the branch, and every PR merging into it
/// answers -- the child under test included. Exempting the child would make
/// one branch long-lived for one child and a stack tip for another, which is
/// worse than either answer: two children of one parent would cancel each
/// other out, and a git-flow PR older than the release PR would be held on it
/// and never reviewed.
///
/// The cost is a stack whose top PR was opened before its bottom PR, which
/// this cannot tell from an integration branch and does not hold. That is the
/// safe way to be wrong: the two PRs are both reviewed, which costs a second
/// read of shared context. Holding the wrong PR costs a review nobody does.
fn based_on(
    branches: &[Branch],
    default_branch: Option<&str>,
    above: usize,
    below: usize,
) -> bool {
    let Some(head) = branches[below].head_ref else {
        return false;
    };
    if above == below || branches[above].base_ref != Some(head) || default_branch == Some(head) {
        return false;
    }
    !branches
        .iter()
        .any(|b| b.base_ref == Some(head) && b.number < branches[below].number)
}

/// Does `above` carry `below`'s tip commit, which puts it on top of `below`?
fn carries_tip(branches: &[Branch], above: usize, below: usize) -> bool {
    above != below
        && branches[below].head.is_some_and(|tip| branches[above].commits.contains(tip))
}

/// A relation that points in a circle would leave every PR in it waiting on
/// another, and none of them reviewed. Free the lowest-numbered member of each
/// circle: something in it has to be reviewed, and the oldest PR is the one to
/// review. Two PRs on the same tip are the way this happens in practice.
fn break_circles(branches: &[Branch], on: &mut [Option<usize>]) {
    for start in 0..on.len() {
        let Some(circle) = circle_from(on, start) else {
            continue;
        };
        if let Some(&free) = circle.iter().min_by_key(|&&i| branches[i].number) {
            on[free] = None;
        }
    }
}

/// The members of the circle `start` leads into, if it leads into one.
fn circle_from(on: &[Option<usize>], start: usize) -> Option<Vec<usize>> {
    let mut path: Vec<usize> = Vec::new();
    let mut at = start;
    loop {
        if let Some(first) = path.iter().position(|&seen| seen == at) {
            return Some(path[first..].to_vec());
        }
        path.push(at);
        at = on[at]?;
    }
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

/// The line for a PR that was being watched and now sits on another open PR.
/// It says the same three things the sweep's line says -- which PR, why, and
/// the way out -- because a watch list that shrinks without them reads as a
/// lost PR.
pub fn dropped_line(pr: u64, on: StackedOn) -> String {
    format!(
        "PR #{pr} now sits on another open PR ({}); dropping it from the loop, \
         and --stacked reviews it anyway",
        on.detail()
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
        parents(prs, Some("main")).get(&n).copied()
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
        let held = parents(&prs, Some("main"));
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
        let held = parents(&prs, Some("main"));
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
        let held = parents(&prs, Some("main"));
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
        assert!(parents(&prs, Some("main")).is_empty());
    }

    #[test]
    fn a_pr_with_no_commit_data_is_left_alone() {
        // Every fixture in this repo's suite predates the oid field, and each
        // must stay reviewable: no commits means no overlap anyone can prove.
        let prs = vec![pr(9, "main", &[]), pr(8, "main", &[]), pr(7, "main", &["a"])];
        assert!(parents(&prs, Some("main")).is_empty());
    }

    #[test]
    fn two_forks_with_the_same_branch_name_are_not_a_stack() {
        // `headRefName` is a name in the head repository. Two contributors
        // working on their own fork's `patch-1`, both merging into main,
        // would otherwise each read as based on the other.
        let mut prs = vec![pr(9, "patch-1", &["a"]), pr(8, "main", &["b"])];
        prs[1].head_ref_name = Some("patch-1".into());
        prs[1].is_cross_repository = true;
        assert!(parents(&prs, Some("main")).is_empty(), "a fork branch names nothing here");

        // The same two branches in the base repository are a stack.
        prs[1].is_cross_repository = false;
        assert_eq!(held_on(&prs, 9).map(|s| s.pr), Some(8));
    }

    #[test]
    fn a_pr_is_never_held_on_one_it_shares_no_work_with() {
        // #18 merged both branches to fix a conflict, so it carries #12's work
        // and #15's. That does not relate #12 and #15 to each other. Holding
        // #15 until #12 lands would leave real work unreviewed for a merge
        // that changes nothing about it.
        let prs = vec![
            pr(12, "main", &["p", "q"]),
            pr(15, "main", &["a", "b"]),
            pr(18, "main", &["a", "b", "p", "q", "m", "t"]),
        ];
        let held = parents(&prs, Some("main"));
        assert_eq!(held.get(&18).map(|s| s.pr), Some(12), "#18 carries both");
        assert_eq!(held.get(&12), None, "#12 shares nothing with #15");
        assert_eq!(held.get(&15), None, "and #15 shares nothing with #12");
    }

    #[test]
    fn a_chain_of_overlaps_holds_each_pr_on_the_one_it_overlaps() {
        // #1 and #3 share nothing; only #2 touches both. Each hold names the
        // PR it actually overlaps, and #1 stays reviewable.
        let prs = vec![
            pr(1, "main", &["x", "a"]),
            pr(2, "main", &["x", "y", "b"]),
            pr(3, "main", &["y", "c"]),
        ];
        let held = parents(&prs, Some("main"));
        assert_eq!(held.get(&2), Some(&StackedOn { pr: 1, why: Why::Commits(1) }));
        assert_eq!(held.get(&3), Some(&StackedOn { pr: 2, why: Why::Commits(1) }));
        assert_eq!(held.get(&1), None);
    }

    #[test]
    fn no_hold_ever_reports_nothing_in_common() {
        // Why::Commits(0) means a hold nothing justifies. Both commit tests
        // need a commit in common, so it must be unreachable.
        let prs = vec![
            pr(1, "main", &["x", "a"]),
            pr(2, "main", &["x", "y", "b"]),
            pr(3, "main", &["y", "c"]),
            pr(4, "main", &["z"]),
        ];
        for (n, on) in parents(&prs, Some("main")) {
            assert_ne!(on.why, Why::Commits(0), "#{n} is held on nothing");
        }
    }

    #[test]
    fn a_long_lived_branch_is_not_a_stack() {
        // git-flow: #99 merges `develop` onward, and four PRs merge into
        // `develop`. None of them is stacked on #99 -- their work is not in
        // its diff, and it lands by a route of its own.
        let mut prs = vec![pr(99, "main", &["r1"])];
        prs[0].head_ref_name = Some("develop".into());
        for n in 1..=4 {
            prs.push(pr(n, "develop", &[&format!("f{n}")]));
        }
        assert!(parents(&prs, Some("main")).is_empty(), "no PR waits on the release");

        // A feature opened after the release PR is no different.
        prs.push(pr(100, "develop", &["f100"]));
        assert!(parents(&prs, Some("main")).is_empty());
    }

    #[test]
    fn a_stack_opened_from_the_top_down_is_not_held() {
        // #9 merges into #12's branch, but #9 was opened first, which is what
        // a PR merging into a long-lived branch looks like. Nothing in the
        // data tells the two apart, so this is the deliberate miss: both PRs
        // are reviewed, and the cost is one second read of shared context.
        // The other way to be wrong holds a PR nobody then reviews.
        let prs = vec![pr(9, "pr12", &["a"]), pr(12, "main", &["b"])];
        assert_eq!(held_on(&prs, 9), None);
        assert_eq!(held_on(&prs, 12), None);
    }

    #[test]
    fn one_branch_gets_one_answer_whoever_is_asking() {
        // Whether a branch is long-lived is a fact about the branch. Judging
        // it per child would let two children of one parent cancel each
        // other's hold, and would hold a git-flow PR opened before the release
        // PR while freeing one opened after it.
        let mut release = pr(99, "main", &["r1"]);
        release.head_ref_name = Some("develop".into());
        let older = pr(1, "develop", &["f1"]);
        let newer = pr(100, "develop", &["f100"]);
        let held = parents(&[release, older, newer], Some("main"));
        assert!(held.is_empty(), "neither child waits on the release: {held:?}");

        // Two children of one parent, and the parent opened last. Both get the
        // same answer, whatever it is.
        let mut parent = pr(12, "main", &["a"]);
        parent.head_ref_name = Some("pr12".into());
        let held = parents(&[pr(9, "pr12", &["b"]), pr(10, "pr12", &["c"]), parent], Some("main"));
        assert!(held.is_empty(), "one rule for both children: {held:?}");
    }

    #[test]
    fn the_pr_underneath_is_the_same_one_every_run() {
        // Two open PRs can share a head branch, and the API lists PRs by when
        // they were last updated. The lowest number wins, so the held line
        // does not change under a repo nobody touched.
        let mut prs = vec![pr(20, "shared", &["a"]), pr(8, "main", &["b"]), pr(5, "main", &["c"])];
        prs[1].head_ref_name = Some("shared".into());
        prs[2].head_ref_name = Some("shared".into());
        assert_eq!(held_on(&prs, 20).map(|s| s.pr), Some(5));
        prs.reverse();
        assert_eq!(held_on(&prs, 20).map(|s| s.pr), Some(5), "order does not decide it");
    }

    #[test]
    fn a_pr_that_merges_the_default_branch_onward_is_not_a_stack() {
        // #1 merges `main` into `develop`, so its head branch is `main`.
        // Every PR merging into `main` would otherwise wait on it.
        let mut prs = vec![pr(1, "develop", &["r1"])];
        prs[0].head_ref_name = Some("main".into());
        prs.push(pr(9, "main", &["a"]));
        assert!(parents(&prs, Some("main")).is_empty());
    }

    #[test]
    fn a_group_always_keeps_one_reviewable_pr() {
        // Two PRs on the same tip sit on each other, so neither is a keeper by
        // the ancestry rule. Holding both would leave work nobody reviews.
        let prs = vec![pr(9, "main", &["a", "b"]), pr(8, "main", &["a", "b"])];
        let held = parents(&prs, Some("main"));
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
        assert_eq!(parents(&prs, Some("main")).len(), 1);
    }

    #[test]
    fn the_dropped_line_says_which_pr_and_the_way_out() {
        // A PR can become stacked after it was reviewed, and the sweep's own
        // held line says nothing about a PR that has gone quiet. This is the
        // only line the reader gets, so it carries all three answers.
        assert_eq!(
            dropped_line(16, StackedOn { pr: 15, why: Why::Base }),
            "PR #16 now sits on another open PR (based on #15); dropping it from the loop, \
             and --stacked reviews it anyway"
        );
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
