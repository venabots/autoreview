//! `autoreview stats`' flags. Hand-rolled like the parent's: a handful of
//! flags, exact error strings, exit 1 on bad input.

use crate::cli::require_value;
use std::path::PathBuf;

pub const HELP: &str = r#"autoreview stats: how each panelist model has done, across every recorded review.

Usage: autoreview stats [--import] [--since WHEN] [--repo NAME] [--json]
                        [--ledger PATH] [--transcripts DIR] [--help]

Every finished review is appended to a ledger -- one JSON line per review
with the panel, what each panelist reported, and which findings the synthesis
kept after checking them against the code. This reads that ledger back and
folds it per model.

  --import            First read past reviews out of Claude Code's session
                      transcripts (~/.claude/projects) into the ledger. Safe to
                      repeat: a review already recorded is skipped.
  --since WHEN        Only reviews after WHEN: a span (7d, 2w) or a date
                      (2026-08-01).
  --repo NAME         Only reviews of a repo whose name contains NAME.
  --json              The same numbers as JSON, for another tool.
  --ledger PATH       Read (and import into) this ledger instead of
                      $AUTOREVIEW_LEDGER or ~/.local/state/autoreview/ledger.jsonl.
  --transcripts DIR   Where --import looks for transcripts (default: Claude
                      Code's projects directory, under $CLAUDE_CONFIG_DIR when
                      set).
  --help, -h          Show this help.

Columns:
  RUNS       times the model was launched on a panel
  AVAIL      launches that came back with a review, with a 95% interval
  RAW/RUN    findings the model itself reported, per answered run
  KEPT       findings the synthesis kept that name this model
  UNIQUE     kept findings no other panelist raised
  HIGH+      kept findings at HIGH or CRITICAL
  DROPPED    findings the synthesis listed under Disagreements: the model
             raised them, and verification overruled or downgraded them
  KEEP RATE  kept findings over reported findings, run by run, with a 95%
             interval -- the closest thing the pipeline has to precision
  SAMPLE     proven (30+ answered runs), emerging (10+), or thin

A `Flagged by` name that matches no panelist on its run (another review bot
the synthesis credited, say) is not scored; the footer says how many.
"#;

#[derive(Debug, Clone, PartialEq, Default)]
pub struct Opts {
    pub import: bool,
    /// An epoch second; runs before it are left out.
    pub since: Option<i64>,
    pub repo: Option<String>,
    pub json: bool,
    pub ledger: Option<PathBuf>,
    pub transcripts: Option<PathBuf>,
}

pub enum Parsed {
    Run(Opts),
    Help,
}

/// Days in a month, Gregorian, leap year included.
fn days_in_month(year: u32, month: u32) -> u32 {
    match month {
        1 | 3 | 5 | 7 | 8 | 10 | 12 => 31,
        4 | 6 | 9 | 11 => 30,
        2 if year.is_multiple_of(4) && (!year.is_multiple_of(100) || year.is_multiple_of(400)) => 29,
        2 => 28,
        _ => 0,
    }
}

/// `7d`, `2w`, or `YYYY-MM-DD`, as the epoch second it names.
pub fn parse_since(raw: &str, now: i64) -> Result<i64, String> {
    let bad = || format!("error: --since expects a span like 7d or 2w, or a date like 2026-08-01, got \"{raw}\"");
    if raw.len() == 10 && raw.is_ascii() {
        // parse_iso checks the shape, not the calendar, so an impossible date
        // (month 13, or 2026-02-30) would quietly land on another day. Check
        // the month and the day-of-month, leap year included, before trusting it.
        let u = |s: &str| s.parse::<u32>().ok();
        let (y, mo, d) = (u(&raw[0..4]), u(&raw[5..7]), u(&raw[8..10]));
        let plausible = matches!((y, mo, d), (Some(y), Some(mo), Some(d))
            if (1..=12).contains(&mo) && (1..=days_in_month(y, mo)).contains(&d));
        return crate::prlist::parse_iso(&format!("{raw}T00:00:00Z")).filter(|_| plausible).ok_or_else(bad);
    }
    // By character, not byte: a multibyte last character must be refused,
    // not split in the middle.
    let unit = raw.chars().next_back().ok_or_else(bad)?;
    let num = &raw[..raw.len() - unit.len_utf8()];
    let n: i64 = num.parse().map_err(|_| bad())?;
    if n <= 0 {
        return Err(bad());
    }
    let per = match unit {
        'd' => 86_400,
        'w' => 7 * 86_400,
        _ => return Err(bad()),
    };
    // A huge span must give the error, not overflow the subtraction (a panic
    // in a debug build, a wrong cutoff in release).
    n.checked_mul(per).and_then(|secs| now.checked_sub(secs)).ok_or_else(bad)
}

pub fn parse(args: &[String], now: i64) -> Result<Parsed, String> {
    let mut opts = Opts::default();
    let mut it = args.iter().cloned();
    let value = |flag: &str, v: Option<String>| require_value(flag, v).map_err(|e| e.msg);
    while let Some(arg) = it.next() {
        match arg.as_str() {
            "--help" | "-h" => return Ok(Parsed::Help),
            "--import" => opts.import = true,
            "--json" => opts.json = true,
            "--since" => opts.since = Some(parse_since(&value("--since", it.next())?, now)?),
            "--repo" => opts.repo = Some(value("--repo", it.next())?),
            "--ledger" => opts.ledger = Some(PathBuf::from(value("--ledger", it.next())?)),
            "--transcripts" => opts.transcripts = Some(PathBuf::from(value("--transcripts", it.next())?)),
            other => {
                if let Some(v) = other.strip_prefix("--since=") {
                    opts.since = Some(parse_since(&value("--since", Some(v.to_string()))?, now)?);
                } else if let Some(v) = other.strip_prefix("--repo=") {
                    opts.repo = Some(value("--repo", Some(v.to_string()))?);
                } else if let Some(v) = other.strip_prefix("--ledger=") {
                    opts.ledger = Some(PathBuf::from(value("--ledger", Some(v.to_string()))?));
                } else if let Some(v) = other.strip_prefix("--transcripts=") {
                    opts.transcripts = Some(PathBuf::from(value("--transcripts", Some(v.to_string()))?));
                } else {
                    return Err(format!("unknown arg: {other}"));
                }
            }
        }
    }
    Ok(Parsed::Run(opts))
}

#[cfg(test)]
mod tests {
    use super::*;

    fn args(s: &str) -> Vec<String> {
        s.split_whitespace().map(String::from).collect()
    }

    fn opts(s: &str) -> Opts {
        match parse(&args(s), 1_000_000) {
            Ok(Parsed::Run(o)) => o,
            Ok(Parsed::Help) => panic!("help"),
            Err(e) => panic!("{e}"),
        }
    }

    #[test]
    fn spans_and_dates() {
        assert_eq!(parse_since("7d", 1_000_000), Ok(1_000_000 - 7 * 86_400));
        assert_eq!(parse_since("2w", 1_000_000), Ok(1_000_000 - 14 * 86_400));
        assert_eq!(parse_since("2026-08-01", 0), Ok(1_785_542_400));
        assert!(parse_since("0d", 1).is_err());
        assert!(parse_since("soon", 1).unwrap_err().contains("--since expects"));
        assert!(parse_since("2026-13-01", 1).is_err());
        // A non-ASCII value must be refused, not split mid-character.
        assert!(parse_since("日", 1).is_err());
        assert!(parse_since("7é", 1).is_err());
        assert!(parse_since("", 1).is_err());
        // An impossible calendar date is refused, not rounded to another day.
        assert!(parse_since("2026-02-30", 1).is_err());
        assert!(parse_since("2026-04-31", 1).is_err());
        assert!(parse_since("2025-02-29", 1).is_err());
        assert!(parse_since("2024-02-29", 0).is_ok(), "2024 is a leap year");
        // A huge span gives the error instead of overflowing the subtraction.
        assert!(parse_since("9223372036854775807d", 0).is_err());
        assert!(parse_since("9223372036854775807w", 0).is_err());
        assert!(parse_since("", 1).is_err());
        assert!(parse_since("日", 1).is_err(), "a multibyte value is refused, not split");
        assert!(parse_since("7日", 1).is_err());
        assert!(parse_since("2026-08-0é", 1).is_err());
    }

    #[test]
    fn flags_in_both_spellings() {
        let o = opts("--import --json --since 7d --repo widgets --ledger /l --transcripts /t");
        assert!(o.import && o.json);
        assert_eq!(o.since, Some(1_000_000 - 7 * 86_400));
        assert_eq!(o.repo.as_deref(), Some("widgets"));
        assert_eq!(o.ledger, Some(PathBuf::from("/l")));
        assert_eq!(o.transcripts, Some(PathBuf::from("/t")));
        let o = opts("--since=2w --repo=acme --ledger=/x --transcripts=/y");
        assert_eq!(o.since, Some(1_000_000 - 14 * 86_400));
        assert_eq!(o.repo.as_deref(), Some("acme"));
        assert_eq!(o.ledger, Some(PathBuf::from("/x")));
        assert_eq!(o.transcripts, Some(PathBuf::from("/y")));
    }

    #[test]
    fn refusals() {
        assert!(matches!(parse(&args("--help"), 0), Ok(Parsed::Help)));
        assert_eq!(parse(&args("--bogus"), 0).err(), Some("unknown arg: --bogus".into()));
        assert_eq!(parse(&args("--repo"), 0).err(), Some("error: --repo expects a value".into()));
        assert_eq!(
            parse(&args("--repo --json"), 0).err(),
            Some("error: --repo expects a value, but found the flag --json".into())
        );
    }
}
