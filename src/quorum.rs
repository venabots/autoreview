//! A review that came back clean but was not approved, because too few of
//! the panel answered.
//!
//! The review skill approves only when enough of the panel returned (gate 1
//! in `skills/auto-review/SKILL.md`). That is right: two clean answers out of
//! four is not the review the gate asks for. But it is not the same as a
//! review that found a bug, and a summary that shows both as "commented"
//! sends a reader to open a PR that is only waiting for a reviewer to come
//! back. This names the difference, and approves nothing.
//!
//! The answer comes from the trailer, because only the reviewer knows how
//! many panelists returned. Whether the PR is approved still comes from
//! GitHub's readback (`job.verdict`), and an approved PR says nothing here.

use crate::job::Job;
use crate::report::Trailer;

/// The share of the panel the gate asks for, as a fraction: 3 of every 4.
const SHARE: (usize, usize) = (3, 4);

/// The fewest panelists the gate ever approves on, whatever the share says.
const FLOOR: usize = 2;

/// How far short of the gate a clean review fell.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub struct Shortfall {
    pub answered: usize,
    pub launched: usize,
    pub needed: usize,
}

/// How many of `launched` panelists the gate needs: `ceil(0.75 × launched)`,
/// and never fewer than two.
pub fn needed(launched: usize) -> usize {
    let (num, den) = SHARE;
    (launched * num).div_ceil(den).max(FLOOR)
}

/// The shortfall when this review was clean but too few of the panel
/// answered. None for an approved PR, a review with any finding above
/// polish, a review whose trailer did not say, a panel that met the gate,
/// and a panel where nobody answered -- nothing came back to be clean.
pub fn clean_but_short(job: &Job) -> Option<Shortfall> {
    if job.verdict.as_deref() == Some("approved") {
        return None;
    }
    let trailer = job.trailer.as_ref()?;
    if !clean(trailer)? {
        return None;
    }
    let launched = trailer.panel.len();
    let answered = trailer.panel.iter().filter(|p| p.ok == Some(true)).count();
    let needed = needed(launched);
    (answered > 0 && answered < needed).then_some(Shortfall { answered, launched, needed })
}

/// Whether the findings are polish at most. None when the reviewer did not
/// report its findings: an unreported count is not a zero.
fn clean(trailer: &Trailer) -> Option<bool> {
    let findings = trailer.findings.as_ref()?;
    let must = findings.must_fix?;
    let should = findings.should_fix?;
    Some(must == 0 && should == 0)
}

/// The sentence, for the summary and the detail pane. The bash suite greps
/// for it.
pub fn line(s: &Shortfall) -> String {
    format!(
        "looks clean, but only {} of {} reviewers answered; approval needs {}",
        s.answered, s.launched, s.needed
    )
}

/// The few characters a row has room for.
pub fn short(s: &Shortfall) -> String {
    format!("clean {}/{}", s.answered, s.launched)
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::report::{Findings, Panelist};

    fn panelist(ok: bool) -> Panelist {
        Panelist { ok: Some(ok), ..Panelist::default() }
    }

    fn job(verdict: &str, must: u64, should: u64, answered: usize, launched: usize) -> Job {
        let mut job = Job::new(9);
        job.verdict = Some(verdict.into());
        job.trailer = Some(Trailer {
            findings: Some(Findings { must_fix: Some(must), should_fix: Some(should), polish: Some(2) }),
            panel: (0..launched).map(|i| panelist(i < answered)).collect(),
            ..Trailer::default()
        });
        job
    }

    #[test]
    fn the_gate_is_three_quarters_and_never_fewer_than_two() {
        assert_eq!(needed(4), 3, "one of four may drop out");
        assert_eq!(needed(3), 3, "2.25 rounds up");
        assert_eq!(needed(5), 4);
        assert_eq!(needed(8), 6);
        assert_eq!(needed(2), 2);
        assert_eq!(needed(1), 2, "one reviewer is never a quorum");
    }

    #[test]
    fn a_clean_review_short_of_the_gate_is_named() {
        let s = clean_but_short(&job("commented", 0, 0, 2, 4)).unwrap();
        assert_eq!(s, Shortfall { answered: 2, launched: 4, needed: 3 });
        assert_eq!(line(&s), "looks clean, but only 2 of 4 reviewers answered; approval needs 3");
        assert_eq!(short(&s), "clean 2/4");
    }

    #[test]
    fn polish_is_still_clean() {
        // The gate approves on polish, so polish is no reason to look.
        assert!(clean_but_short(&job("commented", 0, 0, 1, 4)).is_some());
    }

    #[test]
    fn a_finding_above_polish_is_not_clean() {
        assert!(clean_but_short(&job("commented", 0, 1, 2, 4)).is_none());
        assert!(clean_but_short(&job("changes requested", 1, 0, 2, 4)).is_none());
    }

    #[test]
    fn a_panel_that_met_the_gate_has_no_shortfall() {
        // Clean and enough answered, but not approved: some other gate held
        // it, and the blockers say which.
        assert!(clean_but_short(&job("commented", 0, 0, 3, 4)).is_none());
    }

    #[test]
    fn nobody_answering_is_not_clean() {
        assert!(clean_but_short(&job("commented", 0, 0, 0, 4)).is_none());
    }

    #[test]
    fn an_approved_pr_says_nothing() {
        assert!(clean_but_short(&job("approved", 0, 0, 2, 4)).is_none());
    }

    #[test]
    fn unreported_findings_are_not_zero() {
        let mut job = job("commented", 0, 0, 2, 4);
        if let Some(t) = job.trailer.as_mut() {
            t.findings = Some(Findings { must_fix: Some(0), should_fix: None, polish: None });
        }
        assert!(clean_but_short(&job).is_none());
        job.trailer = None;
        assert!(clean_but_short(&job).is_none());
    }
}
