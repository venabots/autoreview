//! Babysitting your own PRs: fixing one again as it changes, until it is
//! merged, closed or turned off.
//!
//! A fix runs `/babysit-pr` once. Babysitting decides, at each look, which
//! babysat PR gets another one. Two things keep that from becoming a loop
//! that spends for nothing:
//!
//! - **A fix only follows a change.** A PR that needs work and has not
//!   changed since the last fix is left alone: a second fix of the same
//!   state would find what the first one could not fix. The push a fix
//!   makes is not a change for this purpose -- the first look after the fix
//!   whose checks and merge state are known takes the PR's new state as the
//!   one to compare with. A build or a conflict the fix itself caused is the
//!   exception: that is work.
//! - **A cap.** At most `MAX_STREAK` fixes in a row with nothing new from
//!   anyone else. A new review thread or a review decision that changed
//!   (other than a stale approval the push dismissed) is somebody else
//!   acting, and starts the count again; so does a settled look that finds
//!   the PR clean.
//!
//! A PR with a job running is skipped, and a PR from a fork is never
//! babysat: its branch is not on origin to fix.

use crate::ci::Ci;
use crate::mine::{Merge, MyPr, Review};
use std::collections::{HashMap, HashSet};

/// The most fixes a babysat PR gets in a row while nobody else acts on it.
pub const MAX_STREAK: u32 = 3;

/// How many looks after a fix wait for its checks and merge state to be
/// known. A check that never finishes -- one waiting for an approval --
/// must not stop babysitting that PR for good.
const SETTLE_LOOKS: u32 = 5;

/// What a PR looked like when it was last fixed. A different one is a
/// change worth another fix.
#[derive(Debug, Clone, PartialEq, Eq)]
struct Fingerprint {
    head: String,
    updated_at: String,
    ci: Ci,
    merge: Merge,
}

impl Fingerprint {
    fn of(pr: &MyPr) -> Fingerprint {
        Fingerprint { head: pr.head.clone(), updated_at: pr.updated_at.clone(), ci: pr.ci, merge: pr.merge }
    }
}

/// What `b` or `B` asked for.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum Change {
    /// Babysit this PR, or stop.
    One(u64, bool),
    /// Babysit every PR of yours, or none.
    All(bool),
}

/// One babysat PR's history in this run.
#[derive(Debug, Clone, Default)]
struct Watch {
    /// The state at the last fix, or at the first look after it.
    baseline: Option<Fingerprint>,
    /// A fix just ended: take the next settled look's state as the baseline.
    settle: bool,
    /// Looks spent waiting for that settled state.
    waited: u32,
    streak: u32,
    fixes: u32,
    /// What others last left on the PR: its open threads and the decision.
    threads: Option<HashSet<String>>,
    review: Option<Review>,
}

/// Whether a PR has something a fix can work on.
pub fn needs_work(pr: &MyPr) -> bool {
    pr.merge == Merge::Conflicts || pr.ci == Ci::Failing || pr.review == Review::ChangesRequested || pr.open_threads > 0
}

/// Which of your PRs are babysat, and what each has had.
#[derive(Debug, Clone, Default)]
pub struct Babysat {
    /// Every PR of yours, including ones opened later.
    all: bool,
    prs: HashSet<u64>,
    watches: HashMap<u64, Watch>,
}

impl Babysat {
    /// Babysit one PR, or stop. Stopping one while all are babysat leaves
    /// the rest babysat.
    pub fn set(&mut self, pr: u64, on: bool, mine: &[MyPr]) {
        if on {
            self.prs.insert(pr);
            return;
        }
        if self.all {
            self.all = false;
            self.prs.extend(mine.iter().filter(|p| !p.cross_repo).map(|p| p.number));
        }
        self.prs.remove(&pr);
        self.watches.remove(&pr);
    }

    /// Babysit every PR of yours, or none.
    pub fn set_all(&mut self, on: bool) {
        self.all = on;
        if !on {
            self.prs.clear();
            self.watches.clear();
        }
    }

    pub fn all(&self) -> bool {
        self.all
    }

    pub fn is_babysat(&self, pr: u64) -> bool {
        self.all || self.prs.contains(&pr)
    }

    /// Whether anything is babysat: the run must keep looking if so.
    pub fn any(&self) -> bool {
        self.all || !self.prs.is_empty()
    }

    /// How many fixes babysitting has run on `pr` in this run.
    pub fn fixes(&self, pr: u64) -> u32 {
        self.watches.get(&pr).map_or(0, |w| w.fixes)
    }

    /// The babysat PRs among `mine`, for the view. A PR from a fork is
    /// never fixed, so it is not shown as babysat either.
    pub fn listed(&self, mine: &[MyPr]) -> Vec<(u64, u32)> {
        mine.iter()
            .filter(|p| self.is_babysat(p.number) && !p.cross_repo)
            .map(|p| (p.number, self.fixes(p.number)))
            .collect()
    }

    /// The PRs to fix now, given what this look found. `busy` says which
    /// PRs have a job running or waiting: those wait for the next look.
    pub fn due(&mut self, mine: &[MyPr], busy: impl Fn(u64) -> bool) -> Vec<u64> {
        // A PR that left the list was merged or closed: nothing to babysit.
        let open: HashSet<u64> = mine.iter().map(|p| p.number).collect();
        self.prs.retain(|pr| open.contains(pr));
        self.watches.retain(|pr, _| open.contains(pr));
        let mut out = Vec::new();
        // A PR from a fork has no branch on origin to fix: every fix would
        // be refused, and would still count against the cap.
        let babysat: Vec<&MyPr> = mine.iter().filter(|p| self.is_babysat(p.number) && !p.cross_repo).collect();
        for pr in babysat {
            // A PR with a job running is not looked at at all. What a
            // reviewer does meanwhile must still read as new at the first
            // look after the fix, not be recorded as already seen.
            if busy(pr.number) {
                continue;
            }
            let watch = self.watches.entry(pr.number).or_default();
            let threads: HashSet<String> = pr.thread_ids.iter().cloned().collect();
            // A push dismisses a stale approval on some repos. That is the
            // fix's own push, not a reviewer acting.
            let dismissed = |before: Review| before == Review::Approved && pr.review == Review::Required;
            let someone_acted = watch.threads.as_ref().is_some_and(|before| !threads.is_subset(before))
                || watch.review.is_some_and(|r| r != pr.review && !dismissed(r));
            // Somebody acting starts the count again, and so does a PR that
            // came clean -- once its checks and merge state are known: a
            // push makes both look clean for a minute.
            let settled = pr.ci != Ci::Pending && pr.merge != Merge::Unknown;
            if someone_acted || (settled && !needs_work(pr)) {
                watch.streak = 0;
            }
            watch.threads = Some(threads);
            watch.review = Some(pr.review);
            let now = Fingerprint::of(pr);
            // The first look after a fix records what the fix left, without
            // acting on it -- unless somebody acted meanwhile, which is new
            // work. Checks still running and a merge GitHub has not worked
            // out yet are not what the fix left: the look waits for them, or
            // their settling would read as a change.
            if watch.settle && !someone_acted {
                if !settled && watch.waited < SETTLE_LOOKS {
                    watch.waited += 1;
                    continue;
                }
                watch.settle = false;
                watch.waited = 0;
                // The fix's own push broke the build or left conflicts that
                // were not there before it: that is work, not its baseline.
                let before = watch.baseline.replace(now.clone());
                let broke = before.is_some_and(|b| {
                    (now.ci == Ci::Failing && b.ci != Ci::Failing) || (now.merge == Merge::Conflicts && b.merge != Merge::Conflicts)
                });
                if broke && watch.streak < MAX_STREAK {
                    watch.streak += 1;
                    watch.fixes += 1;
                    out.push(pr.number);
                }
                continue;
            }
            watch.settle = false;
            watch.waited = 0;
            let changed = watch.baseline.as_ref() != Some(&now);
            if needs_work(pr) && (changed || someone_acted) && watch.streak < MAX_STREAK {
                watch.baseline = Some(now);
                watch.streak += 1;
                watch.fixes += 1;
                out.push(pr.number);
            }
        }
        out
    }

    /// A fix on `pr` ended. Its own push is not a change: the next look's
    /// state becomes the one to compare with.
    pub fn fixed(&mut self, pr: u64) {
        if let Some(watch) = self.watches.get_mut(&pr) {
            watch.settle = true;
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    fn pr(n: u64) -> MyPr {
        MyPr {
            number: n,
            title: "My own work".into(),
            draft: false,
            branch: "me/my-own-work".into(),
            head: "sha1".into(),
            updated_at: "2026-10-07T10:00:00Z".into(),
            cross_repo: false,
            review: Review::Required,
            merge: Merge::Clean,
            ci: Ci::Passing,
            open_threads: 1,
            reviewers: Vec::new(),
            thread_ids: Vec::new(),
        }
    }

    fn idle(_: u64) -> bool {
        false
    }

    #[test]
    fn a_babysat_pr_that_needs_work_is_fixed_once_until_it_changes() {
        let mut b = Babysat::default();
        b.set(4, true, &[]);
        let mine = vec![pr(4)];
        assert_eq!(b.due(&mine, idle), vec![4]);
        assert!(b.due(&mine, idle).is_empty(), "the same state again is not a change");
        let pushed = MyPr { head: "sha2".into(), updated_at: "2026-10-07T11:00:00Z".into(), ..pr(4) };
        assert_eq!(b.due(&[pushed], idle), vec![4], "a push by somebody is");
        assert_eq!(b.fixes(4), 2);
    }

    #[test]
    fn the_fix_s_own_push_is_not_a_change() {
        let mut b = Babysat::default();
        b.set(4, true, &[]);
        assert_eq!(b.due(&[pr(4)], idle), vec![4]);
        b.fixed(4);
        let after_fix = MyPr { head: "sha-fix".into(), updated_at: "2026-10-07T10:05:00Z".into(), ..pr(4) };
        assert!(b.due(std::slice::from_ref(&after_fix), idle).is_empty(), "the first look after a fix only records it");
        assert!(b.due(std::slice::from_ref(&after_fix), idle).is_empty());
        let red = MyPr { ci: Ci::Failing, ..after_fix };
        assert_eq!(b.due(&[red], idle), vec![4], "its checks failing later is a change");
    }

    #[test]
    fn a_pr_with_nothing_to_fix_is_left_alone() {
        let mut b = Babysat::default();
        b.set_all(true);
        assert!(b.due(&[MyPr { open_threads: 0, ..pr(4) }], idle).is_empty());
        assert!(needs_work(&MyPr { open_threads: 0, merge: Merge::Conflicts, ..pr(4) }));
        assert!(needs_work(&MyPr { open_threads: 0, ci: Ci::Failing, ..pr(4) }));
        assert!(needs_work(&MyPr { open_threads: 0, review: Review::ChangesRequested, ..pr(4) }));
    }

    #[test]
    fn the_streak_stops_at_the_cap_until_someone_else_acts() {
        let mut b = Babysat::default();
        b.set(4, true, &[]);
        for i in 0..MAX_STREAK {
            let changed = MyPr { head: format!("sha{i}"), ..pr(4) };
            assert_eq!(b.due(&[changed], idle), vec![4], "fix {i}");
        }
        let again = MyPr { head: "sha-more".into(), ..pr(4) };
        assert!(b.due(&[again], idle).is_empty(), "the cap holds");
        let new_thread = MyPr { head: "sha-more2".into(), open_threads: 2, thread_ids: vec!["t9".into()], ..pr(4) };
        assert_eq!(b.due(&[new_thread], idle), vec![4], "a reviewer's new thread starts the count again");
    }

    #[test]
    fn a_reviewer_who_acts_during_a_fix_gets_a_fix() {
        let mut b = Babysat::default();
        b.set(4, true, &[]);
        let one = MyPr { thread_ids: vec!["t1".into()], ..pr(4) };
        assert_eq!(b.due(std::slice::from_ref(&one), idle), vec![4]);
        b.fixed(4);
        let new_thread = MyPr { head: "sha-fix".into(), thread_ids: vec!["t1".into(), "t2".into()], open_threads: 2, ..pr(4) };
        assert_eq!(b.due(&[new_thread], idle), vec![4], "not swallowed by the look after the fix");
    }

    #[test]
    fn the_look_after_a_fix_waits_for_checks_to_settle() {
        let mut b = Babysat::default();
        b.set(4, true, &[]);
        let asked = MyPr { review: Review::ChangesRequested, open_threads: 0, ..pr(4) };
        assert_eq!(b.due(std::slice::from_ref(&asked), idle), vec![4]);
        b.fixed(4);
        let pending = MyPr { head: "sha-fix".into(), ci: Ci::Pending, merge: Merge::Unknown, ..asked.clone() };
        assert!(b.due(&[pending], idle).is_empty());
        let settled = MyPr { head: "sha-fix".into(), ..asked };
        assert!(b.due(std::slice::from_ref(&settled), idle).is_empty(), "checks settling is not a change");
        assert!(b.due(&[settled], idle).is_empty());
    }

    #[test]
    fn a_new_thread_counts_even_when_another_closed() {
        let mut b = Babysat::default();
        b.set(4, true, &[]);
        let three = MyPr { thread_ids: vec!["a".into(), "b".into(), "c".into()], open_threads: 3, ..pr(4) };
        for i in 0..MAX_STREAK {
            assert_eq!(b.due(&[MyPr { head: format!("sha{i}"), ..three.clone() }], idle), vec![4]);
        }
        let swapped = MyPr { head: "sha-x".into(), thread_ids: vec!["d".into()], open_threads: 1, ..pr(4) };
        assert_eq!(b.due(&[swapped], idle), vec![4], "thread d is new, though the count went down");
    }

    #[test]
    fn the_cap_starts_again_once_the_pr_came_clean() {
        let mut b = Babysat::default();
        b.set(4, true, &[]);
        for i in 0..MAX_STREAK {
            assert_eq!(b.due(&[MyPr { head: format!("sha{i}"), ..pr(4) }], idle), vec![4]);
        }
        assert!(b.due(&[MyPr { head: "clean".into(), open_threads: 0, ..pr(4) }], idle).is_empty());
        let conflicts = MyPr { head: "clean".into(), open_threads: 0, merge: Merge::Conflicts, ..pr(4) };
        assert_eq!(b.due(&[conflicts], idle), vec![4], "new trouble after a clean look");
    }

    #[test]
    fn a_thread_opened_while_the_fix_runs_is_new_work_after_it() {
        let mut b = Babysat::default();
        b.set(4, true, &[]);
        let one = MyPr { thread_ids: vec!["t1".into()], ..pr(4) };
        assert_eq!(b.due(std::slice::from_ref(&one), idle), vec![4]);
        let two = MyPr { thread_ids: vec!["t1".into(), "t2".into()], open_threads: 2, ..pr(4) };
        assert!(b.due(std::slice::from_ref(&two), |_| true).is_empty(), "the fix is running");
        b.fixed(4);
        let after = MyPr { head: "sha-fix".into(), ..two };
        assert_eq!(b.due(&[after], idle), vec![4], "t2 arrived during the fix and is new");
    }

    #[test]
    fn a_build_the_fix_broke_gets_a_fix() {
        let mut b = Babysat::default();
        b.set(4, true, &[]);
        assert_eq!(b.due(&[pr(4)], idle), vec![4]);
        b.fixed(4);
        let red = MyPr { head: "sha-fix".into(), ci: Ci::Failing, ..pr(4) };
        assert_eq!(b.due(&[red], idle), vec![4], "it was green before the fix");
    }

    #[test]
    fn a_pending_build_does_not_reset_the_cap() {
        let mut b = Babysat::default();
        b.set(4, true, &[]);
        let red = |i: u32| MyPr { head: format!("sha{i}"), ci: Ci::Failing, open_threads: 0, ..pr(4) };
        for i in 0..MAX_STREAK {
            assert_eq!(b.due(&[red(i)], idle), vec![4]);
            let pending = MyPr { head: format!("p{i}"), ci: Ci::Pending, open_threads: 0, ..pr(4) };
            b.due(&[pending], idle);
        }
        assert!(b.due(&[red(9)], idle).is_empty(), "the cap holds through pending checks");
    }

    #[test]
    fn a_check_that_never_finishes_does_not_stop_babysitting() {
        let mut b = Babysat::default();
        b.set(4, true, &[]);
        assert_eq!(b.due(&[pr(4)], idle), vec![4]);
        b.fixed(4);
        let stuck = MyPr { head: "sha-fix".into(), ci: Ci::Pending, ..pr(4) };
        for _ in 0..=SETTLE_LOOKS {
            assert!(b.due(std::slice::from_ref(&stuck), idle).is_empty());
        }
        let conflicts = MyPr { merge: Merge::Conflicts, ..stuck };
        assert_eq!(b.due(&[conflicts], idle), vec![4], "a later conflict still gets a fix");
    }

    #[test]
    fn stopping_one_of_all_never_babysits_a_fork() {
        let mut b = Babysat::default();
        b.set_all(true);
        let mine = vec![pr(4), MyPr { cross_repo: true, ..pr(5) }];
        b.set(4, false, &mine);
        assert!(!b.any(), "the fork is not left behind to keep the run looking");
    }

    #[test]
    fn a_stale_approval_dismissed_by_the_push_is_not_a_reviewer() {
        let mut b = Babysat::default();
        b.set(4, true, &[]);
        for i in 0..MAX_STREAK {
            assert_eq!(b.due(&[MyPr { head: format!("sha{i}"), review: Review::Approved, ..pr(4) }], idle), vec![4]);
        }
        let dismissed = MyPr { head: "sha-x".into(), review: Review::Required, ..pr(4) };
        assert!(b.due(&[dismissed], idle).is_empty(), "the cap still holds");
    }

    #[test]
    fn a_pr_from_a_fork_is_never_scheduled() {
        let mut b = Babysat::default();
        b.set_all(true);
        let fork = MyPr { cross_repo: true, ..pr(4) };
        assert!(b.due(std::slice::from_ref(&fork), idle).is_empty());
        assert_eq!(b.fixes(4), 0);
        assert!(b.listed(&[fork]).is_empty(), "not shown as babysat");
    }

    #[test]
    fn a_busy_pr_waits_and_a_closed_one_leaves() {
        let mut b = Babysat::default();
        b.set(4, true, &[]);
        assert!(b.due(&[pr(4)], |pr| pr == 4).is_empty(), "a job on it is running");
        assert_eq!(b.due(&[pr(4)], idle), vec![4], "and it is fixed at the next look");
        b.due(&[], idle);
        assert!(!b.is_babysat(4) && !b.any(), "merged or closed: nothing left to babysit");
    }

    #[test]
    fn all_covers_prs_opened_later_and_stopping_one_keeps_the_rest() {
        let mut b = Babysat::default();
        b.set_all(true);
        assert!(b.is_babysat(99), "one opened later");
        let mine = vec![pr(4), pr(5)];
        b.set(4, false, &mine);
        assert!(!b.all() && !b.is_babysat(4) && b.is_babysat(5));
        assert_eq!(b.listed(&mine), vec![(5, 0)]);
        b.set_all(false);
        assert!(!b.any());
    }
}
