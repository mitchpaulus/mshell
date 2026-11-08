//! The GUI frontend's line editor core, ported from mshell's Go
//! (ai/gui-terminal.typ, "The Rust port"). The Go code is the reference;
//! the tools in `tools/` check this port against it.

pub mod edit;
pub mod graphemes;
pub mod keys;
pub mod lexer;
pub mod resolver;
mod grapheme_tables;
mod unicode_tables;

pub use grapheme_tables::GRAPHEME_UNICODE_VERSION;
pub use unicode_tables::UNICODE_VERSION;
