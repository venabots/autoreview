//! Why a review did not end in an approval, in a few lines a reader can act on.
//!
//! The summary already says a PR was commented on rather than approved. It
//! does not say whether that was a misaligned button or a double charge, and
//! that is the difference between "look at it tomorrow" and "look at it now".
//! A reader who has to open every unapproved PR to learn which is which is
//! reading a list, not a report.
//!
//! The asymmetry the rest of the tool keeps holds here too. Whether the PR is
//! approved comes from `job.verdict`, which is GitHub's readback: a reviewer
//! does not get to say its own review landed. Why it is not approved comes
//! from the trailer, because only the reviewer knows.

use crate::job::Job;
use crate::report::Blocker;
use crate::ui::count;

/// The line that opens the block. The bash suite greps for it.
pub const HEADER: &str = "not approved yet because:";

/// How many blockers a reader gets before the tail is counted instead. Three
/// fits under a board row without pushing the next row off a short terminal,
/// and a review with more than three blockers is one you open anyway.
const MAX_SHOWN: usize = 3;

/// The reasons this PR is not approved yet, each already a bullet. Empty for
/// an approved PR, for a review that named no blocker, and for a reviewer too
/// old to report any -- in every one of those the caller prints nothing.
pub fn reasons(job: &Job) -> Vec<String> {
    if job.verdict.as_deref() == Some("approved") {
        return Vec::new();
    }
    let Some(trailer) = &job.trailer else {
        return Vec::new();
    };
    let lines: Vec<String> = trailer.blockers.iter().filter_map(blocker_line).collect();
    let hidden = lines.len().saturating_sub(MAX_SHOWN);
    let mut out: Vec<String> = lines.into_iter().take(MAX_SHOWN).map(|l| format!("- {l}")).collect();
    if hidden > 0 {
        out.push(format!("- {} not shown", count(hidden, "finding")));
    }
    out
}

/// One blocker as a line: `[HIGH] (money, irreversible) src/pay.rs:88 -- a
/// retried checkout charges the card twice`. Every part is optional because
/// every field is; a blocker that says nothing at all is dropped rather than
/// drawn as an empty bullet.
fn blocker_line(b: &Blocker) -> Option<String> {
    let mut head = Vec::new();
    if let Some(severity) = named(b.severity.as_deref()) {
        head.push(format!("[{severity}]"));
    }
    if let Some(tag) = tag(b) {
        head.push(format!("({tag})"));
    }
    if let Some(location) = named(b.location.as_deref()) {
        head.push(location.to_string());
    }
    let head = head.join(" ");
    match (head.is_empty(), named(b.gist.as_deref())) {
        (true, None) => None,
        (true, Some(gist)) => Some(gist.to_string()),
        (false, None) => Some(head),
        (false, Some(gist)) => Some(format!("{head} — {gist}")),
    }
}

/// The two axes in one parenthetical. A domain alone still tells a reader
/// which kind of bug this is, so a reviewer that reported only one axis is
/// not punished by dropping both.
fn tag(b: &Blocker) -> Option<String> {
    let domain = named(b.domain.as_deref());
    let blast = b.reversible.map(|r| if r { "reversible" } else { "irreversible" });
    match (domain, blast) {
        (Some(domain), Some(blast)) => Some(format!("{domain}, {blast}")),
        (Some(domain), None) => Some(domain.to_string()),
        (None, Some(blast)) => Some(blast.to_string()),
        (None, None) => None,
    }
}

fn named(s: Option<&str>) -> Option<&str> {
    s.map(str::trim).filter(|v| !v.is_empty())
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::report::Trailer;

    fn blocker(severity: &str, domain: &str, reversible: bool, location: &str, gist: &str) -> Blocker {
        Blocker {
            severity: Some(severity.into()),
            domain: Some(domain.into()),
            reversible: Some(reversible),
            location: Some(location.into()),
            gist: Some(gist.into()),
        }
    }

    fn job_with(verdict: Option<&str>, blockers: Vec<Blocker>) -> Job {
        let mut job = Job::new(9);
        job.verdict = verdict.map(String::from);
        job.trailer = Some(Trailer { blockers, ..Trailer::default() });
        job
    }

    #[test]
    fn a_blocker_names_its_severity_domain_and_blast_radius() {
        let job = job_with(
            Some("commented"),
            vec![blocker("HIGH", "money", false, "src/pay.rs:88", "a retried checkout charges twice")],
        );
        assert_eq!(
            reasons(&job),
            vec!["- [HIGH] (money, irreversible) src/pay.rs:88 — a retried checkout charges twice"]
        );
    }

    #[test]
    fn an_approved_pr_says_nothing() {
        // The blockers are what the review fixed on its way to approving, or
        // a reviewer that forgot to clear the field. Either way GitHub says
        // approved, and the summary does not argue with it.
        let job = job_with(
            Some("approved"),
            vec![blocker("LOW", "ui", true, "app/page.tsx:12", "the button sits two pixels low")],
        );
        assert!(reasons(&job).is_empty());
    }

    #[test]
    fn a_review_that_posted_nothing_still_explains_itself() {
        // "nothing posted" is the verdict that most needs a reason: there is
        // no review on the PR to go and read.
        let job = job_with(None, vec![blocker("MEDIUM", "correctness", true, "src/q.rs:3", "off by one")]);
        assert_eq!(reasons(&job).len(), 1);
    }

    #[test]
    fn past_three_blockers_the_rest_are_counted() {
        let many: Vec<Blocker> =
            (0..5).map(|n| blocker("MEDIUM", "ui", true, &format!("a.ts:{n}"), "wrong")).collect();
        let lines = reasons(&job_with(Some("changes requested"), many));
        assert_eq!(lines.len(), MAX_SHOWN + 1);
        assert_eq!(lines[MAX_SHOWN], "- 2 findings not shown");
    }

    #[test]
    fn one_hidden_finding_is_singular() {
        let many: Vec<Blocker> =
            (0..4).map(|n| blocker("LOW", "ui", true, &format!("a.ts:{n}"), "wrong")).collect();
        let lines = reasons(&job_with(Some("commented"), many));
        assert_eq!(lines[MAX_SHOWN], "- 1 finding not shown");
    }

    #[test]
    fn a_half_reported_blocker_still_draws() {
        // Every trailer field is optional, so a blocker may arrive with only
        // a sentence. A reader gets the sentence rather than an empty bullet.
        let job = job_with(
            Some("commented"),
            vec![Blocker { gist: Some("the migration drops a column".into()), ..Blocker::default() }],
        );
        assert_eq!(reasons(&job), vec!["- the migration drops a column"]);
    }

    #[test]
    fn a_blocker_that_says_nothing_is_dropped() {
        let job = job_with(Some("commented"), vec![Blocker::default()]);
        assert!(reasons(&job).is_empty());
    }

    #[test]
    fn a_reviewer_that_reported_no_blockers_says_nothing() {
        assert!(reasons(&job_with(Some("commented"), Vec::new())).is_empty());
    }
}
