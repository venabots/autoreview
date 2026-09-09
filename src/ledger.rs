//! The ledger: one line per finished review, kept across runs.
//!
//! Everything else autoreview writes is per run and lands in a temp directory
//! that macOS clears in days. What each model found, and whether it held up,
//! is only worth anything as a series -- so it goes somewhere that outlives
//! the run: `$XDG_STATE_HOME/autoreview/ledger.jsonl`, or
//! `~/.local/state/autoreview/ledger.jsonl`. `$AUTOREVIEW_LEDGER` names a
//! different file; `off` records nothing.
//!
//! Append-only JSON lines rather than a database: one record is one review,
//! an append is atomic for a line this size, and the statistics are cheap to
//! recompute from a few thousand lines. A record is never rewritten, so a
//! reader can never see one half-updated.

use crate::cli::EnvFn;
use crate::findings::{self, Finding};
use crate::report::{Findings, Panelist, Trailer};
use serde::{Deserialize, Serialize};
use std::io::Write;
use std::path::{Path, PathBuf};

pub const VERSION: u32 = 1;

/// One finished review: what drove it, who sat on the panel, what survived.
#[derive(Debug, Clone, PartialEq, Serialize, Deserialize)]
pub struct Run {
    pub v: u32,
    /// Stable across re-imports, so a run recorded twice reads once.
    pub id: String,
    /// When the review finished, as an epoch second.
    pub at: i64,
    /// `autoreview`, `panel`, or `transcript` for an imported one.
    pub source: String,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub repo: Option<String>,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub pr: Option<u64>,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub session: Option<String>,
    /// What landed on the PR: approved, commented, changes-requested, none.
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub decision: Option<String>,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub risk: Option<String>,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub counts: Option<Counts>,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub cost_usd: Option<f64>,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub duration_secs: Option<u64>,
    /// The model that ran the review session and wrote the synthesis.
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub driver_model: Option<String>,
    #[serde(default)]
    pub panel: Vec<PanelEntry>,
    #[serde(default)]
    pub findings: Vec<Finding>,
}

#[derive(Debug, Clone, PartialEq, Serialize, Deserialize, Default)]
pub struct Counts {
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub must_fix: Option<u64>,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub should_fix: Option<u64>,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub polish: Option<u64>,
}

impl From<&Findings> for Counts {
    fn from(f: &Findings) -> Counts {
        Counts { must_fix: f.must_fix, should_fix: f.should_fix, polish: f.polish }
    }
}

/// One panelist as launched: the name the run gave it, the model it reported,
/// and whether it came back with a review.
#[derive(Debug, Clone, PartialEq, Serialize, Deserialize)]
pub struct PanelEntry {
    pub name: String,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub model: Option<String>,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub ok: Option<bool>,
    /// The panelist's own finding count, before synthesis.
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub findings: Option<u64>,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub top: Option<String>,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub duration_secs: Option<u64>,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub exit_code: Option<i32>,
}

impl From<&Panelist> for PanelEntry {
    fn from(p: &Panelist) -> PanelEntry {
        PanelEntry {
            name: p.name.clone().unwrap_or_else(|| "?".into()),
            model: p.model.clone(),
            ok: p.ok,
            findings: p.findings,
            top: p.top.clone(),
            duration_secs: None,
            exit_code: None,
        }
    }
}

/// Where the ledger lives, or None when `$AUTOREVIEW_LEDGER=off` asked for
/// nothing to be recorded (or there is no home to put it in).
pub fn path(env: EnvFn) -> Option<PathBuf> {
    if let Some(v) = env("AUTOREVIEW_LEDGER").filter(|v| !v.is_empty()) {
        if v.eq_ignore_ascii_case("off") {
            return None;
        }
        return Some(PathBuf::from(v));
    }
    let state = match env("XDG_STATE_HOME").filter(|v| !v.is_empty()) {
        Some(x) => PathBuf::from(x),
        None => PathBuf::from(env("HOME").filter(|v| !v.is_empty())?).join(".local/state"),
    };
    Some(state.join("autoreview").join("ledger.jsonl"))
}

/// Add one run. The parent directory is made on first use, and the file is
/// private to its owner: it names repositories, files and line numbers.
pub fn append(path: &Path, run: &Run) -> std::io::Result<()> {
    if let Some(parent) = path.parent() {
        std::fs::create_dir_all(parent)?;
    }
    let mut opts = std::fs::OpenOptions::new();
    opts.append(true).create(true);
    #[cfg(unix)]
    {
        use std::os::unix::fs::OpenOptionsExt;
        opts.mode(0o600);
    }
    let mut file = opts.open(path)?;
    let mut line = serde_json::to_string(run).map_err(std::io::Error::other)?;
    line.push('\n');
    file.write_all(line.as_bytes())
}

/// Every run recorded, in the order written, one per id. A line that will
/// not parse is skipped: a torn write or a record from a newer version must
/// not take the rest of the history with it. No file yet is no history.
pub fn read(path: &Path) -> std::io::Result<Vec<Run>> {
    let raw = match std::fs::read_to_string(path) {
        Ok(s) => s,
        Err(e) if e.kind() == std::io::ErrorKind::NotFound => return Ok(Vec::new()),
        Err(e) => return Err(e),
    };
    let mut seen = std::collections::HashSet::new();
    Ok(raw
        .lines()
        .filter_map(|l| serde_json::from_str::<Run>(l).ok())
        .filter(|r| seen.insert(r.id.clone()))
        .collect())
}

pub fn now() -> i64 {
    std::time::SystemTime::now()
        .duration_since(std::time::UNIX_EPOCH)
        .map(|d| d.as_secs() as i64)
        .unwrap_or(0)
}

/// What a finished autoreview job knows, gathered for the record.
pub struct Reviewed<'a> {
    pub repo: &'a str,
    pub pr: u64,
    pub session: Option<&'a str>,
    pub started_epoch: i64,
    /// GitHub's readback first, the trailer's own claim second.
    pub verdict: Option<&'a str>,
    pub trailer: &'a Trailer,
    /// The synthesized review, for the findings and who flagged them.
    pub review: Option<&'a str>,
    pub cost_usd: Option<f64>,
    pub elapsed_secs: u64,
    pub driver_model: Option<&'a str>,
}

/// The record for one autoreview pass over one PR.
pub fn autoreview_run(r: Reviewed) -> Run {
    let id = match r.session {
        Some(sid) => format!("autoreview:{sid}:{}", r.started_epoch),
        None => format!("autoreview:{}#{}:{}", r.repo, r.pr, r.started_epoch),
    };
    Run {
        v: VERSION,
        id,
        at: now(),
        source: "autoreview".into(),
        repo: Some(r.repo.to_string()),
        pr: Some(r.pr),
        session: r.session.map(str::to_string),
        decision: r.verdict.map(decision_token),
        risk: r.trailer.risk.clone(),
        counts: r.trailer.findings.as_ref().map(Counts::from),
        cost_usd: r.cost_usd,
        duration_secs: Some(r.elapsed_secs),
        driver_model: r.driver_model.map(str::to_string),
        panel: r.trailer.panel.iter().map(PanelEntry::from).collect(),
        findings: r.review.map(findings::parse).unwrap_or_default(),
    }
}

/// One spelling per decision. The summary says "changes requested"; the
/// trailer and the ledger say `changes-requested`.
pub fn decision_token(verdict: &str) -> String {
    verdict.trim().to_ascii_lowercase().replace(' ', "-")
}

/// One panelist as `panel` ran it: the run knows exactly what each one did,
/// so nothing here is self-reported except the model.
pub struct Panelled<'a> {
    pub name: &'a str,
    /// What the panelist said it was running; "unknown" is no model.
    pub model: &'a str,
    pub answered: bool,
    pub report: &'a str,
    pub exit_code: Option<i32>,
    pub elapsed_secs: u64,
}

/// The record for one `panel` run. There is no PR and no GitHub verdict: the
/// target is whatever diff was reviewed, named by the repo directory.
pub fn panel_run(
    repo_dir: &Path,
    started_epoch: i64,
    panel: &[Panelled],
    synthesis: &str,
    driver_model: Option<&str>,
) -> Run {
    let repo = repo_dir.file_name().map(|n| n.to_string_lossy().into_owned());
    Run {
        v: VERSION,
        id: format!("panel:{started_epoch}:{}", std::process::id()),
        at: now(),
        source: "panel".into(),
        repo,
        pr: None,
        session: None,
        decision: None,
        risk: findings::risk(synthesis),
        counts: None,
        cost_usd: None,
        duration_secs: Some((now() - started_epoch).max(0) as u64),
        driver_model: driver_model.map(str::to_string),
        panel: panel
            .iter()
            .map(|p| PanelEntry {
                name: p.name.to_string(),
                model: (p.model != "unknown" && !p.model.is_empty()).then(|| p.model.to_string()),
                ok: Some(p.answered),
                findings: p.answered.then(|| findings::count_raw(p.report)).flatten(),
                top: p.answered.then(|| findings::top_severity(p.report)).flatten(),
                duration_secs: Some(p.elapsed_secs),
                exit_code: p.exit_code,
            })
            .collect(),
        findings: findings::parse(synthesis),
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::report::parse_trailer;

    fn env_of<'a>(vars: &'a [(&'a str, &'a str)]) -> impl Fn(&str) -> Option<String> + 'a {
        move |k| vars.iter().find(|(n, _)| *n == k).map(|(_, v)| v.to_string())
    }

    fn tmp() -> PathBuf {
        let d = std::env::temp_dir().join(format!("ar-ledger-{}-{}", std::process::id(), now()));
        std::fs::create_dir_all(&d).unwrap();
        d
    }

    fn run(id: &str) -> Run {
        Run {
            v: VERSION,
            id: id.into(),
            at: 1,
            source: "test".into(),
            repo: None,
            pr: None,
            session: None,
            decision: None,
            risk: None,
            counts: None,
            cost_usd: None,
            duration_secs: None,
            driver_model: None,
            panel: Vec::new(),
            findings: Vec::new(),
        }
    }

    #[test]
    fn off_records_nothing() {
        assert_eq!(path(&env_of(&[("AUTOREVIEW_LEDGER", "off"), ("HOME", "/h")])), None);
        assert_eq!(path(&env_of(&[("AUTOREVIEW_LEDGER", "OFF"), ("HOME", "/h")])), None);
    }

    #[test]
    fn the_path_is_the_override_then_xdg_then_home() {
        assert_eq!(
            path(&env_of(&[("AUTOREVIEW_LEDGER", "/x/l.jsonl"), ("HOME", "/h")])),
            Some(PathBuf::from("/x/l.jsonl"))
        );
        assert_eq!(
            path(&env_of(&[("XDG_STATE_HOME", "/s"), ("HOME", "/h")])),
            Some(PathBuf::from("/s/autoreview/ledger.jsonl"))
        );
        assert_eq!(
            path(&env_of(&[("HOME", "/h"), ("AUTOREVIEW_LEDGER", "")])),
            Some(PathBuf::from("/h/.local/state/autoreview/ledger.jsonl"))
        );
        assert_eq!(path(&env_of(&[])), None);
    }

    #[test]
    fn appended_runs_read_back_once_each() {
        let file = tmp().join("nested/ledger.jsonl");
        assert!(read(&file).unwrap().is_empty(), "no file is no history");
        append(&file, &run("a")).unwrap();
        append(&file, &run("b")).unwrap();
        append(&file, &run("a")).unwrap();
        // A torn line must not take the rest with it.
        std::fs::OpenOptions::new()
            .append(true)
            .open(&file)
            .unwrap()
            .write_all(b"{\"v\":1,\"id\":\"c\"\n")
            .unwrap();
        let runs = read(&file).unwrap();
        assert_eq!(runs.iter().map(|r| r.id.as_str()).collect::<Vec<_>>(), vec!["a", "b"]);
    }

    #[cfg(unix)]
    #[test]
    fn the_file_is_private() {
        use std::os::unix::fs::PermissionsExt;
        let file = tmp().join("ledger.jsonl");
        append(&file, &run("a")).unwrap();
        let mode = std::fs::metadata(&file).unwrap().permissions().mode() & 0o777;
        assert_eq!(mode, 0o600);
    }

    #[test]
    fn an_autoreview_pass_becomes_one_record() {
        let text = "### Risk\nMEDIUM\n### should-fix\n- [MEDIUM] src/a.rs:1 — issue. Flagged by 2: codex (gpt-5.5), claude (claude-opus-4.7)\n\n```autoreview\n{\"decision\":\"commented\",\"risk\":\"MEDIUM\",\"findings\":{\"must_fix\":0,\"should_fix\":1,\"polish\":0},\"panel\":[{\"name\":\"codex\",\"model\":\"gpt-5.5\",\"ok\":true,\"findings\":1,\"top\":\"MEDIUM\"},{\"name\":\"claude\",\"model\":\"claude-opus-4.7\",\"ok\":false}]}\n```";
        let trailer = parse_trailer(text).unwrap();
        let r = autoreview_run(Reviewed {
            repo: "acme/widgets",
            pr: 9,
            session: Some("sid"),
            started_epoch: 100,
            verdict: Some("changes requested"),
            trailer: &trailer,
            review: Some(text),
            cost_usd: Some(0.42),
            elapsed_secs: 12,
            driver_model: Some("claude-fable-5"),
        });
        assert_eq!(r.id, "autoreview:sid:100");
        assert_eq!(r.source, "autoreview");
        assert_eq!(r.repo.as_deref(), Some("acme/widgets"));
        assert_eq!(r.pr, Some(9));
        assert_eq!(r.decision.as_deref(), Some("changes-requested"));
        assert_eq!(r.risk.as_deref(), Some("MEDIUM"));
        assert_eq!(r.counts.as_ref().and_then(|c| c.should_fix), Some(1));
        assert_eq!(r.panel.len(), 2);
        assert_eq!(r.panel[0].name, "codex");
        assert_eq!(r.panel[0].findings, Some(1));
        assert_eq!(r.panel[1].ok, Some(false));
        assert_eq!(r.findings.len(), 1);
        assert_eq!(r.findings[0].count, 2);
        assert_eq!(r.driver_model.as_deref(), Some("claude-fable-5"));
    }

    #[test]
    fn a_panel_run_records_what_the_run_saw() {
        let panel = [
            Panelled {
                name: "codex",
                model: "gpt-5.5",
                answered: true,
                report: "Model: gpt-5.5\n- [LOW] a.rs:1 — nit\n- [HIGH] b.rs:2 — bug",
                exit_code: Some(0),
                elapsed_secs: 40,
            },
            Panelled {
                name: "claude",
                model: "unknown",
                answered: false,
                report: "",
                exit_code: Some(1),
                elapsed_secs: 3,
            },
        ];
        let synthesis = "### Risk\nHIGH\n### must-fix\n- [HIGH] b.rs:2 — bug. Flagged by: codex (gpt-5.5)";
        let r = panel_run(Path::new("/x/widgets"), 100, &panel, synthesis, Some("opus-5"));
        assert_eq!(r.source, "panel");
        assert_eq!(r.repo.as_deref(), Some("widgets"));
        assert_eq!(r.risk.as_deref(), Some("HIGH"));
        assert_eq!(r.driver_model.as_deref(), Some("opus-5"));
        assert_eq!(r.panel[0].findings, Some(2));
        assert_eq!(r.panel[0].top.as_deref(), Some("HIGH"));
        assert_eq!(r.panel[0].duration_secs, Some(40));
        assert_eq!(r.panel[1].model, None, "\"unknown\" is no model");
        assert_eq!(r.panel[1].ok, Some(false));
        assert_eq!(r.panel[1].findings, None);
        assert_eq!(r.findings.len(), 1);
    }

    #[test]
    fn a_run_with_no_session_is_keyed_by_pr() {
        let trailer = Trailer::default();
        let r = autoreview_run(Reviewed {
            repo: "acme/widgets",
            pr: 9,
            session: None,
            started_epoch: 100,
            verdict: None,
            trailer: &trailer,
            review: None,
            cost_usd: None,
            elapsed_secs: 0,
            driver_model: None,
        });
        assert_eq!(r.id, "autoreview:acme/widgets#9:100");
        assert_eq!(r.decision, None);
        assert!(r.findings.is_empty());
    }
}
