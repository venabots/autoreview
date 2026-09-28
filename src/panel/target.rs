//! What the panel is reviewing, and the three shapes that come in: a diff of
//! the working tree, a diff of committed work, or no diff at all -- the code
//! at HEAD, audited as it stands.
//!
//! The distinction decides more than the diff text. Uncommitted work only
//! exists in the user's own checkout, so panelists read it there and touch
//! nothing. Committed work has a ref, so each panelist gets a worktree of its
//! own and may run the tests.

use anyhow::{Context, Result, bail};
use std::path::Path;
use std::process::Command;

#[derive(Debug, Clone, PartialEq)]
pub enum Target {
    Uncommitted,
    Staged,
    Base(String),
    /// The code at HEAD, or only the part under a path, with no change.
    Tree(Option<String>),
}

/// What the panelists are shown. A diff says what changed; without one there
/// is nothing to hand over but the names of the files to read.
#[derive(Debug, Clone, PartialEq)]
pub enum Subject {
    Diff(String),
    /// The files to audit, named rather than carried: a whole repository does
    /// not fit in a prompt, and a panelist with read tools can open them.
    /// `scope` is the path the run was narrowed to, if any.
    Files { scope: Option<String>, files: Vec<String> },
}

#[derive(Debug)]
pub struct Resolved {
    /// What the report says it reviewed.
    pub label: String,
    pub subject: Subject,
    /// True when each panelist gets its own worktree and may run commands.
    pub isolated: bool,
    /// The commit every worktree pins to; None for working-tree targets.
    pub sha: Option<String>,
    /// Files git is not tracking. No diff covers them, so the panelists are
    /// told to read them from the tree instead.
    pub untracked: Vec<String>,
}

fn git(repo_root: &Path, args: &[&str]) -> Result<std::process::Output> {
    Command::new("git")
        .args(args)
        .current_dir(repo_root)
        .output()
        .with_context(|| format!("running git {}", args.join(" ")))
}

fn git_stdout(repo_root: &Path, args: &[&str]) -> Result<String> {
    let out = git(repo_root, args)?;
    if !out.status.success() {
        bail!(
            "git {} failed: {}",
            args.join(" "),
            String::from_utf8_lossy(&out.stderr).trim()
        );
    }
    Ok(String::from_utf8_lossy(&out.stdout).to_string())
}

pub fn resolve(target: &Target, repo_root: &Path) -> Result<Resolved> {
    let resolved = match target {
        // Returned as it is: it has no diff, so the empty-diff advice below
        // has nothing to say about it.
        Target::Tree(path) => return tree(repo_root, path.as_deref()),
        // Against HEAD rather than the index, so staged and unstaged edits
        // both reach the panel -- "what I have not committed yet" is the
        // whole of it, not the half that happens to be staged.
        Target::Uncommitted => Resolved {
            label: format!("uncommitted changes on {}", branch(repo_root)),
            subject: Subject::Diff(git_stdout(repo_root, &["diff", "HEAD"])?),
            isolated: false,
            sha: None,
            untracked: untracked_files(repo_root)?,
        },
        Target::Staged => Resolved {
            label: format!("staged changes on {}", branch(repo_root)),
            subject: Subject::Diff(git_stdout(repo_root, &["diff", "--cached"])?),
            isolated: false,
            sha: None,
            // Nothing untracked is in the index, so nothing untracked is part
            // of what this target reviews.
            untracked: Vec::new(),
        },
        Target::Base(base) => {
            // A base that starts with a dash reaches git as an option, not a
            // ref: `--output=<path>` makes git write the diff to a file and
            // print nothing, and the run would then stop with "nothing to
            // review", which names the wrong cause entirely.
            if base.starts_with('-') {
                bail!("--base expects a ref, and \"{base}\" starts with a dash");
            }
            let verified = git(repo_root, &["rev-parse", "--verify", "--quiet", &format!("{base}^{{commit}}")])?;
            if !verified.status.success() {
                bail!("--base: no commit named \"{base}\" in this repository");
            }
            // Three dots: the diff of what this branch added, not of every
            // change on the base since it forked. Reviewing the latter would
            // flag other people's commits as this branch's work.
            let range = format!("{base}...HEAD");
            let diff = git_stdout(repo_root, &["diff", &range])?;
            let sha = git_stdout(repo_root, &["rev-parse", "HEAD"])?.trim().to_string();
            let commits = git_stdout(repo_root, &["rev-list", "--count", &format!("{base}..HEAD")])?
                .trim()
                .parse::<usize>()
                .unwrap_or(0);
            Resolved {
                label: format!(
                    "{} on {} vs {base}",
                    crate::ui::count(commits, "commit"),
                    branch(repo_root)
                ),
                subject: Subject::Diff(diff),
                isolated: true,
                // A committed target is what the worktrees are pinned to;
                // anything untracked is not part of it.
                sha: Some(sha),
                untracked: Vec::new(),
            }
        }
    };

    // An empty diff is not a review anyone wants: every panelist would spend
    // a model call to report nothing, and the synthesis would agree with them.
    if let Subject::Diff(diff) = &resolved.subject
        && diff.trim().is_empty()
    {
        // A change that only adds files is the common way to land here, and
        // "the diff is empty" is a baffling thing to be told while looking at
        // the new files. Name them.
        // Asked for here rather than taken from the target: an empty diff is
        // the one moment untracked files explain themselves, even for a target
        // that would not otherwise review them.
        // Best-effort here alone: the run is already failing, and a second
        // failure should not replace the reason with a git error.
        let untracked = untracked_files(repo_root).unwrap_or_default();
        if !untracked.is_empty() {
            let (subject, verb) = if untracked.len() == 1 { ("file", "is") } else { ("files", "are") };
            // Staging is enough for a working-tree diff; a base-vs-HEAD diff
            // only sees what has been committed.
            let advice = match target {
                Target::Base(_) => "commit them first",
                _ => "add them with `git add` first",
            };
            bail!(
                "nothing to review: the diff for {} is empty. {} {subject} {verb} not tracked by git yet, so no diff covers them: {}. To review them, {advice}.",
                resolved.label,
                untracked.len(),
                summarize(&untracked)
            );
        }
        bail!("nothing to review: the diff for {} is empty", resolved.label);
    }
    Ok(resolved)
}

/// The committed files at HEAD, or those under one path.
///
/// HEAD rather than the working tree, so every panelist gets a worktree and
/// may run the tests -- an audit that can run the suite reports a failing
/// test as evidence, not a guess. The label names the commit, so a reader
/// with uncommitted edits can see they were not part of it.
fn tree(repo_root: &Path, path: Option<&str>) -> Result<Resolved> {
    let verified = git(repo_root, &["rev-parse", "--verify", "--quiet", "HEAD^{commit}"])?;
    if !verified.status.success() {
        bail!("--tree: this repository has no commits yet");
    }
    let sha = String::from_utf8_lossy(&verified.stdout).trim().to_string();
    let scope = match path {
        Some(path) => scope_for(repo_root, path)?,
        None => None,
    };
    // The SHA, not HEAD: a commit landing between the two calls would
    // otherwise list one commit's files and pin the worktrees to another.
    // Literal pathspecs, so a directory named like git's pathspec magic, such
    // as `:(top)src`, names that directory and nothing else.
    let mut args = vec!["--literal-pathspecs", "ls-tree", "-r", "-z", "--full-tree", sha.as_str()];
    if let Some(scope) = &scope {
        args.extend(["--", scope.as_str()]);
    }
    let files = blob_paths(&git_stdout(repo_root, &args)?);
    if files.is_empty() {
        match &scope {
            Some(scope) => bail!("nothing to review: no file under \"{}\" at HEAD", crate::report::sanitize_for_display(scope)),
            None => bail!("nothing to review: HEAD has no files"),
        }
    }
    let short = sha.get(..7).unwrap_or(&sha);
    let under = scope
        .as_deref()
        .map(|s| format!(" under {}", crate::report::sanitize_for_display(s)))
        .unwrap_or_default();
    Ok(Resolved {
        label: format!(
            "{}{under} at {short} on {}",
            crate::ui::count(files.len(), "file"),
            branch(repo_root)
        ),
        subject: Subject::Files { scope, files },
        isolated: true,
        sha: Some(sha),
        untracked: Vec::new(),
    })
}

/// The file paths in `ls-tree -r -z` output, without submodules. A submodule
/// is a gitlink whose directory is empty in every worktree, so a panelist told
/// to read it has nothing to open.
fn blob_paths(raw: &str) -> Vec<String> {
    raw.split('\0')
        .filter_map(|entry| entry.split_once('\t'))
        .filter(|(meta, _)| meta.split(' ').nth(1) == Some("blob"))
        .map(|(_, path)| path.to_string())
        .collect()
}

/// The path the user named, as a path from the repository root. Relative
/// paths are read from where the user stands, so `panel --tree .` inside
/// `src/` means `src/`, not the whole repository.
fn scope_for(repo_root: &Path, path: &str) -> Result<Option<String>> {
    let as_path = Path::new(path);
    if as_path.is_absolute() {
        let real = real_path(as_path);
        let Ok(inside) = real.strip_prefix(repo_root) else {
            bail!("--tree: \"{path}\" is outside this repository");
        };
        return scope_within("", &inside.to_string_lossy());
    }
    // From the process's own directory, not the root: that is where the user
    // typed the path.
    // Checked: an empty prefix from a failed call would read the path from the
    // root, and audit the wrong directory without a word.
    let prefix = Command::new("git")
        .args(["rev-parse", "--show-prefix"])
        .output()
        .context("running git rev-parse --show-prefix")?;
    if !prefix.status.success() {
        bail!(
            "git rev-parse --show-prefix failed: {}",
            String::from_utf8_lossy(&prefix.stderr).trim()
        );
    }
    scope_within(String::from_utf8_lossy(&prefix.stdout).trim(), path)
}

/// The path with its longest existing ancestor made canonical. git reports
/// the root at its realpath, and on macOS a path under /var is really under
/// /private/var. The path names something at HEAD, which need not exist on
/// disk, so the part that does not exist is kept as typed.
fn real_path(path: &Path) -> std::path::PathBuf {
    path.ancestors()
        .find_map(|ancestor| {
            let real = ancestor.canonicalize().ok()?;
            let rest = path.strip_prefix(ancestor).ok()?;
            Some(real.join(rest))
        })
        .unwrap_or_else(|| path.to_path_buf())
}

/// Join a relative path onto the directory the user stands in, both relative
/// to the repository root, and resolve `.` and `..` by hand. Lexical on
/// purpose: the path names something at HEAD, which need not exist on disk.
/// None means the root itself, which is the whole repository.
fn scope_within(prefix: &str, path: &str) -> Result<Option<String>> {
    let parts = prefix.split('/').chain(path.split('/')).try_fold(Vec::new(), |parts: Vec<&str>, part| {
        match part {
            "" | "." => Some(parts),
            ".." => parts.split_last().map(|(_, rest)| rest.to_vec()),
            name => Some([parts.as_slice(), &[name]].concat()),
        }
    });
    match parts {
        None => bail!("--tree: \"{path}\" is outside this repository"),
        Some(parts) if parts.is_empty() => Ok(None),
        Some(parts) => Ok(Some(parts.join("/"))),
    }
}

/// Every file git is not tracking. The error is propagated rather than read
/// as "there are none": a review that quietly leaves out every new file
/// because one git call failed is worse than one that refuses to start.
fn untracked_files(repo_root: &Path) -> Result<Vec<String>> {
    // -z, so git does not apply core.quotePath: a name with a non-ASCII or
    // unusual character would otherwise reach the prompt in an escaped form
    // that no read tool can open.
    Ok(git_stdout(repo_root, &["ls-files", "--others", "--exclude-standard", "-z"])?
        .split('\0')
        // Only empty fields, not whitespace-only ones: " " is a legal
        // filename, and dropping it would leave a file nobody reviews.
        .filter(|l| !l.is_empty())
        .map(str::to_string)
        .collect())
}

/// A readable list for a message, saying how many it did not name rather than
/// stopping silently.
fn summarize(files: &[String]) -> String {
    const SHOWN: usize = 10;
    // Sanitized: -z removed git's quoting, and these names go to a terminal.
    let names: Vec<String> = files
        .iter()
        .take(SHOWN)
        .map(|f| crate::report::sanitize_for_display(f))
        .collect();
    if files.len() <= SHOWN {
        return names.join(", ");
    }
    format!("{}, and {} more", names.join(", "), files.len() - SHOWN)
}

fn branch(repo_root: &Path) -> String {
    git_stdout(repo_root, &["rev-parse", "--abbrev-ref", "HEAD"])
        .map(|s| s.trim().to_string())
        .unwrap_or_else(|_| "HEAD".into())
}

#[cfg(test)]
mod tests {
    use super::*;

    fn scope(prefix: &str, path: &str) -> Option<String> {
        scope_within(prefix, path).expect("inside the repository")
    }

    #[test]
    fn a_tree_path_is_read_from_where_the_user_stands() {
        assert_eq!(scope("", "src"), Some("src".into()));
        assert_eq!(scope("", "src/panel/"), Some("src/panel".into()));
        assert_eq!(scope("src/", "panel"), Some("src/panel".into()));
        assert_eq!(scope("src/", "."), Some("src".into()));
        assert_eq!(scope("src/panel/", "../bin"), Some("src/bin".into()));
        assert_eq!(scope("", "./src//panel"), Some("src/panel".into()));
        // The root itself is the whole repository, which has no scope.
        assert_eq!(scope("", "."), None);
        assert_eq!(scope("src/", ".."), None);
    }

    #[test]
    fn only_files_are_listed_not_submodules() {
        // A submodule is a gitlink, and its worktree directory is empty, so
        // no panelist could read it.
        let raw = "100644 blob aaa\tsrc/a.rs\x00160000 commit bbb\tvendor/lib\x00100755 blob ccc\tbin/run\x00";
        assert_eq!(blob_paths(raw), vec!["src/a.rs".to_string(), "bin/run".to_string()]);
        // A name may hold a tab; only the first one ends the metadata.
        assert_eq!(blob_paths("100644 blob aaa\ta\tb.rs\x00"), vec!["a\tb.rs".to_string()]);
    }

    #[test]
    fn a_tree_path_outside_the_repository_is_refused() {
        let e = scope_within("", "..").err().unwrap();
        assert_eq!(e.to_string(), "--tree: \"..\" is outside this repository");
        assert!(scope_within("src/", "../../x").is_err());
    }

    #[test]
    fn a_base_target_is_isolated_and_a_working_tree_one_is_not() {
        // The property that decides worktrees and permissions, stated once.
        assert_eq!(Target::Base("main".into()), Target::Base("main".into()));
        assert_ne!(Target::Uncommitted, Target::Staged);
    }
}
