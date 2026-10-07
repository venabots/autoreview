//! Your own open PRs in this repo, and where each one stands: what the
//! reviewers decided, how many review threads are still open, whether it
//! conflicts with its base, and what its checks say.
//!
//! The review list hides your PRs on purpose (`prlist::filter_prs`): the
//! sweep reviews other people's work. The My PRs tab is the other half, so
//! it has its own query. A search by author finds every one of your PRs,
//! where the review list's page of the 50 most recently updated would drop
//! them in a busy repo.

use crate::ci::Ci;
use crate::repo::RepoContext;
use anyhow::{Context, Result, bail};
use serde::Deserialize;
use std::process::Command;

/// How many of your PRs one look reads. More than anybody has open at once.
const LIMIT: usize = 50;

const QUERY: &str = "
      query($q:String!) {
        search(query:$q, type:ISSUE, first:50) {
          nodes {
            ... on PullRequest {
              number
              title
              isDraft
              headRefName
              isCrossRepository
              reviewDecision
              mergeable
              latestReviews(first:20) { nodes { author { login } state } }
              reviewThreads(first:100) { nodes { isResolved } }
              headCommit: commits(last:1) { nodes { commit { statusCheckRollup { state } } } }
            }
          }
        }
      }";

/// What the reviewers decided, as GitHub reports it.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum Review {
    Approved,
    ChangesRequested,
    /// The branch rules ask for a review and none has approved yet.
    Required,
    /// No rule asks for one, and nobody has decided.
    None,
}

impl Review {
    fn from_raw(raw: Option<&str>) -> Review {
        match raw {
            Some("APPROVED") => Review::Approved,
            Some("CHANGES_REQUESTED") => Review::ChangesRequested,
            Some("REVIEW_REQUIRED") => Review::Required,
            _ => Review::None,
        }
    }
}

/// Whether the PR merges into its base.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum Merge {
    Clean,
    Conflicts,
    /// GitHub has not worked it out yet; it does so lazily after a push.
    Unknown,
}

impl Merge {
    fn from_raw(raw: Option<&str>) -> Merge {
        match raw {
            Some("MERGEABLE") => Merge::Clean,
            Some("CONFLICTING") => Merge::Conflicts,
            _ => Merge::Unknown,
        }
    }
}

/// One of your PRs, as the tab shows it.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct MyPr {
    pub number: u64,
    pub title: String,
    pub draft: bool,
    pub branch: String,
    /// From a fork: its branch is not on `origin`, so no worktree can be
    /// made for it here.
    pub cross_repo: bool,
    pub review: Review,
    pub merge: Merge,
    pub ci: Ci,
    pub open_threads: usize,
    /// Each reviewer's latest review that decided something: who, and what.
    pub reviewers: Vec<(String, Review)>,
}

impl MyPr {
    /// What most needs doing about this PR, in a word or two. Ordered by
    /// what blocks a merge first: a conflict and a red build stop it
    /// whatever the reviewers say, and a requested change outranks a comment
    /// that is merely open.
    pub fn state_word(&self) -> String {
        if self.merge == Merge::Conflicts {
            return "conflicts".into();
        }
        if self.ci == Ci::Failing {
            return "CI failing".into();
        }
        if self.review == Review::ChangesRequested {
            return "changes req".into();
        }
        if self.open_threads > 0 {
            return crate::ui::count(self.open_threads, "thread");
        }
        if self.draft {
            return "draft".into();
        }
        if self.ci == Ci::Pending {
            return "CI pending".into();
        }
        match self.review {
            Review::Approved => "approved".into(),
            _ => "awaiting review".into(),
        }
    }

    /// How urgent the state word is: lower is listed first.
    pub fn urgency(&self) -> u8 {
        match self.state_word().as_str() {
            "conflicts" => 0,
            "CI failing" => 1,
            "changes req" => 2,
            w if w.ends_with("thread") || w.ends_with("threads") => 3,
            "CI pending" => 4,
            "awaiting review" => 5,
            "draft" => 6,
            _ => 7,
        }
    }
}

/// Your PRs, most urgent first and newest first within that.
pub fn ranked(mut prs: Vec<MyPr>) -> Vec<MyPr> {
    prs.sort_by(|a, b| a.urgency().cmp(&b.urgency()).then(b.number.cmp(&a.number)));
    prs
}

#[derive(Deserialize)]
struct Login {
    login: Option<String>,
}

#[derive(Deserialize)]
struct ReviewNode {
    author: Option<Login>,
    state: Option<String>,
}

#[derive(Deserialize)]
struct Nodes<T> {
    #[serde(default = "Vec::new")]
    nodes: Vec<T>,
}

impl<T> Default for Nodes<T> {
    fn default() -> Self {
        Nodes { nodes: Vec::new() }
    }
}

#[derive(Deserialize)]
struct Thread {
    #[serde(rename = "isResolved", default)]
    is_resolved: bool,
}

#[derive(Deserialize)]
struct Rollup {
    state: Option<String>,
}

#[derive(Deserialize)]
struct Commit {
    #[serde(rename = "statusCheckRollup")]
    rollup: Option<Rollup>,
}

#[derive(Deserialize)]
struct CommitNode {
    commit: Commit,
}

/// One search result. Every field is optional: a node that is not a pull
/// request comes back empty, and is dropped rather than refused.
#[derive(Deserialize)]
struct Node {
    number: Option<u64>,
    #[serde(default)]
    title: String,
    #[serde(rename = "isDraft", default)]
    is_draft: bool,
    #[serde(rename = "headRefName", default)]
    head_ref_name: String,
    #[serde(rename = "isCrossRepository", default)]
    is_cross_repository: bool,
    #[serde(rename = "reviewDecision")]
    review_decision: Option<String>,
    mergeable: Option<String>,
    #[serde(rename = "latestReviews", default)]
    latest_reviews: Nodes<ReviewNode>,
    #[serde(rename = "reviewThreads", default)]
    review_threads: Nodes<Thread>,
    #[serde(rename = "headCommit", default)]
    head_commit: Nodes<CommitNode>,
}

impl Node {
    fn into_pr(self) -> Option<MyPr> {
        let number = self.number?;
        let ci = Ci::from_state(
            self.head_commit
                .nodes
                .first()
                .and_then(|n| n.commit.rollup.as_ref())
                .and_then(|r| r.state.as_deref()),
        );
        let reviewers = self
            .latest_reviews
            .nodes
            .into_iter()
            .filter_map(|r| {
                let who = r.author?.login?;
                match Review::from_raw(r.state.as_deref()) {
                    Review::None | Review::Required => None,
                    decided => Some((who, decided)),
                }
            })
            .collect();
        Some(MyPr {
            number,
            title: self.title,
            draft: self.is_draft,
            branch: self.head_ref_name,
            cross_repo: self.is_cross_repository,
            review: Review::from_raw(self.review_decision.as_deref()),
            merge: Merge::from_raw(self.mergeable.as_deref()),
            ci,
            open_threads: self.review_threads.nodes.iter().filter(|t| !t.is_resolved).count(),
            reviewers,
        })
    }
}

/// The PRs out of a search response, or why there are none. An `errors`
/// array or a missing data path is a failed look, not an empty list: the
/// tab would otherwise say you have no PRs.
pub fn parse(parsed: &serde_json::Value) -> Result<Vec<MyPr>> {
    if let Some(errors) = parsed.get("errors").and_then(|e| e.as_array())
        && let Some(first) = errors.first()
    {
        let msg = first.get("message").and_then(|m| m.as_str()).unwrap_or("unknown error");
        bail!("gh api graphql returned errors: {msg}");
    }
    let Some(nodes) = parsed.pointer("/data/search/nodes") else {
        bail!("gh api graphql returned no search results");
    };
    let nodes: Vec<Node> = serde_json::from_value(nodes.clone()).context("parsing your PRs")?;
    Ok(ranked(nodes.into_iter().filter_map(Node::into_pr).take(LIMIT).collect()))
}

/// The search that finds your open PRs in this repo.
fn search_terms(ctx: &RepoContext) -> String {
    format!("repo:{}/{} is:pr is:open author:@me", ctx.owner, ctx.name)
}

/// Your open PRs in this repo, most urgent first.
pub fn fetch(ctx: &RepoContext) -> Result<Vec<MyPr>> {
    let out = Command::new("gh")
        .args(["api", "graphql", "-f"])
        .arg(format!("q={}", search_terms(ctx)))
        .arg("-f")
        .arg(format!("query={QUERY}"))
        .output()
        .context("running gh api graphql")?;
    if !out.status.success() {
        bail!("{}", String::from_utf8_lossy(&out.stderr).trim());
    }
    let parsed: serde_json::Value = serde_json::from_slice(&out.stdout).context("parsing gh api graphql output")?;
    parse(&parsed)
}

#[cfg(test)]
mod tests {
    use super::*;
    use serde_json::json;

    fn pr() -> MyPr {
        MyPr {
            number: 4,
            title: "My own work".into(),
            draft: false,
            branch: "me/my-own-work".into(),
            cross_repo: false,
            review: Review::Required,
            merge: Merge::Clean,
            ci: Ci::Passing,
            open_threads: 0,
            reviewers: Vec::new(),
        }
    }

    #[test]
    fn the_state_word_names_what_blocks_the_merge_first() {
        assert_eq!(pr().state_word(), "awaiting review");
        assert_eq!(MyPr { review: Review::Approved, ..pr() }.state_word(), "approved");
        assert_eq!(MyPr { draft: true, ..pr() }.state_word(), "draft");
        assert_eq!(MyPr { ci: Ci::Pending, ..pr() }.state_word(), "CI pending");
        assert_eq!(MyPr { open_threads: 1, ..pr() }.state_word(), "1 thread");
        assert_eq!(MyPr { open_threads: 2, ..pr() }.state_word(), "2 threads");
        assert_eq!(MyPr { review: Review::ChangesRequested, open_threads: 2, ..pr() }.state_word(), "changes req");
        assert_eq!(MyPr { ci: Ci::Failing, review: Review::ChangesRequested, ..pr() }.state_word(), "CI failing");
        assert_eq!(MyPr { merge: Merge::Conflicts, ci: Ci::Failing, ..pr() }.state_word(), "conflicts");
    }

    #[test]
    fn the_most_urgent_pr_is_listed_first() {
        let list = ranked(vec![
            MyPr { number: 1, review: Review::Approved, ..pr() },
            MyPr { number: 2, merge: Merge::Conflicts, ..pr() },
            MyPr { number: 3, open_threads: 2, ..pr() },
            MyPr { number: 5, open_threads: 1, ..pr() },
        ]);
        let order: Vec<u64> = list.iter().map(|p| p.number).collect();
        assert_eq!(order, vec![2, 5, 3, 1], "newest first within the same state");
    }

    #[test]
    fn a_search_response_becomes_your_prs() {
        let raw = json!({"data":{"search":{"nodes":[
            {"number":4,"title":"My own work","isDraft":false,"headRefName":"me/my-own-work",
             "headRefOid":"sha4","isCrossRepository":false,"reviewDecision":"CHANGES_REQUESTED",
             "mergeable":"MERGEABLE",
             "latestReviews":{"nodes":[{"author":{"login":"alice"},"state":"CHANGES_REQUESTED"},
                                       {"author":{"login":"bob"},"state":"COMMENTED"}]},
             "reviewThreads":{"nodes":[{"isResolved":false},{"isResolved":true},{"isResolved":false}]},
             "headCommit":{"nodes":[{"commit":{"statusCheckRollup":{"state":"SUCCESS"}}}]}},
            {}
        ]}}});
        let prs = parse(&raw).unwrap();
        assert_eq!(prs.len(), 1, "a node that is not a PR is dropped");
        let p = &prs[0];
        assert_eq!((p.number, p.branch.as_str()), (4, "me/my-own-work"));
        assert_eq!(p.review, Review::ChangesRequested);
        assert_eq!(p.open_threads, 2);
        assert_eq!(p.ci, Ci::Passing);
        assert_eq!(p.reviewers, vec![("alice".to_string(), Review::ChangesRequested)], "a comment decides nothing");
    }

    #[test]
    fn a_failed_search_is_not_an_empty_list() {
        assert!(parse(&json!({"errors":[{"message":"rate limited"}]})).unwrap_err().to_string().contains("rate limited"));
        assert!(parse(&json!({"data":{}})).is_err());
        assert!(parse(&json!({"data":{"search":{"nodes":[]}}})).unwrap().is_empty());
    }

    #[test]
    fn the_search_is_this_repo_and_your_open_prs() {
        let ctx = RepoContext {
            owner: "acme".into(),
            name: "widgets".into(),
            repo_root: std::path::PathBuf::from("/src"),
            me: "me".into(),
        };
        assert_eq!(search_terms(&ctx), "repo:acme/widgets is:pr is:open author:@me");
    }
}
