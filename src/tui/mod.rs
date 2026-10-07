//! The full-screen review view, `autoreview --tui`.
//!
//! The run drawn as two panes: every PR this run is responsible for on the
//! left, and on the right what the selected one's review is doing or what
//! its last review found. It keeps the reviews that ran, which is where a
//! person wants to go back to.

pub mod actions;
mod detail;
mod help;
mod input;
pub mod keys;
mod layout;
mod list;
mod markdown;
pub mod mine_view;
pub mod model;
mod screen;
mod terminal;
mod text;

pub use keys::Action;
pub use model::{Archived, Wait};
pub use screen::{Header, Screen};
