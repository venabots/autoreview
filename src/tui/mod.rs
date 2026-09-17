//! The full-screen review view, `autoreview --tui`.
//!
//! The same engine as the inline board, drawn as two panes: every PR this
//! run is responsible for on the left, and on the right what the selected
//! one's review is doing or what its last review found. The inline board
//! shows the reviews that are running; this view keeps the ones that ran,
//! which is where a person wants to go back to.

pub mod keys;
pub mod model;

pub use model::{Archived, Wait};
