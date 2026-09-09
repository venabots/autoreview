//! What a synthesized review says about each finding, read back from its text.
//!
//! The synthesis is prose written to a shape: every finding that survived
//! verification is one bullet with a severity tag, a location, and a trailing
//! `Flagged by:` clause naming the panelists that raised it. That clause is
//! the only place the review records which model found what, so it is what
//! the stats stand on. Everything here is best-effort: a line that does not
//! fit the shape is skipped, never a reason to fail.

use serde::{Deserialize, Serialize};

pub const SEVERITIES: [&str; 4] = ["CRITICAL", "HIGH", "MEDIUM", "LOW"];

/// One panelist named in a `Flagged by:` clause, as the synthesis wrote it:
/// `codex (gpt-5.5)` gives both, a bare `claude-opus` gives only the name.
#[derive(Debug, Clone, PartialEq, Eq, Hash, Serialize, Deserialize)]
pub struct Source {
    pub name: String,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub model: Option<String>,
}

/// A finding the synthesis surfaced, reduced to what the stats need.
#[derive(Debug, Clone, PartialEq, Serialize, Deserialize)]
pub struct Finding {
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub severity: Option<String>,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub location: Option<String>,
    #[serde(default)]
    pub flagged_by: Vec<Source>,
    /// How many panelists raised it. At least `flagged_by.len()`; larger when
    /// the synthesis wrote a count without names ("Flagged by 3.").
    pub count: u32,
    /// Listed under `### Disagreements`: the synthesis surfaced it to say it
    /// did not hold up, so it counts against its sources, not for them.
    #[serde(default, skip_serializing_if = "std::ops::Not::not")]
    pub dropped: bool,
}

/// Which bucket a heading opens, so a bullet under `### should-fix` that
/// carries no tag of its own still gets a severity.
#[derive(Clone, Copy, PartialEq)]
enum Section {
    Other,
    Bucket(&'static str),
    Disagreements,
}

fn section_of(heading: &str) -> Section {
    let h = heading.trim_start_matches('#').trim().to_ascii_lowercase();
    if h.contains("disagreement") {
        Section::Disagreements
    } else if h.starts_with("must-fix") || h.starts_with("must fix") {
        Section::Bucket("HIGH")
    } else if h.starts_with("should-fix") || h.starts_with("should fix") {
        Section::Bucket("MEDIUM")
    } else if h.starts_with("polish") {
        Section::Bucket("LOW")
    } else {
        Section::Other
    }
}

/// Every `Flagged by` bullet in the text, in order, duplicates removed. A
/// resumed session quotes its earlier findings when it re-checks them, and
/// the same finding must not count twice.
pub fn parse(text: &str) -> Vec<Finding> {
    let mut out: Vec<Finding> = Vec::new();
    let mut section = Section::Other;
    for raw in text.lines() {
        let line = raw.trim();
        if line.starts_with('#') {
            section = section_of(line);
            continue;
        }
        let Some(finding) = parse_line(line, section) else { continue };
        if !out.iter().any(|f| same_finding(f, &finding)) {
            out.push(finding);
        }
    }
    out
}

fn same_finding(a: &Finding, b: &Finding) -> bool {
    a.severity == b.severity
        && a.location == b.location
        && a.count == b.count
        && a.flagged_by.len() == b.flagged_by.len()
        && a.flagged_by.iter().all(|s| b.flagged_by.contains(s))
}

fn parse_line(line: &str, section: Section) -> Option<Finding> {
    let (head, tail) = line.split_once("Flagged by")?;
    let head = unlink(head);
    let severity = SEVERITIES
        .iter()
        .find(|s| head.contains(&format!("[{s}]")))
        .map(|s| s.to_string())
        .or(match section {
            Section::Bucket(s) => Some(s.to_string()),
            _ => None,
        });
    let (count, flagged_by) = parse_sources(tail);
    if count == 0 {
        return None;
    }
    Some(Finding {
        severity,
        location: location_in(&head),
        flagged_by,
        count,
        dropped: section == Section::Disagreements,
    })
}

/// The clause after "Flagged by": `: a (m), b (m2)`, `2: a (m) [LOW], b (m2)
/// [MEDIUM] — using higher.`, `3.` or `all 4.` A count with no names is still
/// a count.
fn parse_sources(tail: &str) -> (u32, Vec<Source>) {
    // The note after the list ("— using higher.") is not a panelist.
    let tail = tail.split(" — ").next().unwrap_or(tail);
    let tail = tail.split(" -- ").next().unwrap_or(tail).trim();
    let (count_part, list) = match tail.split_once(':') {
        Some((c, l)) => (c.trim(), Some(l)),
        None => (tail.trim_end_matches('.').trim(), None),
    };
    let explicit = count_part
        .trim_start_matches("all")
        .trim()
        .trim_end_matches('.')
        .trim();
    let explicit: Option<u32> = if explicit.is_empty() { None } else { explicit.parse().ok() };
    // Words where a count was expected ("Flagged by nobody") are not a clause.
    if explicit.is_none() && !count_part.is_empty() {
        return (0, Vec::new());
    }
    let sources: Vec<Source> = list
        .map(|l| l.split(',').filter_map(parse_source).collect())
        .unwrap_or_default();
    let count = explicit.unwrap_or(sources.len() as u32).max(sources.len() as u32);
    (count, sources)
}

fn parse_source(item: &str) -> Option<Source> {
    let item = unlink(item);
    // An inline severity ("[LOW]") records which panelist said what; the
    // finding's own severity is the one the synthesis chose.
    let item = match item.find('[') {
        Some(i) => &item[..i],
        None => item.as_str(),
    };
    let item = item.trim().trim_end_matches('.').trim_matches(|c| c == '`' || c == '*').trim();
    if item.is_empty() {
        return None;
    }
    let (name, model) = match item.split_once('(') {
        Some((n, m)) => (n.trim(), Some(m.trim_end_matches(')').trim())),
        None => (item, None),
    };
    let ok_name = !name.is_empty()
        && name.chars().all(|c| c.is_ascii_alphanumeric() || matches!(c, '-' | '_' | '.' | ':' | '/'));
    if !ok_name {
        return None;
    }
    Some(Source {
        name: name.to_string(),
        model: model.filter(|m| !m.is_empty()).map(str::to_string),
    })
}

/// `[text](url)` becomes `text`, so a location wrapped in a link to the PR
/// reads the same as a bare one.
fn unlink(s: &str) -> String {
    let mut out = String::with_capacity(s.len());
    let mut rest = s;
    while let Some(open) = rest.find('[') {
        let Some(close_rel) = rest[open..].find(']') else { break };
        let close = open + close_rel;
        // A tag like "[HIGH]" is a bracket that is not a link: keep it and
        // carry on after it.
        if !rest[close..].starts_with("](") {
            out.push_str(&rest[..=close]);
            rest = &rest[close + 1..];
            continue;
        }
        let Some(end_rel) = rest[close..].find(')') else { break };
        out.push_str(&rest[..open]);
        out.push_str(&rest[open + 1..close]);
        rest = &rest[close + end_rel + 1..];
    }
    out.push_str(rest);
    out
}

/// The first `path:line` token, which is how the synthesis names where a
/// finding lives. A path has a dot or a slash in it, so a stray `9:` or a
/// `Fix:` label is not mistaken for one.
fn location_in(head: &str) -> Option<String> {
    head.split_whitespace()
        .map(|t| t.trim_matches(|c: char| !c.is_ascii_alphanumeric() && c != '/' && c != '.'))
        .find(|t| {
            let Some((path, line)) = t.rsplit_once(':') else { return false };
            let digits = line.split('-').all(|p| !p.is_empty() && p.bytes().all(|b| b.is_ascii_digit()));
            !path.is_empty() && digits && (path.contains('.') || path.contains('/'))
        })
        .map(str::to_string)
}

/// The synthesized risk, from the line under `### Risk`.
pub fn risk(text: &str) -> Option<String> {
    let mut in_risk = false;
    for raw in text.lines() {
        let line = raw.trim();
        if line.starts_with('#') {
            in_risk = line.trim_start_matches('#').trim().eq_ignore_ascii_case("risk");
            continue;
        }
        if in_risk && !line.is_empty() {
            let word = line.trim_start_matches(['*', '`', '-', ' ']);
            return SEVERITIES.iter().find(|s| word.starts_with(*s)).map(|s| s.to_string());
        }
    }
    None
}

/// How many findings a panelist's own report carries: its severity-tagged
/// bullets. `NO_FINDINGS` is a report of zero, and a report with neither is
/// not a count at all.
pub fn count_raw(report: &str) -> Option<u64> {
    let bullets = report
        .lines()
        .map(str::trim)
        .filter(|l| l.starts_with('-') || l.starts_with('*'))
        .filter(|l| {
            let l = unlink(l);
            SEVERITIES.iter().any(|s| l.contains(&format!("[{s}]")))
        })
        .count() as u64;
    if bullets == 0 && !report.contains("NO_FINDINGS") {
        return None;
    }
    Some(bullets)
}

/// The most severe tag among a report's bullets.
pub fn top_severity(report: &str) -> Option<String> {
    SEVERITIES
        .iter()
        .find(|s| report.lines().any(|l| l.trim_start().starts_with('-') && l.contains(&format!("[{s}]"))))
        .map(|s| s.to_string())
}

#[cfg(test)]
mod tests {
    use super::*;

    fn src(name: &str, model: Option<&str>) -> Source {
        Source { name: name.into(), model: model.map(str::to_string) }
    }

    #[test]
    fn a_single_source_with_its_model() {
        let f = parse("- [LOW] src/a.rs:130 — the issue. Fix: the change. Flagged by: codex (gpt-5.5)");
        assert_eq!(f.len(), 1);
        assert_eq!(f[0].severity.as_deref(), Some("LOW"));
        assert_eq!(f[0].location.as_deref(), Some("src/a.rs:130"));
        assert_eq!(f[0].flagged_by, vec![src("codex", Some("gpt-5.5"))]);
        assert_eq!(f[0].count, 1);
        assert!(!f[0].dropped);
    }

    #[test]
    fn consensus_with_inline_severities_and_a_note() {
        let f = parse(
            "- [MEDIUM] src/a.rs:130 — the issue. Fix: the change. Flagged by 2: claude (claude-opus-5) [LOW], codex (gpt-5.5) [MEDIUM] — using higher.",
        );
        assert_eq!(f[0].count, 2);
        assert_eq!(
            f[0].flagged_by,
            vec![src("claude", Some("claude-opus-5")), src("codex", Some("gpt-5.5"))]
        );
        assert_eq!(f[0].severity.as_deref(), Some("MEDIUM"));
    }

    #[test]
    fn linked_locations_and_bold_tags_read_the_same() {
        let f = parse(
            "- **[HIGH]** [apps/api/src/kyb.ts:104](https://github.com/o/r/pull/1/files#diff-abcR104) — bad. Flagged by: claude-fable (claude-fable-5)",
        );
        assert_eq!(f[0].severity.as_deref(), Some("HIGH"));
        assert_eq!(f[0].location.as_deref(), Some("apps/api/src/kyb.ts:104"));
        assert_eq!(f[0].flagged_by, vec![src("claude-fable", Some("claude-fable-5"))]);
    }

    #[test]
    fn a_count_without_names_is_still_a_count() {
        let f = parse("- [MEDIUM] a.ts:90 — issue. Flagged by 3.\n- [LOW] b.ts:1 — issue. Flagged by all 4.");
        assert_eq!(f.len(), 2);
        assert_eq!(f[0].count, 3);
        assert!(f[0].flagged_by.is_empty());
        assert_eq!(f[1].count, 4);
    }

    #[test]
    fn a_bare_name_with_a_trailing_period() {
        let f = parse("- `[LOW]` `trace.ts:233` — issue. Flagged by: claude-opus.");
        assert_eq!(f[0].flagged_by, vec![src("claude-opus", None)]);
        assert_eq!(f[0].location.as_deref(), Some("trace.ts:233"));
    }

    #[test]
    fn the_bucket_heading_supplies_a_missing_severity() {
        let text = "### should-fix\n\n- src/a.rs:1 — issue. Flagged by: codex (gpt-5)\n\n### polish\n\n- src/b.rs:2 — nit. Flagged by: codex (gpt-5)";
        let f = parse(text);
        assert_eq!(f[0].severity.as_deref(), Some("MEDIUM"));
        assert_eq!(f[1].severity.as_deref(), Some("LOW"));
    }

    #[test]
    fn disagreements_are_dropped_findings() {
        let text = "### must-fix\n- [HIGH] a.rs:1 — real. Flagged by: codex (gpt-5)\n### Disagreements\n- [HIGH] b.rs:2 — not a bug on inspection. Flagged by: claude (claude-opus-5)";
        let f = parse(text);
        assert!(!f[0].dropped);
        assert!(f[1].dropped);
    }

    #[test]
    fn a_requoted_finding_counts_once() {
        let line = "- [LOW] a.rs:1 — issue. Flagged by: codex (gpt-5)";
        assert_eq!(parse(&format!("{line}\n\nlater:\n{line}")).len(), 1);
    }

    #[test]
    fn prose_that_is_not_a_finding_is_skipped() {
        assert!(parse("Nothing was flagged by anyone.\nFlagged by nobody in particular.").is_empty());
        assert!(parse("### Overview\n**Reviewing:** PR #9").is_empty());
    }

    #[test]
    fn the_risk_line() {
        assert_eq!(risk("### Risk\n\n**MEDIUM** — touches real logic.").as_deref(), Some("MEDIUM"));
        assert_eq!(risk("### Risk\nLOW: docs only.").as_deref(), Some("LOW"));
        assert_eq!(risk("### Overview\nLOW"), None);
    }

    #[test]
    fn raw_counts_from_a_panelist_report() {
        assert_eq!(count_raw("- [LOW] a.rs:1 — nit\n- [HIGH] b.rs:2 — bug"), Some(2));
        assert_eq!(count_raw("NO_FINDINGS"), Some(0));
        assert_eq!(count_raw("I could not review this."), None);
        assert_eq!(top_severity("- [LOW] a.rs:1 — nit\n- [HIGH] b.rs:2 — bug").as_deref(), Some("HIGH"));
        assert_eq!(top_severity("NO_FINDINGS"), None);
    }
}
