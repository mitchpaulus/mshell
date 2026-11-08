//! Prints tokens as `msh --lex` does, for comparison with it
//! (tools/lexcompare.py).
//!
//! Usage: mshlex [--editor] [--cases] [FILE]
//!   --editor  the line editor's settings (msh --lex --lex-editor)
//!   --cases   input is length-prefixed cases (msh --lex --lex-cases)
//! An error token prints the text scanned, not Go's message.

use msh_editor::lexer::{Lexer, Token};
use std::io::{Read, Write};

fn dump(out: &mut impl Write, lexer: &mut Lexer, tokens: &mut Vec<Token>, input: &str) -> std::io::Result<()> {
    lexer.tokenize_into(input, tokens);
    for t in tokens.iter() {
        writeln!(out, "{}:{}:{} {}", t.line, t.col, t.kind.name(), t.text(input))?;
    }
    Ok(())
}

fn main() -> std::io::Result<()> {
    let args: Vec<String> = std::env::args().skip(1).collect();
    let editor = args.iter().any(|a| a == "--editor");
    let cases = args.iter().any(|a| a == "--cases");
    let file = args.iter().find(|a| !a.starts_with("--"));
    let mut data = Vec::new();
    match file {
        Some(path) => data = std::fs::read(path)?,
        None => {
            std::io::stdin().read_to_end(&mut data)?;
        }
    }
    let mut lexer = if editor { Lexer::editor() } else { Lexer::default() };
    let mut tokens = Vec::new();
    let stdout = std::io::stdout();
    let mut out = std::io::BufWriter::with_capacity(1 << 20, stdout.lock());
    if !cases {
        let input = std::str::from_utf8(&data).expect("mshlex: input is not UTF-8");
        return dump(&mut out, &mut lexer, &mut tokens, input);
    }
    let mut rest = &data[..];
    let mut n = 0;
    while !rest.is_empty() {
        let size = u32::from_le_bytes(rest[..4].try_into().unwrap()) as usize;
        let input = std::str::from_utf8(&rest[4..4 + size]).expect("mshlex: case is not UTF-8");
        writeln!(out, "=== case {n}")?;
        dump(&mut out, &mut lexer, &mut tokens, input)?;
        rest = &rest[4 + size..];
        n += 1;
    }
    out.flush()
}
