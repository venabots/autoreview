//! The wall clock, in whole seconds since the epoch.
//!
//! Six modules each carried their own copy of this. A clock set before 1970
//! reads as 0 rather than failing: every caller measures an age or a rest,
//! and a zero only ends one early.

pub fn epoch_secs() -> i64 {
    std::time::SystemTime::now()
        .duration_since(std::time::UNIX_EPOCH)
        .map(|d| d.as_secs() as i64)
        .unwrap_or(0)
}
