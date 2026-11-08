//! The GUI frontend's line editor core, ported from mshell's Go
//! (ai/gui-terminal.typ, "The Rust port"). The Go code is the reference;
//! the tools in `tools/` check this port against it.

pub mod lexer;
mod unicode_tables;

pub use unicode_tables::UNICODE_VERSION;
