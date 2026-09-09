//! `autoreview stats`: how each panelist model has done, across every review
//! the ledger holds.
//!
//! A cohort is one model on one backend. What the ledger can say about it:
//! how often it came back with a review at all, how much it reported, and how
//! much of that the synthesis kept after checking it against the code -- the
//! only verification step in the pipeline, so "kept" is the closest thing to
//! "right" the data has. Every ratio is shown with its numerator, denominator
//! and a Wilson interval, because ten runs and three hundred do not deserve
//! the same confidence and a bare percentage hides which is which.

pub mod cli;
pub mod import;
pub mod render;

use crate::findings::Finding;
use crate::ledger::Run;
use serde::Serialize;
use std::collections::{BTreeMap, HashMap};

use crate::panel::panelist::BACKENDS;

/// Runs enough to call a cohort's numbers settled, and enough to show them
/// at all with a caveat.
pub const PROVEN_RUNS: u32 = 30;
pub const EMERGING_RUNS: u32 = 10;

/// One model on one backend, spelled one way. The trailer is agent-written,
/// so the same model arrives as `xai/grok-4.6` and `grok-4.6`, and the same
/// panelist as `codex` and `codex-gpt-5.6-sol`; this is what folds them.
#[derive(Debug, Clone, Default, PartialEq, Eq, Hash, PartialOrd, Ord, Serialize)]
pub struct Key {
    pub backend: String,
    pub model: String,
}

pub fn backend_of(name: &str) -> String {
    let lower = name.trim().to_ascii_lowercase();
    for b in BACKENDS {
        if lower == b || lower.starts_with(&format!("{b}-")) {
            return b.to_string();
        }
    }
    lower.split('-').next().unwrap_or(&lower).to_string()
}

/// The cohort a panelist belongs to, from the name the run gave it and the
/// model it reported. A missing model falls back to whatever the name
/// carries after the backend ("codex-gpt-5.6-sol" is gpt-5.6-sol).
pub fn canonical(name: &str, model: Option<&str>) -> Key {
    let backend = backend_of(name);
    let from_name = name
        .trim()
        .to_ascii_lowercase()
        .strip_prefix(&format!("{backend}-"))
        .map(str::to_string);
    let raw = model
        .map(|m| m.trim().to_ascii_lowercase())
        .filter(|m| !m.is_empty() && m != "unknown" && m != "?")
        // A "model" that is just the panelist's name again ("codex-gpt-5.6-sol")
        // says nothing the name did not; the name carries the model.
        .filter(|m| *m != name.trim().to_ascii_lowercase())
        .or(from_name)
        .unwrap_or_else(|| "unknown".into());
    Key { model: normalize_model(&raw), backend }
}

fn normalize_model(raw: &str) -> String {
    let mut m = raw.to_string();
    // A provider prefix says where the model was served from, which is the
    // backend's business -- except a local ollama model, which is a
    // different thing from the hosted model of the same name.
    if let Some((provider, name)) = m.rsplit_once('/') {
        m = if provider == "ollama" { format!("ollama/{name}") } else { name.to_string() };
    }
    let mut segs: Vec<String> = m.split('-').map(String::from).collect();
    // A trailing eight digit part is a release date ("claude-haiku-4-5-20251001"):
    // the dated snapshot is the same model as the short id, so fold it on.
    let is_date = |s: &str| s.len() == 8 && s.bytes().all(|b| b.is_ascii_digit());
    if segs.len() >= 3 && is_date(&segs[segs.len() - 1]) {
        segs.pop();
    }
    // "claude-fable-5-1" and "claude-fable-5.1" are one model. Only a one or
    // two digit tail is a minor version, so a date never joins as one.
    let minor = |s: &str| (1..=2).contains(&s.len()) && s.bytes().all(|b| b.is_ascii_digit());
    let n = segs.len();
    if n >= 3 && minor(&segs[n - 1]) && minor(&segs[n - 2]) {
        let head = segs[..n - 2].join("-");
        segs = vec![format!("{head}-{}.{}", segs[n - 2], segs[n - 1])];
    }
    segs.join("-")
}

#[derive(Debug, Clone, Default, Serialize)]
pub struct Cohort {
    pub key: Key,
    /// Times launched, answered or not.
    pub runs: u32,
    pub answered: u32,
    pub failed: u32,
    /// What the panelists reported themselves, before synthesis.
    pub raw_findings: u64,
    /// Answered runs that reported a count, so raw_findings has a per-run.
    pub raw_runs: u32,
    /// Findings the synthesis kept that name this cohort.
    pub kept: u32,
    /// Kept findings only this cohort raised.
    pub kept_unique: u32,
    /// Kept findings at HIGH or CRITICAL.
    pub kept_high: u32,
    /// Kept findings the synthesis listed under Disagreements: surfaced to
    /// say they did not hold up.
    pub dropped: u32,
    /// The keep rate's parts: per run, the kept findings that fit inside the
    /// reported count, over the reported count.
    pub keep_num: u64,
    pub keep_den: u64,
    /// Per-launch durations, for the median. Not serialized: a JSON reader
    /// wants `median_secs`, not one number per launch.
    #[serde(skip)]
    pub durations: Vec<u64>,
    pub first_at: i64,
    pub last_at: i64,
}

impl Cohort {
    pub fn availability(&self) -> Ratio {
        Ratio::of(self.answered as u64, (self.answered + self.failed) as u64)
    }
    pub fn keep_rate(&self) -> Ratio {
        Ratio::of(self.keep_num, self.keep_den)
    }
    pub fn raw_per_run(&self) -> Option<f64> {
        (self.raw_runs > 0).then(|| self.raw_findings as f64 / self.raw_runs as f64)
    }
    pub fn sample(&self) -> &'static str {
        if self.answered >= PROVEN_RUNS {
            "proven"
        } else if self.answered >= EMERGING_RUNS {
            "emerging"
        } else {
            "thin"
        }
    }
    pub fn median_secs(&self) -> Option<u64> {
        if self.durations.is_empty() {
            return None;
        }
        let mut d = self.durations.clone();
        d.sort_unstable();
        Some(d[d.len() / 2])
    }
}

/// A count over a count, with the interval that says how much to trust it.
#[derive(Debug, Clone, PartialEq, Serialize)]
pub struct Ratio {
    pub numerator: u64,
    pub denominator: u64,
    pub value: Option<f64>,
    pub low: Option<f64>,
    pub high: Option<f64>,
}

impl Ratio {
    pub fn of(numerator: u64, denominator: u64) -> Ratio {
        let (low, high) = wilson(numerator, denominator).unzip();
        Ratio {
            numerator,
            denominator,
            value: (denominator > 0).then(|| numerator as f64 / denominator as f64),
            low,
            high,
        }
    }
}

/// The 95% Wilson score interval: where the true rate plausibly is, given
/// `k` of `n`. Unlike the normal approximation it stays inside 0..1 and does
/// not collapse to a point at 0 of 3 or 3 of 3.
pub fn wilson(k: u64, n: u64) -> Option<(f64, f64)> {
    if n == 0 {
        return None;
    }
    let z = 1.96_f64;
    let n = n as f64;
    let p = k as f64 / n;
    let denom = 1.0 + z * z / n;
    let centre = p + z * z / (2.0 * n);
    let margin = z * ((p * (1.0 - p) + z * z / (4.0 * n)) / n).sqrt();
    Some((((centre - margin) / denom).max(0.0), ((centre + margin) / denom).min(1.0)))
}

/// Which cohort a `Flagged by` name means, in the context of one run's
/// roster. The synthesis writes the name the run gave the panelist, so an
/// exact match wins; a bare backend name ("claude") means the one claude on
/// the panel when there was one; anything else is taken at face value.
fn resolve(source: &crate::findings::Source, roster: &[(String, Key)]) -> Option<Key> {
    let name = source.name.trim().to_ascii_lowercase();
    // An explicit model is authoritative. Credit the panelist that ran that
    // exact model, or no one -- never fall back to the name, which would
    // credit a different model that shares the backend. "claude (claude-opus-5)"
    // on a panel whose only claude ran claude-fable-5 is not that panelist.
    if let Some(model) = source.model.as_deref() {
        let with_model = canonical(&source.name, Some(model));
        if let Some((_, key)) = roster.iter().find(|(_, k)| *k == with_model) {
            return Some(key.clone());
        }
        // The roster may not have recorded a model. A sole backend panelist
        // whose model is unknown is the one meant; more than one, or a known
        // and different model, is not -- leave it unscored.
        let mut unknown: Vec<&Key> =
            roster.iter().map(|(_, k)| k).filter(|k| k.backend == with_model.backend && k.model == "unknown").collect();
        unknown.dedup();
        return match unknown[..] {
            [key] => Some(key.clone()),
            _ => None,
        };
    }
    if let Some((_, key)) = roster.iter().find(|(n, _)| n.eq_ignore_ascii_case(&name)) {
        return Some(key.clone());
    }
    // With no model, the name is all there is. The synthesis sometimes names a
    // panelist by its model alone ("glm-5.3"),
    // by backend and model ("opencode-glm-5.3") when the run called it plain
    // "opencode", or by the model's family ("glm") when only one was on the
    // panel.
    let as_model = normalize_model(&name);
    let mut by_model: Vec<&Key> = roster.iter().map(|(_, k)| k).filter(|k| k.model == as_model).collect();
    by_model.dedup();
    if let [key] = by_model[..] {
        return Some(key.clone());
    }
    let as_key = canonical(&source.name, None);
    if roster.iter().any(|(_, k)| *k == as_key) {
        return Some(as_key);
    }
    // A bare backend name ("claude") means the one panelist on that backend.
    // A longer name that merely starts with the backend ("claude-code-bot",
    // "claude-opus-5") is a different thing -- an outside bot, or a model not
    // on the panel -- so it is not credited to that sole panelist.
    let backend = backend_of(&name);
    let mut on_backend: Vec<&Key> = roster.iter().map(|(_, k)| k).filter(|k| k.backend == backend).collect();
    on_backend.dedup();
    if name == backend
        && let [key] = on_backend[..]
    {
        return Some(key.clone());
    }
    let mut by_family: Vec<&Key> = roster
        .iter()
        .map(|(_, k)| k)
        .filter(|k| k.model.starts_with(&format!("{as_model}-")) || k.model.starts_with(&format!("{as_model}/")))
        .collect();
    by_family.dedup();
    if let [key] = by_family[..] {
        return Some(key.clone());
    }
    // Not a panelist on this run. Another reviewer bot the synthesis credited,
    // or prose the parser let through: either way it is not a model's score.
    None
}

fn is_high(f: &Finding) -> bool {
    matches!(f.severity.as_deref(), Some("HIGH") | Some("CRITICAL"))
}

/// What the fold found: the cohorts, and the credits it could not place.
#[derive(Debug, Default)]
pub struct Folded {
    pub cohorts: Vec<Cohort>,
    /// `Flagged by` names that matched nobody on their run's panel, with how
    /// often each appeared. Shown so a systematic miss is visible, not lost.
    pub unresolved: BTreeMap<String, u32>,
}

/// Fold every run into its cohorts.
pub fn aggregate(runs: &[Run]) -> Vec<Cohort> {
    fold(&runs.iter().collect::<Vec<_>>()).cohorts
}

pub fn fold(runs: &[&Run]) -> Folded {
    let mut cohorts: BTreeMap<Key, Cohort> = BTreeMap::new();
    let mut unresolved: BTreeMap<String, u32> = BTreeMap::new();
    for run in runs {
        let roster: Vec<(String, Key)> = run
            .panel
            .iter()
            .map(|p| (p.name.trim().to_ascii_lowercase(), canonical(&p.name, p.model.as_deref())))
            .collect();

        // What each cohort was credited with in this run's synthesis.
        let mut kept_here: HashMap<Key, u32> = HashMap::new();
        for f in &run.findings {
            let mut keys: Vec<Key> = Vec::new();
            for s in &f.flagged_by {
                match resolve(s, &roster) {
                    Some(k) => keys.push(k),
                    None => *unresolved.entry(s.name.trim().to_ascii_lowercase()).or_insert(0) += 1,
                }
            }
            keys.sort();
            keys.dedup();
            let unique = f.count == 1 && keys.len() == 1;
            for key in keys {
                let c = cohorts.entry(key.clone()).or_insert_with(|| Cohort { key: key.clone(), ..Default::default() });
                if f.dropped {
                    c.dropped += 1;
                    continue;
                }
                c.kept += 1;
                if unique {
                    c.kept_unique += 1;
                }
                if is_high(f) {
                    c.kept_high += 1;
                }
                *kept_here.entry(key).or_insert(0) += 1;
            }
        }

        // The roster: launched, answered, and what each reported.
        let mut raw_here: HashMap<Key, u64> = HashMap::new();
        for (p, (_, key)) in run.panel.iter().zip(&roster) {
            let c = cohorts.entry(key.clone()).or_insert_with(|| Cohort { key: key.clone(), ..Default::default() });
            c.runs += 1;
            match p.ok {
                Some(true) => c.answered += 1,
                Some(false) => c.failed += 1,
                None => {}
            }
            if p.ok == Some(true)
                && let Some(n) = p.findings
            {
                c.raw_findings += n;
                c.raw_runs += 1;
                *raw_here.entry(key.clone()).or_insert(0) += n;
            }
            if let Some(d) = p.duration_secs {
                c.durations.push(d);
            }
            if c.first_at == 0 || run.at < c.first_at {
                c.first_at = run.at;
            }
            c.last_at = c.last_at.max(run.at);
        }

        // The keep rate needs both sides from the same run: kept findings
        // are only a rate against the count they were kept from. A run whose
        // review text was not read has an empty findings list for a reason
        // that is not "nothing was kept", so it must not drag the rate down.
        if run.reviewed {
            for (key, raw) in raw_here {
                let kept = kept_here.get(&key).copied().unwrap_or(0) as u64;
                let c = cohorts.get_mut(&key).expect("cohort was created above");
                c.keep_num += kept.min(raw);
                c.keep_den += raw;
            }
        }
    }
    let mut out: Vec<Cohort> = cohorts.into_values().collect();
    out.sort_by(|a, b| b.kept.cmp(&a.kept).then(b.runs.cmp(&a.runs)).then(a.key.cmp(&b.key)));
    Folded { cohorts: out, unresolved }
}

/// The runs a report is about: after `since`, in a repo whose name contains
/// `repo` (case-insensitive), when either is given.
pub fn select<'a>(runs: &'a [Run], since: Option<i64>, repo: Option<&str>) -> Vec<&'a Run> {
    let repo = repo.map(str::to_ascii_lowercase);
    runs.iter()
        .filter(|r| since.is_none_or(|s| r.at >= s))
        .filter(|r| match &repo {
            Some(needle) => r.repo.as_deref().is_some_and(|x| x.to_ascii_lowercase().contains(needle)),
            None => true,
        })
        .collect()
}

/// What the runs as a whole say, above the per-model table.
#[derive(Debug, Clone, Default, Serialize)]
pub struct Overview {
    pub runs: usize,
    pub repos: usize,
    pub first_at: Option<i64>,
    pub last_at: Option<i64>,
    pub decisions: BTreeMap<String, usize>,
    pub risks: BTreeMap<String, usize>,
    pub sources: BTreeMap<String, usize>,
}

pub fn overview(runs: &[&Run]) -> Overview {
    let mut o = Overview { runs: runs.len(), ..Default::default() };
    let mut repos = std::collections::HashSet::new();
    for r in runs {
        if let Some(repo) = &r.repo {
            repos.insert(repo.to_ascii_lowercase());
        }
        o.first_at = Some(o.first_at.map_or(r.at, |f| f.min(r.at)));
        o.last_at = Some(o.last_at.map_or(r.at, |l| l.max(r.at)));
        *o.decisions.entry(r.decision.clone().unwrap_or_else(|| "unknown".into())).or_insert(0) += 1;
        *o.risks.entry(r.risk.clone().unwrap_or_else(|| "unknown".into())).or_insert(0) += 1;
        *o.sources.entry(r.source.clone()).or_insert(0) += 1;
    }
    o.repos = repos.len();
    o
}

/// Cohorts that are costing runs without contributing: launched enough to
/// matter and answering less than half the time. A misconfigured alias looks
/// exactly like this, and nothing else in the pipeline says so.
pub fn attention(cohorts: &[Cohort]) -> Vec<String> {
    cohorts
        .iter()
        .filter(|c| c.answered + c.failed >= 5 && c.availability().value.is_some_and(|v| v < 0.5))
        .map(|c| {
            format!(
                "{} on {} answered {} of {} launches",
                c.key.model, c.key.backend, c.answered, c.answered + c.failed
            )
        })
        .collect()
}

/// The subcommand: parse, maybe import, read, fold, print. Returns the exit
/// status; everything it has to say is already on stdout or stderr.
pub fn main(args: &[String]) -> i32 {
    let opts = match cli::parse(args, crate::ledger::now()) {
        Ok(cli::Parsed::Help) => {
            print!("{}", cli::HELP);
            return 0;
        }
        Ok(cli::Parsed::Run(o)) => o,
        Err(msg) => {
            eprintln!("{msg}");
            if msg.starts_with("unknown arg") {
                eprint!("{}", cli::HELP);
            }
            return 1;
        }
    };
    let Some(path) = opts.ledger.clone().or_else(|| crate::ledger::path(&crate::cli::real_env)) else {
        eprintln!("error: the ledger is off ($AUTOREVIEW_LEDGER=off); pass --ledger PATH to read one anyway");
        return 1;
    };
    let mut runs = match crate::ledger::read(&path) {
        Ok(r) => r,
        Err(e) => {
            eprintln!("error: could not read the ledger at {}: {e}", path.display());
            return 1;
        }
    };
    if opts.import {
        let dir = opts.transcripts.clone().unwrap_or_else(crate::session::projects_dir);
        match import::from_transcripts(&dir, &path, &runs) {
            Ok(done) => {
                eprintln!(
                    "imported {} from {} under {} ({} already recorded)",
                    crate::ui::count(done.added, "review"),
                    crate::ui::count(done.files, "transcript"),
                    dir.display(),
                    done.skipped
                );
                if done.added > 0 {
                    runs = crate::ledger::read(&path).unwrap_or(runs);
                }
            }
            Err(e) => {
                eprintln!("error: import failed: {e}");
                return 1;
            }
        }
    }
    let selected = select(&runs, opts.since, opts.repo.as_deref());
    // An empty result under --json is still JSON: a tool reading the output
    // must not get a friendly sentence where it expected an object.
    if selected.is_empty() && !opts.json {
        if runs.is_empty() {
            println!("no reviews recorded yet in {}", path.display());
            println!("every autoreview pass and panel run is recorded from now on;");
            println!("`autoreview stats --import` reads past reviews out of Claude Code's transcripts");
        } else {
            println!("no reviews match ({} recorded in {})", crate::ui::count(runs.len(), "review"), path.display());
        }
        return 0;
    }
    let folded = fold(&selected);
    let report = render::Report {
        overview: overview(&selected),
        attention: attention(&folded.cohorts),
        unresolved: folded.unresolved,
        cohorts: folded.cohorts,
        ledger: &path,
        since: opts.since,
    };
    if opts.json {
        println!("{}", render::json(&report));
    } else if std::io::IsTerminal::is_terminal(&std::io::stdout()) {
        print!("{}", render::table(&report));
    } else {
        print!("{}", render::plain(&report));
    }
    0
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::findings::Source;
    use crate::ledger::{PanelEntry, VERSION};

    fn key(b: &str, m: &str) -> Key {
        Key { backend: b.into(), model: m.into() }
    }

    #[test]
    fn names_and_models_fold_to_one_spelling() {
        assert_eq!(canonical("codex", Some("gpt-5.6-sol")), key("codex", "gpt-5.6-sol"));
        assert_eq!(canonical("codex-gpt-5.6-sol", Some("gpt-5.6-sol")), key("codex", "gpt-5.6-sol"));
        assert_eq!(canonical("codex-gpt-5.6-sol", Some("codex-gpt-5.6-sol")), key("codex", "gpt-5.6-sol"));
        assert_eq!(canonical("codex-gpt-5.6-sol", None), key("codex", "gpt-5.6-sol"));
        assert_eq!(canonical("opencode-xai-grok-4.6", Some("xai/grok-4.6")), key("opencode", "grok-4.6"));
        assert_eq!(canonical("opencode", Some("zai-coding-plan/glm-5.3")), key("opencode", "glm-5.3"));
        assert_eq!(canonical("opencode-ollama-grok-4.6", Some("ollama/grok-4.6")), key("opencode", "ollama/grok-4.6"));
        assert_eq!(canonical("claude-fable", Some("claude-fable-5-1")), key("claude", "claude-fable-5.1"));
        assert_eq!(canonical("claude-fable", Some("claude-fable-5.1")), key("claude", "claude-fable-5.1"));
        // A dated snapshot folds onto the short id rather than splitting off.
        assert_eq!(canonical("claude", Some("claude-haiku-4-5-20251001")), key("claude", "claude-haiku-4.5"));
        assert_eq!(canonical("claude", Some("claude-haiku-4-5")), key("claude", "claude-haiku-4.5"));
        assert_eq!(canonical("claude", Some("claude-haiku-4.5")), key("claude", "claude-haiku-4.5"));
        assert_eq!(canonical("claude", Some("Claude-Opus-5")), key("claude", "claude-opus-5"));
        assert_eq!(canonical("claude-fable", Some("fable")), key("claude", "fable"));
        assert_eq!(canonical("claude", Some("unknown")), key("claude", "unknown"));
        assert_eq!(canonical("codex", None), key("codex", "unknown"));
        assert_eq!(canonical("gemini", Some("gemini-3")), key("gemini", "gemini-3"));
    }

    #[test]
    fn wilson_stays_inside_the_unit_interval() {
        assert_eq!(wilson(0, 0), None);
        let (lo, hi) = wilson(3, 3).unwrap();
        assert!(lo > 0.4 && lo < 0.5, "3 of 3 is not certainty: {lo}");
        assert_eq!(hi, 1.0);
        let (lo, hi) = wilson(0, 3).unwrap();
        assert_eq!(lo, 0.0);
        assert!(hi > 0.5 && hi < 0.6);
        let (lo, hi) = wilson(179, 185).unwrap();
        assert!(lo > 0.93 && hi < 0.99);
    }

    fn run(at: i64, panel: Vec<PanelEntry>, findings: Vec<Finding>) -> Run {
        Run {
            v: VERSION,
            id: format!("r{at}"),
            at,
            source: "autoreview".into(),
            repo: Some("acme/widgets".into()),
            pr: Some(1),
            session: None,
            decision: Some("commented".into()),
            risk: Some("LOW".into()),
            counts: None,
            cost_usd: None,
            duration_secs: None,
            driver_model: None,
            panel,
            findings,
            reviewed: true,
        }
    }

    fn entry(name: &str, model: &str, ok: Option<bool>, findings: Option<u64>) -> PanelEntry {
        PanelEntry {
            name: name.into(),
            model: Some(model.into()),
            ok,
            findings,
            top: None,
            duration_secs: None,
            exit_code: None,
        }
    }

    fn finding(sev: &str, sources: &[(&str, Option<&str>)], count: u32, dropped: bool) -> Finding {
        Finding {
            severity: Some(sev.into()),
            location: None,
            flagged_by: sources
                .iter()
                .map(|(n, m)| Source { name: n.to_string(), model: m.map(str::to_string) })
                .collect(),
            count,
            dropped,
        }
    }

    #[test]
    fn cohorts_fold_the_roster_and_the_kept_findings() {
        let runs = vec![
            run(
                100,
                vec![entry("codex", "gpt-5.5", Some(true), Some(3)), entry("claude", "claude-opus-5", Some(true), Some(1))],
                vec![
                    finding("HIGH", &[("codex", Some("gpt-5.5"))], 1, false),
                    finding("LOW", &[("codex", Some("gpt-5.5")), ("claude", Some("claude-opus-5"))], 2, false),
                    // A bare backend name resolves to the one claude on the panel.
                    finding("MEDIUM", &[("claude", None)], 1, true),
                ],
            ),
            run(
                200,
                vec![entry("codex", "gpt-5.5", Some(false), None), entry("claude", "claude-opus-5", Some(true), Some(0))],
                vec![],
            ),
        ];
        let cohorts = aggregate(&runs);
        assert_eq!(cohorts.len(), 2);
        let codex = cohorts.iter().find(|c| c.key.backend == "codex").unwrap();
        assert_eq!((codex.runs, codex.answered, codex.failed), (2, 1, 1));
        assert_eq!(codex.raw_findings, 3);
        assert_eq!(codex.raw_runs, 1);
        assert_eq!(codex.kept, 2);
        assert_eq!(codex.kept_unique, 1);
        assert_eq!(codex.kept_high, 1);
        assert_eq!((codex.keep_num, codex.keep_den), (2, 3));
        assert_eq!(codex.availability().value, Some(0.5));
        assert_eq!(codex.sample(), "thin");
        assert_eq!((codex.first_at, codex.last_at), (100, 200));

        let claude = cohorts.iter().find(|c| c.key.backend == "claude").unwrap();
        assert_eq!(claude.kept, 1);
        assert_eq!(claude.kept_unique, 0);
        assert_eq!(claude.dropped, 1, "the disagreement counts against it");
        // Kept in run 1 (1) against reported (1); reported 0 in run 2.
        assert_eq!((claude.keep_num, claude.keep_den), (1, 1));
        // Sorted by kept: codex first.
        assert_eq!(cohorts[0].key.backend, "codex");
    }

    #[test]
    fn a_source_is_matched_to_the_roster_however_it_is_spelled() {
        let roster = vec![
            ("opencode-glm".to_string(), key("opencode", "glm-5.3")),
            ("opencode-grok".to_string(), key("opencode", "grok-4.6")),
            ("codex".to_string(), key("codex", "gpt-5.6-sol")),
        ];
        let src = |n: &str, m: Option<&str>| Source { name: n.into(), model: m.map(str::to_string) };
        assert_eq!(resolve(&src("Opencode-Grok", None), &roster), Some(key("opencode", "grok-4.6")), "by name");
        assert_eq!(resolve(&src("glm-5.3", None), &roster), Some(key("opencode", "glm-5.3")), "by model");
        assert_eq!(resolve(&src("opencode-glm-5.3", None), &roster), Some(key("opencode", "glm-5.3")), "by backend-model");
        assert_eq!(resolve(&src("codex", None), &roster), Some(key("codex", "gpt-5.6-sol")), "the one codex");
        assert_eq!(resolve(&src("grok", None), &roster), Some(key("opencode", "grok-4.6")), "by family");
        assert_eq!(resolve(&src("opencode", Some("glm-5.3")), &roster), Some(key("opencode", "glm-5.3")), "backend + model the run launched under a longer id");
        assert_eq!(resolve(&src("opencode", Some("zai-coding-plan/glm-5.3")), &roster), Some(key("opencode", "glm-5.3")), "...however the model was spelled");
        assert_eq!(resolve(&src("claude", Some("claude-opus-5")), &roster), None, "a model no panelist ran is not scored");
        assert_eq!(resolve(&src("ci-sdk", Some("v2")), &roster), None, "a credited non-panelist is not scored");
        // An explicit model beats a bare name when a panelist is named for the
        // backend but runs a different model.
        let two_claude = vec![
            ("claude".to_string(), key("claude", "claude-fable-5")),
            ("claude-opus".to_string(), key("claude", "claude-opus-5")),
        ];
        assert_eq!(
            resolve(&src("claude", Some("claude-opus-5")), &two_claude),
            Some(key("claude", "claude-opus-5")),
            "the model, not the name, decides"
        );
        assert_eq!(resolve(&src("opencode", None), &roster), None, "two opencodes, no model: ambiguous");
        // A bare backend name credits the sole panelist; a longer name that
        // merely starts with the backend does not.
        let fable = vec![("claude".to_string(), key("claude", "claude-fable-5"))];
        assert_eq!(resolve(&src("claude", None), &fable), Some(key("claude", "claude-fable-5")), "bare backend");
        assert_eq!(resolve(&src("claude-code-bot", None), &fable), None, "an outside bot is not the panelist");
        assert_eq!(resolve(&src("claude-opus-5", None), &fable), None, "a bare model name the panel did not run is not credited");
        // A model-bearing source matches a panelist whose model was not
        // recorded, but not one whose model is known and different.
        let no_model = vec![("claude".to_string(), key("claude", "unknown"))];
        assert_eq!(resolve(&src("claude", Some("claude-opus-5")), &no_model), Some(key("claude", "unknown")), "sole unknown-model panelist");
        assert_eq!(resolve(&src("claude", Some("claude-opus-5")), &fable), None, "a known, different model is not credited");
        assert_eq!(resolve(&src("coderabbitai", None), &roster), None, "not a panelist");
        assert_eq!(resolve(&src("coderabbitai", None), &[]), None, "...even with no roster to check");
    }

    #[test]
    fn a_run_with_no_review_text_does_not_drag_the_keep_rate() {
        // One reviewed run kept 1 of 2, then three runs whose review text was
        // never read reported 3 each. The rate is 1 of 2, not 1 of 11.
        let reviewed = run(
            1,
            vec![entry("codex", "gpt-5.5", Some(true), Some(2))],
            vec![finding("LOW", &[("codex", Some("gpt-5.5"))], 1, false)],
        );
        let mut text_less = run(2, vec![entry("codex", "gpt-5.5", Some(true), Some(3))], vec![]);
        text_less.reviewed = false;
        text_less.id = "t".into();
        let runs = vec![reviewed, text_less];
        let c = &aggregate(&runs)[0];
        assert_eq!((c.keep_num, c.keep_den), (1, 2));
        assert_eq!(c.raw_findings, 5, "the reported counts still show under RAW/RUN");
    }

    #[test]
    fn kept_never_exceeds_reported_in_the_rate() {
        let runs = vec![run(
            1,
            vec![entry("codex", "gpt-5.5", Some(true), Some(1))],
            vec![
                finding("LOW", &[("codex", Some("gpt-5.5"))], 1, false),
                finding("LOW", &[("codex", Some("gpt-5.5"))], 1, false),
            ],
        )];
        let c = &aggregate(&runs)[0];
        assert_eq!(c.kept, 2);
        assert_eq!((c.keep_num, c.keep_den), (1, 1));
    }

    #[test]
    fn selection_by_time_and_repo() {
        let mut a = run(100, vec![], vec![]);
        a.repo = Some("acme/widgets".into());
        let mut b = run(200, vec![], vec![]);
        b.repo = Some("acme/gadgets".into());
        let runs = vec![a, b];
        assert_eq!(select(&runs, None, None).len(), 2);
        assert_eq!(select(&runs, Some(150), None).len(), 1);
        assert_eq!(select(&runs, None, Some("Gadgets")).len(), 1);
        assert_eq!(select(&runs, None, Some("acme")).len(), 2);
        assert_eq!(select(&runs, Some(150), Some("widgets")).len(), 0);
    }

    #[test]
    fn overview_counts_what_the_runs_concluded() {
        let runs = [run(100, vec![], vec![]), run(200, vec![], vec![])];
        let refs: Vec<&Run> = runs.iter().collect();
        let o = overview(&refs);
        assert_eq!(o.runs, 2);
        assert_eq!(o.repos, 1);
        assert_eq!((o.first_at, o.last_at), (Some(100), Some(200)));
        assert_eq!(o.decisions.get("commented"), Some(&2));
        assert_eq!(o.risks.get("LOW"), Some(&2));
    }

    #[test]
    fn a_cohort_that_never_answers_is_called_out() {
        let panel: Vec<PanelEntry> = (0..5).map(|_| entry("claude-fable", "fable", Some(false), None)).collect();
        let runs: Vec<Run> = panel.into_iter().enumerate().map(|(i, p)| run(i as i64, vec![p], vec![])).collect();
        let notes = attention(&aggregate(&runs));
        assert_eq!(notes, vec!["fable on claude answered 0 of 5 launches"]);
        let few: Vec<Run> = runs.into_iter().take(4).collect();
        assert!(attention(&aggregate(&few)).is_empty(), "four launches is too few to say");
    }
}
