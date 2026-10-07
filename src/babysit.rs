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
//!   takes the PR's new state as the one to compare with.
//! - **A cap.** At most `MAX_STREAK` fixes in a row with nothing new from
//!   anyone else. A new review thread or a new review decision is somebody
//!   else acting, and starts the count again.

use crate::ci::Ci;
use crate::mine::{Merge, MyPr, Review};
use std::collections::{HashMap, HashSet};

/// The most fixes a babysat PR gets in a row while nobody else acts on it.
pub const MAX_STREAK: u32 = 3;

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
    /// A fix just ended: take the next look's state as the baseline.
    settle: bool,
    streak: u32,
    fixes: u32,
    /// What others last left on the PR: open threads and the decision.
    threads: Option<usize>,
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
            self.prs.extend(mine.iter().map(|p| p.number));
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

    /// The babysat PRs among `mine`, for the view.
    pub fn listed(&self, mine: &[MyPr]) -> Vec<(u64, u32)> {
        mine.iter().filter(|p| self.is_babysat(p.number)).map(|p| (p.number, self.fixes(p.number))).collect()
    }

    /// The PRs to fix now, given what this look found. `busy` says which
    /// PRs have a job running or waiting: those wait for the next look.
    pub fn due(&mut self, mine: &[MyPr], busy: impl Fn(u64) -> bool) -> Vec<u64> {
        // A PR that left the list was merged or closed: nothing to babysit.
        let open: HashSet<u64> = mine.iter().map(|p| p.number).collect();
        self.prs.retain(|pr| open.contains(pr));
        self.watches.retain(|pr, _| open.contains(pr));
        let mut out = Vec::new();
        let babysat: Vec<&MyPr> = mine.iter().filter(|p| self.is_babysat(p.number)).collect();
        for pr in babysat {
            let watch = self.watches.entry(pr.number).or_default();
            let someone_acted = watch.threads.is_some_and(|t| pr.open_threads > t)
                || watch.review.is_some_and(|r| r != pr.review);
            if someone_acted {
                watch.streak = 0;
            }
            watch.threads = Some(pr.open_threads);
            watch.review = Some(pr.review);
            if busy(pr.number) {
                continue;
            }
            let now = Fingerprint::of(pr);
            if watch.settle {
                watch.settle = false;
                watch.baseline = Some(now);
                continue;
            }
            let changed = watch.baseline.as_ref() != Some(&now);
            if needs_work(pr) && changed && watch.streak < MAX_STREAK {
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
        let new_thread = MyPr { head: "sha-more2".into(), open_threads: 2, ..pr(4) };
        assert_eq!(b.due(&[new_thread], idle), vec![4], "a reviewer's new thread starts the count again");
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
