//! Past reviews, read out of Claude Code's session transcripts.
//!
//! Before the ledger existed, the only durable record of a review was the
//! transcript of the session that ran it: every assistant turn, including
//! the synthesis and the fenced `autoreview` trailer the system prompt asks
//! for. Each trailer is one review; the text since the previous trailer is
//! the review it belongs to, and the `Flagged by` lines in it say which
//! panelist raised what. This reads all of that back once, so the statistics
//! start from the history rather than from today.

use crate::findings;
use crate::ledger::{self, Counts, PanelEntry, Run, VERSION};
use crate::report::Trailer;
use serde_json::Value;
use std::collections::{HashMap, HashSet};
use std::path::{Path, PathBuf};

/// A content fingerprint of a review, the same for a live record and the
/// transcript it was read from, and different for a distinct review. The
/// import skips a transcript trailer already recorded live by matching on
/// this, rather than on time proximity, which could never tell a re-recorded
/// review from a separate one that finished nearby. The decision is left out:
/// a live record reads it back from GitHub, so it can differ from the
/// trailer's own claim that the import stores.
fn review_signature(r: &Run) -> String {
    let mut panel: Vec<String> = r
        .panel
        .iter()
        .map(|p| {
            format!(
                "{}:{}:{}:{}:{}",
                p.name,
                p.model.as_deref().unwrap_or(""),
                p.ok.map_or("", |ok| if ok { "y" } else { "n" }),
                p.findings.map_or(-1, |n| n as i64),
                p.top.as_deref().unwrap_or("")
            )
        })
        .collect();
    panel.sort();
    let counts = r.counts.as_ref().map_or(String::new(), |c| {
        format!("{:?}/{:?}/{:?}", c.must_fix, c.should_fix, c.polish)
    });
    format!("{}|{}|{}|{}", r.session.as_deref().unwrap_or(""), r.risk.as_deref().unwrap_or(""), counts, panel.join(";"))
}

#[derive(Debug, Default, PartialEq)]
pub struct Imported {
    pub files: usize,
    pub added: usize,
    /// Trailers already in the ledger, by id or by a live record of the
    /// same session.
    pub skipped: usize,
}

/// `<command-args>9</command-args>`, `/auto-review 9`, `panel review 9`,
/// `pr-review-tab 9 --babysit`: the PR the session was started for.
pub fn pr_in_prompt(prompt: &str) -> Option<u64> {
    if let Some(rest) = prompt.split("<command-args>").nth(1) {
        let arg = rest.split('<').next().unwrap_or("").trim();
        if let Some(n) = first_int(arg) {
            return Some(n);
        }
    }
    const CUES: [&str; 6] =
        ["auto-review", "panel-review", "panel review", "pr-review-tab", "recheck-pr", "review pr"];
    let lower = prompt.to_ascii_lowercase();
    for cue in CUES {
        if let Some(i) = lower.find(cue)
            && let Some(n) = first_int(&prompt[i + cue.len()..])
        {
            return Some(n);
        }
    }
    None
}

fn first_int(s: &str) -> Option<u64> {
    s.split(|c: char| !c.is_ascii_digit())
        .find(|t| !t.is_empty())
        .and_then(|t| t.parse().ok())
        .filter(|n: &u64| *n > 0)
}

/// Every text block of a message, joined. A user message may be a bare
/// string; an assistant message is always blocks.
fn message_text(entry: &Value) -> String {
    match entry.pointer("/message/content") {
        Some(Value::String(s)) => s.clone(),
        Some(Value::Array(blocks)) => blocks
            .iter()
            .filter(|b| b.get("type").and_then(Value::as_str) == Some("text"))
            .filter_map(|b| b.get("text").and_then(Value::as_str))
            .collect::<Vec<_>>()
            .join("\n\n"),
        _ => String::new(),
    }
}

fn epoch_of(entry: &Value) -> Option<i64> {
    let ts = entry.get("timestamp").and_then(Value::as_str)?;
    let whole = ts.split_once('.').map_or(ts, |(w, _)| w);
    let whole = whole.strip_suffix('Z').unwrap_or(whole);
    crate::prlist::parse_iso(&format!("{whole}Z"))
}

/// Every fenced trailer block in a text, in order, with the text that led
/// up to each. `report::parse_trailer` wants only the last one; an import
/// wants them all.
fn trailers_in(text: &str) -> Vec<(Trailer, usize)> {
    let mut out = Vec::new();
    let mut from = 0;
    while let Some(rel) = text[from..].find("```autoreview") {
        let start = from + rel;
        let body_start = start + "```autoreview".len();
        let Some(end_rel) = text[body_start..].find("```") else { break };
        let body = text[body_start..body_start + end_rel].trim();
        // A fence with no decision and no panel is not a review trailer: it is
        // an empty object, or a doc that explains the format. Every Trailer
        // field is optional, so without this check `{}` imports as a review.
        if let Ok(mut t) = serde_json::from_str::<Trailer>(body)
            && (t.decision.is_some() || !t.panel.is_empty())
        {
            // The same sanitize a live trailer gets: strip control bytes and
            // cap the roster, so an imported record is no less safe to store.
            crate::report::sanitize(&mut t);
            out.push((t, body_start + end_rel + 3));
        }
        from = body_start + end_rel + 3;
    }
    out
}

/// `owner/name` from a remote url, when the checkout is still there to ask.
/// A transcript names only its working directory, and a deleted worktree
/// keeps its basename as the next best thing.
fn repo_label(cwd: &str, cache: &mut HashMap<String, String>) -> String {
    if let Some(r) = cache.get(cwd) {
        return r.clone();
    }
    let label = repo_slug(Path::new(cwd));
    cache.insert(cwd.to_string(), label.clone());
    label
}

/// A repository's `owner/name`, from the origin remote when the checkout is
/// still there to ask, and the directory basename otherwise. Both `panel` and
/// the import name a repository this way, so one repository is one row however
/// it was recorded -- a worktree's own directory name does not become a second.
pub fn repo_slug(dir: &Path) -> String {
    let basename =
        dir.file_name().map(|n| n.to_string_lossy().into_owned()).unwrap_or_else(|| dir.display().to_string());
    if !dir.is_dir() {
        return basename;
    }
    std::process::Command::new("git")
        .args(["-C", &dir.display().to_string(), "config", "--get", "remote.origin.url"])
        .output()
        .ok()
        .filter(|o| o.status.success())
        .and_then(|o| slug_of_remote(String::from_utf8_lossy(&o.stdout).trim()))
        .unwrap_or(basename)
}

/// `git@github.com:acme/widgets.git` and `https://github.com/acme/widgets`
/// are both `acme/widgets`.
pub fn slug_of_remote(url: &str) -> Option<String> {
    let trimmed = url.trim().trim_end_matches('/').trim_end_matches(".git");
    let tail = trimmed.rsplit(['/', ':']).take(2).collect::<Vec<_>>();
    if tail.len() != 2 || tail.iter().any(|p| p.is_empty()) {
        return None;
    }
    Some(format!("{}/{}", tail[1], tail[0]))
}

/// The reviews one transcript holds. Pure, so it can be tested on text;
/// `repo` is resolved by the caller from the cwd the transcript names.
pub fn runs_in_transcript(text: &str, repo_of: &mut dyn FnMut(&str) -> String) -> Vec<Run> {
    let mut runs = Vec::new();
    let mut session: Option<String> = None;
    let mut pr: Option<u64> = None;
    let mut cwd: Option<String> = None;
    // The review a trailer closes: every assistant text since the last one.
    let mut pending = String::new();
    for line in text.lines() {
        let Ok(entry) = serde_json::from_str::<Value>(line) else { continue };
        if session.is_none() {
            session = entry.get("sessionId").and_then(Value::as_str).map(str::to_string);
        }
        if cwd.is_none() {
            cwd = entry.get("cwd").and_then(Value::as_str).map(str::to_string);
        }
        match entry.get("type").and_then(Value::as_str) {
            Some("user") => {
                if pr.is_none() {
                    pr = pr_in_prompt(&message_text(&entry));
                }
            }
            Some("assistant") => {
                let body = message_text(&entry);
                if body.trim().is_empty() {
                    continue;
                }
                let blocks = trailers_in(&body);
                if blocks.is_empty() {
                    pending.push_str(&body);
                    pending.push('\n');
                    continue;
                }
                let uuid = entry.get("uuid").and_then(Value::as_str).unwrap_or("").to_string();
                let at = epoch_of(&entry).unwrap_or_else(ledger::now);
                let model = entry.pointer("/message/model").and_then(Value::as_str).map(str::to_string);
                let mut cursor = 0;
                for (i, (trailer, end)) in blocks.iter().enumerate() {
                    let review = format!("{pending}{}", &body[cursor..*end]);
                    cursor = *end;
                    pending.clear();
                    // The review text was captured only when it holds a real
                    // synthesis. A bare sign-off before the trailer is not one,
                    // so its reported counts must not enter the keep rate.
                    let reviewed = findings::is_synthesis(&review);
                    let sid = session.clone().unwrap_or_default();
                    runs.push(Run {
                        v: VERSION,
                        id: format!("transcript:{sid}:{uuid}{}", if i == 0 { String::new() } else { format!(":{i}") }),
                        at,
                        source: "transcript".into(),
                        repo: cwd.as_deref().map(&mut *repo_of),
                        pr,
                        session: session.clone(),
                        // Through the same spelling a live record uses, so one
                        // verdict does not split across two forms in the footer.
                        decision: trailer.decision.as_deref().map(ledger::decision_token),
                        risk: trailer.risk.clone(),
                        counts: trailer.findings.as_ref().map(Counts::from),
                        cost_usd: None,
                        duration_secs: None,
                        driver_model: model.clone(),
                        panel: trailer.panel.iter().map(PanelEntry::from).collect(),
                        findings: findings::parse(&review),
                        reviewed,
                    });
                }
                // Whatever followed the last block starts the next review.
                pending.push_str(&body[cursor..]);
            }
            _ => {}
        }
    }
    runs
}

fn transcripts_under(dir: &Path, out: &mut Vec<PathBuf>) {
    let Ok(entries) = std::fs::read_dir(dir) else { return };
    for entry in entries.flatten() {
        // file_type does not follow the link, so a symbolic link to a parent
        // directory cannot send the walk into an endless loop.
        let Ok(kind) = entry.file_type() else { continue };
        let path = entry.path();
        if kind.is_dir() {
            transcripts_under(&path, out);
        } else if kind.is_file() && path.extension().is_some_and(|e| e == "jsonl") {
            out.push(path);
        }
    }
}

/// Read every transcript under `dir` into the ledger at `path`, skipping what
/// is already there -- a record already in the ledger by id, and a review
/// autoreview recorded live, matched by content fingerprint. The fingerprint
/// is best effort: two reviews in one session with identical panel results,
/// counts and risk share a fingerprint, so a re-import after live recording
/// can drop one as a near-duplicate. This has no effect on the common path --
/// a first import into an empty ledger matches nothing and imports everything,
/// and live recording is the source of truth from then on.
pub fn from_transcripts(dir: &Path, path: &Path, existing: &[Run]) -> std::io::Result<Imported> {
    // Owned and updated as runs are written, so the same id in two transcript
    // files is added once, not once per file.
    let mut known: HashSet<String> = existing.iter().map(|r| r.id.clone()).collect();
    // A review autoreview recorded live holds the same trailer this import
    // would read, under a different id, so it must not be counted twice. They
    // are matched by content fingerprint, which is identical for the two
    // records of one review and different for a separate review in the same
    // session -- unlike time proximity, which cannot tell them apart.
    let live: HashSet<String> =
        existing.iter().filter(|r| r.source != "transcript").map(review_signature).collect();
    let mut files = Vec::new();
    transcripts_under(dir, &mut files);
    files.sort();
    let mut result = Imported::default();
    let mut repos: HashMap<String, String> = HashMap::new();
    for file in &files {
        let Ok(text) = std::fs::read_to_string(file) else { continue };
        // Cheap gate before parsing a thousand JSON lines: no fence, no review.
        if !text.contains("```autoreview") {
            continue;
        }
        result.files += 1;
        for run in runs_in_transcript(&text, &mut |cwd| repo_label(cwd, &mut repos)) {
            if known.contains(&run.id) || live.contains(&review_signature(&run)) {
                result.skipped += 1;
                continue;
            }
            ledger::append(path, &run)?;
            known.insert(run.id.clone());
            result.added += 1;
        }
    }
    Ok(result)
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn the_pr_number_in_the_first_prompt() {
        assert_eq!(pr_in_prompt("<command-message>auto-review</command-message>\n<command-name>/auto-review</command-name>\n<command-args>6</command-args>"), Some(6));
        assert_eq!(pr_in_prompt("/auto-review 1772"), Some(1772));
        assert_eq!(pr_in_prompt("panel review 27"), Some(27));
        assert_eq!(pr_in_prompt("pr-review-tab 9 --babysit 30"), Some(9));
        assert_eq!(pr_in_prompt("/recheck-pr 12"), Some(12));
        assert_eq!(pr_in_prompt("Review PR #14 for bugs"), Some(14));
        assert_eq!(pr_in_prompt("# Code review request\n\nYou are one member of a panel"), None);
        assert_eq!(pr_in_prompt("is it possible to get statistics"), None);
    }

    #[test]
    fn remote_urls_become_slugs() {
        assert_eq!(slug_of_remote("git@github.com:acme/widgets.git").as_deref(), Some("acme/widgets"));
        assert_eq!(slug_of_remote("https://github.com/acme/widgets").as_deref(), Some("acme/widgets"));
        assert_eq!(slug_of_remote("https://github.com/acme/widgets.git/").as_deref(), Some("acme/widgets"));
        assert_eq!(slug_of_remote(""), None);
    }

    fn line(v: Value) -> String {
        serde_json::to_string(&v).unwrap()
    }

    fn user(text: &str) -> String {
        line(serde_json::json!({
            "type": "user", "sessionId": "s1", "cwd": "/w/widgets", "uuid": "u0",
            "timestamp": "2026-08-26T20:00:00.000Z",
            "message": {"role": "user", "content": text}
        }))
    }

    fn assistant(uuid: &str, ts: &str, text: &str) -> String {
        line(serde_json::json!({
            "type": "assistant", "sessionId": "s1", "cwd": "/w/widgets", "uuid": uuid,
            "timestamp": ts,
            "message": {"role": "assistant", "model": "claude-fable-5",
                        "content": [{"type": "text", "text": text}]}
        }))
    }

    const TRAILER: &str = "```autoreview\n{\"decision\":\"commented\",\"risk\":\"MEDIUM\",\"findings\":{\"must_fix\":0,\"should_fix\":1,\"polish\":0},\"panel\":[{\"name\":\"codex\",\"model\":\"gpt-5.5\",\"ok\":true,\"findings\":2,\"top\":\"MEDIUM\"}]}\n```";

    #[test]
    fn a_transcript_yields_one_run_per_trailer() {
        let text = [
            line(serde_json::json!({"type": "queue-operation", "sessionId": "s1"})),
            user("<command-message>auto-review</command-message>\n<command-args>9</command-args>"),
            assistant("u1", "2026-08-26T20:30:00.000Z", "### should-fix\n- [MEDIUM] src/a.rs:1 — issue. Flagged by: codex (gpt-5.5)"),
            line(serde_json::json!({"type": "user", "message": {"role": "user", "content": [{"type": "tool_result", "content": "ok"}]}})),
            assistant("u2", "2026-08-26T20:36:55.570Z", &format!("Posted.\n\n{TRAILER}")),
            assistant("u3", "2026-08-26T21:00:00.000Z", "Rechecked: fixed.\n- [LOW] src/b.rs:2 — nit. Flagged by: codex (gpt-5.5)"),
            assistant("u4", "2026-08-26T21:01:00.000Z", &format!("{TRAILER}\nDone.")),
        ]
        .join("\n");
        let mut asked = Vec::new();
        let runs = runs_in_transcript(&text, &mut |cwd| {
            asked.push(cwd.to_string());
            "acme/widgets".into()
        });
        assert_eq!(runs.len(), 2);
        let r = &runs[0];
        assert_eq!(r.id, "transcript:s1:u2");
        assert_eq!(r.source, "transcript");
        assert_eq!(r.at, 1_787_776_615);
        assert_eq!(r.repo.as_deref(), Some("acme/widgets"));
        assert_eq!(r.pr, Some(9));
        assert_eq!(r.session.as_deref(), Some("s1"));
        assert_eq!(r.decision.as_deref(), Some("commented"));
        assert_eq!(r.risk.as_deref(), Some("MEDIUM"));
        assert_eq!(r.driver_model.as_deref(), Some("claude-fable-5"));
        assert_eq!(r.panel.len(), 1);
        assert_eq!(r.panel[0].findings, Some(2));
        assert_eq!(r.findings.len(), 1, "the finding before the first trailer belongs to it");
        assert_eq!(r.findings[0].severity.as_deref(), Some("MEDIUM"));
        let r2 = &runs[1];
        assert_eq!(r2.id, "transcript:s1:u4");
        assert_eq!(r2.findings.len(), 1, "and the one after it belongs to the next");
        assert_eq!(r2.findings[0].severity.as_deref(), Some("LOW"));
        assert_eq!(asked, vec!["/w/widgets", "/w/widgets"]);
    }

    #[test]
    fn a_transcript_with_no_trailer_is_no_run() {
        let text = [user("hello"), assistant("u1", "2026-08-26T20:30:00.000Z", "hi")].join("\n");
        assert!(runs_in_transcript(&text, &mut |_| "x".into()).is_empty());
    }

    #[test]
    fn an_empty_fence_is_not_a_review() {
        // A transcript that only explains the trailer format holds fences with
        // no decision and no panel; none of them is a recorded review.
        let text = [
            user("/auto-review 9"),
            assistant("u1", "2026-08-26T20:30:00.000Z", "The trailer looks like:\n```autoreview\n{}\n```"),
            assistant("u2", "2026-08-26T20:31:00.000Z", "Or:\n```autoreview\n{\"hello\":\"world\"}\n```"),
        ]
        .join("\n");
        assert!(runs_in_transcript(&text, &mut |_| "x".into()).is_empty());
    }

    #[test]
    fn a_trailer_with_no_synthesis_text_is_not_counted_in_the_keep_rate() {
        // A sign-off before the trailer, with no synthesis, must not add its
        // reported findings to the keep-rate denominator.
        let bare = "```autoreview\n{\"decision\":\"commented\",\"risk\":\"LOW\",\"findings\":{\"must_fix\":1,\"should_fix\":2,\"polish\":0},\"panel\":[{\"name\":\"codex\",\"model\":\"gpt-5.5\",\"ok\":true,\"findings\":3,\"top\":\"HIGH\"}]}\n```";
        let text = [
            user("/auto-review 9"),
            assistant("u1", "2026-08-26T20:30:00.000Z", &format!("I ran the panel and posted the review.\n\n{bare}")),
        ]
        .join("\n");
        let runs = runs_in_transcript(&text, &mut |_| "acme/widgets".into());
        assert_eq!(runs.len(), 1);
        assert!(!runs[0].reviewed, "no synthesis text was captured");
    }

    #[test]
    fn importing_skips_what_the_ledger_has() {
        let dir = std::env::temp_dir().join(format!("ar-import-{}-{}", std::process::id(), ledger::now()));
        let projects = dir.join("projects/-w-widgets");
        std::fs::create_dir_all(&projects).unwrap();
        let text = [
            user("/auto-review 9"),
            assistant("u2", "2026-08-26T20:36:55.570Z", &format!("Posted.\n\n{TRAILER}")),
        ]
        .join("\n");
        std::fs::write(projects.join("s1.jsonl"), &text).unwrap();
        std::fs::write(projects.join("other.jsonl"), user("nothing here")).unwrap();
        let path = dir.join("ledger.jsonl");

        let first = from_transcripts(&dir.join("projects"), &path, &[]).unwrap();
        assert_eq!(first, Imported { files: 1, added: 1, skipped: 0 });
        let existing = ledger::read(&path).unwrap();
        assert_eq!(existing.len(), 1);
        assert_eq!(existing[0].repo.as_deref(), Some("widgets"), "a cwd that is gone keeps its basename");

        let again = from_transcripts(&dir.join("projects"), &path, &existing).unwrap();
        assert_eq!(again, Imported { files: 1, added: 0, skipped: 1 });

        // A session autoreview recorded live is not imported on top, matched
        // by content rather than time. A live record with a different id but
        // the same content skips the import.
        let mut live = existing[0].clone();
        live.id = "autoreview:s1:100".into();
        live.source = "autoreview".into();
        live.at = existing[0].at + 99_999; // far in time, still the same review
        let live_only = from_transcripts(&dir.join("projects"), &dir.join("fresh.jsonl"), &[live]).unwrap();
        assert_eq!(live_only, Imported { files: 1, added: 0, skipped: 1 }, "same content, skipped");

        // A live record of a DIFFERENT review (different panel result) in the
        // same session does not mask this transcript's review.
        let mut other = existing[0].clone();
        other.id = "autoreview:s1:200".into();
        other.source = "autoreview".into();
        other.panel[0].findings = Some(99);
        let distinct = from_transcripts(&dir.join("projects"), &dir.join("fresh2.jsonl"), &[other]).unwrap();
        assert_eq!(distinct, Imported { files: 1, added: 1, skipped: 0 }, "distinct content, imported");
    }
}
