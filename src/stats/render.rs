//! The stats report on the screen: a table on a terminal, aligned text when
//! piped, JSON on request. Model names are agent-authored, so they go through
//! the same sanitizer as everything else the summary prints.

use super::{Cohort, Overview, Ratio};
use crate::report::sanitize_for_display;
use crate::ui::{align, count, fmt_dur};
use comfy_table::{Attribute, Cell, Color};
use serde::Serialize;
use std::path::Path;

pub struct Report<'a> {
    pub overview: Overview,
    pub cohorts: Vec<Cohort>,
    pub attention: Vec<String>,
    /// `Flagged by` names that matched no panelist, with their counts.
    pub unresolved: std::collections::BTreeMap<String, u32>,
    pub ledger: &'a Path,
    pub since: Option<i64>,
}

/// Agent-authored names, cut to a width a table can hold.
const LABEL_WIDTH: usize = 40;

const HEADER: [&str; 11] =
    ["MODEL", "BACKEND", "RUNS", "AVAIL", "RAW/RUN", "KEPT", "UNIQUE", "HIGH+", "DROPPED", "KEEP RATE", "SAMPLE"];

/// `97% (93-99)`: the point and where the truth plausibly is.
pub fn ratio_label(r: &Ratio) -> String {
    match (r.value, r.low, r.high) {
        (Some(v), Some(lo), Some(hi)) => {
            format!("{:.0}% ({:.0}-{:.0})", v * 100.0, lo * 100.0, hi * 100.0)
        }
        _ => "-".into(),
    }
}

fn label(s: &str) -> String {
    console::truncate_str(&sanitize_for_display(s), LABEL_WIDTH, "…").to_string()
}

fn model_label(c: &Cohort) -> String {
    label(&c.key.model)
}

fn row(c: &Cohort) -> Vec<String> {
    vec![
        model_label(c),
        label(&c.key.backend),
        c.runs.to_string(),
        ratio_label(&c.availability()),
        c.raw_per_run().map_or("-".into(), |v| format!("{v:.1}")),
        c.kept.to_string(),
        c.kept_unique.to_string(),
        c.kept_high.to_string(),
        c.dropped.to_string(),
        ratio_label(&c.keep_rate()),
        c.sample().to_string(),
    ]
}

/// Civil date from an epoch second (Howard Hinnant's civil_from_days).
pub fn fmt_date(epoch: i64) -> String {
    let z = epoch.div_euclid(86_400) + 719_468;
    let era = z.div_euclid(146_097);
    let doe = z - era * 146_097;
    let yoe = (doe - doe / 1460 + doe / 36_524 - doe / 146_096) / 365;
    let y = yoe + era * 400;
    let doy = doe - (365 * yoe + yoe / 4 - yoe / 100);
    let mp = (5 * doy + 2) / 153;
    let d = doy - (153 * mp + 2) / 5 + 1;
    let m = if mp < 10 { mp + 3 } else { mp - 9 };
    let y = if m <= 2 { y + 1 } else { y };
    format!("{y:04}-{m:02}-{d:02}")
}

fn headline(r: &Report) -> String {
    let o = &r.overview;
    let span = match (o.first_at, o.last_at) {
        (Some(a), Some(b)) if fmt_date(a) == fmt_date(b) => format!(" on {}", fmt_date(a)),
        (Some(a), Some(b)) => format!(" from {} to {}", fmt_date(a), fmt_date(b)),
        _ => String::new(),
    };
    let repos = if o.repos > 0 { format!(" across {}", count(o.repos, "repo")) } else { String::new() };
    format!("{}{span}{repos}", count(o.runs, "review"))
}

fn breakdown(map: &std::collections::BTreeMap<String, usize>) -> String {
    let mut pairs: Vec<(&String, &usize)> = map.iter().collect();
    pairs.sort_by(|a, b| b.1.cmp(a.1).then(a.0.cmp(b.0)));
    pairs.iter().map(|(k, v)| format!("{v} {k}")).collect::<Vec<_>>().join(", ")
}

fn footer(r: &Report) -> Vec<String> {
    let mut lines = Vec::new();
    if !r.overview.decisions.is_empty() {
        lines.push(format!("decisions: {}", breakdown(&r.overview.decisions)));
    }
    if !r.overview.risks.is_empty() {
        lines.push(format!("risk: {}", breakdown(&r.overview.risks)));
    }
    let timed: Vec<String> = r
        .cohorts
        .iter()
        .filter_map(|c| c.median_secs().map(|s| format!("{} {}", model_label(c), fmt_dur(s))))
        .collect();
    if !timed.is_empty() {
        lines.push(format!("median time per review: {}", timed.join(", ")));
    }
    for note in &r.attention {
        lines.push(format!("needs attention: {note}"));
    }
    if !r.unresolved.is_empty() {
        let total: u32 = r.unresolved.values().sum();
        let mut names: Vec<(&String, &u32)> = r.unresolved.iter().collect();
        names.sort_by(|a, b| b.1.cmp(a.1).then(a.0.cmp(b.0)));
        let shown: Vec<String> = names.iter().take(5).map(|(n, k)| format!("{} x{k}", label(n))).collect();
        let more = if names.len() > 5 { format!(", {} more", names.len() - 5) } else { String::new() };
        lines.push(format!(
            "not scored: {} credited to a name not on its panel ({}{more})",
            count(total as usize, "finding"),
            shown.join(", ")
        ));
    }
    lines.push(format!("ledger: {}", r.ledger.display()));
    lines
}

/// Aligned text, for a pipe or a file.
pub fn plain(r: &Report) -> String {
    let mut out = String::new();
    out.push_str(&headline(r));
    out.push('\n');
    if !r.cohorts.is_empty() {
        let mut rows = vec![HEADER.map(String::from).to_vec()];
        rows.extend(r.cohorts.iter().map(row));
        out.push('\n');
        out.push_str(&align(&rows));
    }
    out.push('\n');
    for line in footer(r) {
        out.push_str(&line);
        out.push('\n');
    }
    out
}

fn sample_cell(c: &Cohort) -> Cell {
    match c.sample() {
        "proven" => Cell::new("proven").fg(Color::Green),
        "emerging" => Cell::new("emerging").fg(Color::Yellow),
        other => Cell::new(other).add_attribute(Attribute::Dim),
    }
}

fn availability_cell(c: &Cohort) -> Cell {
    let label = ratio_label(&c.availability());
    match c.availability().value {
        Some(v) if v < 0.5 => Cell::new(label).fg(Color::Red),
        Some(v) if v < 0.9 => Cell::new(label).fg(Color::Yellow),
        Some(_) => Cell::new(label).fg(Color::Green),
        None => Cell::new(label).add_attribute(Attribute::Dim),
    }
}

/// The terminal rendering, in the summary's own style.
pub fn table(r: &Report) -> String {
    let mut out = String::new();
    out.push_str(&console::style(headline(r)).bold().to_string());
    out.push('\n');
    if !r.cohorts.is_empty() {
        let mut t = crate::ui::new_table();
        t.set_header(HEADER.to_vec());
        for c in &r.cohorts {
            let cells = row(c);
            t.add_row(vec![
                Cell::new(&cells[0]).add_attribute(Attribute::Bold),
                Cell::new(&cells[1]),
                Cell::new(&cells[2]),
                availability_cell(c),
                Cell::new(&cells[4]),
                Cell::new(&cells[5]),
                Cell::new(&cells[6]),
                Cell::new(&cells[7]),
                Cell::new(&cells[8]),
                Cell::new(&cells[9]),
                sample_cell(c),
            ]);
        }
        out.push_str(&t.to_string());
        out.push('\n');
    }
    for line in footer(r) {
        let styled = if line.starts_with("needs attention") {
            console::style(line).yellow().to_string()
        } else {
            console::style(line).dim().to_string()
        };
        out.push_str(&styled);
        out.push('\n');
    }
    out
}

#[derive(Serialize)]
struct CohortJson<'a> {
    #[serde(flatten)]
    cohort: &'a Cohort,
    availability: Ratio,
    keep_rate: Ratio,
    raw_per_run: Option<f64>,
    sample: &'static str,
    median_secs: Option<u64>,
}

#[derive(Serialize)]
struct ReportJson<'a> {
    generated_at: i64,
    since: Option<i64>,
    ledger: String,
    overview: &'a Overview,
    attention: &'a [String],
    cohorts: Vec<CohortJson<'a>>,
}

pub fn json(r: &Report) -> String {
    let doc = ReportJson {
        generated_at: crate::ledger::now(),
        since: r.since,
        ledger: r.ledger.display().to_string(),
        overview: &r.overview,
        attention: &r.attention,
        cohorts: r
            .cohorts
            .iter()
            .map(|c| CohortJson {
                cohort: c,
                availability: c.availability(),
                keep_rate: c.keep_rate(),
                raw_per_run: c.raw_per_run(),
                sample: c.sample(),
                median_secs: c.median_secs(),
            })
            .collect(),
    };
    serde_json::to_string_pretty(&doc).unwrap_or_else(|e| format!("{{\"error\":\"{e}\"}}"))
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::stats::Key;

    #[test]
    fn dates_from_epochs() {
        assert_eq!(fmt_date(0), "1970-01-01");
        assert_eq!(fmt_date(1_785_542_400), "2026-08-01");
        assert_eq!(fmt_date(1_757_376_000), "2025-09-09");
    }

    #[test]
    fn ratios_read_as_a_point_and_an_interval() {
        assert_eq!(ratio_label(&Ratio::of(179, 185)), "97% (93-99)");
        assert_eq!(ratio_label(&Ratio::of(0, 0)), "-");
        assert_eq!(ratio_label(&Ratio::of(0, 5)), "0% (0-43)");
    }

    fn cohort() -> Cohort {
        Cohort {
            key: Key { backend: "codex".into(), model: "gpt-5.5".into() },
            runs: 12,
            answered: 11,
            failed: 1,
            raw_findings: 22,
            raw_runs: 11,
            kept: 9,
            kept_unique: 4,
            kept_high: 2,
            dropped: 1,
            keep_num: 9,
            keep_den: 22,
            durations: vec![30, 90, 60],
            first_at: 0,
            last_at: 86_400,
        }
    }

    #[test]
    fn the_plain_report_is_greppable() {
        let mut overview = Overview { runs: 12, repos: 2, first_at: Some(0), last_at: Some(86_400), ..Default::default() };
        overview.decisions.insert("commented".into(), 8);
        overview.decisions.insert("approved".into(), 4);
        let r = Report {
            overview,
            cohorts: vec![cohort()],
            attention: vec!["fable on claude answered 0 of 5 launches".into()],
            unresolved: Default::default(),
            ledger: Path::new("/l/ledger.jsonl"),
            since: None,
        };
        let out = plain(&r);
        assert!(out.starts_with("12 reviews from 1970-01-01 to 1970-01-02 across 2 repos\n"), "{out}");
        assert!(out.contains("MODEL    BACKEND  RUNS  AVAIL        RAW/RUN  KEPT  UNIQUE  HIGH+  DROPPED  KEEP RATE    SAMPLE"), "{out}");
        assert!(out.contains("gpt-5.5  codex    12    92% (65-99)  2.0      9     4       2      1        41% (23-61)  emerging"), "{out}");
        assert!(out.contains("decisions: 8 commented, 4 approved\n"), "{out}");
        assert!(out.contains("median time per review: gpt-5.5 1m00s\n"), "{out}");
        assert!(out.contains("needs attention: fable on claude answered 0 of 5 launches\n"), "{out}");
        assert!(out.ends_with("ledger: /l/ledger.jsonl\n"), "{out}");
    }

    #[test]
    fn the_json_report_carries_the_ratios() {
        let r = Report {
            overview: Overview::default(),
            cohorts: vec![cohort()],
            attention: vec![],
            unresolved: Default::default(),
            ledger: Path::new("/l"),
            since: Some(5),
        };
        let v: serde_json::Value = serde_json::from_str(&json(&r)).unwrap();
        assert_eq!(v["since"], 5);
        assert_eq!(v["cohorts"][0]["key"]["model"], "gpt-5.5");
        assert_eq!(v["cohorts"][0]["availability"]["numerator"], 11);
        assert_eq!(v["cohorts"][0]["keep_rate"]["denominator"], 22);
        assert_eq!(v["cohorts"][0]["sample"], "emerging");
        assert_eq!(v["cohorts"][0]["median_secs"], 60);
    }
}
